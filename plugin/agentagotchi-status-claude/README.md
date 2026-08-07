# Agentagotchi Status (Claude Code)

Mirrors Claude Code task states to a local ESP32 pet via the Agentagotchi
Edge Bridge.

Observes lifecycle events through Claude Code hooks and sends only task
identifiers and generic state changes to a local bridge. Prompts, commands,
tool inputs and outputs, notification text, assistant messages, transcripts,
and full file paths are discarded at the owner-only sanitize step and never
cross an Edge boundary.

## Privacy boundary

Each hook payload is reduced on the owner's machine to:

- an opaque Task Presence ID (Edge-assigned UUID)
- a generic state/reason (`running`/`working`, `needs_input`/`permission`, …)
- a subagent count
- a timestamp

The `session_id` stays on the Edge and is never relayed upstream or to a feed.
`cwd` is reduced to its basename for Edge-local dedup only.

## Status-only

This adapter registers no device capabilities. Claude Code has no verified
mechanism to focus an existing running session by the identifier hooks emit
(see `docs/adr/0008`), so the device displays Claude status but offers no
focus tap. Approve/deny and dictation are later, separately-versioned
milestones and are not part of this plugin.

## Requirements

- The Agentagotchi Edge Bridge binary (`agentagotchi`) either at
  `~/Library/Application Support/Agentagotchi/bin/agentagotchi` or on `PATH`,
  or its path set in the `AGENTAGOTCHI_BRIDGE` environment variable.
- `agentagotchi serve` running (the Edge Bridge) for status to reach a device.

## Development install

Point Claude Code at this directory while developing:

```
claude --plugin-dir ./plugin/agentagotchi-status-claude
```

Hook configuration loads at session start; restart Claude Code after editing
`hooks/hooks.json`. Test with `claude --debug` to watch hook registration.
