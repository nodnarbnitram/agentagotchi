package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"agentagotchi.local/agentagotchi/internal/hook"
	"agentagotchi.local/agentagotchi/internal/model"
)

const maxSeenEvents = 4096

type persisted struct {
	Tasks map[string]*model.Task `json:"tasks"`
	Seq   uint64                 `json:"seq"`
}

type Store struct {
	mu       sync.RWMutex
	path     string
	tasks    map[string]*model.Task
	agents   map[string]map[string]struct{}
	seen     map[string]struct{}
	seenFIFO []string
	seq      uint64
}

func New(path string) (*Store, error) {
	s := &Store{
		path:   path,
		tasks:  make(map[string]*model.Task),
		agents: make(map[string]map[string]struct{}),
		seen:   make(map[string]struct{}),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	if s.path == "" {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if p.Tasks != nil {
		s.tasks = p.Tasks
	}
	s.seq = p.Seq
	return nil
}

func (s *Store) Apply(ev model.HookEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[ev.EventID]; ok {
		return false
	}
	s.remember(ev.EventID)

	task := s.tasks[ev.SessionID]
	if task == nil {
		title := "Codex task"
		if ev.Workspace != "" {
			title += " · " + safeTitle(ev.Workspace)
		}
		task = &model.Task{
			ID: ev.SessionID, Title: title, State: model.StateReady,
			Reason: model.ReasonCompleted, UpdatedAt: ev.At, Acknowledged: true,
		}
		s.tasks[ev.SessionID] = task
	}
	if ev.Workspace != "" && task.Title == "Codex task" {
		task.Title = "Codex task · " + safeTitle(ev.Workspace)
	}

	switch ev.Event {
	case "SessionStart":
		task.Acknowledged = true
	case "UserPromptSubmit":
		task.State = model.StateRunning
		task.Reason = model.ReasonWorking
		task.Acknowledged = false
	case "PreToolUse":
		if hook.IsQuestionTool(ev.ToolName) {
			task.State = model.StateNeedsInput
			task.Reason = model.ReasonQuestion
			task.Acknowledged = false
		}
	case "PermissionRequest":
		task.State = model.StateNeedsInput
		task.Reason = model.ReasonPermission
		task.Acknowledged = false
	case "PostToolUse":
		if task.State == model.StateNeedsInput &&
			(hook.IsQuestionTool(ev.ToolName) ||
				task.Reason == model.ReasonPermission ||
				task.Reason == model.ReasonApproval) {
			task.State = model.StateRunning
			task.Reason = model.ReasonWorking
		}
	case "SubagentStart":
		if s.agents[ev.SessionID] == nil {
			s.agents[ev.SessionID] = make(map[string]struct{})
		}
		if ev.AgentID != "" {
			s.agents[ev.SessionID][ev.AgentID] = struct{}{}
		}
		task.SubagentCount = len(s.agents[ev.SessionID])
		if task.State != model.StateNeedsInput {
			task.State = model.StateRunning
			task.Reason = model.ReasonWorking
		}
		task.Acknowledged = false
	case "SubagentStop":
		if ev.AgentID != "" {
			delete(s.agents[ev.SessionID], ev.AgentID)
		}
		task.SubagentCount = len(s.agents[ev.SessionID])
	case "Stop":
		task.State = model.StateReady
		task.Reason = model.ReasonCompleted
		task.SubagentCount = 0
		task.Acknowledged = false
		delete(s.agents, ev.SessionID)
	case "SessionEnd":
		task.Acknowledged = true
		task.SubagentCount = 0
		delete(s.agents, ev.SessionID)
	}
	task.UpdatedAt = ev.At
	s.seq++
	_ = s.persistLocked()
	return true
}

func (s *Store) Enrich(id, title, runtimeStatus string, failed bool, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[id]
	if task == nil {
		return false
	}
	changed := false
	if clean := safeTitle(title); clean != "" && clean != task.Title {
		task.Title = clean
		changed = true
	}
	if failed || runtimeStatus == "systemError" {
		if task.State != model.StateBlocked || task.Reason != model.ReasonFailed {
			task.State = model.StateBlocked
			task.Reason = model.ReasonFailed
			task.Acknowledged = false
			changed = true
		}
	} else if strings.Contains(runtimeStatus, "waitingOnApproval") {
		if task.State != model.StateNeedsInput {
			task.State = model.StateNeedsInput
			task.Reason = model.ReasonApproval
			task.Acknowledged = false
			changed = true
		}
	} else if runtimeStatus == "idle" && task.State == model.StateRunning {
		task.State = model.StateReady
		task.Reason = model.ReasonCompleted
		task.Acknowledged = false
		changed = true
	}
	if changed {
		task.UpdatedAt = at.UTC()
		s.seq++
		_ = s.persistLocked()
	}
	return changed
}

func (s *Store) Acknowledge(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[id]
	if task == nil {
		return false
	}
	task.Acknowledged = true
	task.UpdatedAt = time.Now().UTC()
	s.seq++
	_ = s.persistLocked()
	return true
}

func (s *Store) IDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	return ids
}

func (s *Store) Snapshot() model.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks := make([]model.Task, 0, len(s.tasks))
	counts := model.Counts{}
	for _, ptr := range s.tasks {
		t := *ptr
		if t.Acknowledged && (t.State == model.StateReady || t.State == model.StateIdle) {
			continue
		}
		switch t.State {
		case model.StateNeedsInput:
			counts.NeedsInput++
		case model.StateBlocked:
			counts.Blocked++
		case model.StateReady:
			counts.Ready++
		case model.StateRunning:
			counts.Running++
		}
		tasks = append(tasks, t)
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		ri, rj := model.Rank(tasks[i].State), model.Rank(tasks[j].State)
		if ri != rj {
			return ri > rj
		}
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	aggregate := model.StateIdle
	if len(tasks) > 0 {
		aggregate = tasks[0].State
	}
	return model.Snapshot{
		Type: "snapshot", Version: 1, Seq: s.seq, GeneratedAt: time.Now().UTC(),
		AggregateState: aggregate, Tasks: tasks, Counts: counts,
	}
}

func (s *Store) remember(id string) {
	s.seen[id] = struct{}{}
	s.seenFIFO = append(s.seenFIFO, id)
	if len(s.seenFIFO) > maxSeenEvents {
		old := s.seenFIFO[0]
		s.seenFIFO = s.seenFIFO[1:]
		delete(s.seen, old)
	}
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(persisted{Tasks: s.tasks, Seq: s.seq}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func safeTitle(v string) string {
	v = strings.Join(strings.Fields(v), " ")
	if len(v) > 64 {
		v = v[:61] + "..."
	}
	return v
}
