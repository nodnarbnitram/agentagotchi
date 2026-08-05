# Spec: Edge Bridge

## Purpose

The Edge Bridge is the single runtime role per machine that owns the Task
Presences produced there. It terminates Harness Adapter IPC, keeps the private
task-to-harness capability registry, serves a LAN feed/pairing endpoint for
devices, optionally connects outbound to a Home Bridge, routes device actions,
and provides full CLI management.

## ADDED Requirements

### Requirement: One Edge per machine, shared across adapters

One Edge Bridge SHALL run per machine and be shared by every Harness Adapter on
that machine. The Edge SHALL own the Task Presences produced on its machine and
allow concurrent Codex and Pi adapters without separate bridge processes or
task collisions.

#### Scenario: Concurrent adapters share one Edge

- **WHEN** a Codex adapter and a Pi adapter run on the same machine
- **THEN** both report to the single Edge Bridge concurrently without separate
  bridge processes

#### Scenario: Edge owns its Task Presences

- **WHEN** Harness Sessions produce presence on a machine
- **THEN** the Edge Bridge is the authority for those Task Presences

### Requirement: Private capability registry

The Edge SHALL maintain a private registry mapping each Task Presence ID to its
owning adapter, native session id, and registered capabilities. The registry
SHALL be used to dispatch device actions and SHALL NOT be exposed upstream.

#### Scenario: Registry resolves actions

- **WHEN** a device action targets a Task Presence ID
- **THEN** the Edge resolves it through the private registry to the owning
  adapter and registered capability

#### Scenario: Registry is not exposed upstream

- **WHEN** the Edge publishes presence to Home or devices
- **THEN** the private registry contents are not included

### Requirement: LAN feed/pairing endpoint

The Edge SHALL expose a LAN Presence Feed and pairing endpoint for devices. The
Edge MAY advertise its feed/pairing endpoints via Bonjour. Network discovery
SHALL NOT be treated as trust.

#### Scenario: Device connects to LAN feed

- **WHEN** a paired device connects to the Edge LAN endpoint
- **THEN** the Edge serves its authenticated Presence Feed

#### Scenario: Discovery is not trust

- **WHEN** a device discovers an Edge endpoint via Bonjour
- **THEN** the Edge still requires the Pairing Ceremony before serving presence

### Requirement: Optional outbound Home connection

The Edge SHALL optionally open an outbound authenticated WSS connection to its
paired Home Bridge and send complete absolute snapshots of its current Task
Presences with an Edge generation and monotonic revision. On reconnect the Edge
SHALL resend the complete snapshot.

#### Scenario: Edge sends absolute snapshot upstream

- **WHEN** the Edge is paired with a Home Bridge and its presence changes
- **THEN** it sends a complete absolute snapshot of its current Task Presences
  with an Edge generation and monotonic revision

#### Scenario: Reconnect resends complete snapshot

- **WHEN** the Edge reconnects to Home
- **THEN** it resends the complete current snapshot without replaying raw
  lifecycle events

### Requirement: Device-action capability router

The Edge SHALL route device actions only through the owning registered Harness
Adapter capability, fail closed, and never fall back to another app, task,
machine, or harness. (See `device-actions`.)

#### Scenario: Action dispatched only to owning capability

- **WHEN** a device action arrives for a Task Presence
- **THEN** the Edge dispatches it only through the owning adapter's registered
  capability and never falls back to another target

### Requirement: Full CLI management

The Edge SHALL provide full CLI management for bootstrap, pairing, status,
revocation, and recovery, including on headless Linux/VPS deployments.

#### Scenario: Headless CLI bootstrap

- **WHEN** an Edge runs on a headless Linux/VPS host
- **THEN** an operator can bootstrap, pair, check status, revoke, and recover
  entirely via the CLI

### Requirement: Cross-platform daemon

The Edge SHALL run as a cross-platform daemon, including headless Linux/VPS
operation. It SHALL NOT gain Home-only administration authority, and a
co-located Home SHALL NOT gain local harness capabilities merely because both
roles run on one machine.

#### Scenario: Edge runs headless

- **WHEN** the Edge is deployed on a VPS without a desktop
- **THEN** it operates fully as a daemon with owner-only local IPC

#### Scenario: Roles stay isolated when co-located

- **WHEN** an Edge and a Home run on the same machine
- **THEN** the Home does not gain local harness capabilities and the Edge does
  not gain Home administration authority
