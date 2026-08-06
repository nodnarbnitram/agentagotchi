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
`adapters/pi/` and `home/`):

| Plan requirement | Automated evidence |
| --- | --- |
| Privacy boundary structural exclusion (allowlists, fail-closed schemas) | `internal/contract/contract_test.go`, `home/test/run-tests.mjs` (wire validation) |
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
| Home merge: per-Edge replacement, origin convergence, fail-closed validation | `home/test/run-tests.mjs` |
| Home pairing ceremony parity with Edge | `home/test/run-tests.mjs` |
| Pi adapter: leased session, reconnect absolute resend, offline retention, end | `adapters/pi/test/run-tests.mjs` |
| Pet asset pipeline + RGB565 contract | `tools/test_pet_assets.py` |
| Release contracts (firmware pins, launch agent, sensor build) | `tools/test_release_contracts.py` |
| Firmware sensor math (host-compiled) | `firmware/tests/test_sensor_math.c` |

## Pending physical/operator steps

| Step | Owner | Blocking |
| --- | --- | --- |
| ESP-IDF 5.5.x `idf.py -C firmware build` (rename + multi-feed changes) | operator with IDF | yes, before flash |
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
