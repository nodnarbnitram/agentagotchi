# Agentagotchi

Agentagotchi is a multi-harness agent presence system. Coding agents (Codex
today; Pi read-only; more later) report privacy-filtered **Task Presence** to a
local **Edge Bridge**, which owns authority over its own tasks and fans a
sanitized feed out to paired devices. An optional **Home Bridge** (Cloudflare
Worker + Durable Objects) merges multiple Edges into one remote feed without
ever gaining task authority. An ESP32-S3-BOX-3 renders the feed: one glanceable
pet, a priority-sorted task tray, and deliberate device actions (Focus where
the owning harness advertises it; dismiss → acknowledge/snooze).

The system is privacy-first: only opaque Task Presence IDs, bounded Safe
Titles, generic state/reason, allowlisted capabilities, counts, ordering
metadata, and timestamps may cross the owning Edge. Prompts, commands,
transcripts, full paths, and native session IDs never leave the Edge that owns
them. Actions are never queued; uncertainty fails closed and resolves by
retrying against fresh state.

## Repository map

| Path | Component | Stack |
| --- | --- | --- |
| `cmd/`, `internal/` | Edge Bridge (`agentagotchi` binary), semantic core, Codex adapter, feed/pairing/admin services | Go |
| `packages/pi-adapter/` | Pi harness adapter (status-only presence) | TypeScript (Pi extension) |
| `packages/home/` | Home Bridge Worker + Durable Object + admin UI | TypeScript (Cloudflare Workers) |
| `firmware/` | BOX-3 firmware | C, ESP-IDF 5.5.x |
| `clients/macos-admin/` | macOS admin client for the Edge | Swift (SwiftUI) |
| `plugin/` | Codex status plugin (hook wiring) | Codex plugin |
| `assets/`, `tools/` | Pet artwork source + generators, release contract tests | Python |
| `docs/` | Protocol, threat model, release/hardware verification | — |
| `openspec/` | Change-driven specs (source of truth for behavior) | — |
| `scripts/` | Install/provisioning helpers | sh |
| `release/firmware/` | Versioned flash bundle (binaries + lock + hashes) | — |

JavaScript/TypeScript packages are npm workspaces rooted at the repo
(`npm install` at the root installs all `packages/*`).

## Status behavior

Tasks sort `Needs input > Blocked > Ready > Running > Idle`. Tapping the pet
requests Focus for the featured task **only when its owning harness advertises
the Focus capability** (Codex does; Pi does not). Long-press dismisses the
featured task: terminal tasks are acknowledged away, `needs_input` tasks are
snoozed for ten minutes. A new transition into `Needs input` plays one soft
chirp; reconnects and sensor refreshes do not repeat the alert.

The SENSOR dock behavior is intentionally low-frequency:

| Signal | Interface | Update behavior |
| --- | --- | --- |
| AHT30 temperature/humidity | I²C, SCL GPIO40 / SDA GPIO41 | Every 30 seconds; CRC checked; last good value expires after 5 minutes |
| 18650 voltage | Calibrated ADC1 channel 9, GPIO10 | 64-sample average every 60 seconds plus low-pass filtering |
| Radar presence | GPIO21 interrupt | 200 ms edge debounce; stays present for 30 seconds |
| Wi-Fi | Station RSSI | Updated every 5 seconds |

## Prerequisites

- macOS on Apple silicon with Go 1.22+ and Node 20+ (Home/Pi packages).
- The Codex desktop app; set `AGENTAGOTCHI_CODEX_BIN` for a custom location.
- [ESP-IDF 5.5.x](https://docs.espressif.com/projects/esp-idf/en/v5.5.1/esp32s3/get-started/index.html)
  activated when building or flashing firmware.
- Python 3 with `venv` support; `make test` creates the pinned dev
  environment under the ignored `work/venv` on first run.
- One ESP32-S3-BOX-3 attached to an ESP32-S3-BOX-3-SENSOR dock.

Use only a compatible 18650 cell and observe polarity. The battery percentage
is a voltage-derived estimate, not a fuel-gauge measurement.

## Build and test

```sh
make test          # Go suite + firmware host tests + asset/release contracts + plugin validation
npm install        # workspace deps for packages/*
npm test           # Home Bridge + Pi adapter test suites
make build-host      # work/bin/agentagotchi
make macos-admin     # work/macos-admin/AgentagotchiAdmin.app (optional admin client)
idf.py -C firmware build
make assets        # regenerate shared pet artwork after asset changes
```

## Install the Mac Edge Bridge

```sh
scripts/install-macos.sh
```

Builds an arm64 binary, installs it under `~/Library/Application Support/Agentagotchi`,
copies the Codex hook plugin to `~/plugins/agentagotchi-status`, and loads
`com.agentagotchi.edge` as a LaunchAgent. Restart Codex after installing or
updating hooks, then open `/hooks` and trust all nine Agentagotchi hooks.

The installer also builds and installs the optional macOS admin app
(`clients/macos-admin`) as `AgentagotchiAdmin.app` under the app support
directory when a Swift toolchain is present. Open it from there to manage
pairing; the bundle is unsigned and intended as a local owner tool.

The Edge creates these private local files with owner-only permissions:
`identity.json` (random bearer token), `bridge-cert.pem`/`bridge-key.pem`
(self-signed LAN identity), and `state.json` (generic task state only).
The private key, Wi-Fi password, prompts, commands, transcripts, and tool
payloads are never logged.

## Flash and provision

Put Wi-Fi credentials in the ignored project-root `.env`
(`WIFI_SSID=…`, `WIFI_PASSWORD=…`), connect the BOX-3 over USB, then:

```sh
make provision-env
```

The script sends the password over stdin and never prints it. Set
`AGENTAGOTCHI_SERIAL` when more than one serial device is connected, and
`AGENTAGOTCHI_TEMP_UNIT=C` for Celsius. Bonjour advertises
`_agentagotchi._tcp`; the stored hostname/port is the fallback.

## Privacy and authority boundary

The Edge uses official Codex hooks for lifecycle changes and the read-only
Codex App Server for metadata/outcome. It never starts, resumes, answers,
approves, or modifies a task. Device actions are capability-gated and
validated per action against fresh state; the only mutating action today is
Focus, which opens a validated canonical deep link.

See [docs/PROTOCOL.md](docs/PROTOCOL.md) for the wire formats,
[docs/THREAT_MODEL.md](docs/THREAT_MODEL.md) for the security model,
[docs/HARDWARE_ACCEPTANCE.md](docs/HARDWARE_ACCEPTANCE.md) for the hardware
checklist, and [docs/RELEASE_VERIFICATION.md](docs/RELEASE_VERIFICATION.md)
for the automated-vs-physical coverage split.
