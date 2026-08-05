# Agentagotchi multi-harness and remote-presence rebuild

Approved implementation brief. Rebuild the current prototype in place around
machine-local Edge Bridges, an optional remote Home Bridge, and devices that can
consume several presence feeds simultaneously.

The existing code, protocol, and Codex-only handoff are reference material, not
compatibility constraints. Git history is the rollback path. Prefer a coherent
replacement over adapters around accidental prototype boundaries.

`Agentagotchi` is the product name. Rename Codex-specific project, binary,
package, UI, and protocol terminology as part of the rebuild.

## Required scenarios

### Same machine, several harnesses

One Edge Bridge receives privacy-safe reports from every supported harness on
that machine. Codex and Pi must be able to report concurrently without separate
bridge processes or task collisions.

```text
Codex adapter ─┐
               ├─> Edge Bridge ─> BOX-3
Pi adapter ────┘
```

### Remote harness plus local harnesses

A Pi session on a VPS reports to the VPS Edge. That Edge makes an outbound,
authenticated connection to Home. BOX-3 receives remote presence from Home
while continuing to receive local presence directly from a paired LAN Edge.

```text
local Codex/Pi ─> desktop Edge ───────────────> BOX-3

VPS Pi ─────────> VPS Edge ─> Home Bridge ───> BOX-3
```

Home remains reachable when no Edge is online. A Home outage must not break a
direct LAN Edge-to-device feed.

### Future clients

Home may serve additional privacy-safe clients later. Do not make BOX-3 the
semantic model owner or bake display-specific behavior into Edge/Home state.

## Scope decisions

- Rebuild in this repository; do not preserve runtime compatibility with the
  prototype protocol, credentials, persistence, commands, or provisioning.
- Replace `docs/PROTOCOL.md` atomically with the new contracts and update host
  and firmware together. Do not build parallel legacy/new state machines.
- Keep protocol/schema identification fail-closed, but implement only the
  current contract; there is no migration matrix for the prototype.
- Pi status is in scope.
- Claude Code remains architecturally supported but its adapter is deferred
  until it can be developed and tested with the user's Anthropic subscription.
- Focus is the first device action. Approve/deny/respond and microphone/STT are
  later capability-routed actions, not part of the status rebuild.
- Procedural color configuration, custom pet designs, and bridge-delivered pet
  assets are deferred.

## Domain model

Use the canonical language in [`CONTEXT.md`](CONTEXT.md).

### Ownership

- One **Edge Bridge** runs per machine, shared by all Harness Adapters there.
- The Edge owns the Task Presences produced on its machine.
- A **Home Bridge** is an optional, always-reachable aggregator and relay for
  one Home. It is not the authority for Edge-owned presence.
- A device may consume several Presence Feeds at once.
- Harness-native session identifiers never leave the owning Edge.

### Public task identity

The Edge assigns an opaque canonical UUID to each Task Presence. It keeps the
private mapping from that UUID to `{adapter, native session id, capabilities}`.
Home preserves the Task Presence ID unchanged when relaying it.

Do not use native Pi/Codex/Claude session IDs on Edge-to-Home or device wires.
Do not expose harness type, machine identity, hostnames, usernames, paths, or
source routing metadata as independent status fields.

### Safe titles

Safe titles may identify an allowlisted harness (`Codex`, `Pi`, later `CC`) and
an optional user-approved Edge alias. Never derive an alias from a hostname,
cwd, session name, prompt, transcript, or command. Keep labels bounded and
sanitized. Routing must never depend on title text.

## Directed topology and pairing

Support only these directed delivery relationships:

1. Edge Bridge -> device
2. Edge Bridge -> Home Bridge -> device

A device may pair with several Edge/Home feed publishers. An Edge may publish
directly to devices, to one Home, or both. Home relays only the Edges explicitly
paired with it. Do not add Edge-to-Edge, Home-to-Home, arbitrary mesh, or more
than one relay hop.

### Pairing

