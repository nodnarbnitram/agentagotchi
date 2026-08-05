# Agentagotchi for ESP32-S3-BOX-3

Agentagotchi turns one ESP32-S3-BOX-3 plus the SENSOR dock into a local status
companion for Codex. It shows the highest-priority task, aggregate task counts,
subagent count, and a large animated pet with a desktop-style speech bubble.
The bubble measures each message, expands to nearly the full display width,
and wraps long task titles across as many as three lines.
The 20-pixel status bar adds Wi-Fi,
approximate 18650 charge, temperature, humidity, and radar presence without
putting sensor work on the LVGL task.

The implementation has three pieces:

- `agentagotchi serve`: an arm64 macOS launch agent that reduces privacy-filtered
  Codex hook events, reads task metadata from a read-only Codex App Server
  subprocess, and serves an authenticated certificate-pinned WSS connection.
- `agentagotchi hook`: the hook receiver. It discards prompts, commands, tool
  input/output, transcripts, assistant output, and full paths before emitting
  an event.
- ESP-IDF firmware: independent networking, sensor, audio, and LVGL work queues
  for the BOX-3 and SENSOR dock.

## Status behavior

Tasks are sorted by `Needs input > Blocked > Ready > Running`. Tapping the pet
asks macOS to focus the highest-priority Codex task. Tapping the count row opens
the task tray. A new transition into `Needs input` plays one soft chirp; the
firmware compares task transitions so reconnects and sensor refreshes do not
repeat the alert.

The SENSOR dock behavior is intentionally low-frequency:

| Signal | Interface | Update behavior |
| --- | --- | --- |
| AHT30 temperature/humidity | I²C, SCL GPIO40 / SDA GPIO41 | Every 30 seconds; CRC checked by the official driver; last good value expires after 5 minutes |
| 18650 voltage | Calibrated ADC1 channel 9, GPIO10 | 64-sample average every 60 seconds plus low-pass filtering; 301 kΩ / 100 kΩ divider; approximate Li-ion curve |
| Radar presence | GPIO21 interrupt | 200 ms edge debounce; stays present for 30 seconds after detection |
| Wi-Fi | Station RSSI | Updated every 5 seconds |

No charging icon is shown because the dock does not route a readable charger
status signal to the ESP32. IR, microSD, microphone use, automatic
presence-dimming, OTA, and the second BOX-3 are outside v1.

