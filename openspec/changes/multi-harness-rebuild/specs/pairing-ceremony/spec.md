# Spec: Pairing Ceremony

## Purpose

The single short-lived, one-use device-code authorization state machine that
establishes every Pairing. The connecting client displays a code, the
receiving service's authenticated administrator approves it, and a unique,
revocable, role-scoped credential is issued — identical in shape for
Edge→device, Edge→Home, and Home→device.

## ADDED Requirements

### Requirement: One device-code state machine for all directions

The Pairing Ceremony SHALL use one short-lived device-code authorization state
machine for all three role-scoped directions: Edge Bridge→device, Edge
Bridge→Home Bridge, and Home Bridge→device. The shape SHALL be identical for
all directions, with role-specific grants.

#### Scenario: Same ceremony for every direction

- **WHEN** any of the three pairing directions is established
- **THEN** the same device-code state machine is used with a role-specific
  grant

### Requirement: Short-lived one-use codes

The connecting client SHALL request a short-lived code from the receiving
service and display it to the user (BOX-3 screen or Edge CLI). Codes SHALL be
one-use, expire quickly, and SHALL NOT become long-lived secrets. Codes SHALL
NOT be replayable.

#### Scenario: Code expires and is one-use

- **WHEN** a pairing code is used once or its short lifetime elapses
- **THEN** it becomes invalid and cannot be reused or replayed

#### Scenario: Code is displayed to the user

- **WHEN** a connecting client requests a pairing code
- **THEN** it displays the code to the user on the BOX-3 screen or Edge CLI

### Requirement: Administrator approval issues a scoped credential

The user SHALL approve the code in the receiving service's authenticated
administration client (Edge CLI/app or the Home web UI). Successful approval
SHALL issue a unique, random, revocable credential scoped to that relationship
and role. The client SHALL pin/authenticate the receiving service identity.

#### Scenario: Approval issues unique role-scoped credential

- **WHEN** the administrator approves a pairing code
- **THEN** the ceremony issues a unique, random, revocable credential scoped to
  that relationship and role

#### Scenario: Client pins the service identity

- **WHEN** pairing succeeds
- **THEN** the connecting client pins/authenticates the receiving service
  identity

#### Scenario: Unapproved code grants nothing

- **WHEN** a code is never approved before expiry
- **THEN** no credential is issued

### Requirement: Credentials are unique, scoped, revocable, owner-only

Each credential SHALL be unique to one relationship and role, individually
revocable, and stored in owner-only credential storage. An Edge-publish
credential SHALL NOT administer Home or publish as another Edge. Revocation
SHALL disconnect the relationship and block reconnect.

#### Scenario: Credential is scoped and cannot escalate

- **WHEN** a client presents an Edge-publish credential
- **THEN** it cannot administer Home or publish as another Edge

#### Scenario: Revocation disconnects and blocks reconnect

- **WHEN** an administrator revokes a pairing credential
- **THEN** the relationship is disconnected and reconnect with that credential
  is blocked

#### Scenario: Credentials never shared across relationships

- **WHEN** credentials are issued
- **THEN** no Home secret is reused across devices or Edges

### Requirement: Discovery is not trust

Bonjour MAY discover Edge feed/pairing endpoints on a LAN, and a remote Home
SHALL use an explicit HTTPS URL and publicly trusted TLS. Network discovery
SHALL NOT by itself authorize presence delivery.

#### Scenario: Remote Home uses explicit trusted TLS

- **WHEN** an Edge connects to a remote Home
- **THEN** it uses an explicit HTTPS URL and publicly trusted TLS

#### Scenario: Discovery does not authorize

- **WHEN** an endpoint is discovered on the network
- **THEN** presence delivery still requires a successful Pairing Ceremony

### Requirement: No long-lived secret exposure

The ceremony SHALL NOT print, log, transmit in status, or persist outside
owner-only credential storage any long-lived token, private key, Wi-Fi
credential, prompt, command, tool payload, or transcript.

#### Scenario: Secrets stay out of logs and status

- **WHEN** pairing and credential operations occur
- **THEN** no long-lived token, private key, Wi-Fi credential, prompt, command,
  tool payload, or transcript is printed, logged, transmitted in status, or
  persisted outside owner-only credential storage
