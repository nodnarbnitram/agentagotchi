package edge

import (
	"context"
	"errors"
	"sync"
	"testing"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

func routerFixture(t *testing.T) (*presence.Core, *Router, string, *int) {
	t.Helper()
	core := presence.New(presence.Config{}, 1)
	lease, err := core.Attach("codex", []contract.Capability{contract.CapabilityFocus})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: "019fa063-b4d1-7d81-bced-7f9f55ec7611",
		SafeTitle:       "Codex", State: presence.StateRunning, Reason: presence.ReasonWorking,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	id := core.Snapshot("edge", "edge").Tasks[0].TaskPresenceID
	if err := core.SetCapabilities(id, []contract.Capability{contract.CapabilityFocus}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	router := NewRouter(core)
	router.Register("codex", contract.CapabilityFocus, func(_ context.Context, native string, _ contract.FeedAction) error {
		calls++
		if native != "019fa063-b4d1-7d81-bced-7f9f55ec7611" {
			return errors.New("wrong native session")
		}
		return nil
	})
	return core, router, id, &calls
}

func actionFor(core *presence.Core, id, actionID string, capability contract.Capability) contract.FeedAction {
	_, revision := core.Revision()
	return contract.FeedAction{
		Schema: contract.SchemaFeedV1, Type: "action", ActionID: actionID,
		Capability: capability, TaskPresenceID: id, SeenRevision: revision,
	}
}

func TestRouterDispatchesExactCapabilityAndDeduplicates(t *testing.T) {
	core, router, id, calls := routerFixture(t)
	action := actionFor(core, id, "action-1", contract.CapabilityFocus)
	if got := router.Dispatch(context.Background(), action).Status; got != "ok" {
		t.Fatalf("status = %q", got)
	}
	if got := router.Dispatch(context.Background(), action).Status; got != "ok" {
		t.Fatalf("deduplicated status = %q", got)
	}
	if *calls != 1 {
		t.Fatalf("handler calls = %d, want 1", *calls)
	}
}

func TestRouterConcurrentDuplicateExecutesAtMostOnce(t *testing.T) {
	core, router, id, _ := routerFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	var mu sync.Mutex
	router.Register("codex", contract.CapabilityFocus, func(_ context.Context, _ string, _ contract.FeedAction) error {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release
		return nil
	})
	action := actionFor(core, id, "concurrent", contract.CapabilityFocus)
	done := make(chan struct{})
	go func() {
		_ = router.Dispatch(context.Background(), action)
		close(done)
	}()
	<-started
	if got := router.Dispatch(context.Background(), action).Status; got != "failed" {
		t.Fatalf("in-flight duplicate status = %q, want failed", got)
	}
	close(release)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("concurrent duplicate handler calls = %d", calls)
	}
}

func TestRouterFailsClosed(t *testing.T) {
	core, router, id, calls := routerFixture(t)
	stale := actionFor(core, id, "stale", contract.CapabilityFocus)
	stale.SeenRevision--
	if got := router.Dispatch(context.Background(), stale).Status; got != "stale" {
		t.Fatalf("stale revision status = %q", got)
	}
	unknown := actionFor(core, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "unknown", contract.CapabilityFocus)
	if got := router.Dispatch(context.Background(), unknown).Status; got != "stale" {
		t.Fatalf("unknown task status = %q", got)
	}
	unsupported := actionFor(core, id, "unsupported", "approve")
	if got := router.Dispatch(context.Background(), unsupported).Status; got != "unsupported" {
		t.Fatalf("unknown capability status = %q", got)
	}
	if *calls != 0 {
		t.Fatalf("fail-closed actions invoked handler %d times", *calls)
	}
}
