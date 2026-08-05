# Spec: Pi Adapter

## Purpose

The Pi Harness Adapter is a leased, status-only adapter that maps Pi session
signals into the shared presence model honestly, resends absolute state after
reconnect, and advertises no Focus capability until an exact integration is
designed and tested.

## ADDED Requirements

### Requirement: Honest status-only mapping

The Pi adapter SHALL map only explicit Pi signals to semantic reports: session
active / `agent_start` to `running` + `working`; `agent_settled` with explicit
idle confirmation to `ready` + `completed`; explicit reliable failure metadata
optionally to `blocked` + `failed`; and extension unload/disconnect to end or
lease expiry. It SHALL NOT synthesize `needs_input` from tool-name or UI
heuristics.

#### Scenario: Session start reports running

- **WHEN** a Pi session becomes active or `agent_start` fires
- **THEN** the adapter reports `running` + `working`

#### Scenario: Settled with idle confirmation reports ready

- **WHEN** `agent_settled` fires with explicit idle confirmation
- **THEN** the adapter reports `ready` + `completed`

#### Scenario: Explicit failure metadata may report blocked

- **WHEN** Pi provides explicit reliable failure metadata
- **THEN** the adapter may report `blocked` + `failed`

#### Scenario: Unload ends the presence

- **WHEN** the Pi extension unloads or disconnects
- **THEN** the adapter reports end or the lease expires

#### Scenario: No inferred needs_input

- **WHEN** Pi exposes no explicit input-request signal
- **THEN** the adapter does not report `needs_input` from tool-name or UI
  heuristics

### Requirement: Pi session identity stays Edge-private

The adapter SHALL use Pi's stable session identity only inside the
Edge-private mapping. It SHALL NOT transmit session file paths, full cwd,
prompts, tool input, transcript data, or prompt-derived session names.

#### Scenario: Private identity mapping

- **WHEN** the adapter reports a Pi Harness Session
- **THEN** Pi's stable session identity is used only inside the Edge-private
  mapping and never placed on an upstream or device wire

#### Scenario: No path or transcript leakage

- **WHEN** the adapter emits a report
- **THEN** it contains no session file path, full cwd, prompt, tool input,
  transcript data, or prompt-derived session name

### Requirement: Leased session with absolute resend

The Pi adapter SHALL operate as a leased adapter session, coalesce rapid
updates, and resend its complete absolute current state after reconnect.

#### Scenario: Reconnect resends absolute state

- **WHEN** the Pi adapter reconnects to the Edge
- **THEN** it resends its complete absolute current state so the Edge converges

#### Scenario: Rapid updates coalesce

- **WHEN** Pi emits rapid successive signals
- **THEN** the adapter coalesces them into bounded absolute reports

### Requirement: No Focus capability

The Pi adapter SHALL NOT advertise a Focus capability until an exact
integration is designed and tested. Status-only Pi Task Presences SHALL remain
browsable.

#### Scenario: Pi presence advertises no Focus

- **WHEN** a Pi Task Presence is published
- **THEN** it does not advertise the Focus capability and remains browsable in
  the task list
