// Package edge implements the one-per-machine Edge Bridge runtime role.
package edge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentagotchi.local/agentagotchi/internal/adapters/codex"
	"agentagotchi.local/agentagotchi/internal/adapters/codex/appserver"
	"agentagotchi.local/agentagotchi/internal/config"
	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/pairing"
	"agentagotchi.local/agentagotchi/internal/presence"
	"agentagotchi.local/agentagotchi/internal/ws"
)

type Options struct {
	DataDir           string
	HostName          string
	Port              int
	CodexBinary       string
	DisableMDNS       bool
	DisableAppServer  bool
	Logger            *log.Logger
	FeedAuthenticator FeedAuthenticator
	FocusRunner       codex.CommandRunner
	PresenceConfig    presence.Config
}

type FeedAuthenticator interface {
	Authenticate(*http.Request) bool
}

type BearerFeedAuthenticator struct{ Token string }

func BearerToken(r *http.Request) string {
	const prefix = "bearer "
	h := r.Header.Get("Authorization")
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func (a BearerFeedAuthenticator) Authenticate(r *http.Request) bool {
	return tokenEqual(bearerToken(r.Header.Get("Authorization")), a.Token)
}

// Version is the Edge service version reported on the admin surface.
const Version = "0.2.0"

type Service struct {
	id               config.Identity
	ceremony         *pairing.Ceremony
	credStore        *pairing.Store
	version          string
	startedAt        time.Time
	edgeID           string
	statePath        string
	socketPath       string
	core             *presence.Core
	router           *Router
	codexReducer     *codex.Reducer
	codexLease       presence.Lease
	leaseDuration    time.Duration
	authenticator    FeedAuthenticator
	logger           *log.Logger
	hub              *feedHub
	notify           chan struct{}
	http             *http.Server
	seqMu            sync.Mutex
	codexIngestMu    sync.Mutex
	codexSeq         uint64
	codexBinary      string
	disableMDNS      bool
	disableAppServer bool
}

func NewService(opts Options) (*Service, error) {
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "agentagotchi: ", log.LstdFlags)
	}
	if opts.DataDir == "" {
		opts.DataDir = config.DefaultDataDir()
	}
	id, err := config.EnsureIdentity(opts.DataDir, opts.HostName, opts.Port)
	if err != nil {
		return nil, err
	}
	statePath := filepath.Join(opts.DataDir, "state.json")
	state, err := loadAndAdvanceState(statePath)
	if err != nil {
		return nil, err
	}
	core := presence.New(opts.PresenceConfig, state.Generation)
	core.RestoreAliases(state.Aliases)
	codexLease, err := core.AttachLocal("codex", []contract.Capability{contract.CapabilityFocus})
	if err != nil {
		return nil, err
	}
	authenticator := opts.FeedAuthenticator
	leaseDuration := opts.PresenceConfig.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	s := &Service{
		id: id, edgeID: edgeIDFromCertificate(id.CertPath),
		version: Version, startedAt: time.Now().UTC(),
		statePath: statePath, socketPath: filepath.Join(opts.DataDir, "edge.sock"),
		core: core, codexReducer: codex.NewReducer(), codexLease: codexLease,
		leaseDuration: leaseDuration, authenticator: authenticator,
		logger: opts.Logger, hub: newFeedHub(), notify: make(chan struct{}, 1),
		codexBinary: opts.CodexBinary, disableMDNS: opts.DisableMDNS,
		disableAppServer: opts.DisableAppServer,
	}
	s.router = NewRouter(core)
	if err := s.initPairing(); err != nil {
		return nil, err
	}
	if authenticator == nil {
		authenticator = PairingFeedAuthenticator{LegacyToken: id.Token, Ceremony: s.ceremony}
	}
	s.authenticator = authenticator
	focus := codex.FocusHandler(opts.FocusRunner)
	s.router.Register("codex", contract.CapabilityFocus, func(_ context.Context, nativeID string, _ contract.FeedAction) error {
		return focus(nativeID)
	})
	s.http = &http.Server{
		Addr:              ":" + strconv.Itoa(id.Port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s, nil
}

func Serve(ctx context.Context, opts Options) error {
	s, err := NewService(opts)
	if err != nil {
		return err
	}
	return s.Serve(ctx)
}

func (s *Service) Core() *presence.Core      { return s.core }
func (s *Service) Router() *Router           { return s.router }
func (s *Service) Identity() config.Identity { return s.id }
func (s *Service) SocketPath() string        { return s.socketPath }

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"version":1}`+"\n")
	})
	mux.HandleFunc("/feed/v1", s.handleFeed)
	return mux
}

func (s *Service) Serve(ctx context.Context) error {
	errs := make(chan error, 2)
	go func() { errs <- s.serveIPC(ctx) }()
	go func() {
		err := s.http.ListenAndServeTLS(s.id.CertPath, s.id.KeyPath)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()
	go s.broadcastLoop(ctx)
	go s.maintenanceLoop(ctx)
	if !s.disableMDNS {
		go advertiseMDNS(ctx, s.id.Port, s.logger)
	}
	if !s.disableAppServer {
		go s.enrichFromAppServer(ctx)
	}
	s.logger.Printf("Edge ready at wss://%s:%d/feed/v1", s.id.HostName, s.id.Port)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.http.Shutdown(shutdownCtx)
		_ = persistState(s.statePath, persistedState{
			Generation: s.generation(), Aliases: s.core.Aliases(),
		})
		return nil
	case err := <-errs:
		if err == nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

func (s *Service) generation() uint64 {
	generation, _ := s.core.Revision()
	return generation
}

func (s *Service) applyCodexHook(event contract.IPCHookEvent) {
	s.codexIngestMu.Lock()
	defer s.codexIngestMu.Unlock()
	report, end, applied := s.codexReducer.Apply(event)
	if !applied {
		return
	}
	s.seqMu.Lock()
	s.codexSeq++
	seq := s.codexSeq
	s.seqMu.Unlock()
	var reports []presence.Report
	var ends []string
	if end {
		ends = []string{event.NativeSessionID}
	} else {
		reports = []presence.Report{report}
	}
	changed, err := s.core.ApplyReports(s.codexLease.ID, seq, reports, ends)
	if err != nil {
		return
	}
	if !end {
		if id, ok := s.core.TaskPresenceIDFor("codex", event.NativeSessionID); ok {
			beforeGeneration, beforeRevision := s.core.Revision()
			_ = s.core.SetCapabilities(id, []contract.Capability{contract.CapabilityFocus})
			afterGeneration, afterRevision := s.core.Revision()
			changed = changed || beforeGeneration != afterGeneration || beforeRevision != afterRevision
		}
	}
	if changed {
		s.signal()
	}
}

func (s *Service) handleFeed(w http.ResponseWriter, r *http.Request) {
	if !s.authenticator.Authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := ws.Upgrade(w, r)
	if err != nil {
		return
	}
	s.hub.add(conn, BearerToken(r))
	defer s.hub.remove(conn)
	if err := s.writeSnapshot(conn); err != nil {
		return
	}
	for {
		frame, err := conn.ReadText()
		if err != nil {
			return
		}
		var action contract.FeedAction
		if err := contract.DecodeStrict(frame, contract.SchemaFeedV1, &action); err != nil {
			return
		}
		result := s.router.Dispatch(r.Context(), action)
		if result.Status == "ok" {
			_, before := s.core.Revision()
			_ = s.core.Acknowledge(action.TaskPresenceID) // Terminal success also acknowledges.
			_, after := s.core.Revision()
			if after != before {
				s.signal()
			}
		}
		encoded, err := contract.Encode(contract.SchemaFeedV1, result)
		if err != nil || conn.WriteText(encoded) != nil {
			return
		}
	}
}

func (s *Service) writeSnapshot(conn *ws.Conn) error {
	snapshot := s.core.Snapshot(s.edgeID, "edge")
	b, err := contract.Encode(contract.SchemaFeedV1, snapshot)
	if err != nil {
		return err
	}
	return conn.WriteText(b)
}

func (s *Service) broadcastLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.notify:
			snapshot := s.core.Snapshot(s.edgeID, "edge")
			b, err := contract.Encode(contract.SchemaFeedV1, snapshot)
			if err == nil {
				s.hub.broadcast(b)
			}
		}
	}
}

func (s *Service) maintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	ping := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.core.EnforceRetention() || s.core.ExpireLeases() {
				s.signal()
			}
		case <-ping.C:
			s.hub.ping()
		}
	}
}

func (s *Service) enrichFromAppServer(ctx context.Context) {
	for {
		client, err := appserver.Start(ctx, s.codexBinary)
		if err != nil {
			s.logger.Printf("read-only Codex App Server unavailable")
		} else {
			s.logger.Printf("read-only Codex App Server connected via %s", appserver.BinaryForDiagnostics(s.codexBinary))
			appserver.Poll(ctx, client, s.codexReducer.IDs, func(info appserver.ThreadInfo) {
				s.codexIngestMu.Lock()
				defer s.codexIngestMu.Unlock()
				report, changed := s.codexReducer.Enrich(info.ID, info.Title, info.RuntimeStatus, info.Failed, time.Now())
				if !changed {
					return
				}
				s.seqMu.Lock()
				s.codexSeq++
				seq := s.codexSeq
				s.seqMu.Unlock()
				if applied, err := s.core.ApplyReports(s.codexLease.ID, seq, []presence.Report{report}, nil); err == nil && applied {
					s.signal()
				}
			})
			_ = client.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (s *Service) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func edgeIDFromCertificate(certPath string) string {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return "edge-unknown"
	}
	sum := sha256.Sum256(b)
	return "edge-" + hex.EncodeToString(sum[:16])
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

type feedHub struct {
	mu      sync.RWMutex
	clients map[*ws.Conn]string // conn -> presented bearer token
}

func newFeedHub() *feedHub { return &feedHub{clients: make(map[*ws.Conn]string)} }

func (h *feedHub) add(conn *ws.Conn, token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = token
}

func (h *feedHub) remove(conn *ws.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	_ = conn.Close()
}

// dropToken disconnects every connection authenticated with token (used on
// credential revocation).
func (h *feedHub) dropToken(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn, presented := range h.clients {
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1 {
			delete(h.clients, conn)
			_ = conn.Close()
		}
	}
}

func (h *feedHub) snapshot() []*ws.Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := make([]*ws.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	return clients
}

func (h *feedHub) broadcast(snapshot []byte) {
	for _, conn := range h.snapshot() {
		if err := conn.WriteText(snapshot); err != nil {
			h.remove(conn)
		}
	}
}

func (h *feedHub) ping() {
	for _, conn := range h.snapshot() {
		if err := conn.Ping(); err != nil {
			h.remove(conn)
		}
	}
}
