# Agentagotchi protocol

This document is the single source of truth for every wire contract in the
Agentagotchi system. Host and firmware implementations change together; there
is no parallel legacy/new protocol state machine (`docs/adr/0007`).

The system has five contracts, one per boundary:

| Contract | Boundary | Transport | Initiator |
| --- | --- | --- | --- |
| Harness Adapter IPC | Harness Adapter → Edge Bridge | Unix domain socket | Adapter |
| Edge upstream | Edge Bridge → Home Bridge | WSS (outbound) | Edge |
| Presence Feed | Edge/Home → device or client | WSS | Device/client |
| Pairing Ceremony | Any pairing peer | HTTPS/WSS + local admin | Connecting peer |
| Administration | CLI/app/browser → Edge or Home | Local IPC / HTTPS | Admin client |

Device Capability actions ride the Presence Feed (device → Edge/Home) and the
Edge upstream (Home → Edge) and are defined in one place below.

## Shared rules

### Privacy boundary

Only these fields may cross an Edge boundary (upstream, feeds, remote status):

- Task Presence ID (opaque, Edge-assigned UUID)
- Safe Title
- generic `state` / `reason` vocabulary
- allowlisted capability names
- counts and ordering metadata
- timestamps

Prompts, commands, tool payloads, transcripts, full paths, credentials,
tokens, private keys, Wi-Fi secrets, and native harness session IDs never
cross. Native session IDs are Edge-private and are never serialized into any
wire message or shared persistence (`docs/adr/0005`).

### State and reason vocabulary

States: `idle`, `running`, `needs_input`, `ready`, `blocked`.
Reasons: `working`, `question`, `approval`, `permission`, `completed`,
`failed`.

Priority (Featured Task ordering): `needs_input > blocked > ready > running >
idle`.

### Identity and ordering

Every Task Presence has an opaque **Task Presence ID** (UUID, Edge-assigned at
first observation, stable across reconnects). Every presence message carries
**ordering metadata**:

- `originEdgeId` — stable Edge identity
- `originGeneration` — increments each time the owning Edge restarts its
  presence store
- `originRevision` — strictly monotonic per generation
- `producerId`, `producerSeq` — per-adapter-sequence for adapter-originated
  reports

Receivers reject stale, replayed, or out-of-order reports and repair gaps by
discarding and resynchronizing from the next absolute snapshot
(`docs/adr/0004`). There are no event-log replays and no queued mutations
(`docs/adr/0006`).

### Safe Title

A Safe Title is an allowlisted harness name plus an optional user-approved
Edge alias, bounded (≤ 64 bytes after whitespace normalization) and sanitized.
It is never derived from hostnames, paths, session names, prompts,
transcripts, or commands. Routing never depends on it.

### Schema identification

Every message carries a `schema` string, e.g. `"agentagotchi.feed.v1"`.
Receivers fail closed: an unknown or missing `schema` rejects the message.
Bump the schema version for any breaking change.

## Harness Adapter IPC

Owner-only Unix domain socket on the Edge machine. The socket is created with
`0600` permissions in the Edge data directory (itself `0700`). No raw payload
is ever logged.

Two modes share one frame format (newline-delimited JSON, max frame 64 KiB,
strict field decoding — unknown fields reject the frame):

**One-shot mode** (Codex hooks): connect, send one `hook_event`, close.

**Leased session mode** (persistent adapters such as Pi): connect, send
`adapter_hello`, then stream absolute `presence_report` messages. The Edge
assigns a lease; reports reference the lease. Disconnect or lease expiry ends
all Task Presences owned by that lease without fabricating completion or
failure.

### Messages (adapter → Edge)

```json
{"schema":"agentagotchi.ipc.v1","type":"hook_event","eventId":"…","harness":"codex","nativeSessionId":"…","event":"Stop","toolName":"…","turnId":"…","agentId":"…","at":"…"}
```

