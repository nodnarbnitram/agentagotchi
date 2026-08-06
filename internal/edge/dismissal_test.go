package edge

import (
	"net/http/httptest"
	"testing"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

// TestDismissalActionsOverFeed proves acknowledge/snooze are Edge-global
// controls: state-gated, fail-closed, revision-converging, never queued.
func TestDismissalActionsOverFeed(t *testing.T) {
	s := newTestService(t, 0)
	lease, err := s.core.Attach("codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.core.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: "native-terminal", SafeTitle: "Codex",
		State: presence.StateReady, Reason: presence.ReasonCompleted,
	}, {
		NativeSessionID: "native-input", SafeTitle: "Codex",
		State: presence.StateNeedsInput, Reason: presence.ReasonPermission,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	snap := s.core.Snapshot(s.edgeID, "edge")
	var terminalID, inputID string
	for _, task := range snap.Tasks {
		switch task.State {
		case "ready":
			terminalID = task.TaskPresenceID
		case "needs_input":
			inputID = task.TaskPresenceID
		}
	}

	server := httptest.NewTLSServer(s.Handler())
	defer server.Close()
	conn, reader := dialTestWebSocket(t, server.URL, "/feed/v1", "test-token")
	defer conn.Close()
	var feedSnap contract.FeedSnapshot
	if err := contract.DecodeStrict(readServerText(t, reader), contract.SchemaFeedV1, &feedSnap); err != nil {
		t.Fatal(err)
	}
	revision := feedSnap.Origin.Revision

	act := func(id, capability, target string, rev uint64) contract.ActionResult {
		t.Helper()
		writeClientJSON(t, conn, contract.FeedAction{
			Schema: contract.SchemaFeedV1, Type: "action", ActionID: id,
			Capability: contract.Capability(capability), TaskPresenceID: target, SeenRevision: rev,
		})
		var result contract.ActionResult
		if err := contract.DecodeStrict(readServerText(t, reader), contract.SchemaFeedV1, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	// Snooze the input-gated task: ok, excluded from Featured, still in feed.
	if r := act("snooze-1", "snooze", inputID, revision); r.Status != "ok" {
		t.Fatalf("snooze = %q", r.Status)
	}
	if _, ok := s.core.Featured(); !ok {
		t.Fatal("expected another featured candidate (terminal task)")
	}
	snap = s.core.Snapshot(s.edgeID, "edge")
	for _, task := range snap.Tasks {
		if task.TaskPresenceID == inputID && !task.Snoozed {
			t.Fatal("snoozed task not marked in feed")
		}
	}

	// Acknowledge the terminal task: ok, removed from the feed.
	_, revision = s.core.Revision()
	if r := act("ack-1", "acknowledge", terminalID, revision); r.Status != "ok" {
		t.Fatalf("acknowledge = %q", r.Status)
	}
	snap = s.core.Snapshot(s.edgeID, "edge")
	for _, task := range snap.Tasks {
		if task.TaskPresenceID == terminalID {
			t.Fatal("acknowledged terminal task still in feed")
		}
	}

	// Fail closed: acknowledge the needs_input task (non-terminal).
	_, revision = s.core.Revision()
	if r := act("ack-2", "acknowledge", inputID, revision); r.Status != "failed" {
		t.Fatalf("acknowledge non-terminal = %q, want failed", r.Status)
	}
	// Fail closed: snooze a task that no longer exists.
	_, revision = s.core.Revision()
	if r := act("snooze-2", "snooze", terminalID, revision); r.Status != "stale" {
		t.Fatalf("snooze unknown = %q, want stale", r.Status)
	}
	// Fail closed: stale revision.
	if r := act("ack-3", "acknowledge", inputID, revision-1); r.Status != "stale" {
		t.Fatalf("stale revision = %q, want stale", r.Status)
	}
}

// TestDismissalNeverAdvertisedAsCapability proves dismissal controls stay out
// of capabilities[] (control-plane only).
func TestDismissalNeverAdvertisedAsCapability(t *testing.T) {
	c := presence.New(presence.Config{}, 1)
	lease, err := c.Attach("codex", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: "native-1", SafeTitle: "Codex",
		State: presence.StateReady, Reason: presence.ReasonCompleted,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SetCapabilities(
		c.Snapshot("e", "edge").Tasks[0].TaskPresenceID,
		[]contract.Capability{contract.CapabilityAcknowledge},
	); err == nil {
		t.Fatal("acknowledge advertised as a task capability")
	}
	snap := c.Snapshot("e", "edge")
	for _, task := range snap.Tasks {
		for _, cap := range task.Capabilities {
			if contract.IsDismissal(cap) {
				t.Fatalf("dismissal control advertised: %v", cap)
			}
		}
	}
}
