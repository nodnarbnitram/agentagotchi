package edge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

const MaxIPCFrameBytes = 64 << 10

type ipcEnvelope struct {
	Schema string `json:"schema"`
	Type   string `json:"type"`
}

func readIPCFrame(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxIPCFrameBytes {
		return nil, fmt.Errorf("IPC frame exceeds %d bytes", MaxIPCFrameBytes)
	}
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return nil, errors.New("IPC frame missing newline delimiter")
		}
		return nil, err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	if len(line) == 0 {
		return nil, errors.New("empty IPC frame")
	}
	return line, nil
}

func decodeIPCEnvelope(frame []byte) (ipcEnvelope, error) {
	var envelope ipcEnvelope
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return envelope, errors.New("invalid IPC JSON")
	}
	if envelope.Schema != contract.SchemaIPCV1 {
		return envelope, fmt.Errorf("wrong IPC schema")
	}
	return envelope, nil
}

// SendHook sends one bounded newline-delimited hook frame. Callers use short
// deadlines because hook telemetry must never delay the Agent Harness.
func SendHook(socketPath string, event contract.IPCHookEvent) error {
	b, err := contract.Encode(contract.SchemaIPCV1, event)
	if err != nil {
		return err
	}
	if len(b)+1 > MaxIPCFrameBytes {
		return errors.New("hook IPC frame exceeds limit")
	}
	conn, err := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
	_, err = conn.Write(append(b, '\n'))
	return err
}

