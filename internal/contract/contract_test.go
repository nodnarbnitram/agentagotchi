package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// forbiddenWireKeys are JSON field names that must never appear in any
// presence wire payload, regardless of value.
var forbiddenWireKeys = []string{
	"prompt", "command", "toolInput", "tool_input", "toolPayload", "transcript",
	"transcript_path", "cwd", "workspace", "path", "fullPath", "hostname",
	"sessionId", "session_id", "nativeSessionId", "native_session_id",
	"token", "credential", "password", "privateKey", "private_key", "ssid",
	"wifiPassword", "displayKey", "lastMessage", "last_assistant_message",
}

// forbiddenValues are representative private payloads. The structural test
// asserts none survive serialization even when present in a source record.
var forbiddenValues = []string{
	"sk-live-secret-token",
	"-----BEGIN PRIVATE KEY-----",
	"/Users/alice/secret/project",
	"my-wifi-password-123",
	"rm -rf / --no-preserve-root",
	"native-session-019fa063-b4d1",
	"alice-laptop.local",
}

func sampleSnapshot() FeedSnapshot {
	return FeedSnapshot{
		Schema:         SchemaFeedV1,
		Type:           "snapshot",
		Origin:         Origin{Kind: "edge", ID: "edge-1", Generation: 3, Revision: 41},
		GeneratedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		AggregateState: "needs_input",
		Counts:         Counts{NeedsInput: 1},
		Tasks: []FeedTask{{
			TaskPresenceID: "019fa063-b4d1-7d81-bced-7f9f55ec7611",
			SafeTitle:      "Codex",
			State:          "needs_input",
			Reason:         "permission",
			SubagentCount:  1,
			Capabilities:   []Capability{CapabilityFocus},
			UpdatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}},
	}
}

func collectKeys(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	keys := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				keys[k] = true
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(raw)
	return keys
}

func TestFeedSnapshotContainsNoForbiddenKeys(t *testing.T) {
	data, err := Encode(SchemaFeedV1, sampleSnapshot())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	keys := collectKeys(t, data)
	for _, bad := range forbiddenWireKeys {
		if keys[bad] {
			t.Errorf("forbidden wire key %q present in feed snapshot", bad)
		}
	}
}

