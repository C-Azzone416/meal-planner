//go:build windows

package localstore

// syncDir is a no-op on Windows, where a directory cannot be opened and synced like this; skipping
// it keeps a successful write from logging a failure every time.
func syncDir(string) error { return nil }
