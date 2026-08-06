package edge

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStatePersistsOnlyGenerationAndAliasesOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first, err := loadAndAdvanceState(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Aliases["task-id"] = "Approved alias"
	if err := persistState(path, first); err != nil {
		t.Fatal(err)
	}
	second, err := loadAndAdvanceState(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != first.Generation+1 || second.Aliases["task-id"] != "Approved alias" {
		t.Fatalf("restart state = %+v, first = %+v", second, first)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	for _, forbidden := range []string{"nativeSession", "displayKey", "workspace", "title"} {
		if bytes.Contains(b, []byte(forbidden)) {
			t.Fatalf("state contains private field %q: %s", forbidden, b)
		}
	}
}
