//go:build !windows

package localstore

import "os"

// syncDir flushes a directory entry change. A failure is non-fatal to callers: the commit rename
// has already happened.
func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