The GPIO assignments and electrical assumptions follow Espressif's
[BOX-3 hardware overview](https://github.com/espressif/esp-box/blob/master/docs/hardware_overview/esp32_s3_box_3/hardware_overview_for_box_3.md)
and
[SENSOR dock schematic](https://github.com/espressif/esp-box/blob/master/hardware/SCH_ESP32-S3-BOX-3_V1.0/SCH_ESP32-S3-BOX-3-SENSOR-01_V1.1_20230922.pdf).

## Prerequisites

- macOS on Apple silicon with Go 1.22 or newer.
- The Codex desktop app installed in `/Applications/ChatGPT.app` or
  `/Applications/Codex.app`; set `AGENTAGOTCHI_CODEX_BIN` for a custom location.
- [ESP-IDF 5.5.5](https://docs.espressif.com/projects/esp-idf/en/v5.5.5/esp32s3/get-started/index.html)
  activated when building or flashing firmware.
- Python 3 with `venv` support. `make test` installs the pinned Pillow and
  PyYAML development dependencies into the ignored `work/venv` directory on
  its first run. Override `PLUGIN_VALIDATOR` when the Codex plugin validator is
  not in the default Codex skills directory.
- One ESP32-S3-BOX-3 attached to an ESP32-S3-BOX-3-SENSOR dock.

Use only a compatible 18650 cell and observe polarity. The battery percentage is
a voltage-derived estimate, not a fuel-gauge measurement; without a valid cell
the screen displays `—`.

## Build and test

```sh
make test
make build-host
idf.py -C firmware build
```

`make test` runs the Go reducer/privacy/protocol suite, the host-compiled sensor
math suite, asset/release contract tests, `go vet`, and plugin validation. Its
first run needs network access to create the isolated Python environment.
`idf.py` resolves the pinned components from `firmware/main/idf_component.yml`:
ESP-IDF 5.5.x,
`espressif/esp-box-3` 3.2.0, `esp_websocket_client` 1.7.0, the official AHT30
driver, and mDNS 1.11.3. `firmware/dependencies.lock` records the resolved
transitive component versions and hashes.

The verified, sensor-bar-enabled build is preserved as a directly flashable
[release firmware bundle](release/firmware/BUILD.md), including its exact
offsets, component lock, and SHA-256 manifest.

Regenerate the shared artwork with the same pinned environment:

```sh
make assets
```

The transparent desktop sprite sheet is
`assets/generated/agentagotchi-v1.png` (1536×1872, 8 columns × 13 rows). Its
documented row map is in `assets/generated/agentagotchi-v1.json`. The same build
produces the native-cell 192×144, five-state, eight-frame RGB565 firmware
binary, keeping the BOX-3 pet the same pixel scale as the desktop sheet.
The pet artwork is original to this project; `assets/source/pet_base.png` is
the transparent master used for both the desktop/plugin art and device frames.

## Install the Mac companion

```sh
scripts/install-macos.sh
```

This builds an arm64 binary, installs it under
`~/Library/Application Support/Agentagotchi`, copies the local hook plugin to
`~/plugins/agentagotchi-status`, and loads `com.agentagotchi.edge` as a LaunchAgent.
It also asks Codex to enable the personal plugin and stops with a clear error if
that step is unavailable. Restart Codex after installing or updating hooks, then
open `/hooks` and trust all nine Agentagotchi hooks. Installed and enabled hooks do
not run until their exact definitions have been trusted. If Codex was already
open while trust was granted from the CLI, quit and reopen the desktop app once.

The bridge creates these private local files with owner-only permissions:

- `identity.json`: a random 256-bit bearer token and connection metadata.
- `bridge-cert.pem` / `bridge-key.pem`: a ten-year self-signed P-256 identity
  used only on the trusted LAN.
- `state.json`: generic task state, safe title, task ID, and timestamps.

The private key, Wi-Fi password, prompts, commands, transcripts, and tool
payloads are never logged. The SENSOR readings stay on the BOX-3 unless the
compile-time diagnostic logging option is explicitly enabled; that option
writes values to USB serial and does not add an upstream WSS message.

## Flash and provision

Connect a flashed BOX-3 to the Mac over USB while its screen says
`Waiting for USB provisioning`. Put the network credentials in the ignored
project-root `.env`:

```sh
WIFI_SSID=YOUR_2_4_GHZ_WIFI
WIFI_PASSWORD=YOUR_WIFI_PASSWORD
```

Then provision it with:

```sh
make provision-env
```

The script reads only `WIFI_SSID` and `WIFI_PASSWORD`, sends the password over
stdin, and reports success only after the BOX acknowledges that it saved the
record. It never prints the credentials. Set `AGENTAGOTCHI_SERIAL` if more than
one serial device is connected, and set `AGENTAGOTCHI_TEMP_UNIT=C` to use Celsius.

The direct `agentagotchi provision` command can also flash and provision in one
step when ESP-IDF 5.5.5 is activated. `make provision-env` intentionally uses
`--skip-flash` so rebuilding the Mac helper cannot overwrite a known-good
device image. An already configured v1 device must have its NVS erased before
changing its stored configuration.

Bonjour advertises `_agentagotchi._tcp`. The stored hostname and port remain the
manual fallback. The Mac must be awake and both devices must be on the same
trusted LAN.

## Privacy and authority boundary

The bridge uses official
[Codex hooks](https://learn.chatgpt.com/docs/hooks) for fast lifecycle changes
and the
[Codex App Server](https://developers.openai.com/codex/app-server) only for
read-only `thread/read` metadata and persisted outcome status.
It requests `includeTurns: false`, uses only an explicit task name, ignores the
prompt-derived preview field, and discards App Server stderr.
It never starts, resumes, answers, approves, or modifies a task. The BOX-3 can
only request that macOS focus a validated UUID at `codex://threads/<id>`; if the
deep link fails, the Codex app is activated without opening untrusted input.

See [docs/PROTOCOL.md](docs/PROTOCOL.md) for the wire format and
[docs/HARDWARE_ACCEPTANCE.md](docs/HARDWARE_ACCEPTANCE.md) for the hardware
verification checklist. [docs/RELEASE_VERIFICATION.md](docs/RELEASE_VERIFICATION.md)
separates automated coverage from checks that still require the physical kit.
