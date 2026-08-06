// Package pairing implements the Agentagotchi Pairing Ceremony: one
// short-lived, one-use device-code authorization state machine for all three
// pairing directions (Edge→device, Edge→Home, Home→device), with
// role-specific grants.
//
// Shape: a connecting client requests a code and displays it; an
// authenticated administrator approves it through the receiving service's
// administration surface; the service issues a unique, random, revocable,
// role-scoped credential. Unapproved codes grant nothing. Codes are one-use,
// expire quickly, and are never replayable.
//
// Secrets discipline: codes and credential tokens are held in memory or in
// owner-only credential storage (0600 files in a 0700 directory). They are
// never printed, logged, transmitted in status, or persisted elsewhere.
package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Role scopes what a credential may do.
type Role string

const (
	// RoleFeed authenticates a device/client consuming a Presence Feed.
	RoleFeed Role = "feed"
	// RoleEdgeIngress authenticates an Edge publishing upstream to a Home.
	RoleEdgeIngress Role = "edge-ingress"
	// RoleAdmin administers the issuing service. Issued only by the service's
	// own single-admin bootstrap, never by this ceremony.
	RoleAdmin Role = "admin"
)

// ValidRole reports whether r is issuable by the ceremony (admin excluded).
func ValidRole(r Role) bool {
	return r == RoleFeed || r == RoleEdgeIngress
}

const (
	// CodeTTL bounds a pairing code's life.
	CodeTTL = 10 * time.Minute
	// codeEntropyBytes gives >=128-bit codes.
	codeEntropyBytes = 16
	// tokenEntropyBytes gives 256-bit credential tokens.
	tokenEntropyBytes = 32
	// maxPendingCodes bounds outstanding codes per service.
	maxPendingCodes = 32
)

