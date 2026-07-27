# Codex Pet local protocol v1

## Discovery, transport, and authentication

The Mac advertises `_codex-pet._tcp` through Bonjour. Provisioning also stores a
manual hostname and port. The BOX-3 connects to `wss://<host>:<port>/ws` and
validates the exact self-signed certificate provisioned over USB. The HTTP
upgrade must include `Authorization: Bearer <256-bit-token>`.

Provisioning also supplies a plausible current Unix time. The firmware persists
that value and uses it only as a lower-bound clock bootstrap so TLS certificate
validation works after an offline reboot. It does not treat the bootstrap as a
fresh sensor timestamp; only the SNTP synchronization callback makes
`sensorUpdatedAt` valid.

The token and certificate are device credentials. The server rejects missing or
incorrect bearer tokens before upgrading the connection.

## Snapshot

Every connection starts with a complete snapshot. Later hook or App Server
changes send replacement snapshots with increasing `seq` values:

```json
{
  "type": "snapshot",
  "version": 1,
  "seq": 42,
  "aggregateState": "needs_input",
  "tasks": [
    {
      "id": "019fa063-b4d1-7d81-bced-7f9f55ec7611",
      "title": "Fix the build",
      "state": "needs_input",
      "reason": "permission",
      "subagentCount": 1
    }
  ],
  "counts": {
    "needsInput": 1,
    "blocked": 0,
    "ready": 0,
    "running": 0
  }
}
```

Allowed states are `idle`, `running`, `needs_input`, `ready`, and `blocked`.
Allowed reasons are `working`, `question`, `approval`, `permission`,
`completed`, and `failed`. Reasons are deliberately generic.

The firmware merges its local measurements into the logical snapshot:

```json
{
  "device": {
    "temperatureC": 22.4,
    "humidityRh": 43.1,
    "batteryVoltage": 3.91,
    "batteryPercent": 66,
    "batteryEstimate": true,
    "presence": true,
    "wifiRssi": -54,
    "sensorUpdatedAt": 1785100000
  }
}
```

That `device` object is local state and is not transmitted back to the Mac in
normal operation. A missing or stale sensor value is represented by the absence
of its numeric field and displayed as `—`. `sensorUpdatedAt` is Unix epoch
seconds. It remains `0` internally (and is omitted by any diagnostic serializer)
until the device obtains a valid wall clock through SNTP; monotonic uptime is
used separately for stale-value and presence calculations and is never exposed
as an epoch timestamp.

## Focus action

Tapping the pet sends:

```json
{
  "type": "focus",
  "version": 1,
  "taskId": "019fa063-b4d1-7d81-bced-7f9f55ec7611",
  "seenSeq": 42
}
```

The Mac accepts only canonical UUID task IDs. The action focuses Codex and
acknowledges a completed task in the local status reducer; it does not answer,
approve, resume, or otherwise mutate the Codex task.

## RGB565 pet asset

`pet_device_rgb565.bin` starts with a 16-byte little-endian header:

| Offset | Type | Meaning |
| --- | --- | --- |
| 0 | 4 bytes | ASCII `CPET` |
| 4 | uint16 | Asset version, `1` |
| 6 | uint16 | Width, `96` |
| 8 | uint16 | Height, `72` |
| 10 | uint8 | State count, `5` |
| 11 | uint8 | Frames per state, `8` |
| 12 | uint32 | Pixel-data offset, `16` |

Frames follow as little-endian RGB565 in state order `idle`, `running`,
`needs_input`, `ready`, `blocked`, matching the firmware enum. Each state has
eight 96×72 frames intended for a 100 ms cadence. Transparent source pixels are
flattened to the UI background color `rgb(11, 21, 27)` before RGB565 conversion.
