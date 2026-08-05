# Spec: Codex Adapter

## Purpose

The Codex Harness Adapter translates Codex lifecycle signals into the shared
presence model, keeps App Server enrichment Edge-local and Codex-private, and
provides the exact, fail-closed Focus capability for validated Codex threads.

## ADDED Requirements

### Requirement: Codex lifecycle reduction in the adapter

The Codex adapter SHALL reduce Codex lifecycle signals into the shared
state/reason vocabulary inside the adapter boundary, preserving honest existing
lifecycle fidelity. Host-specific Codex vocabulary SHALL NOT leak into the
semantic core.

#### Scenario: Codex signals map to shared states

- **WHEN** a Codex lifecycle signal fires
- **THEN** the adapter maps it to a shared state and reason and reports an
  absolute statement to the Edge

#### Scenario: No Codex vocabulary in the core

- **WHEN** the adapter reports to the Edge
- **THEN** the report contains only the shared state/reason vocabulary and no
  Codex-specific lifecycle event name

### Requirement: Edge-local Codex-private enrichment

The adapter SHALL keep App Server enrichment local to the Edge and restricted
to Codex-owned private identifiers. Enrichment data SHALL NOT cross the
owning Edge.

#### Scenario: Enrichment stays Edge-local

- **WHEN** the adapter enriches a Codex Task Presence from the App Server
- **THEN** the enrichment is keyed by Codex-owned private IDs and remains
  local to the owning Edge

### Requirement: Exact fail-closed Focus

The adapter SHALL resolve a Focus Action through the Edge-private registry and
open only the exact validated Codex thread. It SHALL provide no app-open
fallback, and SHALL report success only after exact harness capability
success.

#### Scenario: Focus opens the exact thread

- **WHEN** a Focus Action targets a Codex Task Presence that advertises Focus
- **THEN** the adapter resolves the Task Presence ID through the Edge-private
  registry and opens only the exact validated Codex thread

#### Scenario: No app-open fallback

- **WHEN** the exact Codex thread cannot be resolved or opened
- **THEN** the adapter fails closed and does not open a different app, thread,
  or task

#### Scenario: Success reported only on exact success

- **WHEN** a Focus Action dispatch completes
- **THEN** the adapter reports success only when the exact Codex focus
  capability succeeded
