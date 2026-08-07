# Hardware acceptance checklist

Record the firmware commit, ESP-IDF version, BOX-3 serial number, dock revision,
cell model, bridge version, and test date before running this checklist.

## Functional checks

- [x] BOX-3 discovers the Mac through Bonjour.
- [ ] Manual hostname fallback works with Bonjour disabled.
- [ ] A wrong bearer token or different certificate cannot connect.
- [ ] After a Wi-Fi interruption and after a bridge restart, the BOX-3
      reconnects without manual intervention and receives a complete snapshot.
- [ ] `Needs input` appears within three seconds of a Codex prompt, approval, or
      permission event.
- [ ] One and only one chirp plays per new `Needs input` transition.
- [ ] Reconnect and sensor refresh do not duplicate a chirp.
- [ ] Tapping the pet focuses the highest-priority Codex task.
- [ ] Tapping task counts opens and closes the task tray.
- [ ] Wi-Fi, temperature, humidity, approximate battery, and presence render in
      the 20-pixel status bar.
- [ ] The degree, em-dash, and `%RH` glyphs render correctly in the shipped LVGL
      font configuration.
- [ ] Removing the cell produces `—` rather than a charging state.

## Sensor fault checks

- [ ] AHT30 CRC failure is rejected.
- [ ] After three consecutive AHT30 failures, the BSP-owned dock I²C bus is
      reset and the AHT30 driver is recreated.
- [ ] The last good temperature/humidity survives transient failures and changes
      to `—` after five minutes.
- [ ] ADC calibration is active and 64-sample averaging is used.
- [ ] Cell voltage agrees with a multimeter within 0.1 V after calibration.
- [ ] Temperature and humidity agree with a reference meter within practical
      AHT30 tolerance after stabilization.
- [ ] Radar edge chatter within 200 ms is ignored.
- [ ] Presence stays active for 30 seconds after the last valid detection.

## Performance and soak

The four-feed TLS/WSS memory, reconnect/power profile and the long-press row
dismiss gesture are **pending physical validation** on the BOX-3. Do not treat
the firmware build or host tests as acceptance of those limits or gestures.

- [ ] Four authenticated feed slots remain stable together through reconnects.
- [ ] Long-pressing a terminal/input-gated row is distinguishable from a row
      tap and does not dismiss while browsing or scrolling.

- [ ] Pet animation and touch remain smooth with all sensors active.
- [ ] LVGL sustains the target 30 Hz and no sensor operation stalls it over
      50 ms.
- [ ] Sensor task CPU averages under 2%.
- [ ] Eight-hour USB-powered soak completes without reset, leak, UI freeze, or
      reconnect storm.
- [ ] A separate 18650 discharge run completes and the approximate percentage
      decreases monotonically enough to be useful.

If hardware testing finds an unresolved UI or stability regression, rebuild with
`CONFIG_AGENTAGOTCHI_SENSOR_BAR=n`. The shipped default is enabled.

Before release, also compile once with `CONFIG_AGENTAGOTCHI_SENSOR_BAR=n` to verify
that the fallback remains buildable. This compile is a switch-integrity check,
not a substitute for running the shipped, enabled configuration.

---

## Multi-harness rebuild — pending runs (2026-08-05)

The rebuilt firmware (four feed pairings, scrollable list, dismiss gestures)
has NOT been validated on physical hardware. Before release, run and record:

1. ESP-IDF 5.5.x `idf.py -C firmware build` warning-free with both
   `CONFIG_AGENTAGOTCHI_SENSOR_BAR=y` and `=n`.
2. Four concurrent WSS feeds (two Edges + one Home relay + one spare slot):
   memory headroom, reconnect behavior, merge convergence by origin revision.
3. Interaction acceptance: scrollable list bounds, featured-task preemption,
   manual override/re-tap release, pet-tap Focus only when advertised,
   long-press dismiss (terminal→acknowledge, needs_input→snooze), browsing
   never acts on the host.
