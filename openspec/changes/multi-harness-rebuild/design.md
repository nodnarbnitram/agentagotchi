# Design: Multi-Harness and Remote-Presence Rebuild

## Context

See `proposal.md` — Why and What Changes. The current repository is a Codex-only
prototype: a Go 1.22 macOS bridge/hook receiver (`cmd/`, `internal/`), an
authenticated WSS protocol (`docs/PROTOCOL.md`), ESP-IDF 5.5.x firmware for the
ESP32-S3-BOX-3 (`firmware/`), a Codex status plugin (`plugin/`), and Python
asset/release tooling (`tools/`). HANDOFF.md is the approved brief; the ADRs in
`docs/adr/` (0001–0007) fix the load-bearing decisions. This design explains how
to structure the rebuild; it does not restate the spec contracts in
`specs/*/spec.md`.

The governing constraint is architectural: the **semantic core** must stay free
of harness lifecycle, transport, persistence, and display policy so that Codex,
Pi, and future adapters plug into one model, and so the privacy boundary is
enforceable at a single seam. Git history is the only rollback path
(`docs/adr/0007`); there is no compatibility layer.

Two scope amendments relative to HANDOFF.md are recorded here and in the
proposal: Home's deployment target is now a **Cloudflare Worker + Durable
Objects** service (amending HANDOFF.md's "platform-neutral service suitable for
a Linux VPS/container" direction), and the **VPS Edge remote scenario is
deferred** — Home and the Edge's outbound connection are validated with local
Edges paired to Home in this change.

## Goals / Non-Goals

**Goals:**

- A role-separated repository: `semantic core`, `edge service`, `home service`,
  `adapters/codex`, `adapters/pi`, `pairing/auth`, `feed protocol`,
  `edge-upstream protocol`, `administration API`, `edge Native SDK app`,
  `home web app`, and `firmware`, with boundaries that keep host-event
  reduction, transport, persistence, and UI policy out of the semantic store.
- A single semantic-core module consumed by both the Edge and (in relayed,
  privacy-safe form) the Home, so ordering, leases, retention, and dismissal
  logic are written once and tested once.
- Fail-closed, versioned wire contracts that let host and firmware evolve
  together, replaced atomically in `docs/PROTOCOL.md`.
- An implementation order (below) that lands the semantic/privacy foundation
  before any UI or remote networking, per HANDOFF.md's instruction not to skip
  semantic/privacy tests to start UI early.

**Non-Goals:**

- Choosing final package paths or frameworks beyond the boundary list above —
  exact layout is an implementation decision as long as the seams hold.
- Re-deriving the spec contracts (state/reason vocabulary, snapshot semantics,
  action routing) — those live in `specs/*/spec.md`.
- Deferred scope from the proposal's Non-goals (Claude adapter, approve/deny,
  STT, pet customization, mesh/gateway, multi-tenancy).

## Decisions

### 1. Edge-owned semantic core; Home implements only the relay subset

The semantic core (spec: `semantic-core`) is a module that the Edge embeds and
runs in-process. The full model — leases, terminal retention, dismissal, the
capability registry — is Edge-owned. The Home needs only the relay subset:
per-Edge snapshot replacement, origin-revision tracking, fan-out to device
feeds, and reverse action routing (spec: `home-bridge`). Because the Edge is a
Go daemon and the Home now runs on the Cloudflare Workers runtime (Decision 8),
one shared module across both roles is not possible across languages; instead
the contracts (`docs/PROTOCOL.md`) and the schema/contract tests pin both
implementations to the same wire semantics. We reject making the semantic core
a separate networked service: presence must survive a Home outage and stay
machine-local, so the Edge cannot depend on a remote authority (`docs/adr/0001`).

*Alternatives considered:* a central authority service (rejected — breaks the
Home-outage requirement and creates a single point of failure); one shared
module across both roles (rejected — the Edge is Go and the Home is on the
Workers/TypeScript runtime; the Home relay subset is small enough that a
contract-pinned reimplementation carries less drift risk than a WASM port).

### 2. Opaque Task Presence ID assigned at the Edge, mapped privately

Public identity is a canonical UUID assigned by the owning Edge; the
`{adapter, native session id, capabilities}` mapping stays Edge-local
(`docs/adr/0005`). The semantic core owns assignment so identity is created at
the same seam that enforces the privacy allowlist. Adapters never see or choose
the public ID.

