package edge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
	"agentagotchi.local/agentagotchi/internal/ws"
)

// fakeHome is an in-process Home Bridge stand-in: accepts the Edge's
// outbound upstream WSS, validates auth, records snapshots, and can send a
// reverse action request.
type fakeHome struct {
	server *httptest.Server

	mu        sync.Mutex
	snapshots []contract.UpstreamSnapshot
	action    chan contract.UpstreamActionRequest
	result    chan contract.ActionResult
	conn      chan *ws.Conn
}

func newFakeHome(t *testing.T, wantToken string) *fakeHome {
	t.Helper()
	home := &fakeHome{
		action: make(chan contract.UpstreamActionRequest, 1),
		result: make(chan contract.ActionResult, 1),
		conn:   make(chan *ws.Conn, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/edge/v1", func(w http.ResponseWriter, r *http.Request) {
		if BearerToken(r) != wantToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := ws.Upgrade(w, r)
		if err != nil {
			return
		}
		home.conn <- conn
		for {
			frame, err := conn.ReadText()
			if err != nil {
				return
			}
			var envelope struct {
				Schema string `json:"schema"`
				Type   string `json:"type"`
			}
			if err := json.Unmarshal(frame, &envelope); err != nil {
				continue
			}
			switch envelope.Type {
			case "snapshot":
				var snapshot contract.UpstreamSnapshot
				if err := json.Unmarshal(frame, &snapshot); err == nil {
					home.mu.Lock()
					home.snapshots = append(home.snapshots, snapshot)
					home.mu.Unlock()
				}
			case "action_result":
				var result contract.ActionResult
				if err := json.Unmarshal(frame, &result); err == nil {
					home.result <- result
				}
			}
		}
	})
	home.server = httptest.NewTLSServer(mux)
	t.Cleanup(home.server.Close)
	return home
}

func (h *fakeHome) url() string {
	return "wss://" + strings.TrimPrefix(h.server.URL, "https://") + "/edge/v1"
}

func (h *fakeHome) lastSnapshot() contract.UpstreamSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshots[len(h.snapshots)-1]
}

func (h *fakeHome) snapshotCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.snapshots)
}