4. Failure modes on kit: Home loss (direct Edge keeps working), direct Edge
   loss (Home relay keeps working), adapter death, revocation disconnect.

Record results here from real-kit runs only — never from compilation.

---

## Physical validation run — 2026-08-06 (pet long-press dismiss)

Kit: ESP32-S3-BOX-3 (USB Serial/JTAG `/dev/cu.usbmodem2112301`), ESP-IDF v5.5.1,
firmware commit `209e7c2` on branch `feat/pet-longpress-dismiss`, Edge v0.2.0
on macOS (wss://brandons-macbook-pro-2.local:6571/feed/v1).

Observed PASS (user-confirmed on kit):

- **Long-pressing the featured pet dismisses its task.** With the pet
  displaying a finished (`ready`) task, a pet long-press sends the
  `acknowledge` dismissal and clears it from the device.

Build/check context: `idf.py -C firmware build` warning-free (ESP-IDF v5.5.1),
`make test` green (Go, sensor math, pet assets, release contracts, plugin).

Not yet validated on kit: pet long-press for `needs_input` → `snooze`;
tray-row long-press dismiss remains pending physical validation.

---

## Physical validation run — 2026-08-06 (single feed, multi-feed slot provisioning pending)

Kit: ESP32-S3-BOX-3 (USB Serial/JTAG `/dev/cu.usbmodem2112301`), ESP-IDF v5.5.1,
firmware commit `d81d3c7`+uncommitted network fixes, Edge v0.2.0 on macOS
(wss://brandons-macbook-pro-2.local:6571/feed/v1).

Real-hardware defects found and fixed this run (all committed):

1. `app_ui_event_t` union (~10KB `app_snapshot_t`) built on task stacks →
   `pet_sensors` stack overflow at boot. Fixed: single-task static storage at
   all three producer sites (`post_state`, `post_network_state` ×2).
2. `pet_sensors`/`pet_audio`/`pet_network` stacks allocated with
   `MALLOC_CAP_SPIRAM` — invalid for Xtensa context switching. Fixed: internal
   RAM stacks (`xTaskCreatePinnedToCore`). Release contract now asserts this.
3. LVGL draw buffer in PSRAM broke ILI9341 DMA flush (`wait_for_flushing`
   spun, task watchdog starved IDLE0). Fixed: draw buffer stays DMA-internal;
   `BSP_LCD_DRAW_BUF_HEIGHT` 100→40 (64→25.6KB), LVGL pool 64→40KB.
4. LVGL object pool exhausted by 64 tray rows × 3 widgets →
   `lv_label_create` NULL → crash in `make_label`. Fixed: tray virtualized to
   `APP_UI_MAX_ROWS=16` over the priority-sorted 64-task model.
5. Wi-Fi driver + mDNS + websocket client overran internal heap
   (`ESP_ERR_NO_MEM` in `app_network_start`, then `Error create websocket
   task` with internal heap at ~1.3KB). Fixed: Wi-Fi static/dynamic buffer
   trim, mDNS freed after each discovery query (`mdns_free`), websocket task
   7168→6144, transport buffer 4096→2048.

Observed PASS:
- Clean boot, no crash/watchdog for 60s+ across repeated resets.
- Provisioning via `scripts/provision-from-env.sh` (AGOT_PROVISION, slot 0).
- Wi-Fi join, mDNS bridge discovery, WSS connect + authenticate to the Edge
  (`websocket_client: Started`; Edge admin `connected: 1`).
- Codex hook → Edge → BOX-3 feed path: Edge shows `needs_input`, 1 presence,
  1 connected peer. (Device rendered the presence; serial log stays quiet by
  design.)

Pending (needs the multi-feed slot provisioning flow — firmware currently
provisions slot 0 only by design): four concurrent feeds, duplicate
direct/relayed convergence, resource limits under 4 live feeds.
