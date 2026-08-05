# Proposal: Multi-Harness and Remote-Presence Rebuild

## Why

The repository contains a Codex-only prototype: a Go 1.22 macOS bridge and
hook receiver (`cmd/`, `internal/`), an authenticated WSS protocol
(`docs/PROTOCOL.md`), ESP-IDF firmware for the ESP32-S3-BOX-3 (`firmware/`), a
Codex status plugin (`plugin/`), and Python asset/release tooling (`tools/`).
That prototype can only show status from one harness on one machine to one
device.

The approved brief (`HANDOFF.md`) rebuilds it as **Agentagotchi**, a
multi-harness, remote-capable presence system: one Edge Bridge per machine
owns Task Presences from several Harness Adapters (Codex and Pi first), an
optional always-reachable Home Bridge relays privacy-safe presence from remote
Edges, and a BOX-3 merges several direct and Home-relayed Presence Feeds at
once. This is needed now because the prototype's Codex-specific boundaries are
incidental, and preserving them would fossilize mistakes into the new core
(`docs/adr/0007`).

## What Changes

This is an in-place rebuild that deliberately does **not** preserve prototype
protocol, credential, persistence, command, or provisioning compatibility. Git
history is the rollback path.

- **BREAKING** — Replace the Codex-only prototype with a role-separated
  architecture: a semantic core, an Edge Bridge runtime role, an optional Home
  Bridge runtime role, and per-harness Harness Adapters. Rename Codex-specific
  project, binary, package, UI, and protocol terminology to Agentagotchi
  domain language (`CONTEXT.md`).
- **BREAKING** — Replace `docs/PROTOCOL.md` atomically with role-separated,
  fail-closed contracts for Harness Adapter IPC, Edge-to-Home upstream,
  Edge/Home-to-device Presence Feeds, the Pairing Ceremony, device actions, and
  administration. No parallel legacy/new state machines, no migration matrix.
- Introduce **Edge-owned opaque Task Presence IDs**: each Edge assigns a
  canonical UUID per Task Presence and keeps the private mapping to
  `{adapter, native session id, capabilities}` local. Harness-native session
  IDs never leave the owning Edge (`docs/adr/0005`).
- Add a **semantic core** holding absolute presence reports, generation /
  sequence / revision ordering, leases, terminal retention, and global
  acknowledgement — with no harness lifecycle, transport, persistence, or
  display policy inside it.
- Support **two Harness Adapter IPC modes**: one-shot reports (Codex hooks) and
  leased adapter sessions (Pi), both owner-only local IPC, with honest
  state/reason mapping (`idle|running|needs_input|ready|blocked`,
  `working|question|approval|permission|completed|failed`).
- Add the **Pi Harness Adapter** (status-only first) so Codex and Pi report
  concurrently through one Edge Bridge without separate bridge processes.
- Implement the **Pairing Ceremony**: one short-lived, one-use device-code
  authorization state machine issuing unique, revocable, role-scoped
  credentials for Edge→device, Edge→Home, and Home→device relationships.
- Implement **complete-replacement Presence Feed snapshots** on every wire and
  multi-feed merge on BOX-3 (up to four concurrent authenticated WSS feeds),
  merged by Task Presence ID / origin revision (`docs/adr/0004`).
- Implement the **Home Bridge** as a **Cloudflare Worker + Durable Objects**
  service: one Durable Object per Home holding one-Home persistence, built-in
  single-admin bootstrap/login, revocable pairings, outbound Edge-to-Home
  snapshots, reverse action routing, Home-to-device feeds, and a privacy-safe
  browser dashboard. **Amends HANDOFF.md**: Home's deployment target changes
  from "platform-neutral service suitable for a Linux VPS/container" to
  Cloudflare Workers + Durable Objects.
- Add **device actions** routed back along a valid presence route (direct Edge,
  or Home→origin Edge), fail-closed, capability-gated, deduplicated by action
  ID, and never queued (`docs/adr/0006`). **Focus** is the first capability.
- Add **Edge-global dismissal**: acknowledge a terminal (`ready`/`blocked`)
  presence and snooze an input-gated (`needs_input`) one; both are
  episode-scoped and never mutate the harness (`docs/adr/0002`).
- Apply **leases and terminal retention**: transient presence expires on
  owner-lease loss; terminal presence is retained until acknowledged, bounded
  by a 7-day monotonic TTL and a ~200-entry FIFO (`docs/adr/0003`).
- Rework the **BOX-3 UI**: one main pet plus a larger, scrollable,
  touch-friendly task list, Featured Task behavior, FIFO state priority
  (`needs_input > blocked > ready > running > idle`), and pet-tap Focus.
- Build two thin administration clients over shared admin APIs: a Native SDK
  desktop client for Edge and a browser client for Home. Business and
  authorization logic stays in the services.

