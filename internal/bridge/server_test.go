package bridge

import "testing"

func TestBearerAndToken(t *testing.T) {
	if got := bearerToken("Bearer abc"); got != "abc" {
		t.Fatalf("bearer = %q", got)
	}
	if bearerToken("Basic abc") != "" {
		t.Fatal("accepted non-bearer auth")
	}
	if !tokenEqual("same", "same") || tokenEqual("same", "different") {
		t.Fatal("token comparison failed")
	}
}
