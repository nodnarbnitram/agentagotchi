# Spec: Presence Feed

## Purpose

The Edge/Home-to-device contract: an authenticated WSS feed carrying bounded,
complete-replacement snapshots of a pairing's Task Presences, with per-pairing
merge semantics on the consuming device and feed-scoped action, acknowledge,
and snooze control messages.

## ADDED Requirements

### Requirement: Role-specific authenticated feed contract

The Edge/Home-to-device Presence Feed SHALL use a role-specific authenticated
WSS contract distinct from the Edge-to-Home upstream ingress contract. Feed
clients SHALL NOT inject Task Presences.

#### Scenario: Feed contract is distinct from upstream

- **WHEN** a device connects to a Presence Feed
- **THEN** it uses a role-specific authenticated WSS contract that is distinct
  from the Edge-to-Home upstream contract

#### Scenario: Feed client cannot inject presence

- **WHEN** a feed client attempts to publish a Task Presence
- **THEN** the feed rejects it; presence flows only from the publisher to the
  device

### Requirement: Complete-replacement snapshots

Every feed connection SHALL begin with and subsequently send complete
replacement snapshots for that pairing's Presence Feed. Snapshots SHALL be
bounded and use fail-closed schema/protocol identification.

#### Scenario: Connection begins with a complete snapshot

- **WHEN** a device connects to a Presence Feed
- **THEN** the publisher sends a complete replacement snapshot for that
  pairing's Presence Feed

#### Scenario: Each update replaces the whole contribution

- **WHEN** the publisher sends a subsequent snapshot
- **THEN** it is a complete replacement of that pairing's contribution, not an
  incremental delta

#### Scenario: Fail-closed schema identification

- **WHEN** a frame's schema or protocol identification is unrecognized
- **THEN** the feed fails closed rather than guessing

### Requirement: Per-pairing merge by Task Presence ID

A device SHALL store the latest snapshot per active pairing and render the
union across pairings. A new snapshot SHALL replace only the sending pairing's
contribution. Merge SHALL be by Task Presence ID, and a relayed record SHALL
preserve its origin revision so a redundant direct/relayed copy converges to
the newest origin value. Disconnect SHALL remove only that connection's
contribution.

#### Scenario: Snapshot replaces only its pairing's contribution

- **WHEN** a feed delivers a new snapshot
- **THEN** it replaces only that pairing's contribution, not the device-global
  task set

#### Scenario: Union across pairings

- **WHEN** a device consumes several Presence Feeds
- **THEN** it renders the union of the latest snapshot from each active pairing

#### Scenario: Duplicate presence converges by origin revision

- **WHEN** the same Task Presence ID arrives both directly and relayed
- **THEN** the device converges to the copy with the newest origin revision

#### Scenario: Disconnect removes only that feed's contribution

- **WHEN** a feed connection drops
- **THEN** the device removes only that connection's contribution, and a task
  remains if another feed still supplies it

### Requirement: Feed-scoped control messages

The feed SHALL carry device action requests and the Edge-local acknowledge and
snooze dismissal messages back to the publisher. Control messages SHALL be
validated, route-scoped, and deduplicated (see `device-actions`).

#### Scenario: Control messages travel back along the feed

- **WHEN** a device invokes an action, acknowledge, or snooze
- **THEN** the message travels back along the valid presence route for that
  Task Presence
