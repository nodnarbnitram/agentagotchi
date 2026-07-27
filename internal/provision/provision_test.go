package provision

import (
	"encoding/json"
	"testing"
)

func TestProvisionMessageIncludesUnixTime(t *testing.T) {
	payload, err := json.Marshal(message{
		Type: "provision", Version: 1, UnixTime: 1_785_100_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["unixTime"] != float64(1_785_100_000) {
		t.Fatalf("unixTime missing or changed: %s", payload)
	}
}
