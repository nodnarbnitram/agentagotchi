# Release verification

Verification date: 2026-08-05 (automated suite; multi-harness rebuild)

Host environment: macOS 15.x arm64, Go 1.22+, Node 24, Python 3.12+.

This record distinguishes automated source checks from physical acceptance.
Passing the automated suite does not imply that timing, sensor accuracy,
audio, touch, battery behavior, four-feed resource limits, or long-duration
stability have been validated on an ESP32-S3-BOX-3. Physical acceptance is
recorded in `docs/HARDWARE_ACCEPTANCE.md` only after real-kit runs.

## Automated coverage

All of the following run under `make test` (plus `node test/run-tests.mjs` in
`packages/pi-adapter/` and `packages/home/`):

| Plan requirement | Automated evidence |
| --- | --- |
| Privacy boundary structural exclusion (allowlists, fail-closed schemas) | `internal/contract/contract_test.go`, `packages/home/test/run-tests.mjs` (wire validation) |
| Semantic core: opaque IDs, absolute reports, ordering, leases, retention, ack/snooze, capability registry | `internal/presence/presence_test.go` |
| Codex lifecycle reduction (no Codex vocabulary in core) | `internal/adapters/codex/reduce_test.go` |
| Hook sanitizer drops prompts/transcripts/tool input/cwd | `internal/adapters/codex/hook_test.go` |
| Exact fail-closed Codex focus (no app-open fallback) | `internal/adapters/codex/focus_test.go`, `internal/edge/router_test.go` |
| Feed integration: snapshot, action dispatch, unsupported capability | `internal/edge/feed_integration_test.go` |
| Hook-originated presence never leaks native ID/workspace | `internal/edge/feed_integration_test.go` |
| Concurrent Codex+Pi through one Edge, identical native IDs isolated | `internal/edge/concurrent_integration_test.go` |
| Independent adapter failure (Pi lease expiry leaves Codex) | `internal/edge/concurrent_integration_test.go` |
| Pairing ceremony: one-use codes, scoped credentials, revocation disconnect, secrets never in status | `internal/pairing/pairing_test.go`, `internal/edge/pairing_integration_test.go` |
| Dismissal actions (acknowledge/snooze) state-gated, fail-closed, converging | `internal/edge/dismissal_test.go` |
| Edge→Home upstream: absolute resync, change push, reverse action, auth-failure backoff | `internal/edge/upstream_integration_test.go` |
| Home merge: per-Edge replacement, origin convergence, fail-closed validation | `packages/home/test/run-tests.mjs` |
| Home pairing ceremony parity with Edge | `packages/home/test/run-tests.mjs` |
| Pi adapter: leased session, reconnect absolute resend, offline retention, end | `packages/pi-adapter/test/run-tests.mjs` |
| Pet asset pipeline + RGB565 contract | `tools/test_pet_assets.py` |
| Release contracts (firmware pins, launch agent, sensor build) | `tools/test_release_contracts.py` |
| Firmware sensor math (host-compiled) | `firmware/tests/test_sensor_math.c` |

## Local Edge→Home acceptance (wrangler dev) — 2026-08-05

This was the software-path run only. Everything ran on loopback; no
`wrangler deploy`, cloud resource, external write, BOX-3, or physical feed was
used. Redacted frames and logs are in `work/e2e-2026-08-05/`.

