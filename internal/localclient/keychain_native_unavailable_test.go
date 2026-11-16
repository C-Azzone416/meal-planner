//go:build !darwin || !cgo

package localclient

import (
	"errors"
	"testing"
)

func TestNativeKeychainConstructorFailsClosedWhenUnavailable(t *testing.T) {
	store, err := NewMacOSKeychainCredentialStore()
	if store != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("constructor = %T, %v", store, err)
	}
}
