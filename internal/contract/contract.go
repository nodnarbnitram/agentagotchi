// Package contract defines the privacy boundary for every wire message that
// crosses an Edge: Presence Feeds, Edge upstream snapshots, and administration
// status. Wire types are deliberately separate structs containing only
// allowlisted fields — private data is structurally unrepresentable rather
// than filtered after the fact.
//
// Allowlisted (docs/PROTOCOL.md): Task Presence ID, Safe Title, generic
// state/reason, allowlisted capability names, counts, ordering metadata,
// timestamps.
//
// Never allowed: prompts, commands, tool payloads, transcripts, full paths,
// credentials, tokens, private keys, Wi-Fi secrets, native harness session
// IDs, hostnames, workspace names.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Schema identifiers. Receivers fail closed on unknown or missing schemas.
const (
	SchemaFeedV1     = "agentagotchi.feed.v1"
	SchemaUpstreamV1 = "agentagotchi.upstream.v1"
	SchemaIPCV1      = "agentagotchi.ipc.v1"
	SchemaPairingV1  = "agentagotchi.pairing.v1"
	SchemaAdminV1    = "agentagotchi.admin.v1"
)

// Capability is an allowlisted Device Capability name. focus is the only
// capability in this protocol version.
type Capability string

const CapabilityFocus Capability = "focus"

// FeedTask is the wire form of one Task Presence. It has no field that could
// carry a native session ID, path, prompt, transcript, command, or tool
// payload.
type FeedTask struct {
	TaskPresenceID string       `json:"taskPresenceId"`
	SafeTitle      string       `json:"safeTitle"`
	State          string       `json:"state"`
	Reason         string       `json:"reason"`
	SubagentCount  int          `json:"subagentCount"`
	Capabilities   []Capability `json:"capabilities"`
	UpdatedAt      time.Time    `json:"updatedAt"`
	Snoozed        bool         `json:"snoozed"`
}

// Counts is the wire form of per-state Task Presence counts.
type Counts struct {
	NeedsInput int `json:"needsInput"`
	Blocked    int `json:"blocked"`
	Ready      int `json:"ready"`
	Running    int `json:"running"`
}

// Origin identifies the Edge that owns a snapshot, with ordering metadata.
type Origin struct {
	Kind       string `json:"kind"` // "edge" or "home"
	ID         string `json:"id"`
	Generation uint64 `json:"generation"`
	Revision   uint64 `json:"revision"`
}

// FeedSnapshot is the complete-replacement Presence Feed snapshot
// (docs/adr/0004). It is the only presence payload a device or client ever
// receives.
type FeedSnapshot struct {
	Schema         string     `json:"schema"`
	Type           string     `json:"type"` // "snapshot"
	Origin         Origin     `json:"origin"`
	GeneratedAt    time.Time  `json:"generatedAt"`
	AggregateState string     `json:"aggregateState"`
	Counts         Counts     `json:"counts"`
	Tasks          []FeedTask `json:"tasks"`
}

// UpstreamActionRequest is a Home → Edge reverse-routed action.
type UpstreamActionRequest struct {
	Schema         string     `json:"schema"`
	Type           string     `json:"type"` // "action_request"
	ActionID       string     `json:"actionId"`
	Capability     Capability `json:"capability"`
	TaskPresenceID string     `json:"taskPresenceId"`
	SeenRevision   uint64     `json:"seenRevision"`
}

// UpstreamSnapshot is the Edge → Home absolute snapshot. Same allowlist as
// the feed, plus Edge ordering metadata.
type UpstreamSnapshot struct {
	Schema              string     `json:"schema"`
	Type                string     `json:"type"` // "snapshot"
	EdgeID              string     `json:"edgeId"`
	Generation          uint64     `json:"generation"`
	Revision            uint64     `json:"revision"`
	SnapshotGeneratedAt time.Time  `json:"snapshotGeneratedAt"`
	Tasks               []FeedTask `json:"tasks"`
	Counts              Counts     `json:"counts"`
	AggregateState      string     `json:"aggregateState"`
}

// FeedAction is a Device Capability action from a device/client.
type FeedAction struct {
	Schema         string     `json:"schema"`
	Type           string     `json:"type"` // "action"
	ActionID       string     `json:"actionId"`
	Capability     Capability `json:"capability"`
	TaskPresenceID string     `json:"taskPresenceId"`
	SeenRevision   uint64     `json:"seenRevision"`
}

// ActionResult acknowledges an action only on exact outcome; actions are
// never queued (docs/adr/0006).
type ActionResult struct {
	Schema   string `json:"schema"`
	Type     string `json:"type"` // "action_result"
	ActionID string `json:"actionId"`
	Status   string `json:"status"` // ok | stale | unsupported | failed
}

// IPCHookEvent is the sanitized, one-shot Codex hook frame. Workspace is a
// basename only and remains on the owner-only Edge IPC boundary; it is never
// projected into a feed or upstream snapshot.
type IPCHookEvent struct {
	Schema          string    `json:"schema"`
	Type            string    `json:"type"` // "hook_event"
	EventID         string    `json:"eventId"`
	Harness         string    `json:"harness"`
	NativeSessionID string    `json:"nativeSessionId"`
	Event           string    `json:"event"`
	ToolName        string    `json:"toolName,omitempty"`
	TurnID          string    `json:"turnId,omitempty"`
	AgentID         string    `json:"agentId,omitempty"`
	Workspace       string    `json:"workspace,omitempty"`
	At              time.Time `json:"at"`
}

