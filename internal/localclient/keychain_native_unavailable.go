//go:build !darwin || !cgo

package localclient

func newNativeKeychainBackend() (keychainBackend, error) {
	return nil, ErrUnavailable
}
