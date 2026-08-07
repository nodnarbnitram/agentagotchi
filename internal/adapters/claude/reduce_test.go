package claude

import (
	"testing"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

func hook(name string) contract.IPCHookEvent {
	return contract.IPCHookEvent{Event: name, NativeSessionID: "session", EventID: name}
}

func TestReduceLifecycleMapping(t *testing.T) {
	tests := []struct {
		name       string
		event      contract.IPCHookEvent
		wantState  presence.State
		wantReason presence.Reason
	}{
		{"prompt", hook("UserPromptSubmit"), presence.StateRunning, presence.ReasonWorking},
		{"permission", hook("PermissionRequest"), presence.StateNeedsInput, presence.ReasonPermission},
		{"stop", hook("Stop"), presence.StateReady, presence.ReasonCompleted},
	}
	question := hook("PreToolUse")
	question.ToolName = "ExitPlanMode"
	tests = append(tests, struct {
		name       string
		event      contract.IPCHookEvent
		wantState  presence.State
		wantReason presence.Reason
	}{"question", question, presence.StateNeedsInput, presence.ReasonQuestion})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reduce(Reduction{}, tt.event)
			if got.State != tt.wantState || got.Reason != tt.wantReason {
				t.Fatalf("Reduce = %s/%s, want %s/%s", got.State, got.Reason, tt.wantState, tt.wantReason)
			}
		})
	}
}

func TestReduceFailureSurfaces(t *testing.T) {
	running := Reduce(Reduction{}, hook("UserPromptSubmit"))
	if got := Reduce(running, hook("StopFailure")); got.State != presence.StateBlocked || got.Reason != presence.ReasonFailed {
		t.Fatalf("StopFailure = %+v, want blocked/failed", got)
	}
	if got := Reduce(running, hook("PostToolUseFailure")); got.State != presence.StateBlocked || got.Reason != presence.ReasonFailed {
		t.Fatalf("PostToolUseFailure = %+v, want blocked/failed", got)
	}
}

func TestReducePostToolReleasesInputGate(t *testing.T) {
	state := Reduce(Reduction{}, hook("PermissionRequest"))
	state = Reduce(state, hook("PostToolUse"))
	if state.State != presence.StateRunning || state.Reason != presence.ReasonWorking {
		t.Fatalf("PostToolUse did not release input gate: %+v", state)
	}
}

func TestReduceSubagentSet(t *testing.T) {
	state := Reduce(Reduction{}, hook("UserPromptSubmit"))
	start := hook("SubagentStart")
	start.AgentID = "agent-1"
	state = Reduce(state, start)
	state = Reduce(state, start) // duplicate id must not double count
	if state.SubagentCount != 1 {
		t.Fatalf("duplicate subagent count = %d", state.SubagentCount)
	}
	stop := hook("SubagentStop")
	stop.AgentID = "agent-1"
	state = Reduce(state, stop)
	if state.SubagentCount != 0 {
		t.Fatalf("stopped subagent count = %d", state.SubagentCount)
	}
}

func TestReduceNotificationDoesNotOverrideQuestion(t *testing.T) {
	question := hook("PreToolUse")
	question.ToolName = "ExitPlanMode"
	state := Reduce(Reduction{}, question)
	state = Reduce(state, hook("Notification"))
	if state.Reason != presence.ReasonQuestion {
		t.Fatalf("Notification overrode question reason: %+v", state)
	}
	// From a non-input state, Notification raises needs_input/permission.
	state = Reduce(Reduction{}, hook("UserPromptSubmit"))
	state = Reduce(state, hook("Notification"))
	if state.State != presence.StateNeedsInput || state.Reason != presence.ReasonPermission {
		t.Fatalf("Notification from running = %+v", state)
	}
}

func TestSessionEndEndsPresence(t *testing.T) {
	if got := Reduce(Reduction{}, hook("SessionEnd")); !got.End {
		t.Fatal("SessionEnd did not produce an end")
	}
}

func TestReducerDeduplicatesByEventID(t *testing.T) {
	r := NewReducer()
	ev := hook("UserPromptSubmit")
	if _, _, applied := r.Apply(ev); !applied {
		t.Fatal("first event not applied")
	}
	if _, _, applied := r.Apply(ev); applied {
		t.Fatal("duplicate event id applied")
	}
}

func TestReducerEmitsSafeTitleAndNoSubagentLeak(t *testing.T) {
	r := NewReducer()
	report, _, _ := r.Apply(hook("UserPromptSubmit"))
	if report.SafeTitle != "Claude" {
		t.Fatalf("safe title = %q", report.SafeTitle)
	}
	if report.NativeSessionID != "session" {
		t.Fatalf("native session id = %q", report.NativeSessionID)
	}
}
