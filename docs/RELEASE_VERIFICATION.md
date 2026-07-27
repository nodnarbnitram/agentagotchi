# Release verification

Verification date: 2026-07-26

Host environment: macOS 15.5 arm64, Go 1.22.1, Python 3.12.13, and Apple
Clang 17.0.0.

This record distinguishes automated source checks from physical acceptance.
Passing the automated suite does not imply that timing, sensor accuracy, audio,
touch, battery behavior, or long-duration stability have been validated on an
ESP32-S3-BOX-3.

## Automated coverage

| Plan requirement | Automated evidence |
| --- | --- |
| State transitions and priority | `internal/state/store_test.go` |
| Hook privacy filtering | `internal/hook/sanitize_test.go` |
| UUIDv1/v7 task-ID validation | `internal/focus/focus_test.go` |
| Metadata-only App Server parsing and prompt-preview exclusion | `internal/appserver/client_test.go` |
| Persisted success/failure reduction | `internal/state/store_test.go`, `internal/appserver/client_test.go` |
| Pinned certificate profile and owner-only identity files | `internal/config/config_test.go` |
| Provisioning wall-clock bootstrap field and device acknowledgement | `internal/provision/provision_test.go`, live USB check |
| WSS upgrade and bearer-token helpers | `internal/ws/ws_test.go`, `internal/bridge/server_test.go` |
| Temperature conversion | `firmware/tests/test_sensor_math.c` |
| Battery divider and percentage curve | `firmware/tests/test_sensor_math.c` |
| Absent battery and stale values | `firmware/tests/test_sensor_math.c` |
| Radar debounce and presence hold | `firmware/tests/test_sensor_math.c` |
| Sprite-sheet contract and every frame cell | `tools/test_pet_assets.py` |
| RGB565 header, dimensions, ordering, and length | `tools/test_pet_assets.py` |
| Pinned and locked firmware dependencies | `tools/test_release_contracts.py` |
| SENSOR dock compile-time pins/intervals and enabled default | `tools/test_release_contracts.py` |
| LaunchAgent load/keepalive contract | `tools/test_release_contracts.py` |
| Plugin structure and hook manifest | plugin-creator validator invoked by `make test` |

The source suite is run with:

```sh
make test
```

The exact ESP-IDF firmware compile is run separately because it resolves the
pinned managed components and requires an activated ESP-IDF 5.5.5 environment:

```sh
idf.py -C firmware build
```

### Recorded host results

The following checks passed on 2026-07-26:

```text
sensor_math tests passed
Go package tests passed
go vet passed
5 pet asset contract tests passed
3 release contract tests passed
Plugin validation passed
macOS host binary: Mach-O 64-bit executable arm64
codex-pet version: 0.1.0
hook/install shell syntax: passed
hook fail-open smoke (bridge absent): passed
plugin/hook/asset JSON syntax: passed
LaunchAgent plist lint: passed
direct pinned-certificate /healthz: passed
missing WSS bearer token: HTTP 401
authenticated WSS upgrade: HTTP 101
read-only App Server connection: passed
runtime hook privacy smoke: passed
runtime file/socket owner-only modes: passed
provision password-stdin/conflicting-flag checks: passed
```

The host suite used the workspace-provided Python runtime for Pillow and a
temporary PyYAML 6.0.2 target because the system `python3` did not include
those dependencies:

```sh
/Users/brandon/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 \
  -m pip install --target /private/tmp/codex-pet-release-pydeps PyYAML==6.0.2
PYTHONPATH=/private/tmp/codex-pet-release-pydeps make test \
  PYTHON=/Users/brandon/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3
make build-host
file work/bin/codex-pet
./work/bin/codex-pet version
sh -n scripts/install-macos.sh plugin/codex-pet-status/scripts/hook.sh
printf '%s\n' '{"prompt":"secret"}' |
  CODEX_PET_BRIDGE=/nonexistent PATH=/usr/bin:/bin \
  plugin/codex-pet-status/scripts/hook.sh
python3 -m json.tool plugin/codex-pet-status/.codex-plugin/plugin.json
python3 -m json.tool plugin/codex-pet-status/hooks/hooks.json
python3 -m json.tool assets/generated/codex-pet-v1.json
plutil -lint packaging/com.openai.codexpet.plist.in
```

The complete host suite was repeated after the Mac and firmware slices were
final. The temporary Python target and all test/build caches were deleted
afterward.

### Recorded ESP-IDF results

