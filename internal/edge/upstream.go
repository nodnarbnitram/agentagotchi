// Upstream client: the Edge's outbound WSS connection to a Home Bridge.
// Sends complete-replacement absolute snapshots (Edge generation + monotonic
// revision) and receives reverse action requests, which are dispatched
// through the same fail-closed router as direct device actions.
package edge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
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

	mu      sync.Mutex
	conn    *upstreamConn
	lastRev uint64
	lastGen uint64
}

func NewUpstreamClient(cfg UpstreamConfig, core *presence.Core, edgeID string, router *Router) *UpstreamClient {
	return &UpstreamClient{cfg: cfg, core: core, edgeID: edgeID, router: router}
}

// Run keeps the upstream connected until ctx ends.
func (u *UpstreamClient) Run(ctx context.Context) {
	for {
		err := u.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		_ = err
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
		// Reuse the feed router's fail-closed dispatch; SeenRevision maps to
		// the Edge's current revision contract.
		result := u.router.Dispatch(ctx, contract.FeedAction{
			Schema: contract.SchemaFeedV1, Type: "action",
			ActionID: request.ActionID, Capability: request.Capability,
			TaskPresenceID: request.TaskPresenceID, SeenRevision: request.SeenRevision,
		})
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
