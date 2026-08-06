package edge

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
)

// TestConcurrentCodexAndPiThroughOneEdge proves the same-machine,
// several-harnesses acceptance: Codex (hook ingest) and Pi (leased IPC
// session) run concurrently through one Edge with no separate bridge
// processes and no task collisions — including identical native session IDs.
func TestConcurrentCodexAndPiThroughOneEdge(t *testing.T) {
	s := newTestServiceDir(t, shortDataDir(t, "cc"), 18787)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx) }()
	waitForSocket(t, s, serveErr)

	sharedNativeID := "019fa063-b4d1-7d81-bced-7f9f55ec7611"

	// Codex: hook events through the Edge's hook ingest path.
	s.applyCodexHook(contract.IPCHookEvent{
		Schema: contract.SchemaIPCV1, Type: "hook_event", EventID: "evt-c1", Harness: "codex",
		NativeSessionID: sharedNativeID, Event: "UserPromptSubmit", At: time.Now().UTC(),
	})

	// Pi: leased IPC session over the real socket.
	conn, err := net.Dial("unix", s.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writeIPC(t, conn, contract.IPCAdapterHello{
		Schema: contract.SchemaIPCV1, Type: "adapter_hello", Harness: "pi",
		AdapterVersion: "0.1.0", Capabilities: []contract.Capability{},
	})
	var ack contract.IPCHelloAck
	readIPC(t, reader, &ack)
	if ack.LeaseID == "" {
		t.Fatal("no lease granted to Pi adapter")
	}
	writeIPC(t, conn, contract.IPCPresenceReport{
		Schema: contract.SchemaIPCV1, Type: "presence_report", LeaseID: ack.LeaseID, ProducerSeq: 1,
		Reports: []contract.IPCPresence{{
			NativeSessionID: sharedNativeID, SafeTitle: "Pi",
			State: "running", Reason: "working",
		}},
	})

	// Feed must show two distinct Task Presences despite identical native IDs.
	deadline := time.Now().Add(2 * time.Second)
	var snap contract.FeedSnapshot
	for time.Now().Before(deadline) {
		snap = s.core.Snapshot(s.edgeID, "edge")
		if len(snap.Tasks) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(snap.Tasks) != 2 {
		t.Fatalf("concurrent harnesses collided: got %d tasks, want 2: %+v", len(snap.Tasks), snap.Tasks)
	}
	ids := map[string]bool{}
	var codexTask, piTask *contract.FeedTask
	for i := range snap.Tasks {
		task := &snap.Tasks[i]
		if ids[task.TaskPresenceID] {
			t.Fatalf("duplicate Task Presence ID %q", task.TaskPresenceID)
		}
		ids[task.TaskPresenceID] = true
		switch task.SafeTitle {
		case "Codex":
			codexTask = task
		case "Pi":
			piTask = task
		}
	}
	if codexTask == nil || piTask == nil {
		t.Fatalf("expected Codex and Pi presences, got %+v", snap.Tasks)
	}
	// Codex advertises focus (hook-ingested tasks support it); Pi does not.
	if len(piTask.Capabilities) != 0 {
		t.Errorf("Pi presence advertised capabilities %v, want none", piTask.Capabilities)
	}

	// Feed integration: a device sees both in one snapshot and can focus only
	// the Codex task.
	dispatched := ""
	s.router.Register("codex", contract.CapabilityFocus, func(_ context.Context, native string, _ contract.FeedAction) error {
		dispatched = native
		return nil
	})
	server := httptest.NewTLSServer(s.Handler())
	defer server.Close()
	feed, feedReader := dialTestWebSocket(t, server.URL, "/feed/v1", "test-token")
	defer feed.Close()
	var feedSnap contract.FeedSnapshot
	if err := contract.DecodeStrict(readServerText(t, feedReader), contract.SchemaFeedV1, &feedSnap); err != nil {
		t.Fatal(err)
	}
	if len(feedSnap.Tasks) != 2 {
		t.Fatalf("feed snapshot has %d tasks, want 2", len(feedSnap.Tasks))
	}

	// Focus the Codex task: dispatches to the Codex handler with the Codex
	// native ID, not the Pi one.
	action := contract.FeedAction{
		Schema: contract.SchemaFeedV1, Type: "action", ActionID: "focus-codex",
		Capability: contract.CapabilityFocus, TaskPresenceID: codexTask.TaskPresenceID,
		SeenRevision: feedSnap.Origin.Revision,
	}
	writeClientJSON(t, feed, action)
	var result contract.ActionResult
	if err := contract.DecodeStrict(readServerText(t, feedReader), contract.SchemaFeedV1, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || dispatched != sharedNativeID {
		t.Fatalf("codex focus = %q dispatched %q", result.Status, dispatched)
	}

	// Focus the Pi task: unsupported (no advertised capability), never a
	// silent dispatch to the wrong harness.
	dispatched = ""
	action.ActionID = "focus-pi"
	action.TaskPresenceID = piTask.TaskPresenceID
	writeClientJSON(t, feed, action)
	if err := contract.DecodeStrict(readServerText(t, feedReader), contract.SchemaFeedV1, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "unsupported" {
		t.Fatalf("pi focus status = %q, want unsupported", result.Status)
	}
	if dispatched != "" {
		t.Fatalf("pi focus dispatched to %q — wrong harness routed", dispatched)
	}

	// Codex finishes (Stop), Pi keeps running; feed reflects both.
	s.applyCodexHook(contract.IPCHookEvent{
		Schema: contract.SchemaIPCV1, Type: "hook_event", EventID: "evt-c2", Harness: "codex",
		NativeSessionID: sharedNativeID, Event: "Stop", At: time.Now().UTC(),
	})
	writeIPC(t, conn, contract.IPCPresenceReport{
		Schema: contract.SchemaIPCV1, Type: "presence_report", LeaseID: ack.LeaseID, ProducerSeq: 2,
		Reports: []contract.IPCPresence{{
			NativeSessionID: sharedNativeID, SafeTitle: "Pi",
			State: "ready", Reason: "completed",
		}},
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap = s.core.Snapshot(s.edgeID, "edge")
		states := map[string]string{}
		for _, task := range snap.Tasks {
			states[task.SafeTitle] = task.State
		}
		if states["Codex"] == "ready" && states["Pi"] == "ready" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap = s.core.Snapshot(s.edgeID, "edge")
	states := map[string]string{}
	for _, task := range snap.Tasks {
		states[task.SafeTitle] = task.State
	}
	if states["Codex"] != "ready" || states["Pi"] != "ready" {
		t.Fatalf("independent lifecycle updates failed: %v", states)
	}
}

// TestPiLeaseExpiryLeavesCodexUntouched proves independent adapter failure:
// when Pi's lease dies, only Pi presences end.
func TestPiLeaseExpiryLeavesCodexUntouched(t *testing.T) {
	s := newTestServiceDir(t, shortDataDir(t, "pl"), 18788)
	s.leaseDuration = 40 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx) }()
	waitForSocket(t, s, serveErr)

	s.applyCodexHook(contract.IPCHookEvent{
		Schema: contract.SchemaIPCV1, Type: "hook_event", EventID: "evt-x1", Harness: "codex",
		NativeSessionID: "codex-native", Event: "UserPromptSubmit", At: time.Now().UTC(),
	})
	conn, err := net.Dial("unix", s.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	writeIPC(t, conn, contract.IPCAdapterHello{
		Schema: contract.SchemaIPCV1, Type: "adapter_hello", Harness: "pi", AdapterVersion: "0.1.0",
	})
	var ack contract.IPCHelloAck
	readIPC(t, reader, &ack)
	writeIPC(t, conn, contract.IPCPresenceReport{
		Schema: contract.SchemaIPCV1, Type: "presence_report", LeaseID: ack.LeaseID, ProducerSeq: 1,
		Reports: []contract.IPCPresence{{
			NativeSessionID: "pi-native", SafeTitle: "Pi",
			State: "running", Reason: "working",
		}},
	})
	// Kill the Pi connection without ends; lease expiry must clean up.
	conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap := s.core.Snapshot(s.edgeID, "edge")
		if len(snap.Tasks) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := s.core.Snapshot(s.edgeID, "edge")
	if len(snap.Tasks) != 1 || snap.Tasks[0].SafeTitle != "Codex" {
		t.Fatalf("lease expiry removed wrong presences: %+v", snap.Tasks)
	}
	// The Codex presence was not fabricated into a terminal state.
	if snap.Tasks[0].State != "running" {
		t.Fatalf("codex presence mutated to %q by unrelated Pi failure", snap.Tasks[0].State)
	}
}

func writeIPC(t *testing.T, conn net.Conn, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
}

func readIPC(t *testing.T, reader *bufio.Reader, v any) {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(line, v); err != nil {
		t.Fatal(err)
	}
}

func waitForSocket(t *testing.T, s *Service, serveErr <-chan error) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-serveErr:
			t.Fatalf("edge Serve exited before socket came up: %v", err)
		default:
		}
		conn, err := net.Dial("unix", s.SocketPath())
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("edge IPC socket never came up")
}

// shortDataDir keeps unix socket paths under the macOS 104-byte limit, which
// t.TempDir() (keyed on the full test name) can exceed.
func shortDataDir(t *testing.T, tag string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ag"+tag)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