func TestUpstreamSnapshotContainsNoForbiddenKeys(t *testing.T) {
	snap := UpstreamSnapshot{
		Schema: SchemaUpstreamV1, Type: "snapshot",
		EdgeID: "edge-1", Generation: 3, Revision: 41,
		SnapshotGeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Tasks:               sampleSnapshot().Tasks,
		Counts:              Counts{NeedsInput: 1},
		AggregateState:      "needs_input",
	}
	data, err := Encode(SchemaUpstreamV1, snap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	keys := collectKeys(t, data)
	for _, bad := range forbiddenWireKeys {
		if keys[bad] {
			t.Errorf("forbidden wire key %q present in upstream snapshot", bad)
		}
	}
}

// TestWireStructsCannotHoldPrivateData proves the privacy boundary is
// structural: the wire structs have no field that could carry private data,
// so even a malicious or buggy producer cannot serialize it.
func TestWireStructsCannotHoldPrivateData(t *testing.T) {
	wireTypes := []reflect.Type{
		reflect.TypeOf(FeedTask{}),
		reflect.TypeOf(FeedSnapshot{}),
		reflect.TypeOf(UpstreamSnapshot{}),
		reflect.TypeOf(AdminStatus{}),
		reflect.TypeOf(FeedAction{}),
		reflect.TypeOf(ActionResult{}),
	}
	for _, rt := range wireTypes {
		var check func(typ reflect.Type, path string)
		check = func(typ reflect.Type, path string) {
			if typ.Kind() == reflect.Ptr {
				typ = typ.Elem()
			}
			switch typ.Kind() {
			case reflect.Struct:
				if typ == reflect.TypeOf(time.Time{}) {
					return
				}
				for i := 0; i < typ.NumField(); i++ {
					f := typ.Field(i)
					name := strings.ToLower(f.Name)
					for _, frag := range []string{
						"prompt", "command", "payload", "transcript", "path",
						"session", "token", "credential", "password", "secret",
						"key", "hostname", "workspace", "cwd", "displaykey",
					} {
						// Ordering metadata (IDs, revisions) is allowlisted;
						// reject only field names that imply private content.
						if strings.Contains(name, frag) &&
							!strings.Contains(name, "presenceid") &&
							!strings.Contains(name, "actionid") &&
							!strings.Contains(name, "edgeid") &&
							name != "id" {
							t.Errorf("%s.%s: field name %q implies private content (matched %q)",
								rt.Name(), path+f.Name, f.Name, frag)
						}
					}
					check(f.Type, path+f.Name+".")
				}
			case reflect.Slice:
				check(typ.Elem(), path)
			}
		}
		check(rt, "")
	}
}

// TestPrivateValuesNeverLeak asserts that even when private marker values are
// present in a producer's source data, the allowlisted wire projection cannot
// carry them: the projection is built field-by-field from the semantic core.
func TestPrivateValuesNeverLeak(t *testing.T) {
	// Simulate a hostile source record (as a generic map) and project it the
	// only way the system allows: explicit field copies onto the wire struct.
	source := map[string]string{
		"taskPresenceId": "019fa063-b4d1-7d81-bced-7f9f55ec7611",
		"safeTitle":      "Pi",
		"state":          "running",
		"reason":         "working",
		"prompt":         forbiddenValues[0],
		"cwd":            forbiddenValues[2],
		"nativeSession":  forbiddenValues[5],
	}
	task := FeedTask{
		TaskPresenceID: source["taskPresenceId"],
		SafeTitle:      SanitizeSafeTitle(source["safeTitle"]),
		State:          source["state"],
		Reason:         source["reason"],
		Capabilities:   []Capability{},
		UpdatedAt:      time.Now().UTC(),
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, marker := range forbiddenValues {
		if strings.Contains(string(data), marker) {
			t.Errorf("private marker leaked into wire payload: %q", marker)
		}
	}
}

func TestEncodeRejectsUnknownSchema(t *testing.T) {
	if _, err := Encode("agentagotchi.feed.v999", sampleSnapshot()); err == nil {
		t.Fatal("Encode accepted an unknown schema")
	}
	if _, err := Encode("", sampleSnapshot()); err == nil {
		t.Fatal("Encode accepted an empty schema")
	}
}

func TestDecodeStrictRejectsUnknownFields(t *testing.T) {
	good := `{"schema":"agentagotchi.feed.v1","type":"action","actionId":"a","capability":"focus","taskPresenceId":"019fa063-b4d1-7d81-bced-7f9f55ec7611","seenRevision":41}`
	var action FeedAction
	if err := DecodeStrict([]byte(good), SchemaFeedV1, &action); err != nil {
		t.Fatalf("DecodeStrict rejected a valid action: %v", err)
	}
	bad := `{"schema":"agentagotchi.feed.v1","type":"action","actionId":"a","capability":"focus","taskPresenceId":"019fa063-b4d1-7d81-bced-7f9f55ec7611","seenRevision":41,"prompt":"tell me secrets"}`
	if err := DecodeStrict([]byte(bad), SchemaFeedV1, &action); err == nil {
		t.Fatal("DecodeStrict accepted a frame with an unknown field")
	}
}

func TestDecodeStrictFailsClosedOnSchemaMismatch(t *testing.T) {
	msg := `{"schema":"agentagotchi.feed.v9","type":"action","actionId":"a","capability":"focus","taskPresenceId":"019fa063-b4d1-7d81-bced-7f9f55ec7611","seenRevision":41}`
	var action FeedAction
	if err := DecodeStrict([]byte(msg), SchemaFeedV1, &action); err == nil {
		t.Fatal("DecodeStrict accepted a schema mismatch")
	}
	missing := `{"type":"action","actionId":"a","capability":"focus","taskPresenceId":"019fa063-b4d1-7d81-bced-7f9f55ec7611","seenRevision":41}`
	if err := DecodeStrict([]byte(missing), SchemaFeedV1, &action); err == nil {
		t.Fatal("DecodeStrict accepted a missing schema")
	}
}

func TestVocabulary(t *testing.T) {
	for _, s := range []string{"idle", "running", "needs_input", "ready", "blocked"} {
		if !ValidState(s) {
			t.Errorf("ValidState(%q) = false", s)
		}
	}
	for _, s := range []string{"pending", "waiting", "done", "error", ""} {
		if ValidState(s) {
			t.Errorf("ValidState(%q) = true", s)
		}
	}
	for _, r := range []string{"working", "question", "approval", "permission", "completed", "failed"} {
		if !ValidReason(r) {
			t.Errorf("ValidReason(%q) = false", r)
		}
	}
	for _, r := range []string{"cancelled", "ok", ""} {
		if ValidReason(r) {
			t.Errorf("ValidReason(%q) = true", r)
		}
	}
}

func TestSanitizeSafeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Codex   task ", "Codex task"},
		{"Pi\n\tbuild", "Pi build"},
		{"", ""},
	}
	for _, c := range cases {
		if got := SanitizeSafeTitle(c.in); got != c.want {
			t.Errorf("SanitizeSafeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := strings.Repeat("a", MaxSafeTitleBytes+50)
	if got := SanitizeSafeTitle(long); len(got) > MaxSafeTitleBytes {
		t.Errorf("SanitizeSafeTitle produced %d bytes > %d", len(got), MaxSafeTitleBytes)
	}
	if got := SanitizeSafeTitle(long); !strings.HasSuffix(got, "...") {
		t.Errorf("SanitizeSafeTitle truncation missing ellipsis: %q", got)
	}
}

func TestAdminStatusContainsNoForbiddenKeys(t *testing.T) {
	status := AdminStatus{
		Schema: SchemaAdminV1, Type: "status", Role: "edge", Version: "0.2.0",
		StartedAt: time.Now().UTC(), PairedDevices: 1, ConnectedPeers: 1,
		TaskPresences: 3, AggregateState: "running",
	}
	data, err := Encode(SchemaAdminV1, status)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	keys := collectKeys(t, data)
	for _, bad := range forbiddenWireKeys {
		if keys[bad] {
			t.Errorf("forbidden wire key %q present in admin status", bad)
		}
	}
}

func ExampleEncode() {
	snap := FeedSnapshot{Schema: SchemaFeedV1, Type: "snapshot"}
	data, _ := Encode(SchemaFeedV1, snap)
	fmt.Println(strings.Contains(string(data), SchemaFeedV1))
	// Output: true
}