// Code is a pending pairing code. Token is the display secret; it is
// short-lived and never becomes a long-lived credential.
type Code struct {
	ID         string    `json:"id"`
	Token      string    `json:"token"`
	Role       Role      `json:"role"`
	ClientName string    `json:"clientName"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Consumed   bool      `json:"consumed"`
	Approved   bool      `json:"approved"`
}

// Credential is a unique, revocable, role-scoped bearer token bound to one
// relationship. Token is stored owner-only and never logged.
type Credential struct {
	ID         string    `json:"id"`
	Token      string    `json:"token,omitempty"`
	Role       Role      `json:"role"`
	ClientName string    `json:"clientName"`
	IssuedAt   time.Time `json:"issuedAt"`
	Revoked    bool      `json:"revoked"`
}

var (
	ErrCodeNotFound = errors.New("pairing: unknown code")
	ErrCodeExpired  = errors.New("pairing: code expired")
	ErrCodeConsumed = errors.New("pairing: code already consumed")
	ErrNotApproved  = errors.New("pairing: code not approved")
	ErrUnknownCred  = errors.New("pairing: unknown credential")
	ErrRoleInvalid  = errors.New("pairing: invalid role")
	ErrTooManyCodes = errors.New("pairing: too many pending codes")
)

// Ceremony is the device-code state machine for one receiving service.
type Ceremony struct {
	mu  sync.Mutex
	now func() time.Time

	codes       map[string]*Code // by ID
	credentials map[string]*Credential
	byToken     map[string]*Credential // constant-time lookup target
}

// New creates a ceremony. now is injectable for tests.
func New(now func() time.Time) *Ceremony {
	if now == nil {
		now = time.Now
	}
	return &Ceremony{
		now:         now,
		codes:       make(map[string]*Code),
		credentials: make(map[string]*Credential),
		byToken:     make(map[string]*Credential),
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RequestCode issues a short-lived, one-use code for the client to display.
// The token is returned once, to the connecting client only.
func (c *Ceremony) RequestCode(role Role, clientName string) (Code, error) {
	if !ValidRole(role) {
		return Code{}, ErrRoleInvalid
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
	if len(c.codes) >= maxPendingCodes {
		return Code{}, ErrTooManyCodes
	}
	id, err := randomHex(8)
	if err != nil {
		return Code{}, err
	}
	token, err := randomHex(codeEntropyBytes)
	if err != nil {
		return Code{}, err
	}
	now := c.now().UTC()
	code := &Code{
		ID: id, Token: token, Role: role, ClientName: clientName,
		CreatedAt: now, ExpiresAt: now.Add(CodeTTL),
	}
	c.codes[id] = code
	return *code, nil
}

// Approve marks a code approved by the service's authenticated administrator.
// Approval alone issues nothing; the connecting client redeems the code token
// to receive its credential.
func (c *Ceremony) Approve(codeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
	code, ok := c.codes[codeID]
	if !ok {
		return ErrCodeNotFound
	}
	if code.Consumed {
		return ErrCodeConsumed
	}
	code.Approved = true
	return nil
}

// Deny removes a pending code without issuing anything.
func (c *Ceremony) Deny(codeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.codes[codeID]; !ok {
		return ErrCodeNotFound
	}
	delete(c.codes, codeID)
	return nil
}

// Pending lists unexpired, unconsumed codes for the administrator surface.
// Tokens are included because the admin compares the client-displayed code,
// but callers must not log or persist them.
func (c *Ceremony) Pending() []Code {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
	out := make([]Code, 0, len(c.codes))
	for _, code := range c.codes {
		out = append(out, *code)
	}
	return out
}

// Redeem exchanges an approved code token for a unique, revocable,
// role-scoped credential. One use: the code is consumed even on success.
func (c *Ceremony) Redeem(codeToken string) (Credential, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
	var code *Code
	for _, candidate := range c.codes {
		if subtle.ConstantTimeCompare([]byte(candidate.Token), []byte(codeToken)) == 1 {
			code = candidate
			break
		}
	}
	if code == nil {
		return Credential{}, ErrCodeNotFound
	}
	if code.Consumed {
		return Credential{}, ErrCodeConsumed
	}
	if !code.Approved {
		// Not consumed: the administrator may still approve, and the client
		// redeems again. Unapproved codes grant nothing.
		return Credential{}, ErrNotApproved
	}
	code.Consumed = true
	delete(c.codes, code.ID) // one-use, never replayable
	id, err := randomHex(8)
	if err != nil {
		return Credential{}, err
	}
	token, err := randomHex(tokenEntropyBytes)
	if err != nil {
		return Credential{}, err
	}
	cred := &Credential{
		ID: id, Token: token, Role: code.Role, ClientName: code.ClientName,
		IssuedAt: c.now().UTC(),
	}
	c.credentials[id] = cred
	c.byToken[token] = cred
	return *cred, nil
}

// Authenticate resolves a bearer token to its credential, rejecting revoked
// and unknown tokens. Lookup is constant-time over the presented token.
func (c *Ceremony) Authenticate(token string) (Credential, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var match *Credential
	for stored, cred := range c.byToken {
		if subtle.ConstantTimeCompare([]byte(stored), []byte(token)) == 1 {
			match = cred
			break
		}
	}
	if match == nil || match.Revoked {
		return Credential{}, false
	}
	return *match, true
}

// Revoke individually revokes a credential by ID. Callers are responsible for
// dropping any live connection using it.
func (c *Ceremony) Revoke(credentialID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cred, ok := c.credentials[credentialID]
	if !ok {
		return ErrUnknownCred
	}
	cred.Revoked = true
	return nil
}

// List returns credential metadata for the administration surface. Tokens are
// redacted — status never carries long-lived secrets.
func (c *Ceremony) List() []Credential {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Credential, 0, len(c.credentials))
	for _, cred := range c.credentials {
		redacted := *cred
		redacted.Token = ""
		out = append(out, redacted)
	}
	return out
}

// expireLocked drops expired codes.
func (c *Ceremony) expireLocked() {
	now := c.now()
	for id, code := range c.codes {
		if now.After(code.ExpiresAt) {
			delete(c.codes, id)
		}
	}
}

// Store is owner-only credential persistence (0600 files, 0700 directory).
// Secrets live here or in memory, nowhere else.
type Store struct {
	dir string
}

// NewStore creates an owner-only credential store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if err := mkdirOwnerOnly(dir); err != nil {
		return nil, fmt.Errorf("pairing: credential store: %w", err)
	}
	return &Store{dir: dir}, nil
}
