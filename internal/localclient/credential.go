package localclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sync"
)

var bindingIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func safeBindingID(value string) bool { return bindingIDPattern.MatchString(value) }

func credentialKey(p Profile) CredentialKey {
	return CredentialKey{
		Environment: p.Environment,
		APIOrigin:   p.APIOrigin,
		WebOrigin:   p.WebOrigin,
		BindingID:   p.BindingID,
	}
}

func (key CredentialKey) valid() bool {
	return key.Environment == PreviewEnvironment && key.APIOrigin == PreviewOrigin &&
		key.WebOrigin == PreviewOrigin && safeBindingID(key.BindingID)
}

// Credential is an opaque send grant. Every generic representation is
// deliberately redacted; only this package can derive the HTTP header value.
type Credential struct{ raw []byte }

func newCredential(raw []byte) (Credential, error) {
	if !grantPattern.Match(raw) {
		return Credential{}, ErrCredentialMissing
	}
	return Credential{raw: bytes.Clone(raw)}, nil
}

func (Credential) String() string                 { return "[REDACTED]" }
func (Credential) GoString() string               { return "[REDACTED]" }
func (Credential) MarshalText() ([]byte, error)   { return []byte("[REDACTED]"), nil }
func (Credential) MarshalJSON() ([]byte, error)   { return json.Marshal("[REDACTED]") }
func (Credential) Format(state fmt.State, _ rune) { _, _ = io.WriteString(state, "[REDACTED]") }

func (c Credential) valid() bool           { return grantPattern.Match(c.raw) }
func (c Credential) authorization() string { return "Bearer " + string(c.raw) }
func (c Credential) clone() Credential     { return Credential{raw: bytes.Clone(c.raw)} }
func (c *Credential) clear() {
	if c != nil {
		clear(c.raw)
		c.raw = nil
	}
}

// MemoryCredentialStore is an explicitly in-memory implementation for tests
// and synthetic fixtures. It is not a plaintext persistence fallback.
type MemoryCredentialStore struct {
	mu    sync.Mutex
	items map[CredentialKey]Credential
}

func NewMemoryCredentialStore() *MemoryCredentialStore {
	return &MemoryCredentialStore{items: make(map[CredentialKey]Credential)}
}

func (s *MemoryCredentialStore) Get(ctx context.Context, key CredentialKey) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if !key.valid() {
		return Credential{}, ErrCredentialMissing
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.items[key]
	if !ok {
		return Credential{}, ErrCredentialMissing
	}
	return value.clone(), nil
}

func (s *MemoryCredentialStore) Put(ctx context.Context, key CredentialKey, value Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !key.valid() || !value.valid() {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.items[key]
	previous.clear()
	s.items[key] = value.clone()
	return nil
}

func (s *MemoryCredentialStore) Delete(ctx context.Context, key CredentialKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !key.valid() {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.items[key]
	value.clear()
	delete(s.items, key)
	return nil
}
