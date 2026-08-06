# Agentagotchi BOX-3 firmware v0.1.0

This is the directly flashable, sensor-bar-enabled ESP32-S3-BOX-3 build
preserved from the 2026-07-26 release verification.

## Build identity

- Target: `esp32s3`
- ESP-IDF: `5.5.5`
- BSP: `espressif/esp-box-3` `3.2.0`
- Sensor bar: `CONFIG_AGENTAGOTCHI_SENSOR_BAR=y`
- TLS allocations: external PSRAM (`CONFIG_MBEDTLS_EXTERNAL_MEM_ALLOC=y`)
- App binary: `3,669,520` bytes (`0x37fe10`)
- App partition: `7,340,032` bytes (`0x700000`)
- App partition free: `3,670,512` bytes (`0x3801f0`, 50%)
- Flash: 16 MB, DIO, 80 MHz

`dependencies.lock` preserves every resolved component version and registry
hash. The exact enabled build and an isolated
`CONFIG_AGENTAGOTCHI_SENSOR_BAR=n` build both completed warning-free. Only the
shipped, enabled binary is included here.

## Flash layout

| Offset | File |
| --- | --- |
| `0x0000` | `bootloader/bootloader.bin` |
| `0x8000` | `partition_table/partition-table.bin` |
| `0x10000` | `codex_pet_box3.bin` |

`flash_args` and `flasher_args.json` contain this same layout. With ESP-IDF or
esptool activated, flash from this directory with:

```sh
esptool.py --chip esp32s3 --port /dev/cu.usbmodem... \
  --before default_reset --after hard_reset write_flash \
  --flash_mode dio --flash_freq 80m --flash_size 16MB \
  0x0 bootloader/bootloader.bin \
  0x8000 partition_table/partition-table.bin \
  0x10000 codex_pet_box3.bin
```

The flashed device still needs its Wi-Fi and slot-0 feed credentials. Run
`agentagotchi provision --skip-flash --password-stdin ...` while it is waiting
for initial USB provisioning.

This build was flashed to an ESP32-S3-BOX-3 with its SENSOR dock and verified
on hardware for stable display startup, touch/audio/sensor initialization,
acknowledged USB provisioning, 2.4 GHz Wi-Fi association, DHCP, Bonjour bridge
discovery, pinned TLS, and an established authenticated WebSocket session.
Reference-meter calibration, battery discharge, CPU/timing measurements, and
the eight-hour soak test remain documented in
`../../docs/HARDWARE_ACCEPTANCE.md`.