```json
{"schema":"agentagotchi.ipc.v1","type":"adapter_hello","harness":"pi","adapterVersion":"…","capabilities":[]}
```

```json
{"schema":"agentagotchi.ipc.v1","type":"presence_report","leaseId":"…","producerSeq":7,"reports":[{"nativeSessionId":"…","displayKey":"…","state":"running","reason":"working","subagentCount":0}],"ends":["…"]}
```

`presence_report` is an absolute report per native session (upsert); `ends`
lists native session IDs that no longer exist. `displayKey` is Edge-private
dedup/routing metadata, never relayed.

### Messages (Edge → adapter)

```json
{"schema":"agentagotchi.ipc.v1","type":"hello_ack","leaseId":"…","leaseSeconds":30}
```

```json
{"schema":"agentagotchi.ipc.v1","type":"action_request","actionId":"…","capability":"focus","taskPresenceId":"…"}
```

`action_request` is sent only to adapters that advertised the capability, and
only on a leased session (never one-shot). The adapter answers
`action_result` (`ok` | `rejected` | `unsupported`).

## Edge upstream (Edge → Home)

The Edge dials outbound WSS to the Home (`wss://<home>/edge/v1`), presenting
its Edge→Home pairing credential as a bearer token. After authentication the
Edge sends complete-replacement **upstream snapshots**:

```json
{"schema":"agentagotchi.upstream.v1","type":"snapshot","edgeId":"…","generation":3,"revision":41,"snapshotGeneratedAt":"…","tasks":[{"taskPresenceId":"…","safeTitle":"…","state":"…","reason":"…","subagentCount":0,"capabilities":["focus"],"updatedAt":"…","snoozed":false}],"counts":{"needsInput":0,"blocked":0,"ready":0,"running":0},"aggregateState":"idle"}
```

Semantics:

- The Home replaces only that Edge's prior contribution (`docs/adr/0004`);
  other Edges' contributions are untouched.
- `generation` + `revision` implement stale/replay rejection; a gap or reset
  discards and resynchronizes from the newest absolute snapshot.
- The Home never reroutes actions to a different Edge, invents capabilities,
  or queues actions (`docs/adr/0006`).

Reverse routing (Home → Edge on the same connection):

```json
{"schema":"agentagotchi.upstream.v1","type":"action_request","actionId":"…","capability":"focus","taskPresenceId":"…","seenRevision":41}
```

The Edge answers `action_result` with the same `actionId`.

## Presence Feed (Edge/Home → device or client)

WSS at `wss://<host>/feed/v1` (Edge direct, or Home relay). The client
presents its feed pairing credential as a bearer token before upgrade. On
connect, and on every change, the server sends a complete-replacement
snapshot:

```json
{"schema":"agentagotchi.feed.v1","type":"snapshot","origin":{"kind":"edge|home","id":"…","generation":3,"revision":41},"generatedAt":"…","aggregateState":"needs_input","counts":{"needsInput":1,"blocked":0,"ready":0,"running":0},"tasks":[{"taskPresenceId":"…","safeTitle":"…","state":"needs_input","reason":"permission","subagentCount":1,"capabilities":["focus"],"updatedAt":"…","snoozed":false}]}
```

A Home feed merges each connected Edge's contribution keyed by Task Presence
ID and orders by `originGeneration`/`originRevision`; duplicate Task
Presences arriving directly and relayed converge by origin revision.

Device Capability action (device → feed server):

```json
{"schema":"agentagotchi.feed.v1","type":"action","actionId":"…","capability":"focus","taskPresenceId":"…","seenRevision":41}
```

Rules (fail-closed):

- `taskPresenceId` must be a canonical UUID present in the last snapshot the
  client received (`seenRevision` guards staleness).
- The capability must be advertised on that Task Presence.
- The action resolves the owning task and registered capability, dispatches,
  and acknowledges only on exact success — no silent fallback that opens the
  wrong host app, no queueing, no retry storms.
