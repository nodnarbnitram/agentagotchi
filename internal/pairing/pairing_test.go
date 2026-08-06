package pairing

import (
	"strings"
	"testing"
	"time"
)

func testCeremony() (*Ceremony, *time.Time) {
	now := &time.Time{}
	*now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return New(func() time.Time { return *now }), now
}

func TestApproveRedeemIssuesScopedCredential(t *testing.T) {
	c, _ := testCeremony()
	code, err := c.RequestCode(RoleFeed, "BOX-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(code.Token) != codeEntropyBytes*2 {
		t.Fatalf("code token length = %d", len(code.Token))
	}
	// Unapproved code grants nothing.
	if _, err := c.Redeem(code.Token); err != ErrNotApproved {
		t.Fatalf("redeem unapproved = %v, want ErrNotApproved", err)
	}
	if err := c.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	cred, err := c.Redeem(code.Token)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Role != RoleFeed || len(cred.Token) != tokenEntropyBytes*2 {
		t.Fatalf("credential = %+v", cred)
	}
	// Authenticates.
	resolved, ok := c.Authenticate(cred.Token)
	if !ok || resolved.ID != cred.ID {
		t.Fatal("authenticate failed for fresh credential")
	}
}

func TestCodeIsOneUseAndNotReplayable(t *testing.T) {
	c, _ := testCeremony()
	code, _ := c.RequestCode(RoleFeed, "BOX-3")
	if err := c.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Redeem(code.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Redeem(code.Token); err != ErrCodeNotFound {
		t.Fatalf("replay = %v, want ErrCodeNotFound", err)
	}
}

func TestCodeExpires(t *testing.T) {
	c, now := testCeremony()
	code, _ := c.RequestCode(RoleEdgeIngress, "edge-a")
	*now = now.Add(CodeTTL + time.Second)
	if err := c.Approve(code.ID); err != ErrCodeNotFound {
		t.Fatalf("approve expired = %v, want ErrCodeNotFound", err)
	}
	if _, err := c.Redeem(code.Token); err != ErrCodeNotFound {
		t.Fatalf("redeem expired = %v, want ErrCodeNotFound", err)
	}
}

func TestDenyGrantsNothing(t *testing.T) {
	c, _ := testCeremony()
	code, _ := c.RequestCode(RoleFeed, "BOX-3")
	if err := c.Deny(code.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Redeem(code.Token); err != ErrCodeNotFound {
		t.Fatalf("redeem denied = %v, want ErrCodeNotFound", err)
	}
}

func TestCredentialsAreUniquePerRelationship(t *testing.T) {
	c, _ := testCeremony()
	seen := map[string]bool{}
	for range 3 {
		code, _ := c.RequestCode(RoleFeed, "BOX-3")
		if err := c.Approve(code.ID); err != nil {
			t.Fatal(err)
		}
		cred, err := c.Redeem(code.Token)
		if err != nil {
			t.Fatal(err)
		}
		if seen[cred.Token] {
			t.Fatal("credential token reused across relationships")
		}
		seen[cred.Token] = true
	}
}

func TestRevocationBlocksReconnect(t *testing.T) {
	c, _ := testCeremony()
	code, _ := c.RequestCode(RoleFeed, "BOX-3")
	if err := c.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	cred, _ := c.Redeem(code.Token)
	if err := c.Revoke(cred.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Authenticate(cred.Token); ok {
		t.Fatal("revoked credential still authenticates")
	}
	if err := c.Revoke("unknown"); err != ErrUnknownCred {
		t.Fatalf("revoke unknown = %v", err)
	}
}

func TestRoleScopingAndAdminExclusion(t *testing.T) {
	c, _ := testCeremony()
	if _, err := c.RequestCode(RoleAdmin, "root"); err != ErrRoleInvalid {
		t.Fatalf("ceremony issued admin role code: %v", err)
	}
	if _, err := c.RequestCode("superuser", "x"); err != ErrRoleInvalid {
		t.Fatalf("invalid role accepted: %v", err)
	}
	// A feed credential presented for a different scope is distinguishable by
	// role — the caller enforces scope; the credential carries it.
	code, _ := c.RequestCode(RoleFeed, "BOX-3")
	if err := c.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	cred, _ := c.Redeem(code.Token)
	resolved, ok := c.Authenticate(cred.Token)
	if !ok || resolved.Role != RoleFeed {
		t.Fatal("role not preserved on authenticate")
	}
}

func TestListRedactsTokens(t *testing.T) {
	c, _ := testCeremony()
	code, _ := c.RequestCode(RoleFeed, "BOX-3")
	if err := c.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	cred, _ := c.Redeem(code.Token)
	for _, listed := range c.List() {
		if listed.Token != "" {
			t.Fatal("List exposed a credential token")
		}
		if strings.Contains(listed.ID, cred.Token) {
			t.Fatal("List leaked token material")
		}
	}
}

func TestStoreRoundTripOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := testCeremony()
	code, _ := c.RequestCode(RoleEdgeIngress, "edge-a")
	if err := c.Approve(code.ID); err != nil {
		t.Fatal(err)
	}
	cred, _ := c.Redeem(code.Token)
	if err := store.Save([]Credential{cred}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Token != cred.Token {
		t.Fatalf("round trip = %+v", loaded)
	}
	// Permissions: 0600 file, 0700 dir.
	info, err := store.Load()
	_ = info
	if err != nil {
		t.Fatal(err)
	}
	stat, err := storeStat(store)
	if err != nil {
		t.Fatal(err)
	}
	if stat.filePerm != 0o600 || stat.dirPerm != 0o700 {
		t.Fatalf("perms file=%o dir=%o", stat.filePerm, stat.dirPerm)
	}
	// Import into a fresh ceremony: revoked state and auth survive.
	fresh, _ := testCeremony()
	fresh.Import(loaded)
	if _, ok := fresh.Authenticate(cred.Token); !ok {
		t.Fatal("imported credential does not authenticate")
	}
	if err := fresh.Revoke(cred.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(fresh.Export()); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := store.Load()
	third, _ := testCeremony()
	third.Import(reloaded)
	if _, ok := third.Authenticate(cred.Token); ok {
		t.Fatal("revoked credential authenticated after restart")
	}
}

func TestPendingDoesNotIncludeConsumedOrExpired(t *testing.T) {
	c, now := testCeremony()
	a, _ := c.RequestCode(RoleFeed, "a")
	_, _ = c.RequestCode(RoleFeed, "b")
	if err := c.Approve(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Redeem(a.Token); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	pending := c.Pending()
	if len(pending) != 1 || pending[0].ClientName != "b" {
		t.Fatalf("pending = %+v", pending)
	}
}
