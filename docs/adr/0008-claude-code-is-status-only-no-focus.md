# Claude Code is status-only and registers no focus capability

The Claude Code harness adapter reports presence (idle/running/needs_input/
ready/blocked) but attaches with zero capabilities, so the device shows Claude
status but offers no focus action for it.

We chose this over advertising `focus` because there is no verified way to map
a local hook's `session_id` to a focusable desktop target. Codex focus works
because `codex://threads/<uuid>` opens an existing thread by the same UUID the
hooks emit. Claude Code differs on both axes:

- The local hook `session_id` identifies the on-disk session; the
  `claude://code/{session-id}` deep link (documented for the mobile apps and
  the claude.ai universal links) addresses a *cloud* Claude Code session, and
  no source confirms the two identifiers are interchangeable.
- The desktop deep-link article (`claude://` on macOS/Windows/Linux) documents
  only starting a *new* Code session (`claude://code/new`) and opening existing
  *chats/projects* by cloud conversation ID — it lists no route that opens an
  existing *Code* session by a local session ID.

Per the fail-closed capability rule (dispatch only a registered capability; no
silent fallback that opens the wrong host app), opening a new session or
landing on a session list would be the wrong outcome for a focus tap, so the
capability is omitted. The cost is that Claude presence is display-only. A
future reader can revisit this if Anthropic ships a desktop route that opens an
existing Code session by the same identifier the local hooks emit; until then,
do not register `focus` for the `claude` harness.