| Check | Result | Commands / evidence |
| --- | --- | --- |
| Home install and local Worker + Durable Object startup | **PASS** | `(cd home && npm install)`; `(cd home && npx wrangler dev --port 18901)`; `home-wrangler.log` shows `Ready on http://localhost:18901`; admin/DO routes returned 200/101. |
| Admin bootstrap, login, cookie, and CSRF | **PASS** (with local eviction caveat below) | `curl -X POST /admin/bootstrap`; `curl -X POST /admin/login`; `curl GET /admin/api/status`; `admin-bootstrap-login.txt`. |
| Edge-ingress Pairing Ceremony and credential redaction | **PASS** | `curl -X POST /pairing/code`, `GET /admin/api/pairing/pending`, `POST /admin/api/pairing/approve`, `POST /pairing/redeem`, `GET /admin/api/pairing/list`, `GET /admin/api/status`; `pair-edge-*.json`, `pairing-list-final-safe.json`. Credential token appeared only in the redeem response and was absent from list/status. |
| Feed Pairing Ceremony, role scope, and one-use code | **PASS** | The same pairing commands with role `feed`; replay `POST /pairing/redeem` returned 403; `pair-feed-*.json` and `pair-feed-replay.txt`. |
| Loopback `ws://` Edge upstream allowance, with non-loopback rejection | **PASS** | Added `internal/edge/upstream_dial_test.go`; `GOCACHE=$(pwd)/work/gocache go test ./internal/edge -run 'TestDialWSS|TestIsLoopbackHost' -count=1` passed. `wss://` remains the remote-only path. |
| Edge paired to Home and Home status shows a connected Edge | **PASS** | `go run ./cmd/agentagotchi serve --data-dir /tmp/agot-e2e.XXX --home-url ws://127.0.0.1:18901/edge/v1 --home-token <redeemed-credential>`; `go run ./cmd/agentagotchi status`; `home-status-edge-fresh.json` / `home-status-terminal-ready.json`. |
| Hook-originated ready/completed presence relayed to Home | **PASS** | `printf '<SessionStart JSON>' | go run ./cmd/agentagotchi hook --data-dir /tmp/agot-e2e.XXX`, followed by a `Stop` payload; `home-status-terminal-ready.json` contains only `agentagotchi.feed.v1` allowlisted fields. |
| Privacy scan of captured feed/upstream frames and Home/Edge logs | **PASS** | `grep -RFn` for the native session UUID, `/Users/x/secret-project`, and prompt marker across `feed-frames*.jsonl`, `*-frame.json`, `home-wrangler.log`, and `edge.log` returned no matches. |
| Home feed snapshot and allowlisted task projection | **PASS** | Node `ws` client from `node_modules/ws` connected to `ws://127.0.0.1:18901/feed/v1`; `feed-frames.jsonl` received `schema: agentagotchi.feed.v1`, terminal task, and only allowlisted task keys. |
| Exact prescribed reverse dismissal (`seenRevision` copied from Home feed) | **FAIL → FIXED, re-run PASS** | First run failed `stale`: devices only see the Home's merged revision (per-task origin revisions are stripped by the privacy projection), but the Edge validated against its own sequence. Fix: Home fails fast on a device-stale view, then translates `seenRevision` to the owning Edge's last-known revision before forwarding (`home-do.ts`, `presence.originRevisionOf`). |
| Reverse route convergence after dismissal | **FAIL → FIXED, re-run PASS** | Home-relayed dismissals/focuses mutated the Edge core but never emitted the change signal, so no fresh snapshot flowed upstream and focus-success did not acknowledge. Fix: `UpstreamClient.SetOnChange(s.signal)` + acknowledge-on-ok mirroring the direct path (`upstream.go`); tests `TestHomeRelayedDismissalConverges`, `TestHomeRelayedFocusAcknowledgesTerminal`. |
| Action-result schema tag on relayed results | **FAIL → FIXED** | Relayed `action_result` reached the device tagged `agentagotchi.upstream.v1`; Home now re-tags to `agentagotchi.feed.v1`. |
| Home restart / DO eviction continuity | **FAIL → FIXED, re-run PASS** | Presence contributions and admin sessions were in-memory only; a DO restart wiped presence while the Edge believed itself connected. Fix: presence model + sessions persisted to DO storage per spec 5.2 (`presence.dump/load`, `persist()` on every mutation); verified by bouncing `wrangler dev` mid-session — `edges=1 tasks=1` survived, Edge re-synced (rev advanced). |
| Edge-ingress identity binding | **FAIL → FIXED** | Home required `snapshot.edgeId == credential.clientName`, closing any connection whose pairing label differed from the Edge's cert-derived ID. Fix: credential authenticates the connection; the first valid snapshot binds the Edge's stable `edgeId`; mid-stream identity change fails closed. PROTOCOL.md updated. |
| Dismissal re-verification (acknowledge + snooze + stale rejection) | **PASS** | Live `wrangler dev` re-run: `acknowledge` with the Home feed revision → `ok` + converged snapshot with task removed; `snooze` → `ok` + task remains `snoozed=true`; stale-revision action → `stale`. Frames: `work/e2e-2026-08-05-dismissal/`. |
| Revocation and direct-feed independence | **PASS / FAIL** | `POST /admin/api/pairing/revoke`, then `grep 'GET /edge/v1 401 Unauthorized' home-wrangler.log` and `GET /admin/api/status`: Home had no Edge or relayed tasks (`home-status-after-revocation.json`). The local Edge status/direct WSS feed remained available (`edge-status-after-revocation.txt`, `direct-edge-feed-frame.json`). **FAIL:** the current Edge logger emits no reconnect/auth-failure line (`revocation-log-observation.txt`); the Home-side 401 proves the revoked reconnect was rejected. |
| Go/Home automated checks after the run | **PASS / FAIL** | `GOCACHE=$(pwd)/work/gocache go test ./...`, `go vet ./...`, `npm test`, and `npm run types` passed. The lock-version test now accepts any `5.5.x` (`f5034d3`); `make test` fully green. |

