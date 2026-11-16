package localclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

const keychainService = "mealplanner.local-client.v1"

var errKeychainItemNotFound = errors.New("keychain item not found")

// keychainBackend is the narrow native seam. Implementations must complete
// synchronously, must not retain value, and must never include credential data
// in an error. Tests inject a memory-only fake; they never call Security.framework.
type keychainBackend interface {
	get(service, account string) ([]byte, error)
	put(service, account string, value []byte) error
	delete(service, account string) error
}

type keychainCredentialStore struct {
	backend keychainBackend
}

// NewMacOSKeychainCredentialStore constructs the native adapter without
// accessing the Keychain. It is intentionally not wired into the shipped
// command. Unsupported platforms and non-cgo Darwin builds fail closed.
func NewMacOSKeychainCredentialStore() (CredentialStore, error) {
	backend, err := newNativeKeychainBackend()
	if err != nil {
		return nil, ErrUnavailable
	}
	return newKeychainCredentialStore(backend), nil
}

func newKeychainCredentialStore(backend keychainBackend) CredentialStore {
	if backend == nil {
		return UnavailableCredentialStore{}
	}
	return &keychainCredentialStore{backend: backend}
}

func (s *keychainCredentialStore) Get(ctx context.Context, key CredentialKey) (Credential, error) {
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	account, err := keychainAccount(key)
	if err != nil || s == nil || s.backend == nil {
		return Credential{}, ErrCredentialMissing
	}
	raw, err := s.backend.get(keychainService, account)
	if len(raw) > 0 {
		defer clear(raw)
	}
	if errors.Is(err, errKeychainItemNotFound) {
		return Credential{}, ErrCredentialMissing
	}
	if err != nil {
		return Credential{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	credential, err := newCredential(raw)
	if err != nil {
		return Credential{}, ErrCredentialMissing
	}
	return credential, nil
}

func (s *keychainCredentialStore) Put(ctx context.Context, key CredentialKey, value Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	account, err := keychainAccount(key)
	if err != nil || s == nil || s.backend == nil || !value.valid() {
		return ErrUnavailable
	}
	raw := append([]byte(nil), value.raw...)
	defer clear(raw)
	if err := s.backend.put(keychainService, account, raw); err != nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s *keychainCredentialStore) Delete(ctx context.Context, key CredentialKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	account, err := keychainAccount(key)
	if err != nil || s == nil || s.backend == nil {
		return ErrUnavailable
	}
	err = s.backend.delete(keychainService, account)
	if err == nil || errors.Is(err, errKeychainItemNotFound) {
		return nil
	}
	return ErrUnavailable
}

// keychainAccount deliberately includes every non-secret binding component.
// The length framing prevents ambiguous concatenation, and the digest avoids
// exposing origin or account metadata in the Keychain account label.
func keychainAccount(key CredentialKey) (string, error) {
	if !key.valid() {
		return "", ErrUnavailable
	}
	digest := sha256.New()
	for _, field := range []string{
		"mealplanner.local-client.v1/keychain-account",
		key.Environment,
		key.APIOrigin,
		key.WebOrigin,
		key.BindingID,
	} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(field))
	}
	sum := digest.Sum(nil)
	return PreviewEnvironment + "/" + hex.EncodeToString(sum[:16]), nil
}
