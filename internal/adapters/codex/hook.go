// Package codex translates Codex-private lifecycle and metadata into the
// harness-neutral presence vocabulary before it reaches the semantic core.
package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
)

const maxHookBytes = 1 << 20

type rawHook struct {
	SessionID     string          `json:"session_id"`
	TurnID        string          `json:"turn_id"`
	CWD           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolUseID     string          `json:"tool_use_id"`
	AgentID       string          `json:"agent_id"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Prompt        string          `json:"prompt"`
	Transcript    string          `json:"transcript_path"`
	LastMessage   string          `json:"last_assistant_message"`
}

// Sanitize consumes raw Codex hook JSON and returns the only hook shape that
// may cross owner-only adapter IPC. Sensitive source fields are deliberately
// absent from the result. CWD is reduced to a basename for Edge-local use.
func Sanitize(r io.Reader, now time.Time) (contract.IPCHookEvent, error) {
	dec := json.NewDecoder(io.LimitReader(r, maxHookBytes))
	var raw rawHook
	if err := dec.Decode(&raw); err != nil {
		return contract.IPCHookEvent{}, fmt.Errorf("decode hook input: %w", err)
	}
	sessionID := strings.TrimSpace(raw.SessionID)
	eventName := strings.TrimSpace(raw.HookEventName)
	if sessionID == "" || eventName == "" {
		return contract.IPCHookEvent{}, fmt.Errorf("hook input is missing session_id or hook_event_name")
	}

	workspace := ""
	if raw.CWD != "" {
		workspace = filepath.Base(filepath.Clean(raw.CWD))
		if workspace == "." || workspace == string(filepath.Separator) {
			workspace = ""
		}
	}
	event := contract.IPCHookEvent{
		Schema:          contract.SchemaIPCV1,
		Type:            "hook_event",
		Harness:         "codex",
		NativeSessionID: sessionID,
		TurnID:          strings.TrimSpace(raw.TurnID),
		Event:           eventName,
		ToolName:        strings.TrimSpace(raw.ToolName),
		AgentID:         strings.TrimSpace(raw.AgentID),
		Workspace:       workspace,
		At:              now.UTC(),
	}

	// Hash only non-sensitive identifiers. Prompts, transcript paths, tool
	// inputs, and assistant text never participate and become unreachable.
	sum := sha256.Sum256([]byte(strings.Join([]string{
		event.NativeSessionID, event.TurnID, event.Event, event.ToolName,
		strings.TrimSpace(raw.ToolUseID), event.AgentID,
	}, "\x00")))
	event.EventID = hex.EncodeToString(sum[:])
	return event, nil
}

func IsQuestionTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "request_user_input" ||
		strings.HasSuffix(n, "__request_user_input") ||
		strings.HasSuffix(n, ".request_user_input")
}
