package edge

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"agentagotchi.local/agentagotchi/internal/contract"
)

func TestIPCFrameRejectsOversize(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", MaxIPCFrameBytes)+"\n"), MaxIPCFrameBytes)
	if _, err := readIPCFrame(reader); err == nil {
		t.Fatal("oversized IPC frame accepted")
	}
}

func TestIPCStrictDecodeRejectsUnknownFieldAndWrongSchema(t *testing.T) {
	tests := [][]byte{
		[]byte(`{"schema":"agentagotchi.ipc.v1","type":"hook_event","eventId":"e","harness":"codex","nativeSessionId":"n","event":"Stop","at":"2026-01-01T00:00:00Z","surprise":true}`),
		[]byte(`{"schema":"agentagotchi.feed.v1","type":"hook_event","eventId":"e","harness":"codex","nativeSessionId":"n","event":"Stop","at":"2026-01-01T00:00:00Z"}`),
	}
	for _, frame := range tests {
		if envelope, err := decodeIPCEnvelope(frame); err == nil {
			var event contract.IPCHookEvent
			if err := contract.DecodeStrict(frame, contract.SchemaIPCV1, &event); err == nil || envelope.Schema != contract.SchemaIPCV1 {
				t.Fatalf("invalid IPC frame accepted: %s", frame)
			}
		}
	}
	if _, err := readIPCFrame(bufio.NewReader(bytes.NewBufferString(`{"schema":"agentagotchi.ipc.v1"}`))); err == nil {
		t.Fatal("IPC frame without newline accepted")
	}
}
