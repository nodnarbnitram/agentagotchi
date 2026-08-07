package claude

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSanitizeEmitsStrictIPCHookAndDropsSensitiveContent(t *testing.T) {
	input := `{
		"session_id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"cwd":"/Users/example/secret-project",
		"hook_event_name":"UserPromptSubmit",
		"prompt":"my password is hunter2",
		"transcript_path":"/private/transcript.jsonl",
		"last_assistant_message":"private answer",
		"message":"Claude needs your permission",
		"tool_input":{"command":"rm something"},
		"tool_result":{"stdout":"secret output"}
	}`
	ev, err := Sanitize(strings.NewReader(input), time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Schema != "agentagotchi.ipc.v1" || ev.Type != "hook_event" || ev.Harness != "claude" {
		t.Fatalf("wrong IPC envelope: %+v", ev)
	}
	if ev.Workspace != "secret-project" {
		t.Fatalf("workspace = %q", ev.Workspace)
	}
	b, _ := json.Marshal(ev)
	for _, secret := range []string{"hunter2", "transcript", "private answer", "rm something", "secret output", "needs your permission", "/Users/"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("sanitized frame leaked %q: %s", secret, b)
		}
	}
}

func TestSanitizeCapturesAgentIDForSubagents(t *testing.T) {
	input := `{
		"session_id":"abc",
		"hook_event_name":"SubagentStart",
		"agent_id":"agent-def456",
		"agent_type":"Explore"
	}`
	ev, err := Sanitize(strings.NewReader(input), time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if ev.AgentID != "agent-def456" {
		t.Fatalf("agent id = %q", ev.AgentID)
	}
}

func TestSanitizeRequiresSessionAndEvent(t *testing.T) {
	if _, err := Sanitize(strings.NewReader(`{"hook_event_name":"Stop"}`), time.Now()); err == nil {
		t.Fatal("missing session_id accepted")
	}
	if _, err := Sanitize(strings.NewReader(`{"session_id":"abc"}`), time.Now()); err == nil {
		t.Fatal("missing hook_event_name accepted")
	}
}

func TestQuestionTool(t *testing.T) {
	if !IsQuestionTool("ExitPlanMode") {
		t.Fatal("ExitPlanMode not recognized")
	}
	if IsQuestionTool("Bash") {
		t.Fatal("Bash wrongly recognized as a question tool")
	}
}