- Result: `{"schema":"agentagotchi.feed.v1","type":"action_result","actionId":"…","status":"ok|stale|unsupported|failed"}`

`focus` is the only Device Capability in this version.

## Pairing Ceremony

One short-lived, one-use device-code state machine for all three pairing
directions (Edge→device, Edge→Home, Home→device), with role-specific grants.

1. Connecting peer displays or transmits a one-use code (≥ 128-bit entropy,
  ≤ 10-minute TTL, single use).
2. An authenticated administrator on the receiving service approves the code
  via that service's administration surface.
3. The service issues a unique, revocable, role-scoped credential (bearer
  token, ≥ 256-bit entropy) bound to exactly one role: `feed`, `edge-ingress`,
  or `admin`.

Pairing credentials are not administrator credentials. Administrator sessions
are issued only by the built-in single-admin bootstrap/login and never
persisted onto paired clients. Credentials are stored owner-only (`0600`),
transmitted only inside the pairing flow, and revoked individually by ID.
Revocation takes effect on the affected connection immediately.

```json
{"schema":"agentagotchi.pairing.v1","type":"code_request","role":"feed","clientName":"BOX-3"}
{"schema":"agentagotchi.pairing.v1","type":"code_status","codeId":"…","status":"pending|approved|expired|consumed"}
{"schema":"agentagotchi.pairing.v1","type":"credential","credentialId":"…","token":"…","role":"feed","expiresAt":null}
```

## Administration

The Edge exposes administration over its local IPC socket; the Home exposes it
over authenticated HTTPS. Both share the same API shapes and validation
schemas so the Edge CLI, the optional Edge Native SDK app, and the Home
browser client are thin clients (domain rules and authorization live in the
services).

```json
{"schema":"agentagotchi.admin.v1","type":"status"}
{"schema":"agentagotchi.admin.v1","type":"pairing_list"}
{"schema":"agentagotchi.admin.v1","type":"pairing_revoke","credentialId":"…"}
{"schema":"agentagotchi.admin.v1","type":"pairing_approve","codeId":"…"}
{"schema":"agentagotchi.admin.v1","type":"alias_set","taskPresenceId":"…","alias":"…"}
```

Status responses contain connectivity state, pairing counts, Task Presence
counts, and timestamps — never prompts, transcripts, tool payloads, commands,
or filesystem metadata.

## RGB565 pet asset

`pet_device_rgb565.bin` starts with a 16-byte little-endian header:

| Offset | Type | Meaning |
| --- | --- | --- |
| 0 | 4 bytes | ASCII `AGOT` |
| 4 | uint16 | Asset version, `1` |
| 6 | uint16 | Width, `96` |
| 8 | uint16 | Height, `72` |
| 10 | uint8 | State count, `5` |
| 11 | uint8 | Frames per state, `8` |
| 12 | uint32 | Pixel-data offset, `16` |

Frames follow as little-endian RGB565 in state order `idle`, `running`,
`needs_input`, `ready`, `blocked`, matching the firmware enum. Each state has
eight 96×72 frames intended for a 100 ms cadence. Transparent source pixels
are flattened to the UI background color `rgb(11, 21, 27)` before RGB565
conversion.

## Device local sensor merge

The firmware merges local measurements into the displayed snapshot as a
`device` object (`temperatureC`, `humidityRh`, `batteryVoltage`,
`batteryPercent`, `batteryEstimate`, `presence`, `wifiRssi`,
`sensorUpdatedAt`). This is local state, never transmitted back. A missing or
stale value is the absence of its numeric field, displayed as `—`.
`sensorUpdatedAt` is Unix epoch seconds, valid only after SNTP
synchronization; monotonic uptime drives stale/presence calculations and is
never exposed as an epoch timestamp. Provisioning supplies a one-time
lower-bound clock bootstrap solely so TLS validation works after an offline
reboot.
