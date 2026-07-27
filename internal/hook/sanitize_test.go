package hook

import (
	"strings"
	"testing"
	"time"
)

func TestSanitizeDropsSensitiveContent(t *testing.T) {
	input := `{
		"session_id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"turn_id":"turn-1",
		"cwd":"/Users/example/secret-project",
		"hook_event_name":"UserPromptSubmit",
		"prompt":"my password is hunter2",
		"transcript_path":"/private/transcript.jsonl",
		"last_assistant_message":"private answer",
		"tool_input":{"command":"rm something"}
	}`
	ev, err := Sanitize(strings.NewReader(input), time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Workspace != "secret-project" {
		t.Fatalf("workspace = %q", ev.Workspace)
	}
	encoded := strings.ToLower(strings.Join([]string{
		ev.EventID, ev.SessionID, ev.TurnID, ev.Event, ev.ToolName,
		ev.ToolUseID, ev.AgentID, ev.Workspace,
	}, " "))
	for _, secret := range []string{"hunter2", "transcript", "private answer", "rm something", "/users/"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("sanitized event leaked %q", secret)
		}
	}
}

func TestQuestionToolAliases(t *testing.T) {
	for _, name := range []string{
		"request_user_input",
		"functions.request_user_input",
		"mcp__codex__request_user_input",
	} {
		if !IsQuestionTool(name) {
			t.Fatalf("%q was not recognized", name)
		}
	}
}