*Alternatives considered:* carrying harness-native IDs upstream for debuggability
(rejected — violates the privacy boundary and forces per-harness vocabulary into
the core).

### 3. Complete-replacement snapshots on every wire

All three wires (adapter→Edge, Edge→Home, Edge/Home→device) carry bounded
absolute snapshots ordered by generation/producer-sequence/origin-revision, and
reconnects resend full state (`docs/adr/0004`). There is no event log anywhere.
This makes convergence after any gap trivially correct and keeps no history to
leak or retain. The cost (bandwidth on churn) is bounded because the Task
Presence model is small and adapters coalesce rapid updates.

*Alternatives considered:* event-sourced deltas (rejected — convergence after
dropped frames and redundant direct-plus-relayed copies becomes hard, and an
event log is a retention/leak liability).

### 4. Role-separated contracts, replaced atomically in PROTOCOL.md

`docs/PROTOCOL.md` is rewritten wholesale into role-separated contracts: local
adapter IPC, Edge-to-Home upstream, Edge/Home-to-device feeds, the Pairing
Ceremony, device actions, and administration. Contracts are fail-closed on
schema/protocol identification but implement only the current version — no
migration matrix (`docs/adr/0007`). Host and firmware are updated together so
there is never a mixed old/new state machine.

*Alternatives considered:* a universal peer protocol (rejected — the roles have
different authority; a universal protocol smears the privacy and routing seams);
parallel legacy/new state machines (rejected — fossilizes prototype mistakes).

### 5. One Pairing Ceremony state machine, role-scoped grants

A single short-lived, one-use device-code authorization state machine
(`pairing-ceremony`) serves all three directions (Edge→device, Edge→Home,
Home→device) with role-specific grants. Reusing one state machine avoids three
divergent auth flows and concentrates the security surface for threat-modeling.
Bonjour is discovery only, never trust; remote Home uses explicit HTTPS and
publicly trusted TLS.

*Alternatives considered:* separate flows per direction (rejected — three auth
surfaces to audit); long-lived pre-shared keys (rejected — codes must be
one-use and quickly expiring, never long-lived secrets).

### 6. Capability router with fail-closed dispatch and no queue

Device actions (`device-actions`) resolve only within the observed
feed/paired origin, require an advertised allowlisted capability, dispatch only
through the owning Edge's registered adapter capability, deduplicate by action
ID at the origin Edge, acknowledge only after exact success, and are never
queued (`docs/adr/0006`). Focus is the first capability. The router stays open
for later approve/deny/respond/dictate behind deliberate protocol, auth, UX,
privacy, and harness-specific tests.

*Alternatives considered:* store-and-forward queue (rejected — a delayed
focus/approval/response acts on a context that may no longer exist and is unsafe
exactly where correctness matters most).

### 7. Edge-global, episode-scoped dismissal in the core

Acknowledge (terminal) and snooze (input-gated) are recorded once, globally, at
the owning Edge and converge everywhere by snapshot replacement
(`docs/adr/0002`). They are Edge-local control messages that never dispatch to a
harness capability. Per-device dismissal state is deferred so firmware stays
simple and all surfaces agree. The accepted v1 limitation (identical
back-to-back gate on a snoozed task stays snoozed) is documented; an
adapter-provided opaque attention-epoch counter is the designed fix if needed.

### 8. Home Bridge on Cloudflare Workers + Durable Objects

The Home deploys as a Cloudflare Worker with exactly one Durable Object per
Home. The Durable Object holds presence state, pairing state, credentials, and
the Edge/device WebSocket connections; Durable Object storage (SQLite) is the
persistence layer; DO alarms drive time-based checks; WebSocket hibernation
keeps idle long-lived connections open across runtime eviction. The browser
admin client ships as static assets served by the Worker. This amends
HANDOFF.md's "platform-neutral Linux VPS/container" direction: the
always-reachable requirement ("Home remains reachable when no Edge is online")
maps directly to an always-on Worker with no server to operate, and the
DO-per-Home boundary enforces the no-global-tenancy requirement structurally.

