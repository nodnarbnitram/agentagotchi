package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSanitizeEmitsStrictIPCHookAndDropsSensitiveContent(t *testing.T) {
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
	if ev.Schema != "agentagotchi.ipc.v1" || ev.Type != "hook_event" || ev.Harness != "codex" {
		t.Fatalf("wrong IPC envelope: %+v", ev)
	}
	if ev.Workspace != "secret-project" {
		t.Fatalf("workspace = %q", ev.Workspace)
	}
	b, _ := json.Marshal(ev)
	for _, secret := range []string{"hunter2", "transcript", "private answer", "rm something", "/Users/"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("sanitized frame leaked %q: %s", secret, b)
		}
	}
}

func TestQuestionToolAliases(t *testing.T) {
	for _, name := range []string{"request_user_input", "functions.request_user_input", "mcp__codex__request_user_input"} {
		if !IsQuestionTool(name) {
			t.Fatalf("%q was not recognized", name)
		}
	}
}