Use one short-lived device-code authorization state machine — the **Pairing
Ceremony** — for all relationship types, with role-specific grants:

- the connecting client requests a short-lived code from the receiving
  service;
- the connecting client displays the code to the user (BOX-3 screen, Edge
  CLI);
- the user approves it in the receiving service's authenticated
  administration client (Edge CLI/app, or the Home web UI);
- successful approval issues a unique, random, revocable credential scoped to
  that relationship and role;
- the client pins/authenticates the receiving service identity;
- codes are one-use, expire quickly, and never become long-lived secrets.

Bonjour may discover Edge feed/pairing endpoints on a LAN. A remote Home uses an
explicit HTTPS URL and publicly trusted TLS. Network discovery is not trust.

Never reuse one Home secret across devices or Edges. Never print, log, transmit
in status, or persist outside owner-only credential storage any long-lived token,
private key, Wi-Fi credential, prompt, command, tool payload, or transcript.

### Home tenancy and administration

The initial Home deployment hosts exactly one Home. Give the Home a stable
identity distinct from its process, hostname, or VPS. Multi-tenant hosting is
deferred, but storage and authorization must not use process-global assumptions
that would prevent later Home isolation.

Home provides:

- built-in single-administrator bootstrap and login;
- secure server-side sessions and CSRF protection;
- pairing approval and revocation;
- Edge/device connectivity status;
- a privacy-safe Task Presence dashboard;
- no prompts, transcripts, tool payloads, commands, or filesystem metadata.

Pairing credentials are never valid as administrator credentials.

## Runtime roles and administration surfaces

Keep Edge and Home as explicit runtime roles with separate configuration,
credentials, persistence, and permissions. They may ship from one repository or
distribution, but Home must not gain local harness capabilities merely because
both roles run on one machine.

### Edge