func (s *Service) serveIPC(ctx context.Context) error {
	if err := prepareSocketPath(s.socketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on adapter IPC: %w", err)
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("secure adapter IPC socket: %w", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(s.socketPath)
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
		go s.handleIPCConnection(conn)
	}
}

func (s *Service) handleIPCConnection(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReaderSize(conn, MaxIPCFrameBytes)
	frame, err := readIPCFrame(reader)
	if err != nil {
		return // Never log raw or decoded adapter payloads.
	}
	envelope, err := decodeIPCEnvelope(frame)
	if err != nil {
		return
	}
	switch envelope.Type {
	case "hook_event":
		var event contract.IPCHookEvent
		if err := contract.DecodeStrict(frame, contract.SchemaIPCV1, &event); err != nil ||
			event.Type != "hook_event" || event.Harness != "codex" || event.EventID == "" ||
			event.NativeSessionID == "" || event.Event == "" {
			return
		}
		s.applyCodexHook(event)
	case "adapter_hello":
		var hello contract.IPCAdapterHello
		if err := contract.DecodeStrict(frame, contract.SchemaIPCV1, &hello); err != nil ||
			hello.Type != "adapter_hello" || hello.Harness == "" {
			return
		}
		s.handleLeasedAdapter(conn, reader, hello)
	}
}

func (s *Service) handleLeasedAdapter(conn net.Conn, reader *bufio.Reader, hello contract.IPCAdapterHello) {
	lease, err := s.core.Attach(hello.Harness, hello.Capabilities)
	if err != nil {
		return
	}
	defer func() {
		if s.core.Detach(lease.ID) {
			s.signal()
		}
	}()
	ack := contract.IPCHelloAck{
		Schema: contract.SchemaIPCV1, Type: "hello_ack", LeaseID: lease.ID,
		LeaseSeconds: int64(s.leaseDuration / time.Second),
	}
	if err := writeIPCFrame(conn, ack); err != nil {
		return
	}
	session := &leasedAdapterSession{conn: conn, pending: make(map[string]chan contract.IPCActionResult)}
	for _, capability := range hello.Capabilities {
		capability := capability
		s.router.Register(hello.Harness, capability, session.dispatch)
		defer s.router.Register(hello.Harness, capability, nil)
	}
	_ = conn.SetReadDeadline(time.Time{})
	for {
		frame, err := readIPCFrame(reader)
		if err != nil {
			return
		}
		envelope, err := decodeIPCEnvelope(frame)
		if err != nil {
			return
		}
		switch envelope.Type {
		case "action_result":
			var result contract.IPCActionResult
			if err := contract.DecodeStrict(frame, contract.SchemaIPCV1, &result); err != nil ||
				result.Type != "action_result" || result.ActionID == "" ||
				(result.Status != "ok" && result.Status != "rejected" && result.Status != "unsupported") {
				return
			}
			session.deliver(result)
		case "heartbeat":
			var heartbeat contract.IPCHeartbeat
			if err := contract.DecodeStrict(frame, contract.SchemaIPCV1, &heartbeat); err != nil ||
				heartbeat.Type != "heartbeat" || heartbeat.LeaseID != lease.ID || !s.core.Renew(lease.ID) {
				return
			}
		case "presence_report":
			var report contract.IPCPresenceReport
			if err := contract.DecodeStrict(frame, contract.SchemaIPCV1, &report); err != nil ||
				report.Type != "presence_report" || report.LeaseID != lease.ID || report.ProducerSeq == 0 {
				return
			}
			reports := make([]presence.Report, 0, len(report.Reports))
			for _, item := range report.Reports {
				if item.NativeSessionID == "" || !contract.ValidState(item.State) || !contract.ValidReason(item.Reason) || item.SubagentCount < 0 {
					return
				}
				reports = append(reports, presence.Report{
					NativeSessionID: item.NativeSessionID,
					DisplayKey:      item.DisplayKey,
					SafeTitle:       safeHarnessTitle(hello.Harness),
					State:           item.State,
					Reason:          item.Reason,
					SubagentCount:   item.SubagentCount,
				})
			}
			changed, err := s.core.ApplyReports(lease.ID, report.ProducerSeq, reports, report.Ends)
			if err != nil {
				return
			}
			_, beforeCapabilities := s.core.Revision()
			for _, item := range report.Reports {
				if id, ok := s.core.TaskPresenceIDFor(hello.Harness, item.NativeSessionID); ok {
					_ = s.core.SetCapabilities(id, hello.Capabilities)
				}
			}
			_, afterCapabilities := s.core.Revision()
			changed = changed || afterCapabilities != beforeCapabilities
			if !s.core.Renew(lease.ID) {
				return
			}
			if changed {
				s.signal()
			}
		default:
			return
		}
	}
}

type leasedAdapterSession struct {
	conn net.Conn

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan contract.IPCActionResult
}

func (s *leasedAdapterSession) dispatch(ctx context.Context, _ string, action contract.FeedAction) error {
	resultCh := make(chan contract.IPCActionResult, 1)
	s.pendingMu.Lock()
	if _, exists := s.pending[action.ActionID]; exists {
		s.pendingMu.Unlock()
		return errors.New("duplicate in-flight adapter action")
	}
	s.pending[action.ActionID] = resultCh
	s.pendingMu.Unlock()
	defer func() {
		s.pendingMu.Lock()
		delete(s.pending, action.ActionID)
		s.pendingMu.Unlock()
	}()

	request := contract.IPCActionRequest{
		Schema: contract.SchemaIPCV1, Type: "action_request", ActionID: action.ActionID,
		Capability: action.Capability, TaskPresenceID: action.TaskPresenceID,
	}
	s.writeMu.Lock()
	err := writeIPCFrame(s.conn, request)
	s.writeMu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("adapter action timed out")
	case result := <-resultCh:
		if result.Status != "ok" {
			return fmt.Errorf("adapter action %s", result.Status)
		}
		return nil
	}
}

func (s *leasedAdapterSession) deliver(result contract.IPCActionResult) {
	s.pendingMu.Lock()
	ch := s.pending[result.ActionID]
	s.pendingMu.Unlock()
	if ch != nil {
		select {
		case ch <- result:
		default:
		}
	}
}

func writeIPCFrame(w io.Writer, message any) error {
	b, err := contract.Encode(contract.SchemaIPCV1, message)
	if err != nil {
		return err
	}
	if len(b)+1 > MaxIPCFrameBytes {
		return errors.New("IPC frame exceeds limit")
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func safeHarnessTitle(harness string) string {
	switch harness {
	case "codex":
		return "Codex"
	case "pi":
		return "Pi"
	default:
		return "Agent"
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
