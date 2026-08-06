package codex

import (
	"strings"
	"sync"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

// Reduction is the Codex-private reducer state for one native session. The
// agent set and App Server title never enter the semantic core or a wire
// snapshot. Reduction values are copied by Reduce, making the transition a
// pure function of the current value and one sanitized hook event.
type Reduction struct {
	State           presence.State
	Reason          presence.Reason
	SubagentCount   int
	End             bool
	PrivateTitle    string
	activeSubagents map[string]struct{}
}

func initialReduction() Reduction {
	return Reduction{State: presence.StateReady, Reason: presence.ReasonCompleted}
}

func cloneReduction(current Reduction) Reduction {
	next := current
	next.activeSubagents = make(map[string]struct{}, len(current.activeSubagents))
	for id := range current.activeSubagents {
		next.activeSubagents[id] = struct{}{}
	}
	return next
}

// Reduce maps Codex lifecycle vocabulary to the shared state/reason model.
// No caller outside this package needs to understand a Codex event name.
func Reduce(current Reduction, event contract.IPCHookEvent) Reduction {
	if current.State == "" {
		current = initialReduction()
	}
	next := cloneReduction(current)
	next.End = false
	switch event.Event {
	case "SessionStart":
		// Preserve the prototype's neutral initial value until work starts.
	case "UserPromptSubmit":
		next.State, next.Reason = presence.StateRunning, presence.ReasonWorking
	case "PreToolUse":
		if IsQuestionTool(event.ToolName) {
			next.State, next.Reason = presence.StateNeedsInput, presence.ReasonQuestion
		}
	case "PermissionRequest":
		next.State, next.Reason = presence.StateNeedsInput, presence.ReasonPermission
	case "PostToolUse":
		if next.State == presence.StateNeedsInput &&
			(IsQuestionTool(event.ToolName) || next.Reason == presence.ReasonPermission || next.Reason == presence.ReasonApproval) {
			next.State, next.Reason = presence.StateRunning, presence.ReasonWorking
		}
	case "SubagentStart":
		if event.AgentID != "" {
			next.activeSubagents[event.AgentID] = struct{}{}
		}
		next.SubagentCount = len(next.activeSubagents)
		if next.State != presence.StateNeedsInput {
			next.State, next.Reason = presence.StateRunning, presence.ReasonWorking
		}
	case "SubagentStop":
		delete(next.activeSubagents, event.AgentID)
		next.SubagentCount = len(next.activeSubagents)
	case "Stop":
		next.State, next.Reason = presence.StateReady, presence.ReasonCompleted
		next.SubagentCount = 0
		next.activeSubagents = make(map[string]struct{})
	case "SessionEnd":
		next.End = true
		next.SubagentCount = 0
		next.activeSubagents = make(map[string]struct{})
	}
	return next
}

// Enrich applies metadata-only App Server status. Private titles are retained
// solely to keep enrichment Codex-local; they are never used as Safe Titles.
func Enrich(current Reduction, title, runtimeStatus string, failed bool) Reduction {
	next := cloneReduction(current)
	next.PrivateTitle = strings.Join(strings.Fields(title), " ")
	if failed || runtimeStatus == "systemError" {
		next.State, next.Reason = presence.StateBlocked, presence.ReasonFailed
	} else if strings.Contains(runtimeStatus, "waitingOnApproval") {
		next.State, next.Reason = presence.StateNeedsInput, presence.ReasonApproval
	} else if runtimeStatus == "idle" && next.State == presence.StateRunning {
		next.State, next.Reason = presence.StateReady, presence.ReasonCompleted
	}
	return next
}

func reductionChanged(a, b Reduction) bool {
	return a.State != b.State || a.Reason != b.Reason ||
		a.SubagentCount != b.SubagentCount || a.End != b.End || a.PrivateTitle != b.PrivateTitle
}

// Reducer owns per-session Codex-private reduction state and hook event
// deduplication. It emits only absolute semantic reports to its caller.
type Reducer struct {
	mu       sync.Mutex
	sessions map[string]Reduction
	seen     map[string]struct{}
	seenFIFO []string
}

const maxSeenEvents = 4096

func NewReducer() *Reducer {
	return &Reducer{sessions: make(map[string]Reduction), seen: make(map[string]struct{})}
}

func (r *Reducer) Apply(event contract.IPCHookEvent) (presence.Report, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, duplicate := r.seen[event.EventID]; duplicate {
		return presence.Report{}, false, false
	}
	r.remember(event.EventID)
	current := r.sessions[event.NativeSessionID]
	next := Reduce(current, event)
	if next.End {
		delete(r.sessions, event.NativeSessionID)
		return presence.Report{NativeSessionID: event.NativeSessionID}, true, true
	}
	r.sessions[event.NativeSessionID] = next
	return reportFor(event.NativeSessionID, event.Workspace, next), false, true
}

func (r *Reducer) Enrich(nativeID, title, runtimeStatus string, failed bool, _ time.Time) (presence.Report, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[nativeID]
	if !ok {
		return presence.Report{}, false
	}
	next := Enrich(current, title, runtimeStatus, failed)
	if !reductionChanged(current, next) {
		return presence.Report{}, false
	}
	r.sessions[nativeID] = next
	return reportFor(nativeID, "", next), true
}

func (r *Reducer) IDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	return ids
}

func reportFor(nativeID, displayKey string, reduced Reduction) presence.Report {
	return presence.Report{
		NativeSessionID: nativeID,
		DisplayKey:      displayKey,
		SafeTitle:       "Codex",
		State:           reduced.State,
		Reason:          reduced.Reason,
		SubagentCount:   reduced.SubagentCount,
	}
}

func (r *Reducer) remember(id string) {
	r.seen[id] = struct{}{}
	r.seenFIFO = append(r.seenFIFO, id)
	if len(r.seenFIFO) > maxSeenEvents {
		delete(r.seen, r.seenFIFO[0])
		r.seenFIFO = r.seenFIFO[1:]
	}
}
