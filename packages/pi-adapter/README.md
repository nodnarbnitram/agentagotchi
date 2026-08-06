# Agentagotchi Pi Harness Adapter

Status-only Pi adapter for the Agentagotchi Edge Bridge. It reports honest
session presence to the local Edge over the owner-only IPC socket.

## What it reports

| Pi signal | Task Presence |
| --- | --- |
| `agent_start` / session active | `running` + `working` |
| `agent_settled` with `ctx.isIdle()` | `ready` + `completed` |
| `session_shutdown` / unload | end (lease expiry also ends presences) |

It **never**:

- synthesizes `needs_input` from tool names or UI heuristics
- advertises a Focus capability (Pi presences remain browsable, not focusable)
- transmits prompts, transcripts, tool input, session file paths, full cwd,
  or prompt-derived session names

Pi's stable session ID (`ctx.sessionManager.getSessionId()`) is used only
inside the Edge-private mapping and never crosses an Edge boundary.

## Behavior

- Leased adapter session over `agentagotchi.ipc.v1` (heartbeats, lease renewal)
- Reconnects with backoff and resends complete absolute state so the Edge
  converges
- Coalesces reports through a single absolute map (one entry per session)

## Install

Point Pi at the extension directory, e.g. in `~/.pi/agent/settings.json`:

```json
{
  "extensions": ["/path/to/agentagotchi-foundation/adapters/pi"]
}
```

Override the socket path with `AGENTAGOTCHI_EDGE_SOCKET` if the Edge uses a
non-default data directory.
