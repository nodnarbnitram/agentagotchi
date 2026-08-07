// Package claude translates Claude Code-private lifecycle and metadata into
// the harness-neutral presence vocabulary before it reaches the semantic
// core.
package claude

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
	CWD           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolUseID     string          `json:"tool_use_id"`
	AgentID       string          `json:"agent_id"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResult    json.RawMessage `json:"tool_result"`
	Prompt        string          `json:"prompt"`
	Message       string          `json:"message"`
	LastMessage   string          `json:"last_assistant_message"`
	Transcript    string          `json:"transcript_path"`
}

// Sanitize consumes raw Claude Code hook JSON and returns the only hook shape
// that may cross owner-only adapter IPC. Sensitive source fields (prompts,
// tool inputs and results, notification messages, transcript paths) are
// deliberately absent from the result. CWD is reduced to a basename for
// Edge-local use.
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
		Harness:         "claude",
		NativeSessionID: sessionID,
		Event:           eventName,
		ToolName:        strings.TrimSpace(raw.ToolName),
		AgentID:         strings.TrimSpace(raw.AgentID),
		Workspace:       workspace,
		At:              now.UTC(),
	}

	// Hash only non-sensitive identifiers. Prompts, transcript paths, tool
	// inputs and results, notification text, and assistant messages never
	// participate and become unreachable. Claude Code supplies no turn ID, so
	// the event ID derives from session, event, tool, tool_use_id, and agent.
	sum := sha256.Sum256([]byte(strings.Join([]string{
		event.NativeSessionID, event.Event, event.ToolName,
		strings.TrimSpace(raw.ToolUseID), event.AgentID,
	}, "\x00")))
	event.EventID = hex.EncodeToString(sum[:])
	return event, nil
}

// IsQuestionTool reports whether a Claude Code tool blocks on user input.
// ExitPlanMode requires the user to accept or reject the plan, and the Agent
// tool spawns work the user observes, so both surface as question-adjacent.
// Only ExitPlanMode truly blocks awaiting an answer, so it is the question.
func IsQuestionTool(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "exitplanmode"
}
