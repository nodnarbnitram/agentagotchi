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