During the run, local DO execution evicted the in-memory admin session after
some persistence operations; re-login was required before subsequent admin
calls. Admin password/CSRF/cookies stayed owner-readable under `/tmp` and were
not placed in evidence. All issues found in this run (session continuity, presence persistence,
reverse-action revision translation, convergence signaling, schema tagging,
ingress identity binding) were fixed and re-verified on 2026-08-05; see the
rows above.

**Physical BOX-3 direct-feed validation remains pending. Task 6.1 stays open:**
the software path did not validate the physical device's direct + Home-relayed
union, duplicate convergence by origin revision, or four-feed behavior.

## Firmware build validation — 2026-08-05 (ESP-IDF v5.5.1)

`idf.py -C firmware build` on the four-feed rework: **clean build, 0
warnings**. Found and fixed one real resource-limit defect before flash:
`dram0_0_seg` overflowed by 37,688 B (4x feed slots embedded ~88 KB of RX
buffers + per-feed snapshots in static `.bss`). Fix: per-slot array allocated
from PSRAM heap (`f5034d3`). Headroom after fix: DIRAM 291,375/341,760 B
used (85.26%, 50,385 B free), IRAM 100% (code, unchanged), app partition
50% free. This validates compile-time resource limits only; on-device
behavior under four live feeds remains pending physical acceptance.

## Pending physical/operator steps

| Step | Owner | Blocking |
| --- | --- | --- |
| ESP-IDF 5.5.x `idf.py -C firmware build` (rename + multi-feed changes) | ~~operator with IDF~~ **DONE 2026-08-05** | no |
| Single-feed physical boot/feed/interaction acceptance on BOX-3 | ~~operator with kit~~ **PARTIAL 2026-08-06** | boot+feed done; interaction pending |
| Multi-feed (4-slot) physical acceptance — needs slot 1-3 provisioning flow | follow-up change | yes |
| `release/firmware/` bundle refresh (binaries, lock, hashes, BUILD.md together) | operator with IDF | yes, before release |
| Four-feed resource limits + interaction acceptance on BOX-3 | operator with kit | yes (task 4.6) |
| Local Edge→Home acceptance with BOX-3 (direct + relayed convergence) | operator with kit + deployed Home | yes (task 6.1) |
| Home deployment (`wrangler deploy`, explicit authorization) | operator | yes (task 6.1) |
| Failure-mode physical exercise (Home loss, Edge loss, adapter death on kit) | operator with kit | yes (task 6.2 physical part) |
| Long-duration stability on kit | operator with kit | recommended |

## Operator notes

- `wrangler deploy`, flashing, and provisioning run only with explicit user
  authorization; credentials stay on stdin and out of logs.
- The Home admin login currently has no rate limit and uses unsalted
  SHA-256 password hashing — see `docs/THREAT_MODEL.md` "Remaining risks"
  before broad internet exposure.
