package pairing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// persistedFile is the on-disk shape. Tokens are secrets: the file is 0600 in
// a 0700 directory and is the only place they persist.
type persistedFile struct {
	Credentials []Credential `json:"credentials"`
}

// Load reads credentials from disk. A missing store is empty, not an error.
func (s *Store) Load() ([]Credential, error) {
	b, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p persistedFile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("pairing: decode credential store: %w", err)
	}
	return p.Credentials, nil
}

// Save atomically replaces the store with the given credentials.
func (s *Store) Save(credentials []Credential) error {
	b, err := json.Marshal(persistedFile{Credentials: credentials})
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

func (s *Store) path() string {
	return filepath.Join(s.dir, "pairings.json")
}

// Import loads persisted credentials into a ceremony (revocation state and
// role scopes survive restart).
func (c *Ceremony) Import(credentials []Credential) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cred := range credentials {
		copy := cred
		c.credentials[copy.ID] = &copy
		if copy.Token != "" {
			c.byToken[copy.Token] = &copy
		}
	}
}

// Export returns all credentials including tokens, for persistence only.
// Callers must write them only to owner-only storage.
func (c *Ceremony) Export() []Credential {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Credential, 0, len(c.credentials))
	for _, cred := range c.credentials {
		out = append(out, *cred)
	}
	return out
}