*Alternatives considered:* VPS/container per HANDOFF.md's original direction
(superseded — this change deliberately targets Cloudflare; a VPS also adds
server operations for an always-on requirement that Workers provide natively);
Workers + KV/D1 without Durable Objects (rejected — the Home terminates
long-lived Edge and device WSS connections and needs strongly consistent
per-Home coordination, which is the Durable Objects model; KV/D1 lack the
WebSocket and per-Home transaction primitives).

### 9. Two thin admin clients over shared admin APIs

A Native SDK desktop client (Edge) and a browser client (Home, served as static
assets from the Home Worker) are built as two thin clients over shared
administration APIs and validation schemas. Domain rules, schemas, API
semantics, and visual design are shared; rendering components are not, because
Native SDK does not render in a browser. Business and authorization logic stays
in the services, keeping authority seams clean.

### 10. Implementation order: semantic/privacy before UI and remote networking

Work proceeds in HANDOFF.md's dependency order (one release, not release gates):
(1) domain language + protocol contracts + privacy allowlists/schema tests +
semantic-core split; (2) Edge semantic core + Codex adapter; (3) Pi adapter +
same-machine cross-harness; (4) pairing, feeds, BOX-3 multi-source UI; (5) Home
relay (Workers + Durable Objects) + administration; (6) local failure-mode
acceptance + hardening. HANDOFF.md's VPS Pi-through-Home acceptance is
deferred to a follow-up change; Phase 5 is validated with local Edges paired
to Home. Privacy allowlists and schema/contract tests are written *before*
networking code.

## Risks / Trade-offs

- **Snapshot bandwidth on state churn** → Task Presence model is small and
  adapters coalesce rapid updates; bounded frames cap worst case.
- **Four concurrent TLS/WSS feeds exceed ESP32 memory/power budget** → profile
  on physical BOX-3 before accepting the limit; if it fails, open a separate
  Local Feed Gateway design — do not preemptively add Edge-to-Edge forwarding,
  election, or Raft. Compilation is never treated as hardware acceptance.
- **Snoozed task misses an identical back-to-back gate (v1 limitation)** →
  documented; add the adapter-provided opaque attention-epoch counter only if
  real usage requires it.
- **Operational cost of opaque identity** (correlating a Task Presence back to a
  harness session needs owner-only Edge tooling) → accepted trade for the
  privacy boundary (`docs/adr/0005`); provide that tooling in the Edge CLI.
- **Flag-day cutover strands the author's kit on the old protocol** → accepted
  (`docs/adr/0007`); re-provision devices under the Pairing Ceremony, git
  history is the rollback path.
- **Co-located Edge+Home could blur role authority** → enforce separate config,
  credentials, persistence, and permissions so Home never gains local harness
  capabilities and Edge never gains Home admin authority.
- **Home internet exposure** → threat-model pairing, admin sessions, task
  routing, and privacy-safe persistence/logging in the hardening phase before
  release.
- **Workers runtime constraints** (CPU time, Durable Object storage, message
  size) bound the Home's snapshot fan-out → the presence model is small and
  contract-bounded; load-test fan-out at the multi-Edge, four-feed scale.
- **WebSocket hibernation edge cases** (idle Edge/device connections surviving
  runtime eviction) → contract tests for reconnect-free idle periods; snapshot
  semantics already require full resend on reconnect, so gaps self-heal.
- **Vendor coupling to Cloudflare** replaces HANDOFF.md's platform-neutral
  Home → accepted scope amendment; keep the Home's contract boundaries clean
  (role-separated `docs/PROTOCOL.md`) so a later port remains possible.

## Migration Plan

This is a flag-day cutover, not a migration. There is no mixed old/new
operation and no data migration: prototype protocol, credentials, persistence,
commands, and provisioning are replaced atomically. Devices are re-provisioned
under the Pairing Ceremony. Rollback is `git` history — the previous prototype
commit is restorable, but the two are never run simultaneously. Release and
physical hardware acceptance are documented in `docs/RELEASE_VERIFICATION.md`
and `docs/HARDWARE_ACCEPTANCE.md` (physical results recorded only from real
hardware, never from compilation). Install, flash, and provisioning commands
run only with explicit user authorization — that includes `wrangler deploy`
for the Home Worker. The VPS Edge scenario is follow-up scope, not part of
this cutover.

## Open Questions

None that change the specs, approach, or task breakdown. Final package paths,
framework choices, and the firmware dismiss-gesture affordance are deferred
implementation decisions constrained by the boundaries above.
