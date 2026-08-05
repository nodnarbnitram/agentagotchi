package presence

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
)

func testCore() (*Core, *time.Time) {
	now := &time.Time{}
	*now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New(Config{Now: func() time.Time { return *now }}, 1)
	return c, now
}

func mustAttach(t *testing.T, c *Core, adapter string, caps []contract.Capability) Lease {
	t.Helper()
	lease, err := c.Attach(adapter, caps)
	if err != nil {
		t.Fatalf("Attach(%s): %v", adapter, err)
	}
	return lease
}

func upsert(c *Core, lease Lease, seq uint64, nativeID string, state State, reason Reason) error {
	_, err := c.ApplyReports(lease.ID, seq, []Report{{
		NativeSessionID: nativeID,
		SafeTitle:       lease.Adapter,
		State:           state,
		Reason:          reason,
	}}, nil)
	return err
}

func TestTaskPresenceIDsAreOpaqueAndUnique(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	if err := upsert(c, lease, 2, "native-b", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot("edge-1", "edge")
	if len(snap.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(snap.Tasks))
	}
	ids := map[string]bool{}
	for _, task := range snap.Tasks {
		if ids[task.TaskPresenceID] {
			t.Errorf("duplicate Task Presence ID %q", task.TaskPresenceID)
		}
		ids[task.TaskPresenceID] = true
		if task.TaskPresenceID == "native-a" || task.TaskPresenceID == "native-b" {
			t.Errorf("Task Presence ID leaks native session ID: %q", task.TaskPresenceID)
		}
	}
}

func TestIdenticalNativeIDsAcrossAdaptersAreIsolated(t *testing.T) {
	c, _ := testCore()
	codex := mustAttach(t, c, "codex", nil)
	pi := mustAttach(t, c, "pi", nil)
	if err := upsert(c, codex, 1, "same-native-id", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	if err := upsert(c, pi, 1, "same-native-id", StateNeedsInput, ReasonQuestion); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot("edge-1", "edge")
	if len(snap.Tasks) != 2 {
		t.Fatalf("identical native IDs across adapters collided: got %d tasks, want 2", len(snap.Tasks))
	}
	ids := map[string]bool{}
	for _, task := range snap.Tasks {
		if ids[task.TaskPresenceID] {
			t.Errorf("UUID isolation failed: duplicate %q", task.TaskPresenceID)
		}
		ids[task.TaskPresenceID] = true
	}
}

func TestStaleProducerSequenceRejected(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 5, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	if err := upsert(c, lease, 5, "native-a", StateBlocked, ReasonFailed); err == nil {
		t.Fatal("replayed producer sequence accepted")
	}
	if err := upsert(c, lease, 3, "native-a", StateBlocked, ReasonFailed); err == nil {
		t.Fatal("regressed producer sequence accepted")
	}
	// State is untouched by rejected reports.
	snap := c.Snapshot("edge-1", "edge")
	if snap.Tasks[0].State != StateRunning {
		t.Errorf("stale report mutated state to %q", snap.Tasks[0].State)
	}
}

func TestAbsoluteReportUpsertAndEnd(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	// Upsert same native session: same Task Presence ID, updated state.
	if err := upsert(c, lease, 2, "native-a", StateReady, ReasonCompleted); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot("edge-1", "edge")
	if len(snap.Tasks) != 1 || snap.Tasks[0].State != StateReady {
		t.Fatalf("upsert failed: %+v", snap.Tasks)
	}
	// Absolute end: session no longer exists.
	if _, err := c.ApplyReports(lease.ID, 3, nil, []string{"native-a"}); err != nil {
		t.Fatal(err)
	}
	if c.Len() != 0 {
		t.Fatalf("end did not remove presence, len=%d", c.Len())
	}
}

func TestLeaseExpiryEndsPresencesWithoutFabricatingTerminal(t *testing.T) {
	c, now := testCore()
	lease := mustAttach(t, c, "pi", nil)
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(31 * time.Second)
	if !c.ExpireLeases() {
		t.Fatal("ExpireLeases reported no change after deadline passed")
	}
	if c.Len() != 0 {
		t.Fatalf("lease expiry left %d presences", c.Len())
	}
	// The record is gone entirely — never converted to ready/blocked.
	snap := c.Snapshot("edge-1", "edge")
	for _, task := range snap.Tasks {
		if task.State == StateReady || task.State == StateBlocked {
			t.Errorf("lease expiry fabricated terminal state %q", task.State)
		}
	}
}

func TestDetachEndsOnlyOwnPresences(t *testing.T) {
	c, _ := testCore()
	codex := mustAttach(t, c, "codex", nil)
	pi := mustAttach(t, c, "pi", nil)
	if err := upsert(c, codex, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	if err := upsert(c, pi, 1, "native-b", StateNeedsInput, ReasonApproval); err != nil {
		t.Fatal(err)
	}
	if !c.Detach(codex.ID) {
		t.Fatal("Detach returned false for live lease")
	}
	if c.Len() != 1 {
		t.Fatalf("Detach removed wrong presences, len=%d", c.Len())
	}
	snap := c.Snapshot("edge-1", "edge")
	if snap.Tasks[0].State != StateNeedsInput {
		t.Errorf("surviving presence state = %q, want needs_input", snap.Tasks[0].State)
	}
	// Independent adapter failure: Pi's lease still valid.
	if err := upsert(c, pi, 2, "native-b", StateRunning, ReasonWorking); err != nil {
		t.Errorf("surviving adapter rejected after peer detach: %v", err)
	}
}

func TestTerminalRetentionTTL(t *testing.T) {
	c, now := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateReady, ReasonCompleted); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(6 * 24 * time.Hour)
	if c.EnforceRetention() {
		t.Fatal("retention evicted before TTL")
	}
	*now = now.Add(25 * time.Hour) // 7d+1h total
	if !c.EnforceRetention() {
		t.Fatal("retention did not evict after TTL")
	}
	if c.Len() != 0 {
		t.Fatalf("TTL expiry left %d presences", c.Len())
	}
}

func TestTerminalRetentionFIFOKeepsBound(t *testing.T) {
	c, now := testCore()
	lease := mustAttach(t, c, "codex", nil)
	cfgFIFO := 5
	c.cfg.TerminalFIFO = cfgFIFO
	for i := 0; i < cfgFIFO+3; i++ {
		if err := upsert(c, lease, uint64(i+1), fmt.Sprintf("native-%d", i), StateReady, ReasonCompleted); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Minute) // distinct UpdatedAt for FIFO order
	}
	if !c.EnforceRetention() {
		t.Fatal("FIFO bound not enforced")
	}
	if got := c.Len(); got != cfgFIFO {
		t.Fatalf("FIFO kept %d, want %d", got, cfgFIFO)
	}
	// Running tasks are never eviction candidates.
	if err := upsert(c, lease, 100, "native-live", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	c.EnforceRetention()
	snap := c.Snapshot("edge-1", "edge")
	found := false
	for _, task := range snap.Tasks {
		if task.State == StateRunning {
			found = true
		}
	}
	if !found {
		t.Error("retention evicted a non-terminal presence")
	}
}

func TestAcknowledgeRemovesTerminal(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateReady, ReasonCompleted); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot("edge-1", "edge")
	id := snap.Tasks[0].TaskPresenceID
	if err := c.Acknowledge(id); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if c.Len() != 0 {
		t.Fatal("acknowledged terminal presence still in feed")
	}
}

