package focus

import "testing"

func TestValidThreadID(t *testing.T) {
	for _, value := range []string{
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		"019fa063-b4d1-7d81-bced-7f9f55ec7611",
	} {
		if !ValidThreadID(value) {
			t.Fatalf("valid UUID rejected: %q", value)
		}
	}
	for _, value := range []string{
		"", "../../bad", "thr_123", "https://example.com",
		"019fa063-b4d1-0d81-bced-7f9f55ec7611",
		"019fa063-b4d1-7d81-7ced-7f9f55ec7611",
	} {
		if ValidThreadID(value) {
			t.Fatalf("invalid id accepted: %q", value)
		}
	}
}
