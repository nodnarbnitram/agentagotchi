// Package presence is the Agentagotchi semantic core: the harness-neutral
// domain model of Task Presences. It is deliberately free of harness
// lifecycle, transport, persistence, and display policy — the Edge service
// embeds it and owns those concerns.
//
// The core owns:
//   - opaque Task Presence ID assignment and the private
//     {adapter, native session ID, capabilities} mapping (docs/adr/0005)
//   - absolute-report upsert/end (no event log)
//   - ordering: origin generation/revision and producer sequence rejection
//   - adapter leases (expiry ends presences without fabricating completion)
//   - terminal retention: monotonic TTL + per-Edge FIFO bound (docs/adr/0003)
//   - global, episode-scoped dismissal: acknowledge and snooze (docs/adr/0002)
//   - the capability registry
//   - projection of the privacy-safe feed snapshot (internal/contract)
package presence

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
)

// State and Reason reuse the generic vocabulary pinned by internal/contract.
type (
	State  = string
	Reason = string
)

const (
	StateIdle       State = "idle"
	StateRunning    State = "running"
	StateNeedsInput State = "needs_input"
	StateReady      State = "ready"
	StateBlocked    State = "blocked"
)

const (
	ReasonWorking    Reason = "working"
	ReasonQuestion   Reason = "question"
	ReasonApproval   Reason = "approval"
	ReasonPermission Reason = "permission"
	ReasonCompleted  Reason = "completed"
	ReasonFailed     Reason = "failed"
)

// Rank orders states for Featured Task priority:
// needs_input > blocked > ready > running > idle.
func Rank(s State) int {
	switch s {
	case StateNeedsInput:
		return 4
	case StateBlocked:
		return 3
	case StateReady:
		return 2
	case StateRunning:
		return 1
	default:
		return 0
	}
}

// IsTerminal reports whether the presence is a Terminal Task Presence: work
// finished, remaining only as a notification.
func IsTerminal(s State) bool { return s == StateReady || s == StateBlocked }

// Config tunes the semantic core. Zero values use defaults.
type Config struct {
	// TerminalTTL bounds how long Terminal Task Presences survive without
	// acknowledgement, measured in monotonic time. Default 7 days.
	TerminalTTL time.Duration
	// TerminalFIFO bounds retained Terminal Task Presences per Edge; oldest
	// evicted first. Default 200.
	TerminalFIFO int
	// LeaseDuration bounds adapter report leases. Default 30s.
	LeaseDuration time.Duration
	// Now supplies monotonic-time-aware timestamps; injectable for tests.
	Now func() time.Time
}

