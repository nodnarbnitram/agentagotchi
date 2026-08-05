# Spec: Device Actions

## Purpose

Capability-gated, route-validated, fail-closed, deduplicated, never-queued
device actions that travel back along a valid presence route (Focus first),
plus the Edge-global acknowledge and snooze dismissal controls that never
dispatch to a Harness Adapter capability.

## ADDED Requirements

### Requirement: Actions travel back along a valid presence route

A device action SHALL travel back along a valid presence route: BOX-3→direct
Edge→Harness Adapter, or BOX-3→Home→origin Edge→Harness Adapter. The action
SHALL resolve the task only within the receiving feed/paired origin.

#### Scenario: Direct route

- **WHEN** a device invokes an action on a directly-fed Task Presence
- **THEN** the action travels BOX-3→direct Edge→Harness Adapter

#### Scenario: Relayed route

- **WHEN** a device invokes an action on a Home-relayed Task Presence
- **THEN** the action travels BOX-3→Home→origin Edge→Harness Adapter

### Requirement: Fail-closed validation

For every action, the receiving service SHALL validate the schema, action type,
opaque action ID, Task Presence UUID, and the device's observed presence
revision; resolve the task only within the receiving feed/paired origin; and
confirm the Task Presence advertises the requested capability. Unknown task,
stale observation, unavailable capability, wrong origin, and offline Edge SHALL
all fail closed.

#### Scenario: Unknown task fails closed

- **WHEN** an action targets an unknown Task Presence UUID
- **THEN** it fails closed

#### Scenario: Stale observation fails closed

- **WHEN** an action's observed presence revision is stale
- **THEN** it fails closed

#### Scenario: Unavailable capability fails closed

- **WHEN** the Task Presence does not advertise the requested capability
- **THEN** the action fails closed

#### Scenario: Offline origin fails unavailable

- **WHEN** the origin Edge or adapter is offline
- **THEN** the action fails unavailable without changing task state

### Requirement: Dispatch only through the owning capability

The origin Edge SHALL dispatch an action only through the owning registered
Harness Adapter capability and return success only after exact harness
capability success. It SHALL NOT silently fall back to another app, task,
machine, or harness.

#### Scenario: Exact dispatch and success

- **WHEN** an action reaches the origin Edge
- **THEN** it dispatches only through the owning adapter's registered
  capability and returns success only after exact harness capability success

#### Scenario: No fallback

- **WHEN** the exact capability cannot be dispatched
- **THEN** the Edge does not fall back to another app, task, machine, or
  harness

### Requirement: Deduplication and no queueing

The origin Edge SHALL deduplicate retries by action ID so a duplicate action ID
executes at most once. No hop SHALL queue an action for later delivery;
acknowledgement SHALL occur only after exact success.

#### Scenario: Duplicate action ID executes at most once

- **WHEN** the same action ID is retried
- **THEN** the origin Edge executes it at most once

#### Scenario: No action is queued

- **WHEN** an action cannot dispatch immediately
- **THEN** it fails unavailable and is never stored for later delivery

### Requirement: Global acknowledgement after exact success

After an exact successful action, the origin Edge SHALL record the
acknowledgement and publish a new presence revision so all direct feeds, Home,
web UI, and devices converge. An exact successful Device Capability action on a
terminal target also acknowledges it.

#### Scenario: Acknowledgement converges globally

- **WHEN** an action succeeds exactly
- **THEN** the origin Edge records the acknowledgement and publishes a new
  presence revision so every surface converges

#### Scenario: Successful action on a terminal target acknowledges it

- **WHEN** a Focus Action succeeds on a terminal Task Presence
- **THEN** that Task Presence is also acknowledged

### Requirement: Focus is the first capability

Focus SHALL be the first Device Capability. A Task Presence SHALL advertise
only generic, allowlisted actions currently available. Capability names SHALL
be a narrowly scoped control-plane field exposing no harness, machine, prompt,
or command detail, and SHALL NOT be inferred from task state. Status-only
remote sessions SHALL be valid.

#### Scenario: Only advertised allowlisted capabilities

- **WHEN** a Task Presence advertises capabilities
- **THEN** they are generic, allowlisted, currently available actions beginning
  with `focus`

#### Scenario: Capabilities not inferred from state

- **WHEN** a Task Presence changes state
- **THEN** no action is inferred from that state; only explicit capabilities
  are advertised

#### Scenario: Status-only sessions are valid

- **WHEN** a remote session advertises no capabilities
- **THEN** it remains a valid browsable Task Presence

### Requirement: Edge-global dismissal controls

Acknowledge and snooze SHALL be Edge-local control messages, always available
for any Task Presence, deduplicated by ID like actions, and SHALL NOT dispatch
to a Harness Adapter capability or mutate the Harness Session. Acknowledge
SHALL remove a terminal presence from every feed; snooze SHALL keep an
input-gated presence in every feed but stop it from claiming the Featured Task
until its state or reason next changes.

#### Scenario: Acknowledge is Edge-global and non-dispatching

- **WHEN** a terminal Task Presence is acknowledged
- **THEN** the owning Edge removes it from every Presence Feed and does not
  dispatch to any Harness Adapter capability

#### Scenario: Snooze suppresses Featuring without mutation

- **WHEN** an input-gated Task Presence is snoozed
- **THEN** it stays in every feed and task list but stops claiming the Featured
  Task until its state or reason next changes, and the harness is not mutated

#### Scenario: Dismissal is deduplicated

- **WHEN** an acknowledge or snooze message is retried with the same ID
- **THEN** it is deduplicated like an action

#### Scenario: No time-based snooze resurface

- **WHEN** a Task Presence is snoozed
- **THEN** there is no time-based resurface; it stays snoozed until its state
  or reason changes