### Non-goals (deferred per HANDOFF.md)

- Claude Code adapter (architecturally supported, not implemented here).
- Approve/deny/respond and other task-mutating controls beyond Focus.
- Microphone capture, STT, and dictation.
- Pi/terminal focus without an exact supported capability.
- Pet color configuration and custom/downloaded pet designs.
- Edge-to-Edge or Home-to-Home relaying; Local Feed Gateway/election unless
  four-feed hardware profiling fails.
- Multi-tenant Home hosting; per-device acknowledgement/snooze; time-based
  snooze resurface; adapter attention-epoch counters.
- Legacy prototype protocol/runtime compatibility.
- VPS Edge deployment and remote acceptance (Pi on a VPS relaying through
  Home). Home and the Edge's outbound connection are built and validated with
  local Edges in this change; the VPS Edge is follow-up scope.

## Capabilities

### New Capabilities

- `semantic-core`: The Edge-owned presence model — absolute reports, opaque
  Task Presence IDs, state/reason vocabulary, generation/sequence/revision
  ordering, leases, terminal retention, and global acknowledgement/snooze. No
  harness, transport, persistence, or display policy.
- `harness-adapter-ipc`: The owner-only local contract between Harness
  Adapters and the Edge Bridge (one-shot and leased modes), honest state
  mapping, sanitization, and capability registration.
- `codex-adapter`: The Codex Harness Adapter — lifecycle reduction from Codex
  signals, Edge-local App Server enrichment, and exact fail-closed Focus.
- `pi-adapter`: The Pi Harness Adapter — a leased, status-only adapter with
  honest state mapping and absolute resend on reconnect.
- `edge-bridge`: The Edge runtime role — adapter IPC, private capability
  registry, LAN feed/pairing endpoint, optional outbound Home connection, CLI
  management, and the device-action capability router.
- `home-bridge`: The Home runtime role — a Cloudflare Worker + Durable
  Objects service with one-Home persistence, single-admin authentication,
  pairing/revocation, Edge ingress, device feed egress, and the privacy-safe
  dashboard and administration API.
- `pairing-ceremony`: The short-lived, one-use device-code authorization state
  machine issuing unique, revocable, role-scoped credentials for all three
  pairing directions.
- `presence-feed`: The Edge/Home-to-device contract — authenticated WSS,
  bounded complete-replacement snapshots, per-pairing merge semantics, and
  feed-scoped action/acknowledge/snooze control messages.
- `device-actions`: Capability-gated, route-validated, fail-closed,
  deduplicated, never-queued device actions (Focus first) plus Edge-global
  acknowledge/snooze dismissal.
- `box3-firmware`: The BOX-3 firmware — up to four concurrent feeds, per-pairing
  snapshot merge, the scrollable task list, Featured Task and FIFO priority,
  and pet-tap Focus interaction.

### Modified Capabilities

<!-- openspec/specs/ is empty; there are no existing capability specs to modify.
     The rebuild replaces prototype behavior wholesale rather than amending
     versioned specs. -->

- None. There are no existing capability specs in `openspec/specs/`; this
  change introduces the first versioned capability set.

## Impact

- **Runtime roles / code**: Replaces the Codex-only Go bridge in `cmd/` and
  `internal/` with a semantic core plus Edge and Home runtime roles; adds
  `adapters/codex` and `adapters/pi`; adds pairing/auth, feed protocol,
  edge-upstream protocol, and administration API boundaries; adds an Edge
  Native SDK app and a Home web app. The Codex status plugin in `plugin/` is
  superseded by the Codex Harness Adapter.
- **Protocol / wire contracts**: `docs/PROTOCOL.md` is replaced atomically with
  role-separated contracts. **BREAKING** — no compatibility with the prototype
  protocol, credentials, persistence, commands, or provisioning.
- **Firmware**: `firmware/` (ESP-IDF 5.5.x, ESP32-S3-BOX-3) gains multi-feed
  WSS, snapshot merge, and a new task-list interaction model; requires physical
  hardware validation recorded in `docs/HARDWARE_ACCEPTANCE.md`.
- **Persistence / credentials**: New owner-only credential storage, pairing
  state, and privacy-safe presence persistence for Edge and Home. No migration
  from prototype state.
- **Privacy boundary**: Enforced as a product invariant — only opaque Task
  Presence IDs, bounded Safe Titles, generic state/reason, allowlisted
  capabilities, counts, ordering metadata, and timestamps cross the owning Edge.
- **Deployment targets**: Edge runs cross-platform on the local machine
  (headless Linux/VPS support remains a requirement, but VPS deployment
  validation is deferred); Home runs as a Cloudflare Worker with one Durable
  Object per Home.
- **Verification**: `make test` for source changes; `idf.py -C firmware build`
  when firmware changes. No install, flash, or provisioning without explicit
  user authorization.