func (c Config) withDefaults() Config {
	if c.TerminalTTL <= 0 {
		c.TerminalTTL = 7 * 24 * time.Hour
	}
	if c.TerminalFIFO <= 0 {
		c.TerminalFIFO = 200
	}
	if c.LeaseDuration <= 0 {
		c.LeaseDuration = 30 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// privateRecord is the Edge-private side of a Task Presence. It is never
// serialized into any wire message or shared persistence (docs/adr/0005).
type privateRecord struct {
	adapter       string
	nativeSession string
	displayKey    string
	leaseID       string
	producerID    string
}

// TaskPresence is the domain record. Only its allowlisted projection ever
// leaves the Edge.
type TaskPresence struct {
	ID            string
	SafeTitle     string
	State         State
	Reason        Reason
	SubagentCount int
	Capabilities  []contract.Capability
	UpdatedAt     time.Time

	// Dismissal state (global, episode-scoped; docs/adr/0002).
	Snoozed      bool
	snoozeState  State
	snoozeReason Reason

	private privateRecord
}

// Report is one absolute presence report for a native session (upsert).
type Report struct {
	NativeSessionID string
	DisplayKey      string
	SafeTitle       string
	State           State
	Reason          Reason
	SubagentCount   int
}

// Lease identifies an adapter session granted by Attach.
type Lease struct {
	ID        string
	Adapter   string
	ExpiresAt time.Time

	persistent      bool
	lastProducerSeq uint64
}

var (
	ErrStaleReport   = errors.New("presence: stale or replayed report")
	ErrUnknownTask   = errors.New("presence: unknown Task Presence ID")
	ErrNotTerminal   = errors.New("presence: acknowledge requires a terminal state")
	ErrNotInputGated = errors.New("presence: snooze requires needs_input")
	ErrCapability    = errors.New("presence: capability not advertised")
)

// Core is the semantic core. The Edge service owns the single instance.
type Core struct {
	cfg Config

	mu         sync.Mutex
	generation uint64
	revision   uint64
	tasks      map[string]*TaskPresence // by Task Presence ID
	byNative   map[string]string        // adapter+"\x00"+nativeSession -> Task Presence ID
	leases     map[string]*Lease
	alias      map[string]string // Task Presence ID -> user-approved alias
}

// New creates a semantic core. generation starts at 1 and increments on
// Restart; the Edge passes a persisted value across restarts.
func New(cfg Config, generation uint64) *Core {
	if generation == 0 {
		generation = 1
	}
	return &Core{
		cfg:        cfg.withDefaults(),
		generation: generation,
		tasks:      make(map[string]*TaskPresence),
		byNative:   make(map[string]string),
		leases:     make(map[string]*Lease),
		alias:      make(map[string]string),
	}
}

func nativeKey(adapter, nativeSession string) string {
	return adapter + "\x00" + nativeSession
}

// NewTaskPresenceID returns a fresh RFC 4122 version 4 UUID.
func NewTaskPresenceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("presence: Task Presence ID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// Attach registers an adapter session and returns its lease. capabilities
// are the Device Capabilities this adapter may be asked to perform; the
// registry is fail-closed — unknown capabilities are rejected.
func (c *Core) Attach(adapter string, capabilities []contract.Capability) (Lease, error) {
	return c.attach(adapter, capabilities, false)
}

// AttachLocal registers an Edge-owned ingest source that has no remote
// connection or lease lifetime. This is used for one-shot hook receivers: the
// Edge owns the reducer and explicitly ends sessions, so expiring them based
// on an individual hook connection would be incorrect.
func (c *Core) AttachLocal(adapter string, capabilities []contract.Capability) (Lease, error) {
	return c.attach(adapter, capabilities, true)
}

func (c *Core) attach(adapter string, capabilities []contract.Capability, persistent bool) (Lease, error) {
	for _, cap := range capabilities {
		if cap != contract.CapabilityFocus {
			return Lease{}, fmt.Errorf("%w: %q", ErrCapability, cap)
		}
	}
	id, err := NewTaskPresenceID()
	if err != nil {
		return Lease{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	lease := &Lease{
		ID:         id,
		Adapter:    adapter,
		ExpiresAt:  c.cfg.Now().Add(c.cfg.LeaseDuration),
		persistent: persistent,
	}
	c.leases[id] = lease
	return *lease, nil
}

// Renew extends an adapter lease.
func (c *Core) Renew(leaseID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, ok := c.leases[leaseID]
	if !ok {
		return false
	}
	if !lease.persistent {
		lease.ExpiresAt = c.cfg.Now().Add(c.cfg.LeaseDuration)
	}
	return true
}

// ApplyReports applies an absolute batch of reports from one leased adapter:
// each Report upserts one Task Presence; ends terminates native sessions that
// no longer exist. producerSeq must strictly increase per lease; stale or
// replayed batches are rejected whole.
func (c *Core) ApplyReports(leaseID string, producerSeq uint64, reports []Report, ends []string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lease, ok := c.leases[leaseID]
	if !ok {
		return false, ErrStaleReport
	}
	if producerSeq == 0 || (lease.lastProducerSeq != 0 && producerSeq <= lease.lastProducerSeq) {
		return false, ErrStaleReport
	}
	lease.lastProducerSeq = producerSeq
	changed := false
	now := c.cfg.Now()
	for _, r := range reports {
		if r.NativeSessionID == "" || !contract.ValidState(r.State) || !contract.ValidReason(r.Reason) {
			continue // malformed entries are dropped, not fatal
		}
		key := nativeKey(lease.Adapter, r.NativeSessionID)
		tp := c.tasks[c.byNative[key]]
		if tp == nil {
			id, err := NewTaskPresenceID()
			if err != nil {
				return changed, err
			}
			tp = &TaskPresence{ID: id}
			tp.private.adapter = lease.Adapter
			tp.private.nativeSession = r.NativeSessionID
			tp.private.leaseID = leaseID
			tp.private.producerID = leaseID
			c.tasks[id] = tp
			c.byNative[key] = id
			changed = true
		}
		if tp.apply(r, now, c.alias[tp.ID]) {
			changed = true
		}
	}
	for _, nativeID := range ends {
		key := nativeKey(lease.Adapter, nativeID)
		if id, ok := c.byNative[key]; ok {
			c.endLocked(id)
			changed = true
		}
	}
	if changed {
		c.bumpLocked()
	}
	return changed, nil
}

// apply folds one absolute report into the presence. Snooze holds only while
// state and reason are unchanged (episode-scoped, docs/adr/0002).
func (t *TaskPresence) apply(r Report, now time.Time, alias string) bool {
	changed := false
	title := contract.SanitizeSafeTitle(r.SafeTitle)
	if alias != "" {
		title = contract.SanitizeSafeTitle(alias)
	}
	if title != "" && title != t.SafeTitle {
		t.SafeTitle = title
		changed = true
	}
	if t.State != r.State || t.Reason != r.Reason {
		t.State = r.State
		t.Reason = r.Reason
		changed = true
	}
	if t.SubagentCount != r.SubagentCount {
		t.SubagentCount = r.SubagentCount
		changed = true
	}
	// Snooze release: state or reason changed since snoozed.
	if t.Snoozed && (t.snoozeState != t.State || t.snoozeReason != t.Reason) {
		t.Snoozed = false
		changed = true
	}
	if r.DisplayKey != "" && r.DisplayKey != t.private.displayKey {
		t.private.displayKey = r.DisplayKey
	}
	if changed {
		t.UpdatedAt = now.UTC()
	}
	return changed
}

// Detach ends an adapter session: all Task Presences owned by its lease are
// removed without fabricating completion or failure.
func (c *Core) Detach(leaseID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.leases[leaseID]; !ok {
		return false
	}
	delete(c.leases, leaseID)
	changed := false
	for id, tp := range c.tasks {
		if tp.private.leaseID == leaseID {
			c.endLocked(id)
			changed = true
		}
	}
	if changed {
		c.bumpLocked()
	}
	return changed
}

// ExpireLeases ends every lease whose deadline has passed (monotonic).
func (c *Core) ExpireLeases() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	changed := false
	for id, lease := range c.leases {
		if !lease.persistent && now.After(lease.ExpiresAt) {
			delete(c.leases, id)
			for taskID, tp := range c.tasks {
				if tp.private.leaseID == id {
					c.endLocked(taskID)
					changed = true
				}
			}
		}
	}
	if changed {
		c.bumpLocked()
	}
	return changed
}

// Acknowledge dismisses a Terminal Task Presence: the owning Edge removes it
// from every Presence Feed. It resurfaces only if the Harness Session later
// reaches a new terminal state (a fresh report creates a new episode).
func (c *Core) Acknowledge(taskPresenceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tp := c.tasks[taskPresenceID]
	if tp == nil {
		return ErrUnknownTask
	}
	if !IsTerminal(tp.State) {
		return ErrNotTerminal
	}
	c.endLocked(taskPresenceID)
	c.bumpLocked()
	return nil
}

// Snooze sets aside a needs_input Task Presence: it stays in every Presence
// Feed but stops claiming the Featured Task until its state or reason next
// changes.
func (c *Core) Snooze(taskPresenceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tp := c.tasks[taskPresenceID]
	if tp == nil {
		return ErrUnknownTask
	}
	if tp.State != StateNeedsInput {
		return ErrNotInputGated
	}
	if !tp.Snoozed {
		tp.Snoozed = true
		tp.snoozeState = tp.State
		tp.snoozeReason = tp.Reason
		tp.UpdatedAt = c.cfg.Now().UTC()
		c.bumpLocked()
	}
	return nil
}

// SetAlias records a user-approved Edge alias for a Task Presence. Aliases
// are the only user-added Safe Title content.
func (c *Core) SetAlias(taskPresenceID, alias string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.tasks[taskPresenceID]; !ok {
		return ErrUnknownTask
	}
	clean := contract.SanitizeSafeTitle(alias)
	if clean == "" {
		delete(c.alias, taskPresenceID)
	} else {
		c.alias[taskPresenceID] = clean
		c.tasks[taskPresenceID].SafeTitle = clean
	}
	c.bumpLocked()
	return nil
}

// ResolveCapability returns the private routing record for a Task Presence
// if — and only if — it advertises the capability. Fail-closed dispatch uses
// this and nothing else.
func (c *Core) ResolveCapability(taskPresenceID string, capability contract.Capability) (adapter string, nativeSession string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tp := c.tasks[taskPresenceID]
	if tp == nil {
		return "", "", false
	}
	for _, cap := range tp.Capabilities {
		if cap == capability {
			return tp.private.adapter, tp.private.nativeSession, true
		}
	}
	return "", "", false
}

// HasTask reports whether a public Task Presence ID is currently owned by
// this Core. Routers use it to distinguish an unknown task from an
// unadvertised capability without exposing private routing data.
func (c *Core) HasTask(taskPresenceID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.tasks[taskPresenceID]
	return ok
}

// TaskPresenceIDFor returns the public ID for an Edge-private adapter/native
// pair. It is intended only for the Edge's capability-registration seam.
func (c *Core) TaskPresenceIDFor(adapter, nativeSession string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := c.byNative[nativeKey(adapter, nativeSession)]
	return id, ok
}

// RestoreAliases loads the privacy-safe alias map persisted by the Edge.
// Entries need not currently have a live private mapping; a restart rebuilds
// those mappings from fresh adapter reports.
func (c *Core) RestoreAliases(aliases map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, alias := range aliases {
		if clean := contract.SanitizeSafeTitle(alias); clean != "" {
			c.alias[id] = clean
		}
	}
}

// Aliases returns a copy suitable for Edge-local owner-only persistence.
func (c *Core) Aliases() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.alias))
	for id, alias := range c.alias {
		out[id] = alias
	}
	return out
}

// SetCapabilities replaces a Task Presence's advertised capabilities
// (adapter-reported, validated at Attach/ApplyReports boundaries).
func (c *Core) SetCapabilities(taskPresenceID string, capabilities []contract.Capability) error {
	for _, cap := range capabilities {
		if cap != contract.CapabilityFocus {
			return fmt.Errorf("%w: %q", ErrCapability, cap)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	tp := c.tasks[taskPresenceID]
	if tp == nil {
		return ErrUnknownTask
	}
	if capabilitiesEqual(tp.Capabilities, capabilities) {
		return nil
	}
	tp.Capabilities = append([]contract.Capability(nil), capabilities...)
	c.bumpLocked()
	return nil
}

func capabilitiesEqual(a, b []contract.Capability) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// EnforceRetention evicts Terminal Task Presences past TTL (monotonic) and
// trims the terminal set to the FIFO bound, oldest evicted first.
// Acknowledged tasks never reach this path — Acknowledge removes immediately.
func (c *Core) EnforceRetention() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.cfg.Now()
	changed := false
	var terminal []*TaskPresence
	for _, tp := range c.tasks {
		if IsTerminal(tp.State) {
			terminal = append(terminal, tp)
		}
	}
	for _, tp := range terminal {
		if now.Sub(tp.UpdatedAt) > c.cfg.TerminalTTL {
			c.endLocked(tp.ID)
			changed = true
		}
	}
	if changed {
		terminal = terminal[:0]
		for _, tp := range c.tasks {
			if IsTerminal(tp.State) {
				terminal = append(terminal, tp)
			}
		}
	}
	if len(terminal) > c.cfg.TerminalFIFO {
		sort.SliceStable(terminal, func(i, j int) bool {
			return terminal[i].UpdatedAt.Before(terminal[j].UpdatedAt)
		})
		for _, tp := range terminal[:len(terminal)-c.cfg.TerminalFIFO] {
			c.endLocked(tp.ID)
			changed = true
		}
	}
	if changed {
		c.bumpLocked()
	}
	return changed
}

// endLocked removes a presence and its private mappings.
func (c *Core) endLocked(id string) {
	tp := c.tasks[id]
	if tp == nil {
		return
	}
	delete(c.byNative, nativeKey(tp.private.adapter, tp.private.nativeSession))
	delete(c.alias, id)
	delete(c.tasks, id)
}

func (c *Core) bumpLocked() { c.revision++ }

// Revision returns the current origin ordering metadata.
func (c *Core) Revision() (generation, revision uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation, c.revision
}

// Snapshot projects the privacy-safe feed snapshot. Only allowlisted fields
// are copied; the private record is structurally unreachable here.
func (c *Core) Snapshot(originID, originKind string) contract.FeedSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	tasks := make([]contract.FeedTask, 0, len(c.tasks))
	counts := contract.Counts{}
	for _, tp := range c.tasks {
		switch tp.State {
		case StateNeedsInput:
			counts.NeedsInput++
		case StateBlocked:
			counts.Blocked++
		case StateReady:
			counts.Ready++
		case StateRunning:
			counts.Running++
		}
		tasks = append(tasks, contract.FeedTask{
			TaskPresenceID: tp.ID,
			SafeTitle:      tp.SafeTitle,
			State:          tp.State,
			Reason:         tp.Reason,
			SubagentCount:  tp.SubagentCount,
			Capabilities:   append([]contract.Capability(nil), tp.Capabilities...),
			UpdatedAt:      tp.UpdatedAt,
			Snoozed:        tp.Snoozed,
		})
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		// Snoozed presences never claim the top; then priority; then recency.
		if tasks[i].Snoozed != tasks[j].Snoozed {
			return !tasks[i].Snoozed
		}
		ri, rj := Rank(tasks[i].State), Rank(tasks[j].State)
		if ri != rj {
			return ri > rj
		}
		return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt)
	})
	aggregate := StateIdle
	if len(tasks) > 0 {
		aggregate = tasks[0].State
	}
	return contract.FeedSnapshot{
		Schema: contract.SchemaFeedV1,
		Type:   "snapshot",
		Origin: contract.Origin{
			Kind:       originKind,
			ID:         originID,
			Generation: c.generation,
			Revision:   c.revision,
		},
		GeneratedAt:    c.cfg.Now().UTC(),
		AggregateState: aggregate,
		Counts:         counts,
		Tasks:          tasks,
	}
}

// Featured returns the Task Presence that currently claims the Featured Task
// slot: highest priority, never snoozed, most recent first.
func (c *Core) Featured() (contract.FeedTask, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var best *TaskPresence
	for _, tp := range c.tasks {
		if tp.Snoozed {
			continue
		}
		if best == nil ||
			Rank(tp.State) > Rank(best.State) ||
			(Rank(tp.State) == Rank(best.State) && tp.UpdatedAt.After(best.UpdatedAt)) {
			best = tp
		}
	}
	if best == nil {
		return contract.FeedTask{}, false
	}
	return contract.FeedTask{
		TaskPresenceID: best.ID,
		SafeTitle:      best.SafeTitle,
		State:          best.State,
		Reason:         best.Reason,
		SubagentCount:  best.SubagentCount,
		Capabilities:   append([]contract.Capability(nil), best.Capabilities...),
		UpdatedAt:      best.UpdatedAt,
	}, true
}

// Len returns the number of live Task Presences.
func (c *Core) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tasks)
}
