# Spec: Semantic Core

## Purpose

The semantic core is the Edge-owned, harness-agnostic presence model. It holds
absolute Task Presence reports, assigns opaque public identity, enforces
ordering and liveness, and applies terminal retention and global dismissal —
without any harness lifecycle, transport, persistence, or display policy.

## ADDED Requirements

### Requirement: Harness-agnostic state and reason vocabulary

The semantic core SHALL reduce every Task Presence to the shared state set
`idle | running | needs_input | ready | blocked` and the generic reason set
`working | question | approval | permission | completed | failed`. Core state
transitions SHALL NOT switch on any Codex, Pi, Claude, or other
harness-specific lifecycle event name.

#### Scenario: Core state has no harness vocabulary

- **WHEN** a Task Presence transitions between states
- **THEN** the transition is expressed only in the shared state and reason sets
  and contains no harness-specific lifecycle event name

#### Scenario: Terminal vs transient classification

- **WHEN** the semantic core classifies a Task Presence
- **THEN** `ready` and `blocked` are treated as Terminal Task Presences
  (notifications) and `idle`, `running`, and `needs_input` as transient

### Requirement: Edge-owned opaque public identity

The semantic core SHALL assign each Task Presence an opaque canonical UUID —
the Task Presence ID — that is distinct from the private harness-native session
identifier. The core SHALL keep the mapping from Task Presence ID to
`{adapter, native session id, capabilities}` private to the owning Edge and
SHALL NOT expose the native session ID, harness type, machine identity,
hostname, username, or path as an independent status field.

#### Scenario: Public identity is opaque

- **WHEN** a Harness Adapter registers a Harness Session with the Edge
- **THEN** the semantic core assigns a canonical UUID as the Task Presence ID
  and records the private mapping locally without exposing the native session
  ID upstream

#### Scenario: Native-looking IDs from different adapters do not collide

- **WHEN** two Harness Adapters report Harness Sessions whose native session
  identifiers are identical
- **THEN** the semantic core assigns each a distinct Task Presence ID so the
  public identity of one never collides with the other

### Requirement: Absolute reports only

The semantic core SHALL store only absolute statements of current Task Presence
state. It SHALL NOT retain an event log or replay raw harness lifecycle events.

#### Scenario: Absolute statement replaces prior state

- **WHEN** the semantic core accepts an absolute report for a Harness Session
- **THEN** it replaces the stored state for that Task Presence with the
  reported state, reason, safe title, subagent count, capabilities, and
  observed timestamp

### Requirement: Privacy-safe payload allowlist

The semantic core SHALL persist and expose only an allowlisted presence
payload: the opaque Task Presence ID, an optional bounded Safe Title, generic
state and reason, allowlisted capabilities, subagent count, ordering metadata,
and timestamps. Prompts, commands, tool payloads, transcripts, full paths,
credentials, private keys, Wi-Fi secrets, and harness-native session IDs SHALL
be structurally unreachable in the stored and exposed presence model.

#### Scenario: Sensitive fields are structurally excluded

- **WHEN** an absolute report contains a field outside the presence allowlist
- **THEN** that field is unreachable in the stored Task Presence and cannot be
  emitted on any presence or status path

#### Scenario: Safe Title never determines routing

- **WHEN** the semantic core routes or resolves a Task Presence
- **THEN** the routing decision uses the Task Presence ID and the private
  registry, never the Safe Title text

### Requirement: Monotonic ordering and stale rejection

The semantic core SHALL order updates by an Edge generation and a monotonic
producer sequence per adapter session, and SHALL reject stale generations and
out-of-order producer sequences. It SHALL use monotonic time for local lease
and liveness decisions and SHALL NOT compare machine wall clocks to order
updates.

#### Scenario: Stale generation is ignored

- **WHEN** an absolute report arrives with a generation older than the current
  accepted generation for that adapter session
- **THEN** the semantic core ignores the report and does not change stored
  state

#### Scenario: Out-of-order producer sequence is ignored

- **WHEN** an absolute report arrives with a producer sequence lower than the
  last accepted sequence for its generation
- **THEN** the semantic core ignores the report

#### Scenario: Reconnect absolute snapshot repairs gaps

- **WHEN** a leased adapter reconnects and resends its complete absolute
  current state with a new generation
- **THEN** the semantic core converges to that complete state, repairing any
  updates dropped while disconnected

### Requirement: Lease-based transient expiry

When an owner lease expires, the semantic core SHALL remove transient
(`running`, `needs_input`) Task Presences after a short grace period. It SHALL
NOT convert a disconnect into `completed` or `failed`, and SHALL expose bridge
and adapter liveness separately rather than as a pet task state.

#### Scenario: Transient presence expires on lease loss

- **WHEN** an owner lease expires and the grace period elapses
- **THEN** the semantic core removes that owner's transient `running` and
  `needs_input` Task Presences

#### Scenario: Disconnect never fabricates a terminal state

- **WHEN** an adapter or Edge disconnects
- **THEN** no Task Presence is rewritten to `completed` or `failed` as a result
  of the disconnect

### Requirement: Terminal retention policy

The semantic core SHALL retain a Terminal Task Presence until it is
acknowledged, bounded by a 7-day TTL measured in monotonic time (configurable
per Edge) and a per-Edge FIFO bound of approximately 200 terminal presences
with the oldest evicted first. Expiry or eviction SHALL remove the presence
exactly like acknowledgement — it SHALL NOT rewrite it to `completed` or
`failed` — and SHALL be visible in administration diagnostics.

#### Scenario: Terminal presence retained until acknowledged

- **WHEN** a Task Presence enters `ready` or `blocked` and is not acknowledged
- **THEN** the semantic core retains it across owner disconnects

#### Scenario: TTL expiry removes without rewriting

- **WHEN** a Terminal Task Presence exceeds the configured monotonic TTL
- **THEN** the semantic core removes it exactly as acknowledgement does and
  records the expiry in administration diagnostics, without rewriting it to
  `completed` or `failed`

#### Scenario: FIFO bound evicts oldest

- **WHEN** the number of Terminal Task Presences exceeds the FIFO bound
- **THEN** the semantic core evicts the oldest first and records the eviction
  in administration diagnostics

### Requirement: Global episode-scoped dismissal

The semantic core SHALL record acknowledgement and snooze once, globally, at
the owning Edge. Acknowledging a Terminal Task Presence SHALL remove it from
every Presence Feed. Snoozing an input-gated (`needs_input`) Task Presence
SHALL keep it in every feed but stop it from claiming the Featured Task until
its state or reason next changes. Neither SHALL mutate the Harness Session.

#### Scenario: Acknowledge removes a terminal presence globally

- **WHEN** a Terminal Task Presence is acknowledged
- **THEN** the semantic core removes it from every Presence Feed and it
  resurfaces only if the Harness Session later reaches a new terminal state

#### Scenario: Snooze suppresses automatic Featuring

- **WHEN** an input-gated Task Presence is snoozed
- **THEN** it remains in every feed and task list but is skipped by automatic
  Featuring until its state or reason next changes

#### Scenario: Dismissal resets on state or reason change

- **WHEN** a snoozed or acknowledged Task Presence's state or reason changes
- **THEN** the dismissal is reset so a genuinely new completion or approval
  request surfaces again

#### Scenario: Dismissal never mutates the harness

- **WHEN** a Task Presence is acknowledged or snoozed
- **THEN** no signal is dispatched to the owning Harness Session
