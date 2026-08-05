# Spec: Harness Adapter IPC

## Purpose

The owner-only local contract between Harness Adapters and the Edge Bridge. It
defines the two reporting modes (one-shot and leased sessions), the absolute
upsert/end report shape, sanitization, capability registration, and ordering
metadata. Harness-native session identifiers stay private to the owning Edge.

## ADDED Requirements

### Requirement: Owner-only local transport

The Harness Adapter IPC SHALL be a local, owner-only channel. Socket and
storage permissions SHALL be owner-only, frames SHALL be bounded, and schemas
SHALL be strict. Raw payloads SHALL NOT be logged. Adapter failures SHALL NOT
break or delay the Agent Harness.

#### Scenario: IPC is owner-only

- **WHEN** a Harness Adapter connects to the Edge IPC endpoint
- **THEN** the channel enforces owner-only filesystem/socket permissions and
  rejects non-owner access

#### Scenario: Bounded strict frames

- **WHEN** a frame exceeds the contract's size bound or fails schema validation
- **THEN** the Edge rejects it without logging its raw payload

#### Scenario: Adapter failure does not break the harness

- **WHEN** the Edge IPC is unavailable or a report fails
- **THEN** the Agent Harness continues operating without being blocked or
  delayed by the adapter

### Requirement: Two reporting modes

The IPC SHALL support one-shot reports for hook-driven integrations and leased
adapter sessions for long-lived adapters. A leased session SHALL provide
registration, a generation, a monotonic producer sequence, heartbeat, absolute
reports, end/removal, and capability registration.

#### Scenario: One-shot report

- **WHEN** a hook-driven adapter emits a single status event
- **THEN** it reports it as a one-shot absolute upsert without holding a lease

#### Scenario: Leased session lifecycle

- **WHEN** a long-lived adapter connects
- **THEN** it registers a leased session, receives a generation, sends
  heartbeats and absolute reports with a monotonic producer sequence, and can
  end or be removed

#### Scenario: Capability registration

- **WHEN** a leased adapter registers
- **THEN** it declares the allowlisted Device Capabilities it can dispatch for
  its Harness Sessions

### Requirement: Absolute report shape

The semantic report SHALL be an absolute statement. An `upsert` SHALL carry the
private native session id, state, reason, optional safe title, optional
subagent count, capabilities, generation, producer sequence, and observed
timestamp. An `end` SHALL carry the private native session id, generation,
producer sequence, and observed timestamp.

#### Scenario: Upsert carries required fields

- **WHEN** an adapter reports an `upsert`
- **THEN** it includes native session id, state, reason, generation, producer
  sequence, observed timestamp, and may include safe title, subagent count, and
  capabilities

#### Scenario: End signals removal

- **WHEN** an adapter reports an `end` for a Harness Session
- **THEN** the Edge removes the corresponding Task Presence

### Requirement: Sanitization before reporting

The adapter SHALL sanitize and map host-specific signals before reporting to
the Edge. The IPC contract SHALL transmit no prompts, commands, tool payloads,
transcripts, full paths, credentials, private keys, or Wi-Fi secrets, and the
native session id SHALL NOT pass upstream of the owning Edge.

#### Scenario: Host signals are sanitized

- **WHEN** an adapter translates a harness signal into a report
- **THEN** only allowlisted presence fields are placed on the wire and no
  prompt, command, tool payload, transcript, or full path is included

#### Scenario: Native id stays Edge-local

- **WHEN** the Edge accepts a report
- **THEN** the native session id remains in the Edge-private mapping and never
  crosses an Edge-to-Home or device boundary

### Requirement: Honest state mapping

An adapter SHALL report only states and capabilities supported by explicit
harness signals. It SHALL NOT infer `needs_input` from tool names, terminal
text, prompts, or timing heuristics.

#### Scenario: No synthesized needs_input

- **WHEN** a harness exposes no explicit input-request signal
- **THEN** the adapter does not report `needs_input` based on tool names,
  terminal text, prompts, or timing heuristics

#### Scenario: Only supported capabilities advertised

- **WHEN** an adapter reports capabilities for a Harness Session
- **THEN** it advertises only allowlisted Device Capabilities backed by an
  explicit harness capability
