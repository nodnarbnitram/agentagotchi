package codex

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidThreadID(t *testing.T) {
	for _, value := range []string{"6ba7b810-9dad-11d1-80b4-00c04fd430c8", "019fa063-b4d1-7d81-bced-7f9f55ec7611"} {
		if !ValidThreadID(value) {
			t.Fatalf("valid UUID rejected: %q", value)
		}
	}
	for _, value := range []string{"", "../../bad", "thr_123", "https://example.com", "019fa063-b4d1-0d81-bced-7f9f55ec7611", "019fa063-b4d1-7d81-7ced-7f9f55ec7611"} {
		if ValidThreadID(value) {
			t.Fatalf("invalid id accepted: %q", value)
		}
	}
}

func TestFocusOpensOnlyExactThreadAndNeverFallsBack(t *testing.T) {
	var calls [][]string
	run := func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return errors.New("open failed")
	}
	id := "019fa063-b4d1-7d81-bced-7f9f55ec7611"
	if err := focus("darwin", id, run); err == nil {
		t.Fatal("failed exact open reported success")
	}
	want := [][]string{{"/usr/bin/open", "codex://threads/" + id}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %#v, want exact-only %#v", calls, want)
	}
	for _, call := range calls {
		for _, arg := range call {
			if arg == "-b" || arg == "com.openai.codex" {
				t.Fatalf("app-open fallback invoked: %#v", call)
			}
		}
	}
}
