# Spec: BOX-3 Firmware

## Purpose

The ESP32-S3-BOX-3 firmware consumes up to four simultaneous Presence Feeds,
merges per-pairing snapshots by Task Presence ID and origin revision, and
renders one main pet plus a scrollable task list with Featured Task behavior,
FIFO state priority, pet-tap Focus, and dismiss/snooze gestures.

## ADDED Requirements

### Requirement: Up to four concurrent feeds

The firmware SHALL target four simultaneous authenticated WSS Presence Feed
connections. It SHALL store more pairings only if inactive connections have
deterministic priority and visible diagnostics. The four-feed limit SHALL be
accepted only after TLS/WSS memory, stability, reconnect behavior, and power
are profiled on physical BOX-3 hardware; compilation SHALL NOT be treated as
hardware acceptance.

#### Scenario: Four simultaneous feeds

- **WHEN** the device is paired with up to four feed publishers
- **THEN** it maintains four simultaneous authenticated WSS feed connections
  and merges their snapshots

#### Scenario: Extra pairings have deterministic priority

- **WHEN** more than four pairings are stored
- **THEN** inactive connections have deterministic priority and visible
  diagnostics

#### Scenario: Hardware profiling gates the limit

- **WHEN** the four-feed limit is declared accepted
- **THEN** TLS/WSS memory, stability, reconnect behavior, and power have been
  measured on physical BOX-3 hardware and recorded in
  `docs/HARDWARE_ACCEPTANCE.md`, not from compilation alone

### Requirement: Per-pairing snapshot merge

The firmware SHALL store the latest snapshot per active pairing, render the
union across pairings, replace only the sending pairing's contribution on a new
snapshot, merge by Task Presence ID, converge duplicate direct/relayed copies
by origin revision, and remove only a disconnected feed's contribution.

#### Scenario: Union of direct and relayed feeds

- **WHEN** the device consumes a direct local Edge feed and a Home-relayed
  remote feed
- **THEN** local and remote tasks appear together

#### Scenario: Snapshot replaces only its pairing

- **WHEN** a feed delivers a new snapshot
- **THEN** only that pairing's contribution is replaced

#### Scenario: Duplicate converges by origin revision

- **WHEN** the same Task Presence ID arrives directly and relayed
- **THEN** the device keeps the copy with the newest origin revision

#### Scenario: Disconnect removes only that feed

- **WHEN** a feed disconnects
- **THEN** only that feed's contribution is removed; tasks supplied by another
  feed remain

### Requirement: Main pet plus scrollable task list

The firmware SHALL keep one main animated pet and present a larger, scrollable,
touch-friendly task list. Each row SHALL include a pet avatar/current state and
the Safe Title.

#### Scenario: Scrollable touch-friendly list

- **WHEN** several Task Presences are present
- **THEN** the device shows a larger, scrollable, touch-friendly task list
  alongside one main pet

#### Scenario: Row content

- **WHEN** a task row renders
- **THEN** it shows a pet avatar/current state and the Safe Title

### Requirement: Featured Task and browsing separation

Tapping a row SHALL make that Task Presence the Featured Task on BOX-3 only.
Browsing SHALL NEVER cause a host-side action. The main title, reason, state,
and action target SHALL all come from the same Featured Task.

#### Scenario: Row tap features locally

- **WHEN** a user taps a task row
- **THEN** that Task Presence becomes the Featured Task on BOX-3 only, with no
  host-side action

#### Scenario: Browsing never acts on the host

- **WHEN** a user scrolls or taps rows
- **THEN** no harness action is invoked

#### Scenario: Featured Task is coherent

- **WHEN** a Task Presence is featured
- **THEN** the main title, reason, state, and action target all come from that
  same Featured Task

### Requirement: FIFO state priority and preemption

The firmware SHALL preserve the state priority `needs_input > blocked > ready >
running > idle`. Within one rank, tasks SHALL order FIFO by the moment they
entered that rank; repeated updates within the same state SHALL NOT change
queue position. A task entering a strictly higher rank SHALL preempt the
Featured Task; equal-rank arrivals SHALL join the back of that rank's queue;
when the Featured Task resolves/leaves its rank the device SHALL feature the
oldest remaining task at the highest rank. Manual row selection SHALL
temporarily override automatic selection. A snoozed Task Presence SHALL keep
its rank and queue position but be skipped by automatic Featuring until its
snooze ends, and SHALL still be featureable by manual selection. The priority
queue SHALL reclaim the main pet when new urgency arrives.

#### Scenario: Higher rank preempts

- **WHEN** a task enters a strictly higher rank than the Featured Task
- **THEN** it preempts the Featured Task

#### Scenario: FIFO within a rank

- **WHEN** two tasks share a rank
- **THEN** they are ordered FIFO by when each entered that rank, and repeated
  same-state updates do not change position

#### Scenario: Resolution features oldest at highest rank

- **WHEN** the Featured Task resolves or leaves its rank
- **THEN** the device features the oldest remaining task at the highest rank

#### Scenario: Manual override is temporary

- **WHEN** a user manually selects a row
- **THEN** it temporarily overrides automatic selection until new urgency
  reclaims the main pet

#### Scenario: Snoozed task skipped by automatic Featuring

- **WHEN** a Task Presence is snoozed
- **THEN** it keeps its rank and queue position but is skipped by automatic
  Featuring until its snooze ends, and can still be featured manually

### Requirement: Pet-tap Focus and dismissal gestures

Tapping the main pet SHALL invoke Focus only when the Featured Task advertises
it; otherwise the device SHALL show/no-op as unavailable without status side
effects. An explicit dismiss gesture SHALL acknowledge a terminal row and
snooze an input-gated row; browsing and row taps alone SHALL NEVER dismiss
anything.

#### Scenario: Pet tap focuses only when advertised

- **WHEN** a user taps the main pet and the Featured Task advertises Focus
- **THEN** the device invokes the Focus Action

#### Scenario: Pet tap unavailable without Focus

- **WHEN** a user taps the main pet and the Featured Task does not advertise
  Focus
- **THEN** the device shows/no-ops as unavailable without status side effects

#### Scenario: Dismiss gesture acknowledges or snoozes

- **WHEN** a user performs the explicit dismiss gesture
- **THEN** a terminal row is acknowledged and an input-gated row is snoozed

#### Scenario: Browsing never dismisses

- **WHEN** a user scrolls or taps rows without the dismiss gesture
- **THEN** nothing is acknowledged or snoozed
