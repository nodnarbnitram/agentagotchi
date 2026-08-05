package hook

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"agentagotchi.local/agentagotchi/internal/model"
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

func Sanitize(r io.Reader, now time.Time) (model.HookEvent, error) {
	dec := json.NewDecoder(io.LimitReader(r, maxHookBytes))
	var raw rawHook
	if err := dec.Decode(&raw); err != nil {
		return model.HookEvent{}, fmt.Errorf("decode hook input: %w", err)
	}
	if strings.TrimSpace(raw.SessionID) == "" || strings.TrimSpace(raw.HookEventName) == "" {
		return model.HookEvent{}, fmt.Errorf("hook input is missing session_id or hook_event_name")
	}

	workspace := ""
	if raw.CWD != "" {
		workspace = filepath.Base(filepath.Clean(raw.CWD))
		if workspace == "." || workspace == string(filepath.Separator) {
			workspace = ""
		}
	}

	event := model.HookEvent{
		SessionID: strings.TrimSpace(raw.SessionID),
		TurnID:    strings.TrimSpace(raw.TurnID),
		Event:     strings.TrimSpace(raw.HookEventName),
		ToolName:  strings.TrimSpace(raw.ToolName),
		ToolUseID: strings.TrimSpace(raw.ToolUseID),
		AgentID:   strings.TrimSpace(raw.AgentID),
		Workspace: workspace,
		At:        now.UTC(),
	}

	// Hash only non-sensitive identifiers. Sensitive fields are intentionally
	// absent from HookEvent and become unreachable when this function returns.
	sum := sha256.Sum256([]byte(strings.Join([]string{
		event.SessionID, event.TurnID, event.Event, event.ToolName,
		event.ToolUseID, event.AgentID,
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