// IPCAdapterHello starts a long-lived leased adapter session.
type IPCAdapterHello struct {
	Schema         string       `json:"schema"`
	Type           string       `json:"type"` // "adapter_hello"
	Harness        string       `json:"harness"`
	AdapterVersion string       `json:"adapterVersion"`
	Capabilities   []Capability `json:"capabilities"`
}

type IPCHelloAck struct {
	Schema       string `json:"schema"`
	Type         string `json:"type"` // "hello_ack"
	LeaseID      string `json:"leaseId"`
	LeaseSeconds int64  `json:"leaseSeconds"`
}

// IPCPresence is an adapter's private absolute statement. NativeSessionID
// and DisplayKey are accepted only on owner-only IPC and are structurally
// absent from every Edge-crossing wire type above.
type IPCPresence struct {
	NativeSessionID string    `json:"nativeSessionId"`
	DisplayKey      string    `json:"displayKey,omitempty"`
	SafeTitle       string    `json:"safeTitle,omitempty"`
	State           string    `json:"state"`
	Reason          string    `json:"reason"`
	SubagentCount   int       `json:"subagentCount,omitempty"`
	ObservedAt      time.Time `json:"observedAt,omitempty"`
}

type IPCPresenceReport struct {
	Schema      string        `json:"schema"`
	Type        string        `json:"type"` // "presence_report"
	LeaseID     string        `json:"leaseId"`
	ProducerSeq uint64        `json:"producerSeq"`
	Reports     []IPCPresence `json:"reports"`
	Ends        []string      `json:"ends"`
}

type IPCHeartbeat struct {
	Schema  string `json:"schema"`
	Type    string `json:"type"` // "heartbeat"
	LeaseID string `json:"leaseId"`
}

type IPCActionRequest struct {
	Schema         string     `json:"schema"`
	Type           string     `json:"type"` // "action_request"
	ActionID       string     `json:"actionId"`
	Capability     Capability `json:"capability"`
	TaskPresenceID string     `json:"taskPresenceId"`
}

type IPCActionResult struct {
	Schema   string `json:"schema"`
	Type     string `json:"type"` // "action_result"
	ActionID string `json:"actionId"`
	Status   string `json:"status"` // ok | rejected | unsupported
}

// AdminStatus is the wire form of administration status: connectivity,
// counts, and timestamps only.
type AdminStatus struct {
	Schema         string    `json:"schema"`
	Type           string    `json:"type"` // "status"
	Role           string    `json:"role"` // "edge" | "home"
	Version        string    `json:"version"`
	StartedAt      time.Time `json:"startedAt"`
	PairedDevices  int       `json:"pairedDevices"`
	PairedEdges    int       `json:"pairedEdges,omitempty"`
	ConnectedPeers int       `json:"connectedPeers"`
	TaskPresences  int       `json:"taskPresences"`
	AggregateState string    `json:"aggregateState"`
}

// allowedStates and allowedReasons are the generic vocabulary
// (docs/PROTOCOL.md).
var allowedStates = map[string]bool{
	"idle": true, "running": true, "needs_input": true, "ready": true, "blocked": true,
}

var allowedReasons = map[string]bool{
	"working": true, "question": true, "approval": true,
	"permission": true, "completed": true, "failed": true,
}

// ValidState reports whether s is in the generic state vocabulary.
func ValidState(s string) bool { return allowedStates[s] }

// ValidReason reports whether r is in the generic reason vocabulary.
func ValidReason(r string) bool { return allowedReasons[r] }

// MaxSafeTitleBytes bounds Safe Titles (docs/PROTOCOL.md).
const MaxSafeTitleBytes = 64

// SanitizeSafeTitle normalizes whitespace and bounds a Safe Title. Callers
// must never pass hostnames, paths, session names, prompts, transcripts, or
// commands; this function is the final bound, not the source of safety.
func SanitizeSafeTitle(v string) string {
	v = strings.Join(strings.Fields(v), " ")
	if len(v) > MaxSafeTitleBytes {
		v = strings.TrimSpace(v[:MaxSafeTitleBytes-3]) + "..."
	}
	return v
}

// Encode marshals v after asserting its declared schema is known. Wire
// structs carry no private fields by construction; this is the choke point
// where schema identification is enforced on egress.
func Encode(schema string, v any) ([]byte, error) {
	switch schema {
	case SchemaFeedV1, SchemaUpstreamV1, SchemaIPCV1, SchemaPairingV1, SchemaAdminV1:
	default:
		return nil, fmt.Errorf("contract: unknown schema %q", schema)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(b, &probe); err != nil || probe.Schema != schema {
		return nil, fmt.Errorf("contract: encoded message missing schema %q", schema)
	}
	return b, nil
}

// DecodeStrict unmarshals a wire message, rejecting unknown fields and
// verifying the schema matches want. Fail-closed per docs/PROTOCOL.md.
func DecodeStrict(data []byte, wantSchema string, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("contract: decode %s: %w", wantSchema, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("contract: decode %s: trailing JSON value", wantSchema)
	}
	var probe struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("contract: probe %s: %w", wantSchema, err)
	}
	if probe.Schema != wantSchema {
		return fmt.Errorf("contract: schema mismatch: got %q want %q", probe.Schema, wantSchema)
	}
	return nil
}
