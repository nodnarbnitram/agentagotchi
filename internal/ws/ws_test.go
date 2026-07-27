package ws

import "testing"

func TestWebsocketAccept(t *testing.T) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	const want = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := websocketAccept(key); got != want {
		t.Fatalf("accept = %q, want %q", got, want)
	}
}
