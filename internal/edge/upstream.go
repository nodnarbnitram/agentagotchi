// Upstream client: the Edge's outbound WSS connection to a Home Bridge.
// Sends complete-replacement absolute snapshots (Edge generation + monotonic
// revision) and receives reverse action requests, which are dispatched
// through the same fail-closed router as direct device actions.
package edge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

// UpstreamConfig describes one Edge→Home pairing.
type UpstreamConfig struct {
	// URL is the explicit https/wss URL (remote Home uses publicly trusted
	// TLS; discovery alone never authorizes).
	URL string
	// Token is the edge-ingress pairing credential.
	Token string
	// InsecureSkipVerify exists only for local test servers; production must
	// keep publicly trusted TLS verification.
	InsecureSkipVerify bool
}

// UpstreamClient maintains one outbound connection with reconnect + resync.
// Actions are never queued (docs/adr/0006): while disconnected, snapshots are
// not buffered beyond the core's absolute state, which resyncs on reconnect.
type UpstreamClient struct {
	cfg    UpstreamConfig
	core   *presence.Core
	edgeID string
	router *Router

	mu       sync.Mutex
	conn     *upstreamConn
	lastRev  uint64
	lastGen  uint64
	onChange func()
	logger   *log.Logger
}

func NewUpstreamClient(cfg UpstreamConfig, core *presence.Core, edgeID string, router *Router) *UpstreamClient {
	return &UpstreamClient{cfg: cfg, core: core, edgeID: edgeID, router: router}
}

// Run keeps the upstream connected until ctx ends. Dial/handshake failures
// are logged (distinctly for 401, which usually means a revoked credential);
// transport drops after a healthy session retry quietly.
func (u *UpstreamClient) Run(ctx context.Context) {
	for {
		err := u.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && u.logger != nil {
			var hs *HandshakeError
			switch {
			case errors.As(err, &hs) && hs.StatusCode == http.StatusUnauthorized:
				u.logger.Printf("upstream rejected the edge-ingress credential (401); it may be revoked — re-pair")
			case errors.As(err, &hs):
				u.logger.Printf("upstream handshake failed: %v", err)
			default:
				u.logger.Printf("upstream connection ended: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (u *UpstreamClient) connectOnce(ctx context.Context) error {
	dialURL := u.cfg.URL
	tlsConfig := &tls.Config{InsecureSkipVerify: u.cfg.InsecureSkipVerify} //nolint:gosec // test-only path, documented
	conn, err := dialUpstream(ctx, dialURL, u.cfg.Token, tlsConfig)
	if err != nil {
		return err
	}
	u.mu.Lock()
	u.conn = conn
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.conn = nil
		u.mu.Unlock()
		conn.close()
	}()
	// Absolute resync on every (re)connect.
	if err := u.sendSnapshot(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		frame, err := conn.read()
		if err != nil {
			return err
		}
		var request contract.UpstreamActionRequest
		if err := json.Unmarshal(frame, &request); err != nil ||
			request.Schema != contract.SchemaUpstreamV1 || request.Type != "action_request" {
			continue
		}
		// Dispatch like a direct feed action: dismissal controls (acknowledge/
		// snooze) are Edge-global; Device Capabilities go through the router.
		feedAction := contract.FeedAction{
			Schema: contract.SchemaFeedV1, Type: "action",
			ActionID: request.ActionID, Capability: request.Capability,
			TaskPresenceID: request.TaskPresenceID, SeenRevision: request.SeenRevision,
		}
		var result contract.ActionResult
		if contract.IsDismissal(request.Capability) {
			result = u.dismiss(feedAction)
		} else {
			result = u.router.Dispatch(ctx, feedAction)
			if result.Status == "ok" {
				// Mirror the direct feed path: exact success on a terminal target
				// also acknowledges it.
				_ = u.core.Acknowledge(request.TaskPresenceID)
			}
		}
		if result.Status == "ok" && u.onChange != nil {
			// Any successful mutation must converge every surface: local feeds
			// broadcast and the Home receives a fresh absolute snapshot.
			u.onChange()
		}
		result.Schema = contract.SchemaUpstreamV1
		payload, err := json.Marshal(result)
		if err != nil {
			continue
		}
		if err := conn.write(payload); err != nil {
			return err
		}
	}
}

// dismiss applies a Home-relayed dismissal action against the core.
func (u *UpstreamClient) dismiss(action contract.FeedAction) contract.ActionResult {
	result := contract.ActionResult{
		Schema: contract.SchemaUpstreamV1, Type: "action_result", ActionID: action.ActionID,
	}
	_, revision := u.core.Revision()
	if action.ActionID == "" || action.SeenRevision != revision || !u.core.HasTask(action.TaskPresenceID) {
		result.Status = "stale"
		return result
	}
	var err error
	switch action.Capability {
	case contract.CapabilityAcknowledge:
		err = u.core.Acknowledge(action.TaskPresenceID)
	case contract.CapabilitySnooze:
		err = u.core.Snooze(action.TaskPresenceID)
	}
	if err != nil {
		result.Status = "failed"
		return result
	}
	result.Status = "ok"
	return result
}

// SetOnChange wires the Edge's change signal so Home-relayed mutations
// converge local feeds and upstream state. Called once during Service setup.
func (u *UpstreamClient) SetOnChange(fn func()) {
	u.mu.Lock()
	u.onChange = fn
	u.mu.Unlock()
}

// SetLogger wires the Edge logger for upstream connection diagnostics.
func (u *UpstreamClient) SetLogger(logger *log.Logger) {
	u.mu.Lock()
	u.logger = logger
	u.mu.Unlock()
}

// NotifySnapshot pushes the current absolute snapshot when connected. Called
// by the Edge's change signal.
func (u *UpstreamClient) NotifySnapshot() {
	u.mu.Lock()
	conn := u.conn
	u.mu.Unlock()
	if conn == nil {
		return // offline: absolute state resyncs on reconnect, never queued
	}
	_ = u.sendSnapshot()
}

func (u *UpstreamClient) sendSnapshot() error {
	snap := u.core.Snapshot(u.edgeID, "edge")
	u.mu.Lock()
	conn := u.conn
	u.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("upstream not connected")
	}
	upstream := contract.UpstreamSnapshot{
		Schema: contract.SchemaUpstreamV1, Type: "snapshot",
		EdgeID: u.edgeID, Generation: snap.Origin.Generation, Revision: snap.Origin.Revision,
		SnapshotGeneratedAt: snap.GeneratedAt,
		Tasks:               snap.Tasks,
		Counts:              snap.Counts,
		AggregateState:      snap.AggregateState,
	}
	payload, err := contract.Encode(contract.SchemaUpstreamV1, upstream)
	if err != nil {
		return err
	}
	return conn.write(payload)
}

// upstreamConn is a minimal client-side WSS connection (text frames).
type upstreamConn struct {
	write func([]byte) error
	read  func() ([]byte, error)
	close func() error
}

func dialUpstream(ctx context.Context, url, token string, tlsConfig *tls.Config) (*upstreamConn, error) {
	conn, reader, err := dialWSS(ctx, url, token, tlsConfig)
	if err != nil {
		return nil, err
	}
	return &upstreamConn{
		write: func(payload []byte) error { return wsClientWrite(conn, payload) },
		read:  func() ([]byte, error) { return wsClientRead(reader) },
		close: conn.Close,
	}, nil
}
