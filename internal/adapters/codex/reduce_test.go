package codex

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
	question.ToolName = "functions.request_user_input"
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

func TestReducePostToolAndSubagentSet(t *testing.T) {
	state := Reduce(Reduction{}, hook("PermissionRequest"))
	state = Reduce(state, hook("PostToolUse"))
	if state.State != presence.StateRunning || state.Reason != presence.ReasonWorking {
		t.Fatalf("PostToolUse did not release input gate: %+v", state)
	}
	start := hook("SubagentStart")
	start.AgentID = "agent-1"
	state = Reduce(state, start)
	state = Reduce(state, start)
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

func TestRuntimeStatusMappingDoesNotOverrideInputWithIdle(t *testing.T) {
	running := Reduce(Reduction{}, hook("UserPromptSubmit"))
	completed := Enrich(running, "private user title", "idle", false)
	if completed.State != presence.StateReady || completed.Reason != presence.ReasonCompleted {
		t.Fatalf("idle mapping = %+v", completed)
	}
	approval := Enrich(running, "", "active:waitingOnApproval", false)
	if approval.State != presence.StateNeedsInput || approval.Reason != presence.ReasonApproval {
		t.Fatalf("approval mapping = %+v", approval)
	}
	blocked := Enrich(running, "", "systemError", true)
	if blocked.State != presence.StateBlocked || blocked.Reason != presence.ReasonFailed {
		t.Fatalf("failure mapping = %+v", blocked)
	}
	question := Reduce(Reduction{}, contract.IPCHookEvent{Event: "PermissionRequest"})
	if got := Enrich(question, "", "idle", false); got.State != presence.StateNeedsInput {
		t.Fatalf("idle overrode input gate: %+v", got)
	}
}

func TestSessionEndEndsPresence(t *testing.T) {
	if got := Reduce(Reduction{}, hook("SessionEnd")); !got.End {
		t.Fatal("SessionEnd did not produce an end")
	}
}