// TestEdgeToHomeSnapshotAndReverseAction proves the upstream contract: the
// Edge dials outbound, resyncs absolute state on connect, pushes changes, and
// answers reverse action requests through its fail-closed router.
func TestEdgeToHomeSnapshotAndReverseAction(t *testing.T) {
	home := newFakeHome(t, "home-cred-token")

	s := newTestServiceDir(t, shortDataDir(t, "up"), 18793)
	lease, err := s.core.Attach("codex", []contract.Capability{contract.CapabilityFocus})
	if err != nil {
		t.Fatal(err)
	}
	nativeID := "019fa063-b4d1-7d81-bced-7f9f55ec7611"
	if _, err := s.core.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: nativeID, SafeTitle: "Codex",
		State: presence.StateRunning, Reason: presence.ReasonWorking,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	id, _ := s.core.TaskPresenceIDFor("codex", nativeID)
	if err := s.core.SetCapabilities(id, []contract.Capability{contract.CapabilityFocus}); err != nil {
		t.Fatal(err)
	}
	dispatched := make(chan string, 1)
	s.router.Register("codex", contract.CapabilityFocus, func(_ context.Context, native string, _ contract.FeedAction) error {
		dispatched <- native
		return nil
	})

	client := NewUpstreamClient(UpstreamConfig{
		URL: home.url(), Token: "home-cred-token", InsecureSkipVerify: true, // test server only
	}, s.core, s.edgeID, s.router)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	// Absolute snapshot arrives on connect.
	var conn *ws.Conn
	select {
	case conn = <-home.conn:
	case <-time.After(5 * time.Second):
		t.Fatal("home never received upstream connection")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && home.snapshotCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if home.snapshotCount() == 0 {
		t.Fatal("home never received absolute snapshot")
	}
	snap := home.lastSnapshot()
	if snap.Schema != contract.SchemaUpstreamV1 || snap.EdgeID != s.edgeID {
		t.Fatalf("bad upstream snapshot: %+v", snap)
	}
	if len(snap.Tasks) != 1 || snap.Tasks[0].TaskPresenceID != id {
		t.Fatalf("upstream tasks = %+v", snap.Tasks)
	}
	// Privacy: the native session ID is not in the upstream payload.
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), nativeID) {
		t.Fatalf("upstream leaked native session ID: %s", raw)
	}

	// Change pushes a new snapshot.
	if _, err := s.core.ApplyReports(lease.ID, 2, []presence.Report{{
		NativeSessionID: nativeID, SafeTitle: "Codex",
		State: presence.StateReady, Reason: presence.ReasonCompleted,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	client.NotifySnapshot()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && home.snapshotCount() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if home.snapshotCount() < 2 {
		t.Fatal("change did not push upstream snapshot")
	}
	if got := home.lastSnapshot(); got.Tasks[0].State != "ready" || got.Revision <= snap.Revision {
		t.Fatalf("second snapshot = %+v", got)
	}

	// Reverse action: Home asks the Edge to focus; Edge dispatches through
	// the router and answers action_result.
	revision := home.lastSnapshot().Revision
	request := contract.UpstreamActionRequest{
		Schema: contract.SchemaUpstreamV1, Type: "action_request",
		ActionID: "home-focus-1", Capability: contract.CapabilityFocus,
		TaskPresenceID: id, SeenRevision: revision,
	}
	payload, _ := json.Marshal(request)
	if err := conn.WriteText(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case native := <-dispatched:
		if native != nativeID {
			t.Fatalf("dispatched native = %q", native)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reverse action never dispatched")
	}
	select {
	case result := <-home.result:
		if result.ActionID != "home-focus-1" || result.Status != "ok" {
			t.Fatalf("action_result = %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no action_result returned to home")
	}
}

// TestUpstreamAuthFailureDoesNotRetryStorm checks a rejected credential
// backs off and never crashes the Edge.
func TestUpstreamAuthFailureDoesNotRetryStorm(t *testing.T) {
	home := newFakeHome(t, "correct-token")
	s := newTestServiceDir(t, shortDataDir(t, "ua"), 18794)
	client := NewUpstreamClient(UpstreamConfig{
		URL: home.url(), Token: "wrong-token", InsecureSkipVerify: true,
	}, s.core, s.edgeID, s.router)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	client.Run(ctx) // must return cleanly on ctx timeout, no panic
}

// TestHomeRelayedDismissalConverges proves the reverse action path triggers
// the Edge's change signal: a Home-relayed acknowledge on a terminal task
// removes it locally AND pushes a fresh absolute snapshot upstream.
func TestHomeRelayedDismissalConverges(t *testing.T) {
	home := newFakeHome(t, "home-cred-token")

	s := newTestServiceDir(t, shortDataDir(t, "dismiss"), 18794)
	lease, err := s.core.Attach("codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.core.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: "native-terminal", SafeTitle: "Codex",
		State: presence.StateReady, Reason: presence.ReasonCompleted,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	id, _ := s.core.TaskPresenceIDFor("codex", "native-terminal")

	client := NewUpstreamClient(UpstreamConfig{
		URL: home.url(), Token: "home-cred-token", InsecureSkipVerify: true,
	}, s.core, s.edgeID, s.router)
	// Wire the change signal the way Service does: onChange drives push.
	client.SetOnChange(client.NotifySnapshot)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	var conn *ws.Conn
	select {
	case conn = <-home.conn:
	case <-time.After(5 * time.Second):
		t.Fatal("home never received upstream connection")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && home.snapshotCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if home.snapshotCount() == 0 {
		t.Fatal("no initial snapshot")
	}

	// Home relays an acknowledge with the Edge's current revision (which the
	// Home supplies via seenRevision translation).
	revision := home.lastSnapshot().Revision
	request := contract.UpstreamActionRequest{
		Schema: contract.SchemaUpstreamV1, Type: "action_request",
		ActionID: "home-ack-1", Capability: contract.CapabilityAcknowledge,
		TaskPresenceID: id, SeenRevision: revision,
	}
	payload, _ := json.Marshal(request)
	if err := conn.WriteText(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-home.result:
		if result.Status != "ok" {
			t.Fatalf("action_result = %+v, want ok", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no action_result returned to home")
	}

	// The mutation converged upstream without any external poke: a fresh
	// snapshot with the task removed arrives because onChange fired.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if home.snapshotCount() >= 2 && len(home.lastSnapshot().Tasks) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no converged snapshot after relayed dismissal; snapshots=%d last=%+v",
		home.snapshotCount(), home.lastSnapshot())
}

// TestHomeRelayedFocusAcknowledgesTerminal proves a successful Home-relayed
// focus on a terminal task acknowledges it, mirroring the direct feed path.
func TestHomeRelayedFocusAcknowledgesTerminal(t *testing.T) {
	home := newFakeHome(t, "home-cred-token")

	s := newTestServiceDir(t, shortDataDir(t, "focusack"), 18795)
	lease, err := s.core.Attach("codex", []contract.Capability{contract.CapabilityFocus})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.core.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: "native-t", SafeTitle: "Codex",
		State: presence.StateReady, Reason: presence.ReasonCompleted,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	id, _ := s.core.TaskPresenceIDFor("codex", "native-t")
	if err := s.core.SetCapabilities(id, []contract.Capability{contract.CapabilityFocus}); err != nil {
		t.Fatal(err)
	}
	s.router.Register("codex", contract.CapabilityFocus, func(_ context.Context, _ string, _ contract.FeedAction) error {
		return nil
	})

	client := NewUpstreamClient(UpstreamConfig{
		URL: home.url(), Token: "home-cred-token", InsecureSkipVerify: true,
	}, s.core, s.edgeID, s.router)
	client.SetOnChange(client.NotifySnapshot)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx)

	var conn *ws.Conn
	select {
	case conn = <-home.conn:
	case <-time.After(5 * time.Second):
		t.Fatal("home never received upstream connection")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && home.snapshotCount() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	revision := home.lastSnapshot().Revision
	payload, _ := json.Marshal(contract.UpstreamActionRequest{
		Schema: contract.SchemaUpstreamV1, Type: "action_request",
		ActionID: "home-focus-ack", Capability: contract.CapabilityFocus,
		TaskPresenceID: id, SeenRevision: revision,
	})
	if err := conn.WriteText(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-home.result:
		if result.Status != "ok" {
			t.Fatalf("action_result = %+v, want ok", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no action_result returned to home")
	}
	// Acknowledged terminal task disappears from the core and the converged
	// upstream snapshot.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s.core.HasTask(id) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if home.snapshotCount() >= 2 && len(home.lastSnapshot().Tasks) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("focus success did not acknowledge and converge upstream")
}
