# Threat Model — multi-harness rebuild

Scope: pairing ceremony, Home internet exposure, task routing, admin sessions,
and privacy-safe persistence/logging. This document supports the
`multi-harness-rebuild` change; the privacy boundary is a product invariant.

## Assets and trust boundaries

Assets:

1. Task content privacy — prompts, transcripts, tool payloads, commands, full
   paths, native session IDs.
2. Credentials — pairing tokens, the admin password/session, TLS private
   keys, Wi-Fi credentials (device-side only).
3. Action authority — the ability to focus a host application through a
   Device Capability.

Boundaries:

- **Owner-only IPC** (adapter→Edge, admin→Edge): Unix socket, 0600 in a 0700
  data directory. Trusted: local harness adapters, the owner.
- **Edge feed** (Edge→device): LAN WSS, pinned self-signed cert + bearer
  credential. Trusted after pairing: the BOX-3.
- **Edge→Home upstream**: outbound WSS with publicly trusted TLS +
  edge-ingress credential. Trusted after pairing: the Home.
- **Home feed** (Home→device): internet-facing WSS + feed credential.
- **Home admin**: internet-facing HTTPS + cookie session + CSRF.

## Threats and mitigations

### Pairing ceremony

- *Code interception/replay*: codes are one-use, 128-bit, ≤10-minute TTL;
  redemption consumes the code even on success; unapproved codes grant
  nothing. Codes are displayed on the connecting client and approved by the
  authenticated admin — an attacker who only sees the network cannot approve.
- *Credential theft*: credentials are 256-bit random, transmitted once inside
  the pairing flow, stored owner-only (0600/0700), redacted in every list and
  status output. Revocation disconnects live connections (Edge hub token
  sweep; Home attachment-token sweep).
- *Role escalation*: the ceremony issues only `feed` and `edge-ingress`
  roles; `admin` is never issuable. Feed credentials presented for admin or
  ingress paths fail role checks. Pairing credentials are never admin
  credentials.

### Home internet exposure

- *Unauthenticated access*: every non-admin surface requires a bearer
  credential validated against the ceremony; admin surfaces require the
  cookie session. Discovery never authorizes (docs/PROTOCOL.md).
- *Admin brute force*: bootstrap requires a deploy-time secret
  (`ADMIN_BOOTSTRAP_TOKEN`); login compares SHA-256(password) with a
  per-deployment constant — see "Remaining risks" for rate-limiting.
- *Session hijacking*: `HttpOnly; Secure; SameSite=Strict` cookies, 12-hour
  TTL, CSRF token required on every state-changing call.
- *Durable Object isolation*: one DO per Home; all state is scoped to that
  DO's storage — no cross-Home access path exists in the protocol.

### Task routing (actions)

- *Wrong-host focus*: the router resolves `(taskPresenceId, capability)` to
  the Edge-private `(adapter, nativeSessionId)` mapping and dispatches only
  the registered handler — no app-open fallback exists (docs/adr/0006). The
  Home reverse-routes only to the owning Edge connection; it cannot reroute,
  invent capabilities, or queue.
- *Stale/replayed actions*: `seenRevision` must equal the current revision;
  unknown tasks report `stale`; action IDs deduplicate (at-most-once
  execution). Actions are never queued while offline.
- *Malicious capability names*: the registry is a closed set (`focus` only);
  unknown capabilities are rejected at attach, report, and validation.

### Privacy-safe persistence and logging

- *Content leakage into wire payloads*: wire structs are allowlist-only by
  construction (Go `internal/contract`, TS `home/src/wire.ts`); both runtimes
  fail closed on unknown fields and unknown schemas. Structural tests prove
  private fields cannot be serialized.
- *Leakage into persistence*: Edge persists generation + aliases + pairing
  credentials + TLS identity; Home persists pairing state + admin hash. No
  prompts, transcripts, tool payloads, commands, or paths are ever persisted.
- *Leakage into logs*: the hook path decodes and discards without logging
  raw or decoded payloads; App Server stderr is discarded; pairing tokens are
  never logged. The debug/trace paths do not print frame contents.

### Adapter trust

- *Rogue local adapter*: IPC is owner-only (0600 socket); a process that can
  write the socket is already the owner. Adapters can only create Task
  Presences, not read others' private mappings or dispatch actions.
- *Adapter death*: leased sessions expire (monotonic); owned presences end
  without fabricated completion.

## Remaining risks (accepted or follow-up)

1. **Home admin login has no rate limit** — mitigated by a ≥12-char password
   and a deploy-secret bootstrap gate; add DO-alarm-based throttling in a
   follow-up before broad exposure.
2. **Password hashing is unsalted SHA-256** — sufficient against casual
   disclosure of the DO store, but a memory-hard KDF (scrypt/argon2 via a
   Workers-safe implementation) is the follow-up.
3. **LAN feed uses pinned self-signed TLS** — pinning happens at provisioning;
   a LAN MITM before provisioning is out of scope (physical access).
4. **Firmware validation of four-feed resource limits is pending physical
   hardware acceptance** (docs/HARDWARE_ACCEPTANCE.md) — compilation is not
   acceptance.
5. **wrangler deploy and provisioning are operator actions** — they run only
   with explicit authorization, keeping credentials out of automation logs.
