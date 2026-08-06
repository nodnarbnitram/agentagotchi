package edge

import (
	"context"
	"regexp"
	"sync"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

type CapabilityHandler func(context.Context, string, contract.FeedAction) error

type handlerKey struct {
	adapter    string
	capability contract.Capability
}

var canonicalUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const (
	maxRememberedActions = 4096
	actionPending        = "pending"
)

// Router performs synchronous, fail-closed capability dispatch. It stores no
// work for later delivery: a bounded in-flight reservation/result cache only
// ensures retries with the same action ID execute at most once.
type Router struct {
	core *presence.Core

	mu       sync.Mutex
	handlers map[handlerKey]CapabilityHandler
	results  map[string]string
	resultID []string
}

func NewRouter(core *presence.Core) *Router {
	return &Router{
		core: core, handlers: make(map[handlerKey]CapabilityHandler),
		results: make(map[string]string),
	}
}

func (r *Router) Register(adapter string, capability contract.Capability, handler CapabilityHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if handler == nil {
		delete(r.handlers, handlerKey{adapter, capability})
		return
	}
	r.handlers[handlerKey{adapter, capability}] = handler
}

func (r *Router) Dispatch(ctx context.Context, action contract.FeedAction) contract.ActionResult {
	result := contract.ActionResult{
		Schema:   contract.SchemaFeedV1,
		Type:     "action_result",
		ActionID: action.ActionID,
	}
	if action.ActionID == "" || len(action.ActionID) > 128 ||
		action.Type != "action" || !canonicalUUID.MatchString(action.TaskPresenceID) {
		result.Status = "failed"
		return result
	}

	r.mu.Lock()
	if status, ok := r.results[action.ActionID]; ok {
		r.mu.Unlock()
		if status == actionPending {
			result.Status = "failed"
		} else {
			result.Status = status
		}
		return result
	}
	r.results[action.ActionID] = actionPending
	r.resultID = append(r.resultID, action.ActionID)
	if len(r.resultID) > maxRememberedActions {
		delete(r.results, r.resultID[0])
		r.resultID = r.resultID[1:]
	}
	r.mu.Unlock()

	_, revision := r.core.Revision()
	if action.SeenRevision != revision {
		result.Status = "stale"
		return r.finish(result)
	}
	if !r.core.HasTask(action.TaskPresenceID) {
		result.Status = "stale"
		return r.finish(result)
	}
	if action.Capability != contract.CapabilityFocus {
		result.Status = "unsupported"
		return r.finish(result)
	}
	adapter, nativeID, ok := r.core.ResolveCapability(action.TaskPresenceID, action.Capability)
	if !ok {
		if r.core.HasTask(action.TaskPresenceID) {
			result.Status = "unsupported"
		} else {
			result.Status = "stale"
		}
		return r.finish(result)
	}
	r.mu.Lock()
	handler := r.handlers[handlerKey{adapter, action.Capability}]
	r.mu.Unlock()
	if handler == nil || handler(ctx, nativeID, action) != nil {
		result.Status = "failed"
		return r.finish(result)
	}
	result.Status = "ok"
	return r.finish(result)
}

func (r *Router) finish(result contract.ActionResult) contract.ActionResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.results[result.ActionID]; ok && existing != actionPending {
		result.Status = existing
		return result
	}
	r.results[result.ActionID] = result.Status
	return result
}
