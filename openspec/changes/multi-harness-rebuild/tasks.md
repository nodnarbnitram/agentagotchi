# Tasks: Multi-Harness and Remote-Presence Rebuild

Phases follow HANDOFF.md's dependency order (one release, not release gates).
Semantic/privacy tests land before UI or remote networking. Run `make test` for
source changes and `idf.py -C firmware build` when firmware changes. Do not
install, flash, or provision without explicit user authorization.

## 1. Phase 1 — Successor foundation

- [x] 1.1 Adopt Agentagotchi domain language across the repo; rename Codex-specific project, binary, package, UI, and protocol terminology that no longer describes the product
- [x] 1.2 Replace `docs/PROTOCOL.md` atomically with role-separated contracts (adapter IPC, Edge upstream, device feeds, pairing, actions, administration); update host and firmware together, no parallel legacy/new state machines
- [x] 1.3 Define the privacy allowlists (Task Presence ID, Safe Title, generic state/reason, allowlisted capabilities, counts, ordering metadata, timestamps) and write schema/contract tests proving prompts, commands, tool payloads, transcripts, full paths, credentials, private keys, Wi-Fi secrets, and native session IDs are structurally excluded — before any networking code
- [x] 1.4 Split the semantic core into its own module free of harness lifecycle, transport, persistence, and display policy; establish the repo boundaries (semantic core, edge service, home service, adapters/codex, adapters/pi, pairing/auth, feed protocol, edge-upstream protocol, administration API, edge Native SDK app, home web app, firmware)

## 2. Phase 2 — Edge semantic core and Codex adapter

- [x] 2.1 Implement Edge-owned opaque Task Presence ID assignment and the private `{adapter, native session id, capabilities}` mapping
- [x] 2.2 Implement absolute-report storage in the semantic core (upsert/end); no event log
- [x] 2.3 Implement generation/producer-sequence rejection, leases, terminal retention (7-day monotonic TTL + ~200 FIFO), and global acknowledgement/snooze
- [x] 2.4 Move Codex lifecycle reduction into `adapters/codex`; map Codex signals to the shared state/reason vocabulary with honest fidelity and no Codex vocabulary in the core
- [x] 2.5 Implement the Edge capability router and exact fail-closed Codex focus (resolve via private registry, open only the exact validated thread, no app-open fallback)
- [x] 2.6 Implement dual-mode owner-only local IPC (one-shot + leased sessions) with bounded strict frames, owner-only permissions, and no raw-payload logging
- [x] 2.7 Keep App Server enrichment Codex-private and Edge-local

## 3. Phase 3 — Pi and same-machine cross-harness

- [x] 3.1 Implement a leased `adapters/pi` with honest status-only mapping (agent_start→running+working; settled+idle→ready+completed; reliable failure→blocked+failed; unload→end/lease-expiry); no Focus, no inferred needs_input
- [x] 3.2 Run Codex and Pi concurrently through one Edge without separate bridge processes or task collisions
- [x] 3.3 Prove via tests: UUID isolation for identical native IDs across adapters, stale generation/sequence/revision rejection, reconnect absolute-snapshot repair, lease cleanup without fabricated completion/failure, independent adapter failure, and the privacy boundary

## 4. Phase 4 — Pairing, feeds, and BOX-3 multi-source UI

- [ ] 4.1 Implement the device-code Pairing Ceremony with unique, role-scoped, individually revocable credentials (Edge→device, Edge→Home, Home→device); one-use short-lived codes; owner-only credential storage
- [ ] 4.2 Implement complete-replacement Presence Feed snapshots on the Edge/Home→device contract with fail-closed schema identification
- [ ] 4.3 Add up to four concurrent authenticated WSS feed connections to firmware with per-pairing snapshot merge by Task Presence ID/origin revision
- [ ] 4.4 Implement the larger scrollable task list, Featured Task behavior, FIFO priority (`needs_input > blocked > ready > running > idle`) with preemption and manual override, and pet-tap Focus (only when advertised); browsing/row taps never act on the host
- [ ] 4.5 Implement acknowledge/snooze dismiss gestures (terminal→acknowledge, input-gated→snooze); browsing/row taps never dismiss
- [ ] 4.6 Validate four-feed resource limits and interaction behavior on physical BOX-3 hardware; record results in `docs/HARDWARE_ACCEPTANCE.md` (not from compilation)

## 5. Phase 5 — Home relay (Workers + Durable Objects) and administration

- [ ] 5.1 Scaffold the Home Worker project (wrangler) with one Durable Object per Home; the Home has a stable identity independent of process/hostname/infrastructure
- [ ] 5.2 Implement one-Home persistence in Durable Object storage (SQLite): privacy-safe presence model, pairing state, connectivity metadata, role credentials; built-in single-admin bootstrap/login with secure cookie sessions and CSRF protection; pairing credentials are not admin credentials
- [ ] 5.3 Implement Edge ingress: outbound Edge WSS to the Home Durable Object, absolute snapshots (Edge generation + monotonic revision), per-Edge replacement, and WebSocket hibernation for idle connections
- [ ] 5.4 Implement reverse action routing to the owning Edge only; Home never reroutes, invents capabilities, or queues
- [ ] 5.5 Implement Home-to-device Presence Feeds and privacy-safe connectivity/dashboard data (no prompts, transcripts, tool payloads, commands, or filesystem metadata)
- [ ] 5.6 Build the browser administration client as static assets served by the Home Worker (thin client over shared admin APIs/schemas)
- [ ] 5.7 Implement full headless Edge CLI management (bootstrap, pairing, status, revocation, recovery)
- [ ] 5.8 Build the optional Native SDK Edge administration client against the same admin contract (share domain rules/schemas/design, not rendering components)

## 6. Phase 6 — Remote acceptance and hardening

- [ ] 6.1 Run a local Edge paired to Home while local Codex/Pi also reaches BOX-3 directly; verify BOX-3 shows the union of direct and Home-relayed presence and that duplicate Task Presences converge by origin revision
- [ ] 6.2 Exercise failure modes: Home loss (direct local still works), direct Edge loss (Home-relayed still works), adapter death, reconnect, stale replay, duplicate delivery, revocation, and uncertain action retries (fail closed, never queued, deduplicated)
- [ ] 6.3 Threat-model pairing, Home internet exposure, task routing, admin sessions, and privacy-safe persistence/logging
- [ ] 6.4 Verify the privacy invariant end-to-end: no prompt, command, tool payload, transcript, full path, credential, token, private key, or Wi-Fi secret appears in logs, persistence, or status wires
- [ ] 6.5 Complete release and physical hardware acceptance documentation (`docs/RELEASE_VERIFICATION.md`, `docs/HARDWARE_ACCEPTANCE.md`)

## 7. Deferred (do not implement in this change)

- [ ] 7.1 Confirm Claude adapter, approve/deny/respond, microphone/STT, Pi/terminal focus, pet customization, Edge-to-Edge/Home-to-Home relay, multi-tenancy, per-device dismissal, time-based snooze resurface, and legacy compatibility remain deferred and do not leak into the rebuild
- [ ] 7.2 Confirm the VPS Edge remote scenario (Pi on a VPS relaying through Home, VPS deployment validation, and VPS-specific hardening) is deferred to a follow-up change; this change validates Home with local Edges only
