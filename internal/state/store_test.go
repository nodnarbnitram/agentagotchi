package state

import (
	"testing"
	"time"

	"agentagotchi.local/agentagotchi/internal/model"
)

func event(id, name string, at int64) model.HookEvent {
	return model.HookEvent{
		EventID: id + name, SessionID: id, Event: name, At: time.Unix(at, 0).UTC(),
	}
}

func TestPriorityAndTransitions(t *testing.T) {
	s, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	run := event("run", "UserPromptSubmit", 1)
	ready := event("ready", "Stop", 2)
	blocked := event("blocked", "Stop", 3)
	question := event("question", "PreToolUse", 4)
	question.ToolName = "request_user_input"
	for _, ev := range []model.HookEvent{run, ready, blocked, question} {
		if !s.Apply(ev) {
			t.Fatalf("event %s was not applied", ev.EventID)
		}
	}
	if !s.Enrich("blocked", "Broken build", "systemError", true, time.Unix(5, 0)) {
		t.Fatal("blocked state was not enriched")
	}
	got := s.Snapshot()
	if len(got.Tasks) != 4 {
		t.Fatalf("tasks = %d", len(got.Tasks))
	}
	want := []model.State{
		model.StateNeedsInput, model.StateBlocked, model.StateReady, model.StateRunning,
	}
	for i := range want {
		if got.Tasks[i].State != want[i] {
			t.Fatalf("task[%d] = %s, want %s", i, got.Tasks[i].State, want[i])
		}
	}
}

func TestDeduplication(t *testing.T) {
	s, _ := New("")
	ev := event("one", "UserPromptSubmit", 1)
	if !s.Apply(ev) || s.Apply(ev) {
		t.Fatal("event deduplication failed")
	}
}

func TestAcknowledgeHidesReady(t *testing.T) {
	s, _ := New("")
	s.Apply(event("one", "Stop", 1))
	if !s.Acknowledge("one") {
		t.Fatal("ack failed")
	}
	if got := len(s.Snapshot().Tasks); got != 0 {
		t.Fatalf("visible tasks = %d", got)
	}
}

func TestIdleMetadataCompletesRunningTaskWithoutOverridingInput(t *testing.T) {
	s, _ := New("")
	s.Apply(event("running", "UserPromptSubmit", 1))
	if !s.Enrich("running", "Safe title", "idle", false, time.Unix(2, 0)) {
		t.Fatal("idle metadata did not complete running task")
	}
	got := s.Snapshot()
	if len(got.Tasks) != 1 ||
		got.Tasks[0].State != model.StateReady ||
		got.Tasks[0].Reason != model.ReasonCompleted {
		t.Fatalf("unexpected completed task: %+v", got.Tasks)
	}

	question := event("question", "PreToolUse", 3)
	question.ToolName = "request_user_input"
	s.Apply(question)
	if s.Enrich("question", "", "idle", false, time.Unix(4, 0)) {
		t.Fatal("idle metadata overrode a needs-input task")
	}
	got = s.Snapshot()
	if got.Tasks[0].ID != "question" || got.Tasks[0].State != model.StateNeedsInput {
		t.Fatalf("needs-input task was not preserved: %+v", got.Tasks)
	}
}
