package appserver

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBinaryForDiagnosticsDoesNotReturnFullPath(t *testing.T) {
	got := BinaryForDiagnostics("/Applications/ChatGPT.app/Contents/Resources/codex")
	if got != "codex" {
		t.Fatalf("got %q", got)
	}
}

func TestThreadReadIsMetadataOnly(t *testing.T) {
	params, err := json.Marshal(threadReadParams{
		ThreadID:     "019fa063-b4d1-7d81-bced-7f9f55ec7611",
		IncludeTurns: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(params), `"includeTurns":false`) {
		t.Fatalf("thread/read params are not metadata-only: %s", params)
	}
}

func TestThreadInfoDoesNotUsePromptPreview(t *testing.T) {
	var envelope threadReadEnvelope
	if err := json.Unmarshal([]byte(`{
		"thread": {
			"id": "019fa063-b4d1-7d81-bced-7f9f55ec7611",
			"name": null,
			"preview": "private first user prompt",
			"status": {"type": "active", "activeFlags": ["waitingOnUserInput"]},
			"turns": [{"status": "failed", "items": [{"content": "private"}]}]
		}
	}`), &envelope); err != nil {
		t.Fatal(err)
	}
	info := threadInfo(envelope)
	if info.Title != "" {
		t.Fatalf("prompt-derived preview escaped as title: %q", info.Title)
	}
	if info.RuntimeStatus != "active:waitingOnUserInput" {
		t.Fatalf("runtime status = %q", info.RuntimeStatus)
	}
	if info.Failed {
		t.Fatal("turn content affected metadata-only result")
	}
}

func TestThreadInfoMapsSystemError(t *testing.T) {
	var envelope threadReadEnvelope
	if err := json.Unmarshal([]byte(`{
		"thread": {
			"id": "019fa063-b4d1-7d81-bced-7f9f55ec7611",
			"name": "Safe explicit title",
			"status": {"type": "systemError"}
		}
	}`), &envelope); err != nil {
		t.Fatal(err)
	}
	info := threadInfo(envelope)
	if info.Title != "Safe explicit title" || !info.Failed {
		t.Fatalf("unexpected metadata: %+v", info)
	}
}
