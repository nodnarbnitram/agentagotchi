# Spec: Home Bridge

## Purpose

The Home Bridge is an optional, always-reachable aggregator and relay for one
Home, deployed as a Cloudflare Worker with one Durable Object per Home. It
accepts outbound Edge ingress, relays privacy-safe presence to paired devices
and clients, persists pairing and presence state, and provides browser-based
administration — without becoming the task authority and without any local
harness capability.

## ADDED Requirements

### Requirement: Cloudflare Workers deployment

The Home Bridge SHALL deploy as a Cloudflare Worker with exactly one Durable
Object instance per Home as the unit of state, tenancy, and WebSocket
coordination. The Home SHALL remain reachable even when no Edge Bridge is
online.

#### Scenario: Always reachable

- **WHEN** no Edge Bridge is currently connected
- **THEN** the Home remains reachable for device feeds, pairing, and
  administration

#### Scenario: One Durable Object per Home

- **WHEN** a Home is deployed
- **THEN** its presence state, pairing state, credentials, and WebSocket
  connections are held by one Durable Object instance scoped to that Home

### Requirement: One-Home deployment with stable identity

The initial Home deployment SHALL host exactly one Home. The Home SHALL have a
stable identity distinct from its process, hostname, or hosting infrastructure.
Storage and authorization SHALL be scoped to the Home's own Durable Object and
SHALL NOT rely on global state that would prevent later Home isolation.

#### Scenario: Single Home per deployment

- **WHEN** a Home Bridge is deployed
- **THEN** it hosts exactly one Home with a stable identity independent of the
  hosting process, hostname, or infrastructure

#### Scenario: No global tenancy assumptions

- **WHEN** the Home persists or authorizes data
- **THEN** it uses storage scoped to its own Durable Object rather than global
  state, so later multi-Home isolation is not prevented

### Requirement: Built-in single-administrator authentication

The Home SHALL provide built-in single-administrator bootstrap and login,
secure server-side sessions, and CSRF protection. Pairing credentials SHALL
NOT be valid as administrator credentials.

#### Scenario: Single-admin bootstrap and login

- **WHEN** a Home is first deployed
- **THEN** an administrator can bootstrap and log in as the single
  administrator with a secure server-side session

#### Scenario: CSRF protection on administration

- **WHEN** an administrator performs a state-changing administration action
- **THEN** the Home enforces CSRF protection

#### Scenario: Pairing credential is not an admin credential

- **WHEN** a client presents a pairing credential
- **THEN** it cannot authenticate as the administrator

### Requirement: Edge ingress with per-Edge replacement

The Home SHALL accept outbound authenticated WSS connections from paired Edge
Bridges carrying complete absolute snapshots with an Edge generation and
monotonic revision. The Home SHALL replace only that Edge's prior contribution.
Home SHALL NOT receive private native session IDs, source payloads, prompts,
commands, tool data, transcripts, or full paths.

#### Scenario: Snapshot replaces only that Edge's contribution

- **WHEN** a paired Edge sends a snapshot
- **THEN** the Home replaces only that Edge's prior contribution and leaves
  other Edges' contributions intact

#### Scenario: Long-lived connections survive runtime eviction

- **WHEN** an Edge or device WSS connection is idle
- **THEN** the connection remains open across runtime eviction of idle state
  (for example via WebSocket hibernation) without requiring a client reconnect

#### Scenario: Home receives no private data

- **WHEN** an Edge connects to Home
- **THEN** the Home does not receive private native session IDs, source
  payloads, prompts, commands, tool data, transcripts, or full paths

### Requirement: Privacy-safe persistence

The Home SHALL persist only the privacy-safe presence model, pairing state,
connectivity metadata, and the credentials required for its role. It SHALL NOT
persist prompts, transcripts, tool payloads, commands, or filesystem metadata.

#### Scenario: Home persists only safe data

- **WHEN** the Home persists state
- **THEN** it stores only the privacy-safe presence model, pairing state,
  connectivity metadata, and role credentials

#### Scenario: Per-Home isolated storage

- **WHEN** the Home persists state
- **THEN** it writes to the Durable Object storage belonging to that Home, not
  to shared or global storage

### Requirement: Reverse action routing to the owning Edge only

The Home SHALL send a device action upstream only for a Task Presence currently
owned by an authenticated Edge connection. It SHALL NOT retarget an action to
another Edge, invent a capability, or queue an action while an Edge/adapter is
offline.

#### Scenario: Action routed only to owning Edge

- **WHEN** a device action targets a Task Presence relayed through Home
- **THEN** the Home forwards it only to the origin Edge that currently owns
  that Task Presence over its authenticated connection

#### Scenario: Home cannot reroute or invent

- **WHEN** the owning Edge is unavailable or the capability is not advertised
- **THEN** the Home fails unavailable without rerouting to another Edge,
  inventing a capability, or queueing the action

### Requirement: Relay preserves origin identity and revision

The Home SHALL relay a Task Presence preserving its Task Presence ID unchanged
and carrying its origin revision, so a redundant direct/relayed copy converges
to the newest origin value on the device.

#### Scenario: Relay preserves Task Presence ID

- **WHEN** the Home relays a Task Presence to a device
- **THEN** the Task Presence ID is preserved unchanged

#### Scenario: Relay carries origin revision

- **WHEN** the Home relays a Task Presence
- **THEN** it carries the origin revision so a duplicate direct copy converges
  to the newest origin value

### Requirement: Privacy-safe dashboard and administration API

The Home SHALL provide pairing approval and revocation, Edge/device
connectivity status, and a privacy-safe Task Presence dashboard through a
browser-based administration client. The dashboard SHALL expose no prompts,
transcripts, tool payloads, commands, or filesystem metadata.

#### Scenario: Dashboard is privacy-safe

- **WHEN** an administrator views the Home dashboard
- **THEN** it shows pairing approval/revocation, Edge/device connectivity
  status, and privacy-safe Task Presences without prompts, transcripts, tool
  payloads, commands, or filesystem metadata

### Requirement: No local harness authority

The Home SHALL provide no Harness Adapter IPC, Codex App Server process,
desktop focus implementation, or other local-machine authority.

#### Scenario: Home has no local harness capability

- **WHEN** the Home operates
- **THEN** it exposes no Harness Adapter IPC, Codex App Server process, desktop
  focus implementation, or other local-machine authority
