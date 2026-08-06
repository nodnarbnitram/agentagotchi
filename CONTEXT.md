# Agent Presence

This context describes how agent work running across harnesses and machines becomes one user-visible presence model for Agentagotchi devices and future clients.

## Language

**Agent Harness**:
An agent runtime that owns sessions and exposes host-specific status or control signals, such as Codex, Pi, or Claude Code.
_Avoid_: Host, source

**Harness Adapter**:
The integration boundary that translates one Agent Harness's signals and capabilities into the shared presence model.
_Avoid_: Plugin, hook, producer

**Edge Bridge**:
The single bridge associated with one machine, shared by every Harness Adapter on that machine.
_Avoid_: Local bridge, harness bridge

**Home Bridge**:
An always-reachable service for one Home that aggregates Task Presence from paired Edge Bridges and relays that presence to paired devices and clients.
_Avoid_: Central bridge, cloud bridge, remote bridge

**Home**:
The durable trust and isolation boundary containing a set of paired Edge Bridges, devices, and clients. A Home is distinct from the machine or service process hosting its Home Bridge.
_Avoid_: Account, server, household

**Harness Session**:
One work context owned by an Agent Harness on a particular machine.
_Avoid_: Process, host session

**Task Presence**:
The privacy-safe, user-visible representation of a Harness Session exposed through a Presence Feed.
_Avoid_: Transcript, session record

**Task Presence ID**:
An opaque canonical UUID assigned by the owning Edge Bridge and preserved across direct and relayed Presence Feeds. It is distinct from the private, harness-native session identifier.
_Avoid_: Session ID, thread ID

**Presence Feed**:
A privacy-safe view of Task Presences published by an Edge Bridge directly or relayed through one Home Bridge. A device or client may consume more than one Presence Feed at the same time.
_Avoid_: Event stream, telemetry pipe

**Device Capability**:
A generic action currently available for a Task Presence, independent of which Agent Harness implements it.
_Avoid_: Harness command, inferred action

**Featured Task**:
The Task Presence currently represented by the BOX-3's main pet, title, and state. Featuring a task changes device presentation only; it does not invoke harness focus.
_Avoid_: Focused task, active task

**Focus Action**:
A Device Capability that asks the owning Agent Harness to foreground a Harness Session when that harness and machine support it.
_Avoid_: Feature, select

**Terminal Task Presence**:
A Task Presence whose work has finished and that remains only as a notification: `ready` or `blocked`. A still-live presence (`idle`, `running`, `needs_input`) is transient.
_Avoid_: completed task, finished session

**Acknowledged Task Presence**:
A Terminal Task Presence the user has dismissed, either through a successful Device Capability action on it or an explicit dismiss gesture. The owning Edge Bridge removes it from every Presence Feed; it resurfaces only if the Harness Session later reaches a new terminal state.
_Avoid_: read receipt, seen state

**Snoozed Task Presence**:
A Task Presence awaiting user input (`needs_input`) that the user has set aside without approving, denying, or answering. It remains in every Presence Feed but stops claiming the Featured Task until its state or reason next changes.
_Avoid_: dismissed task, muted task, parked task

**Pairing**:
An explicit trust relationship authorizing one role-scoped direction of presence delivery: Edge Bridge to device, Edge Bridge to Home Bridge, or Home Bridge to device.
_Avoid_: Peering, mesh link

**Pairing Ceremony**:
The short-lived, one-use device-code authorization state machine that establishes a Pairing. The connecting client displays the code and the receiving service's authenticated administrator approves it, issuing a unique, revocable, role-scoped credential; the shape is identical for all three Pairing directions.
_Avoid_: Provisioning, onboarding

**Safe Title**:
The only human-readable label on a Task Presence: an allowlisted harness name and an optional user-approved Edge alias, bounded and sanitized. It is never derived from hostnames, paths, session names, prompts, transcripts, or commands, and routing never depends on it.
_Avoid_: Session name, task name
