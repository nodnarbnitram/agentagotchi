// Pairing integration for the Edge service: the Pairing Ceremony, feed
// authentication against role-scoped credentials, and revocation-driven
// disconnects.
package edge

import (
	"crypto/subtle"
	"net/http"
	"path/filepath"
	"time"

	"agentagotchi.local/agentagotchi/internal/pairing"
)

// PairingFeedAuthenticator accepts the Edge's legacy identity token during
// transition, plus any unrevoked feed-scoped pairing credential. Discovery of
// the endpoint never by itself authorizes delivery (docs/PROTOCOL.md).
type PairingFeedAuthenticator struct {
	LegacyToken string
	Ceremony    *pairing.Ceremony
}

func (a PairingFeedAuthenticator) Authenticate(r *http.Request) bool {
	token := BearerToken(r)
	if token == "" {
		return false
	}
	if a.LegacyToken != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(a.LegacyToken)) == 1 {
		return true
	}
	if a.Ceremony == nil {
		return false
	}
	cred, ok := a.Ceremony.Authenticate(token)
	return ok && cred.Role == pairing.RoleFeed
}

// initPairing creates the ceremony and loads owner-only credential storage.
func (s *Service) initPairing() error {
	store, err := pairing.NewStore(filepath.Dir(s.statePath))
	if err != nil {
		return err
	}
	ceremony := pairing.New(time.Now)
	credentials, err := store.Load()
	if err != nil {
		return err
	}
	ceremony.Import(credentials)
	s.credStore = store
	s.ceremony = ceremony
	return nil
}

// persistCredentials writes the ceremony's credentials to owner-only storage.
func (s *Service) persistCredentials() error {
	if s.credStore == nil {
		return nil
	}
	return s.credStore.Save(s.ceremony.Export())
}

// RevokePairing revokes a credential and disconnects every live feed
// connection using it.
func (s *Service) RevokePairing(credentialID string) error {
	var token string
	for _, cred := range s.ceremony.Export() {
		if cred.ID == credentialID {
			token = cred.Token // Export includes tokens; List redacts them.
			break
		}
	}
	if err := s.ceremony.Revoke(credentialID); err != nil {
		return err
	}
	if token != "" {
		s.hub.dropToken(token)
	}
	return s.persistCredentials()
}
