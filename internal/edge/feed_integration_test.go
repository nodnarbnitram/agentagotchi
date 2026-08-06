package edge

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/presence"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := NewService(Options{
		DataDir: t.TempDir(), HostName: "localhost", DisableMDNS: true,
		DisableAppServer: true, FeedAuthenticator: BearerFeedAuthenticator{Token: "test-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFeedSnapshotAndCapabilityActionIntegration(t *testing.T) {
	s := newTestService(t)
	lease, err := s.core.Attach("test-adapter", []contract.Capability{contract.CapabilityFocus})
	if err != nil {
		t.Fatal(err)
	}
	nativeID := "019fa063-b4d1-7d81-bced-7f9f55ec7611"
	if _, err := s.core.ApplyReports(lease.ID, 1, []presence.Report{{
		NativeSessionID: nativeID, SafeTitle: "Agent", State: presence.StateRunning, Reason: presence.ReasonWorking,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	id, _ := s.core.TaskPresenceIDFor("test-adapter", nativeID)
	if err := s.core.SetCapabilities(id, []contract.Capability{contract.CapabilityFocus}); err != nil {
		t.Fatal(err)
	}
	dispatched := ""
	s.router.Register("test-adapter", contract.CapabilityFocus, func(_ context.Context, native string, _ contract.FeedAction) error {
		dispatched = native
		return nil
	})

	server := httptest.NewTLSServer(s.Handler())
	defer server.Close()
	conn, reader := dialTestWebSocket(t, server.URL, "/feed/v1", "test-token")
	defer conn.Close()
	var snapshot contract.FeedSnapshot
	if err := contract.DecodeStrict(readServerText(t, reader), contract.SchemaFeedV1, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].TaskPresenceID != id {
		t.Fatalf("unexpected snapshot: %+v", snapshot.Tasks)
	}
	action := contract.FeedAction{
		Schema: contract.SchemaFeedV1, Type: "action", ActionID: "focus-1",
		Capability: contract.CapabilityFocus, TaskPresenceID: id,
		SeenRevision: snapshot.Origin.Revision,
	}
	writeClientJSON(t, conn, action)
	var result contract.ActionResult
	if err := contract.DecodeStrict(readServerText(t, reader), contract.SchemaFeedV1, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || dispatched != nativeID {
		t.Fatalf("focus result=%+v dispatched=%q", result, dispatched)
	}

	action.ActionID = "unsupported-1"
	action.Capability = "approve"
	writeClientJSON(t, conn, action)
	if err := contract.DecodeStrict(readServerText(t, reader), contract.SchemaFeedV1, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "unsupported" {
		t.Fatalf("unknown capability status = %q", result.Status)
	}
}

func TestHookPresenceFeedDoesNotLeakNativeIDOrWorkspace(t *testing.T) {
	s := newTestService(t)
	s.applyCodexHook(contract.IPCHookEvent{
		Schema: contract.SchemaIPCV1, Type: "hook_event", EventID: "event-1", Harness: "codex",
		NativeSessionID: "019fa063-b4d1-7d81-bced-7f9f55ec7611", Event: "UserPromptSubmit",
		Workspace: "secret-project", At: time.Now().UTC(),
	})
	b, err := contract.Encode(contract.SchemaFeedV1, s.core.Snapshot(s.edgeID, "edge"))
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"019fa063-b4d1-7d81-bced-7f9f55ec7611", "secret-project", "nativeSession", "workspace"} {
		if strings.Contains(string(b), private) {
			t.Fatalf("feed leaked %q: %s", private, b)
		}
	}
}

func dialTestWebSocket(t *testing.T, serverURL, path, token string) (net.Conn, *bufio.Reader) {
	t.Helper()
	u, _ := url.Parse(serverURL)
	conn, err := tls.Dial("tcp", u.Host, &tls.Config{InsecureSkipVerify: true}) // test server only
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	_, _ = fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", path, u.Host, token, key)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		t.Fatalf("websocket status = %d", response.StatusCode)
	}
	return conn, reader
}

func readServerText(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		t.Fatal(err)
	}
	length := uint64(header[1] & 0x7f)
	if length == 126 {
		var n uint16
		if err := binary.Read(reader, binary.BigEndian, &n); err != nil {
			t.Fatal(err)
		}
		length = uint64(n)
	} else if length == 127 {
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			t.Fatal(err)
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func writeClientJSON(t *testing.T, conn net.Conn, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	mask := [4]byte{1, 2, 3, 4}
	frame := []byte{0x81}
	if len(payload) < 126 {
		frame = append(frame, 0x80|byte(len(payload)))
	} else {
		frame = append(frame, 0x80|126, byte(len(payload)>>8), byte(len(payload)))
	}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	if _, err := conn.Write(frame); err != nil {
		t.Fatal(err)
	}
}