- Cross-platform daemon, including headless Linux/VPS operation.
- Owner-only local Harness Adapter IPC.
- LAN feed/pairing endpoint for devices.
- Optional outbound connection to Home.
- Private task-to-harness capability registry.
- Full CLI management for bootstrap, pairing, status, revocation, and recovery.
- Optional native desktop administration app built with
  [Vercel Labs Native SDK](https://github.com/vercel-labs/native).

### Home

- Platform-neutral service suitable for a Linux VPS/container.
- Edge ingress, device feed egress, pairing, persistence, and administration API.
- Browser-based web UI with built-in single-admin authentication.
- No Harness Adapter IPC, Codex App Server process, desktop focus implementation,
  or other local-machine authority.

### Frontend boundary

Native SDK does not render in a browser. Build two thin clients over shared
administration APIs and validation schemas:

- Native SDK desktop client for Edge;
- browser client for Home.

Share domain rules, schemas, API semantics, and visual design—not rendering
components. Keep business and authorization logic in the services.

## Harness Adapter -> Edge contract

Adapters sanitize and map host-specific signals before reporting to Edge. Core
state must never switch on Codex/Pi/Claude lifecycle event names.

Support two owner-only local IPC modes:

1. **One-shot reports** for hook-driven integrations such as the
   current Codex adapter.
2. **Leased adapter sessions** for Pi and future long-lived adapters, with
   registration, generation, monotonic sequence, heartbeat, absolute reports,
   end/removal, and capability registration.

The semantic report is an absolute statement, roughly:

```text
upsert {
  private native session id,
  state,
  reason,
  safe title?,
  subagent count?,
  capabilities[],
  generation,
  producer sequence,
  observed timestamp
}

end { private native session id, generation, producer sequence, observed timestamp }
```

The Edge resolves/assigns the public Task Presence ID. Private native IDs do not
pass upstream. Frames are bounded, schemas strict, socket/storage permissions
owner-only, and raw payloads are never logged. Adapter failures must never break
or delay the agent harness.

On reconnect, a leased adapter resends its complete absolute current state. Edge
ignores stale generations and out-of-order producer sequences.

### Honest state mapping

Shared states remain:

```text
idle | running | needs_input | ready | blocked
```

Reasons remain generic:

```text
working | question | approval | permission | completed | failed
```

An adapter reports only states and capabilities supported by explicit harness
signals. Never infer `needs_input` from tool names, terminal text, prompts, or
timing heuristics.

## Edge -> Home contract

An Edge opens an outbound authenticated WSS connection to its paired Home. Use
a role-specific contract, not a universal peer protocol.

The Edge sends complete absolute snapshots of its current Task Presences with an
Edge generation and monotonic revision. Home replaces only that Edge's prior
contribution. Reconnect resends the complete snapshot; replay of raw lifecycle
events is unnecessary.

Home persists only the privacy-safe presence model, pairing state, connectivity
metadata, and credentials required for its role. It must not receive private
native session IDs, source payloads, prompts, commands, tool data, transcripts,
or full paths.

Home sends a device action upstream only for a Task Presence currently owned by
that authenticated Edge connection. It cannot retarget an action to another
Edge or invent a capability.

## Edge/Home -> device Presence Feed

Use a role-specific authenticated WSS feed contract distinct from Edge upstream
ingress. Every connection begins with and subsequently sends complete
replacement snapshots for that pairing's Presence Feed.

### Device merge

BOX-3 stores the latest snapshot per active pairing and renders the union across
pairings. A new snapshot replaces only the sending pairing's contribution—not
the device-global task set.

Merge by Task Presence ID. A relayed record preserves its origin revision so an
accidental or deliberately redundant direct/relayed copy converges to the newest
origin value. Disconnect removes only that connection's contribution; the task
remains if another feed still supplies it.

Target four simultaneous Presence Feed connections in firmware. Store more
pairings only if inactive connections have deterministic priority and visible
diagnostics. Profile TLS/WSS memory, stability, reconnect behavior, and power on
physical BOX-3 hardware before declaring the limit accepted.

If four feeds exceed an explicit measured hardware budget, open a separate
design for a Local Feed Gateway. Do not preemptively add Edge-to-Edge forwarding,
leader election, or Raft; packet aggregation is not consensus.

### Device capabilities

A Task Presence may advertise only generic, allowlisted actions currently
available, beginning with `focus`. Capability names are a narrowly scoped
control-plane field; they expose no harness, machine, prompt, or command details.

Do not infer actions from task state. Status-only remote sessions are valid.

## Leases, disconnects, and retention

Apply leases at Adapter-to-Edge and Edge-to-Home boundaries.

When an owner lease expires:

- remove transient `running` and `needs_input` presences after a short grace
  period;
- retain already-terminal `ready` and `blocked` notifications under the
  acknowledgement/retention policy: keep until acknowledged, bounded by a
  7-day TTL measured in monotonic time (configurable per Edge) and a per-Edge
  FIFO bound of ~200 terminal presences with oldest evicted first. Expiry
  removes the presence exactly like acknowledgement — it never rewrites it to
  `completed` or `failed` — and is visible in administration diagnostics;
- never convert a disconnect into `completed` or `failed`;
- show bridge/adapter liveness separately in administration UI, not as a pet
  task state.

Use monotonic time for local lease decisions. Do not compare machine wall clocks
to order updates.

## Device actions

Actions travel back along a valid presence route:

```text
BOX-3 -> direct Edge -> Harness Adapter
BOX-3 -> Home -> origin Edge -> Harness Adapter
```

For every action:

1. Validate schema, action type, opaque action ID, Task Presence UUID, and the
   device's observed presence revision.
2. Resolve the task only within the receiving feed/paired origin.
3. Confirm that the Task Presence advertises the requested capability.
4. Dispatch only through the owning Edge's registered Harness Adapter capability.
5. Return success only after exact harness capability success.
6. Deduplicate retries by action ID at the origin Edge.
7. Never silently fall back to another app, task, machine, or harness.

Home must never queue an action while Edge/adapter is offline. Fail unavailable
without changing task state. Delayed focus, approval, or response is unsafe.

Acknowledgement is global at the origin Edge. After an exact successful action,
the Edge records the acknowledgement and publishes a new presence revision so
all direct feeds, Home, web UI, and devices converge.

### Dismissal controls

Two Edge-local control messages manage attention without touching a harness.
Both are always available for any Task Presence, are deduplicated by ID like
actions, and never dispatch to a Harness Adapter capability:

- **Acknowledge** dismisses a terminal (`ready`/`blocked`) Task Presence: the
  owning Edge removes it from every Presence Feed. It resurfaces only if the
  Harness Session later leaves and re-enters a terminal state. An exact
  successful Device Capability action on a terminal target (for example
  Focus) also acknowledges it.
- **Snooze** sets aside an input-gated (`needs_input`) Task Presence without
  approving, denying, or answering: it stays in every feed and task list but
  stops claiming the Featured Task until its state or reason next changes. No
  time-based resurface. Accepted v1 limitation: an identical back-to-back
  gate on a snoozed task produces no state or reason change and stays
  snoozed; an adapter-provided opaque attention-epoch counter is the designed
  fix if real usage requires it.

### Focus

Focus is optional. A local Codex adapter may support exact thread focus; a VPS Pi
adapter may be status-only. Tapping a task in the list does not invoke focus.
Tapping the main pet invokes focus only when the Featured Task advertises it;
otherwise show/no-op as unavailable without status side effects.

Keep the router open for later `approve`, `deny`, `respond`, and `dictate`
capabilities. Those require deliberate protocol, authentication, UX, privacy,
and harness-specific tests before being enabled.

## BOX-3 interaction model

Keep one main animated pet and replace the small tray with a larger, scrollable,
touch-friendly task list. Each row includes a pet avatar/current state plus the
safe title.

- Tapping a row makes that Task Presence the **Featured Task** on BOX-3 only.
- Tapping the featured pet invokes Focus only when advertised.
- Browsing never causes a host-side action.
- An explicit dismiss gesture acknowledges a terminal row and snoozes an
  input-gated row. The exact affordance is a firmware interaction decision;
  browsing and row taps alone never dismiss anything.
- Main title, reason, state, and action target all come from the same Featured
  Task.

### Priority and FIFO

Preserve the current state priority:

```text
needs_input > blocked > ready > running > idle
```

Within one rank, order FIFO by the moment a task entered that rank. Repeated
updates within the same state do not change queue position.

- A task entering a strictly higher rank preempts the Featured Task.
- Equal-rank arrivals join the back of that rank's queue.
- When the Featured Task resolves/leaves its rank, feature the oldest remaining
  task at the highest rank.
- Manual row selection temporarily overrides automatic selection.
- A Snoozed Task Presence keeps its rank and queue position but is skipped by
  automatic Featuring until its snooze ends; it can still be featured by
  manual selection.
- The priority queue reclaims the main pet when new urgency arrives according
  to the rules above; manual browsing must not permanently hide new work.

Pet color mappings, custom pet artwork, remote pet uploads, and multiple pets on
the main canvas are explicitly deferred.

## Adapter scope

### Codex

- Preserve honest existing lifecycle fidelity while moving host vocabulary out
  of semantic core.
- Keep App Server enrichment Edge-local and restricted to Codex-owned private
  IDs.
- Focus must resolve the Task Presence through the private Edge registry and
  open only the exact validated Codex thread. Remove app-open fallback.

### Pi

Status-only first:

| Pi signal | Semantic report |
| --- | --- |
| session active / `agent_start` | `running` + `working` |
| `agent_settled` with explicit idle confirmation | `ready` + `completed` |
| explicit reliable failure metadata | optional `blocked` + `failed` |
| extension unload/disconnect | end or lease expiry |

- Prefer Pi's stable session identity only inside the Edge-private mapping.
- Do not transmit session file paths, full cwd, prompts, tool input, transcript
  data, or prompt-derived session names.
- Do not synthesize `needs_input` from tool-name or UI heuristics.
- Pi has no Focus capability until an exact integration is designed and tested.
- Coalesce rapid updates and resend absolute state after reconnect.

### Claude Code

Keep the Harness Adapter contract host-agnostic, but do not implement or claim
Claude support in this handoff. Add it later with access to the real subscription
and documented lifecycle signals. Do not create speculative fixtures that encode
guessed behavior as fact.

## Implementation order

Phases are dependency order, not release gates: the rebuild ships as one
release.

### Phase 1 — successor foundation

1. Adopt the Agentagotchi domain language and rename Codex-specific project
   surfaces where they no longer describe the product.
2. Replace the protocol document with role-separated contracts for local adapter
   IPC, Edge upstream, device feeds, pairing, actions, and administration.
3. Define privacy allowlists and schema/contract tests before networking code.
4. Split semantic core from Edge, Home, adapter, firmware, and UI concerns.

### Phase 2 — Edge semantic core and Codex adapter

1. Implement Edge-owned opaque Task Presence IDs and private native-ID mapping.
2. Store absolute semantic reports only.
3. Add generation/sequence rejection, leases, terminal retention, and global
   acknowledgement.
4. Move Codex lifecycle reduction into its Harness Adapter.
5. Implement the capability router and exact fail-closed Codex focus.
6. Implement dual-mode owner-only local IPC.
7. Keep App Server enrichment Codex-private and Edge-local.

### Phase 3 — Pi and same-machine cross-harness

1. Add a leased Pi adapter with honest status-only mapping.
2. Run Codex and Pi simultaneously through one Edge.
3. Prove UUID isolation, ordering, lease cleanup, independent adapter failure,
   and privacy boundaries.

### Phase 4 — pairing, feeds, and BOX-3 multi-source UI

1. Implement device-code pairing and unique role-scoped credentials.
2. Implement replacement Presence Feed snapshots.
3. Add up to four concurrent authenticated WSS feed connections to firmware.
4. Merge per-pairing snapshots by Task Presence ID/origin revision.
5. Implement the larger scrollable task list, Featured Task behavior, FIFO
   priority, and pet-tap Focus behavior.
6. Validate resource limits and interaction behavior on physical hardware.

### Phase 5 — Home relay and administration

1. Implement one-Home persistence, built-in single-admin bootstrap/login, and
   revocable pairings.
2. Implement outbound Edge-to-Home snapshots and reverse action routing.
3. Add Home-to-device Presence Feeds and safe connectivity/dashboard data.
4. Build the browser administration client.
5. Implement full headless Edge CLI management.
6. Build the optional Native SDK Edge administration client against the same
   admin contract.

### Phase 6 — remote acceptance and hardening

1. Run Pi on a VPS Edge through Home while local Codex/Pi reaches BOX-3 directly.
2. Exercise Home loss, direct Edge loss, adapter death, reconnect, stale replay,
   duplicate delivery, revocation, and uncertain action retries.
3. Threat-model pairing, Home internet exposure, task routing, admin sessions,
   and privacy-safe persistence/logging.
4. Complete release and physical hardware acceptance documentation.

Do not skip semantic/privacy tests to start UI or remote networking early.

## Required test matrix

### Semantic and privacy

- Core state has no Codex/Pi lifecycle vocabulary.
- Sensitive adapter fields are structurally unreachable after sanitization.
- Safe reports contain only allowed task/status/control metadata.
- Native session IDs never cross Edge upstream or feed boundaries.
- Harness/machine routing does not depend on safe titles.

### Identity, ordering, and liveness

- Same native-looking IDs from different adapters cannot collide publicly.
- Stale generation/sequence/revision is ignored.
- Reconnect absolute snapshot repairs dropped updates.
- Adapter and Edge disconnect expire transient presence without inventing task
  completion/failure.
- Terminal notifications follow acknowledgement/retention policy.

### Pairing and authorization

- Codes expire, are one-use, and cannot be replayed.
- Credentials are unique, scoped, revocable, and stored owner-only.
- Edge-publish credentials cannot administer Home or publish as another Edge.
- Feed clients cannot inject Task Presences.
- Revocation disconnects the relationship and blocks reconnect.

### Feed merge and firmware

- One feed snapshot replaces only its pairing contribution.
- Direct local and Home remote tasks appear together.
- Duplicate Task Presence IDs converge by origin revision.
- Disconnect removes only the lost feed's contribution.
- Four simultaneous TLS/WSS feeds meet measured memory/stability/power budgets.
- Task list scrolling, selection, FIFO priority, preemption, and
  dismiss/snooze gestures work on hardware.

### Actions

- Unknown task, stale observation, unavailable capability, wrong origin, and
  offline Edge all fail closed.
- Home cannot reroute to another Edge.
- No action is queued.
- Duplicate action IDs execute at most once.
- Acknowledgement occurs only after exact success and converges globally.
- Status-only Pi tasks remain browsable without advertising Focus.
- Acknowledge removes a terminal presence globally; snooze suppresses
  automatic Featuring until a state or reason change; neither mutates the
  harness.

### End-to-end acceptance

1. Local Codex and Pi report concurrently through one Edge.
2. VPS Pi reports through Home at the same time.
3. BOX-3 displays the union of direct local and Home-relayed presence.
4. A local Focus succeeds only for an exactly supported task.
5. Loss of Home leaves direct local presence working.
6. Loss of local Edge leaves Home-relayed remote presence working.
7. No prompt, command, tool payload, transcript, full path, credential, token,
   private key, or Wi-Fi secret appears in logs, persistence, or status wires.

## Explicitly deferred

- Claude Code adapter
- Approve/deny/respond and other task-mutating controls
- Microphone capture, STT, and dictation
- Pi/terminal focus without an exact supported capability
- Pet color configuration and custom/downloaded pet designs
- Edge-to-Edge or Home-to-Home relaying
- Local Feed Gateway/election unless four-feed hardware profiling fails
- Multi-tenant Home hosting
- Per-device acknowledgement or snooze state (dismissal is Edge-global)
- Time-based snooze resurface (nagging) and adapter attention-epoch counters
- Legacy prototype protocol/runtime compatibility

## Likely repository shape

Use names that express role rather than today's package layout. Exact paths are
an implementation design decision, but preserve these boundaries:

```text
semantic core
edge service
home service
adapters/codex
adapters/pi
pairing/auth
feed protocol
edge-upstream protocol
administration API
edge Native SDK app
home web app
firmware
```

Do not place host-event reduction, network transport, persistence, or UI policy
inside the semantic store.

## Verification

Run repository tests for every source change and build firmware whenever firmware
changes. The current commands remain the baseline until the rebuild replaces
them deliberately:

```sh
make test
idf.py -C firmware build
```

Compilation is not hardware acceptance. Record physical BOX-3 results only in
`docs/HARDWARE_ACCEPTANCE.md`.

Do not install, flash, or provision without explicit user authorization.

## Done when

- One Edge safely aggregates simultaneous Codex and Pi sessions on one machine.
- A VPS Pi Edge relays through Home while BOX-3 simultaneously consumes a direct
  local Edge feed.
- Pairing is explicit, role-scoped, individually revocable, and content-free.
- BOX-3 merges bounded per-pairing snapshots and implements the agreed task list,
  Featured Task, FIFO priority, and optional Focus interaction.
- Home provides authenticated administration and a privacy-safe dashboard.
- Actions route only to exact advertised origin capabilities, are never queued,
  and acknowledge globally only after success.
- Disconnects cannot leave transient tasks running forever.
- Dismissal (acknowledge/snooze) is Edge-global, episode-scoped, and never
  mutates a harness.
- Claude, additional controls, STT, pet customization, mesh/gateway routing, and
  multi-tenancy remain deferred rather than leaking into the rebuild.