The retained `CONFIG_CODEX_PET_SENSOR_BAR=y` build and an isolated
`CONFIG_CODEX_PET_SENSOR_BAR=n` build both completed warning-free with ESP-IDF
5.5.5 and `espressif/esp-box-3` 3.2.0. `make firmware-test` also passed.

The enabled app is `3,669,520` bytes (`0x37fe10`) and leaves `3,670,512` bytes
(`0x3801f0`, 50%) of its app partition free. Its SHA-256 is
`55189dd8a9f46cfffd3233dda0ecc5f1953c2757745bc290f3a3901b12a1c4e4`.
The exact enabled flash bundle, offsets, hashes, and component lock are in
[`../release/firmware/BUILD.md`](../release/firmware/BUILD.md).

The enabled build command was:

```sh
IDF_TOOLS_PATH="$PWD/work/espressif-tools"
PYTHONPATH="$PWD/work/idf-sandbox"
IDF_COMPONENT_CACHE_PATH="$PWD/work/component-cache"
export IDF_TOOLS_PATH PYTHONPATH IDF_COMPONENT_CACHE_PATH
. "$PWD/work/esp-idf/export.sh"
idf.py -C "$PWD/firmware" build
```

The disabled-switch integrity build used isolated outputs:

```sh
idf.py -C "$PWD/firmware" \
  -B "$PWD/work/build-sensor-bar-off" \
  -D SDKCONFIG="$PWD/work/sdkconfig.sensor-bar-off.generated" \
  -D "SDKCONFIG_DEFAULTS=$PWD/firmware/sdkconfig.defaults;$PWD/work/sdkconfig.sensor-bar-off" \
  build
```

### Recorded hardware bring-up

An ESP32-S3-BOX-3 revision 0.2 with 16 MB PSRAM and its SENSOR dock was flashed
over the native USB Serial/JTAG port on 2026-07-26. The following checks passed:

- Stable LVGL startup with a 192×144 animated pet, speech bubble, and no
  reset or white-screen loop.
- Touch, guarded ES8311 audio, AHT30/sensor task, and radar interrupt
  initialization.
- Acknowledged USB provisioning using `WIFI_SSID` and `WIFI_PASSWORD` from the
  ignored project `.env`; no credential was printed or logged.
- WPA2 association on 2.4 GHz, DHCP address `192.168.10.225`, and Bonjour
  discovery of the Mac bridge's IPv4 address while retaining the provisioned
  hostname for certificate verification.
- Pinned-certificate TLS after routing mbedTLS allocations to the available
  PSRAM.
- An authenticated WSS socket remained established across a bridge restart.

The reference-meter, multimeter, sensor-fault, latency/performance, battery
discharge, and soak checks remain outstanding.

### Installed-path result

The personal marketplace entry and cache-busted
`codex-pet-status@personal` plugin were installed and enabled. Codex initially
skipped all nine hooks pending trust review; after trusting the exact
definitions with `/hooks`, a real `SessionEnd` advanced the bridge sequence
from 2 to 3. The arm64 companion is loaded as `com.openai.codexpet`, its health
endpoint passes, and the BOX WSS socket is established.

## Hardware-only coverage

The following remain unchecked and need the indicated instruments or longer
hardware runs:

- AHT30 CRC fault injection and real I²C bus recovery.
- ADC calibration availability and voltage accuracy on this board.
- Radar electrical behavior and interrupt chatter.
- Wrong-certificate/token rejection on the target and Wi-Fi interruption
  recovery.
- Touch focus, task-tray behavior, display glyphs, and exactly-once chirps.
- Three-second alert latency, 30 Hz LVGL target, sub-50 ms UI stalls, and sensor
  CPU use below 2%.
- Eight-hour USB soak and the separate battery-discharge run.

Record those results in
[`HARDWARE_ACCEPTANCE.md`](HARDWARE_ACCEPTANCE.md). Do not mark a hardware item
complete based only on compilation or host tests.

## Release hygiene

`work/`, ESP-IDF managed components, firmware build output, generated
configuration, and logs are ignored. They are reproducible build inputs or
local state, not source deliverables. `firmware/dependencies.lock` is included
so transitive component versions and hashes remain reproducible. The generated
pet assets and embedded RGB565 binary are also intentionally included because
firmware builds consume them directly.

The final tree contains 81 files and occupies 4.5 MB (2.0 MB is the preserved
flash bundle). It contains no `work/`, managed components, generated
`sdkconfig`, firmware build directory, Python caches, logs, smoke-test identity,
token, or private-key material. Local Markdown links, the copied dependency
lock, JSON/plist/shell syntax, and every release-bundle checksum were checked
after cleanup.
