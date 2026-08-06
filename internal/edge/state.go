package edge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// persistedState is intentionally privacy-safe and minimal. In particular it
// has no native harness session mapping; adapters rebuild that mapping live
// after restart (docs/adr/0004 and docs/adr/0005).
type persistedState struct {
	Generation uint64            `json:"generation"`
	Aliases    map[string]string `json:"aliases"`
}

func loadAndAdvanceState(path string) (persistedState, error) {
	state := persistedState{Aliases: make(map[string]string)}
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &state); err != nil {
			return persistedState{}, fmt.Errorf("decode Edge state: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return persistedState{}, fmt.Errorf("read Edge state: %w", err)
	}
	if state.Aliases == nil {
		state.Aliases = make(map[string]string)
	}
	state.Generation++
	if state.Generation == 0 {
		state.Generation = 1
	}
	if err := persistState(path, state); err != nil {
		return persistedState{}, err
	}
	return state, nil
}

func persistState(path string, state persistedState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Edge data directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("secure Edge data directory: %w", err)
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write Edge state: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit Edge state: %w", err)
	}
	return nil
}
