package bridge

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"codexpet.local/codex-pet/internal/appserver"
	"codexpet.local/codex-pet/internal/config"
	"codexpet.local/codex-pet/internal/focus"
	"codexpet.local/codex-pet/internal/model"
	"codexpet.local/codex-pet/internal/state"
	"codexpet.local/codex-pet/internal/ws"
)

type Options struct {
	DataDir          string
	HostName         string
	Port             int
	CodexBinary      string
	DisableMDNS      bool
	DisableAppServer bool
	Logger           *log.Logger
}

type Server struct {
	id     config.Identity
	store  *state.Store
	hub    *hub
	notify chan struct{}
	logger *log.Logger
	socket string
	http   *http.Server
}

func Serve(ctx context.Context, opts Options) error {
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "codex-pet: ", log.LstdFlags)
	}
	id, err := config.EnsureIdentity(opts.DataDir, opts.HostName, opts.Port)
	if err != nil {
		return err
	}
	store, err := state.New(filepath.Join(opts.DataDir, "state.json"))
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	s := &Server{
		id: id, store: store, hub: newHub(), notify: make(chan struct{}, 1),
		logger: opts.Logger, socket: filepath.Join(opts.DataDir, "bridge.sock"),
	}

	errs := make(chan error, 4)
	go func() { errs <- s.serveHooks(ctx) }()
	go func() { errs <- s.serveHTTPS(ctx) }()
	go s.broadcastLoop(ctx)
	go s.pingLoop(ctx)
	if !opts.DisableMDNS {
		go advertiseMDNS(ctx, id.Port, opts.Logger)
	}
	if !opts.DisableAppServer {
		go s.enrichFromAppServer(ctx, opts.CodexBinary)
	}

	s.logger.Printf("bridge ready at wss://%s:%d/ws", id.HostName, id.Port)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.http != nil {
			_ = s.http.Shutdown(shutdownCtx)
		}
		return nil
	case err := <-errs:
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func SendHook(socketPath string, event model.HookEvent) error {
	conn, err := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	enc := json.NewEncoder(conn)
	return enc.Encode(event)
}

func (s *Server) serveHooks(ctx context.Context) error {
	if err := prepareSocketPath(s.socket); err != nil {
		return err
	}
	listener, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("listen on hook socket: %w", err)
	}
	if err := os.Chmod(s.socket, 0o600); err != nil {
		listener.Close()
		return err
	}
	defer func() {
		listener.Close()
		_ = os.Remove(s.socket)
	}()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handleHook(conn)
	}
}

func (s *Server) handleHook(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var event model.HookEvent
	if err := json.NewDecoder(io.LimitReader(conn, 64<<10)).Decode(&event); err != nil {
		return
	}
	if event.SessionID == "" || event.Event == "" || event.EventID == "" {
		return
	}
	if s.store.Apply(event) {
		s.signal()
	}
}

func (s *Server) serveHTTPS(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"version":1}`+"\n")
	})
	mux.HandleFunc("/ws", s.handleWebSocket)
	s.http = &http.Server{
		Addr:              ":" + strconv.Itoa(s.id.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		_ = s.http.Close()
	}()
	err := s.http.ListenAndServeTLS(s.id.CertPath, s.id.KeyPath)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !tokenEqual(bearerToken(r.Header.Get("Authorization")), s.id.Token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := ws.Upgrade(w, r)
	if err != nil {
		return
	}
	s.hub.add(conn)
	defer s.hub.remove(conn)
	if err := conn.WriteJSON(s.store.Snapshot()); err != nil {
		return
	}
	for {
		var action model.FocusAction
		if err := conn.ReadJSON(&action); err != nil {
			return
		}
		if action.Type != "focus" || action.Version != 1 || action.TaskID == "" {
			continue
		}
		if err := focus.OpenThread(action.TaskID); err != nil {
			s.logger.Printf("focus failed: %v", err)
			continue
		}
		if s.store.Acknowledge(action.TaskID) {
			s.signal()
		}
	}
}

func (s *Server) broadcastLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
			s.hub.broadcast(s.store.Snapshot())
		}
	}
}

func (s *Server) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.hub.ping()
		}
	}
}

func (s *Server) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Server) enrichFromAppServer(ctx context.Context, binary string) {
	for {
		client, err := appserver.Start(ctx, binary)
		if err != nil {
			s.logger.Printf("read-only app-server unavailable: %v", err)
		} else {
			s.logger.Printf("read-only app-server connected via %s", appserver.BinaryForDiagnostics(binary))
			appserver.Poll(ctx, client, s.store.IDs, func(info appserver.ThreadInfo) {
				if s.store.Enrich(info.ID, info.Title, info.RuntimeStatus, info.Failed, time.Now()) {
					s.signal()
				}
			})
			client.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

type hub struct {
	mu      sync.RWMutex
	clients map[*ws.Conn]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[*ws.Conn]struct{})}
}

func (h *hub) add(conn *ws.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = struct{}{}
}

func (h *hub) remove(conn *ws.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	_ = conn.Close()
}

func (h *hub) broadcast(snapshot model.Snapshot) {
	h.mu.RLock()
	clients := make([]*ws.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.RUnlock()
	for _, conn := range clients {
		if err := conn.WriteJSON(snapshot); err != nil {
			h.remove(conn)
		}
	}
}

func (h *hub) ping() {
	h.mu.RLock()
	clients := make([]*ws.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.RUnlock()
	for _, conn := range clients {
		if err := conn.Ping(); err != nil {
			h.remove(conn)
		}
	}
}

func prepareSocketPath(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket path %s", path)
	}
	return os.Remove(path)
}

func bearerToken(value string) string {
	if len(value) < 7 || !strings.EqualFold(value[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func tokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func advertiseMDNS(ctx context.Context, port int, logger *log.Logger) {
	path, err := exec.LookPath("dns-sd")
	if err != nil {
		logger.Printf("Bonjour advertisement skipped: dns-sd not found")
		return
	}
	cmd := exec.CommandContext(ctx, path, "-R", "Codex Pet", "_codex-pet._tcp", "local",
		strconv.Itoa(port), "version=1")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		logger.Printf("Bonjour advertisement stopped: %v", err)
	}
}
