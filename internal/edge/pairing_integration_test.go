package edge

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"agentagotchi.local/agentagotchi/internal/contract"
	"agentagotchi.local/agentagotchi/internal/pairing"
)

func adminCall(t *testing.T, socketPath string, request map[string]string) map[string]any {
	t.Helper()
	request["schema"] = contract.SchemaAdminV1
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

// TestPairingCeremonyOverAdminIPC proves the full Edge→device direction:
// code request, admin approval, unique scoped credential, feed auth with the
// credential, and revocation disconnecting the live feed.
func TestPairingCeremonyOverAdminIPC(t *testing.T) {
	s := newPairingTestService(t, "pr", 18791)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx) }()
	waitForSocket(t, s, serveErr)

	// Unauthenticated feed attempt: rejected.
	server := httptest.NewTLSServer(s.Handler())
	defer server.Close()

	// 1. Connecting client asks the admin surface to begin (in the real flow
	// the BOX-3 displays the code the Edge CLI prints).
	begin := adminCall(t, s.SocketPath(), map[string]string{
		"type": "pairing_begin", "role": "feed", "clientName": "BOX-3",
	})
	if begin["ok"] != true {
		t.Fatalf("pairing_begin: %v", begin)
	}
	code := begin["code"].(map[string]any)
	codeID := code["id"].(string)
	codeToken := code["token"].(string)
	if codeToken == "" || code["role"] != "pairing" && code["role"] != "feed" {
		t.Fatalf("unexpected code payload: %v", code)
	}

	// 2. Unapproved code redeems nothing (redeem happens client-side via the
	// ceremony object; the state machine is shared).
	if _, err := s.ceremony.Redeem(codeToken); err != pairing.ErrNotApproved {
		t.Fatalf("redeem unapproved = %v", err)
	}

	// 3. Admin approves; client redeems a unique feed-scoped credential.
	if reply := adminCall(t, s.SocketPath(), map[string]string{
		"type": "pairing_approve", "codeId": codeID,
	}); reply["ok"] != true {
		t.Fatalf("pairing_approve: %v", reply)
	}
	cred, err := s.ceremony.Redeem(codeToken)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Role != pairing.RoleFeed {
		t.Fatalf("credential role = %q", cred.Role)
	}
	if err := s.persistCredentials(); err != nil {
		t.Fatal(err)
	}

	// 4. Feed authenticates with the pairing credential (not the legacy token).
	_, reader := dialTestWebSocket(t, server.URL, "/feed/v1", cred.Token)
	var snap contract.FeedSnapshot
	if err := contract.DecodeStrict(readServerText(t, reader), contract.SchemaFeedV1, &snap); err != nil {
		t.Fatal(err)
	}

	// 5. List shows the credential with a redacted token.
	list := adminCall(t, s.SocketPath(), map[string]string{"type": "pairing_list"})
	creds := list["credentials"].([]any)
	if len(creds) != 1 {
		t.Fatalf("pairing_list = %v", creds)
	}
	entry := creds[0].(map[string]any)
	if entry["token"] != nil && entry["token"] != "" {
		t.Fatal("admin list exposed a credential token")
	}

	// 6. Revocation disconnects the live feed and blocks reconnect.
	if reply := adminCall(t, s.SocketPath(), map[string]string{
		"type": "pairing_revoke", "credentialId": cred.ID,
	}); reply["ok"] != true {
		t.Fatalf("pairing_revoke: %v", reply)
	}
	// Live connection is dropped: next read fails.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := readServerTextOrErr(reader); err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Reconnect with the revoked credential fails before upgrade.
	conn, err := dialTestWebSocketExpectStatus(t, server.URL, "/feed/v1", cred.Token, 401)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

// TestPairingSecretsNeverInAdminStatus checks status output carries no tokens.
func TestPairingSecretsNeverInAdminStatus(t *testing.T) {
	s := newPairingTestService(t, "ps", 18792)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx) }()
	waitForSocket(t, s, serveErr)

	code, err := s.ceremony.RequestCode(pairing.RoleFeed, "BOX-3")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ceremony.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	cred, err := s.ceremony.Redeem(code.Token)
	if err != nil {
		t.Fatal(err)
	}
	status := adminCall(t, s.SocketPath(), map[string]string{"type": "status"})
	blob, _ := json.Marshal(status)
	for _, secret := range []string{cred.Token, code.Token} {
		if len(secret) > 0 && jsonContains(blob, secret) {
			t.Fatalf("admin status leaked secret material: %s", blob)
		}
	}
}

// newPairingTestService builds a service with the production default
// authenticator (legacy token + pairing credentials).
func newPairingTestService(t *testing.T, tag string, port int) *Service {
	t.Helper()
	s, err := NewService(Options{
		DataDir: shortDataDir(t, tag), HostName: "localhost", Port: port,
		DisableMDNS: true, DisableAppServer: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