func TestAcknowledgeRejectsNonTerminal(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	id := c.Snapshot("edge-1", "edge").Tasks[0].TaskPresenceID
	if err := c.Acknowledge(id); err != ErrNotTerminal {
		t.Fatalf("Acknowledge(running) = %v, want ErrNotTerminal", err)
	}
}

func TestSnoozeExcludesFromFeaturedUntilChange(t *testing.T) {
	c, now := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateNeedsInput, ReasonPermission); err != nil {
		t.Fatal(err)
	}
	id := c.Snapshot("edge-1", "edge").Tasks[0].TaskPresenceID
	if err := c.Snooze(id); err != nil {
		t.Fatalf("Snooze: %v", err)
	}
	if _, ok := c.Featured(); ok {
		t.Fatal("snoozed presence still claims Featured")
	}
	// Snoozed stays in the feed.
	snap := c.Snapshot("edge-1", "edge")
	if len(snap.Tasks) != 1 || !snap.Tasks[0].Snoozed {
		t.Fatalf("snoozed presence missing from feed: %+v", snap.Tasks)
	}
	// Unrelated updates (subagent count) do not release the snooze.
	*now = now.Add(time.Minute)
	if _, err := c.ApplyReports(lease.ID, 2, []Report{{
		NativeSessionID: "native-a", SafeTitle: "codex",
		State: StateNeedsInput, Reason: ReasonPermission, SubagentCount: 2,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Featured(); ok {
		t.Fatal("snooze released on unrelated field change")
	}
	// State/reason change releases it.
	if err := upsert(c, lease, 3, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	if feat, ok := c.Featured(); !ok || feat.TaskPresenceID != id {
		t.Fatal("snooze did not release on state change")
	}
}

func TestSnoozeRejectsNonInputGated(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	id := c.Snapshot("edge-1", "edge").Tasks[0].TaskPresenceID
	if err := c.Snooze(id); err != ErrNotInputGated {
		t.Fatalf("Snooze(running) = %v, want ErrNotInputGated", err)
	}
}

func TestFeaturedPriorityOrder(t *testing.T) {
	c, now := testCore()
	lease := mustAttach(t, c, "codex", nil)
	states := []struct {
		native string
		state  State
		reason Reason
	}{
		{"idle-task", StateIdle, ReasonWorking},
		{"running-task", StateRunning, ReasonWorking},
		{"ready-task", StateReady, ReasonCompleted},
		{"blocked-task", StateBlocked, ReasonFailed},
		{"input-task", StateNeedsInput, ReasonApproval},
	}
	for i, s := range states {
		if err := upsert(c, lease, uint64(i+1), s.native, s.state, s.reason); err != nil {
			t.Fatal(err)
		}
		*now = now.Add(time.Second)
	}
	feat, ok := c.Featured()
	if !ok || feat.State != StateNeedsInput {
		t.Fatalf("Featured = %q, want needs_input", feat.State)
	}
	// Remove the needs_input; blocked claims it next.
	if err := c.Acknowledge(feat.TaskPresenceID); err == nil {
		t.Fatal("acknowledge succeeded on non-terminal needs_input")
	}
	if err := upsert(c, lease, 10, "input-task", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	feat, _ = c.Featured()
	if feat.State != StateBlocked {
		t.Fatalf("Featured = %q after input resolved, want blocked", feat.State)
	}
}

func TestCapabilityResolutionFailClosed(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", []contract.Capability{contract.CapabilityFocus})
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	id := c.Snapshot("edge-1", "edge").Tasks[0].TaskPresenceID
	// Not yet advertised on the task.
	if _, _, ok := c.ResolveCapability(id, contract.CapabilityFocus); ok {
		t.Fatal("resolved capability that was never advertised on the task")
	}
	if err := c.SetCapabilities(id, []contract.Capability{contract.CapabilityFocus}); err != nil {
		t.Fatal(err)
	}
	adapter, native, ok := c.ResolveCapability(id, contract.CapabilityFocus)
	if !ok || adapter != "codex" || native != "native-a" {
		t.Errorf("ResolveCapability = (%q, %q, %v)", adapter, native, ok)
	}
	// Unknown capability never resolves.
	if _, _, ok := c.ResolveCapability(id, "approve"); ok {
		t.Error("resolved unregistered capability")
	}
	if err := c.SetCapabilities(id, []contract.Capability{"approve"}); err == nil {
		t.Error("SetCapabilities accepted unregistered capability")
	}
}

func TestAttachRejectsUnknownCapabilities(t *testing.T) {
	c, _ := testCore()
	if _, err := c.Attach("codex", []contract.Capability{"approve"}); err == nil {
		t.Fatal("Attach accepted unregistered capability")
	}
}

func TestSnapshotContainsOnlyAllowlistedFields(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", []contract.Capability{contract.CapabilityFocus})
	_, err := c.ApplyReports(lease.ID, 1, []Report{{
		NativeSessionID: "native-secret-019fa063",
		DisplayKey:      "/Users/alice/secret/project",
		SafeTitle:       "Codex",
		State:           StateRunning, Reason: ReasonWorking,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetCapabilities(c.Snapshot("e", "edge").Tasks[0].TaskPresenceID,
		[]contract.Capability{contract.CapabilityFocus}); err != nil {
		t.Fatal(err)
	}
	snap := c.Snapshot("edge-1", "edge")
	data, err := contract.Encode(contract.SchemaFeedV1, snap)
	if err != nil {
		t.Fatalf("snapshot failed contract encode: %v", err)
	}
	for _, marker := range []string{"native-secret", "/Users/alice", "DisplayKey", "nativeSession"} {
		if got := string(data); contains(got, marker) {
			t.Errorf("feed snapshot leaks %q: %s", marker, got)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestRevisionBumpsMonotonically(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	_, r0 := c.Revision()
	if err := upsert(c, lease, 1, "native-a", StateRunning, ReasonWorking); err != nil {
		t.Fatal(err)
	}
	g1, r1 := c.Revision()
	if r1 <= r0 {
		t.Errorf("revision did not bump: %d -> %d", r0, r1)
	}
	if g1 != 1 {
		t.Errorf("generation = %d, want 1", g1)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c, _ := testCore()
	lease := mustAttach(t, c, "codex", nil)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = upsert(c, lease, uint64(w*1000+i), fmt.Sprintf("n-%d", w), StateRunning, ReasonWorking)
				_ = c.Snapshot("e", "edge")
				_, _ = c.Featured()
			}
		}(w)
	}
	wg.Wait()
}
