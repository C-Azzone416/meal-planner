//go:build !darwin && !linux

package localstore

import (
	"context"
	"time"
)

// crossProcessLock is false here: only an in-process lock is available, so two processes can race
// between the revision check and the rename. This is a documented limitation (contract 10.6).
const crossProcessLock = false

// openNoFollow is unavailable here; the Lstat check still applies.
const openNoFollow = 0

var procLock = make(chan struct{}, 1)

// acquireLock takes the process-wide lock. path is unused.
func acquireLock(ctx context.Context, path string, timeout time.Duration) (release func(), err error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case procLock <- struct{}{}:
		return func() { <-procLock }, nil
	case <-ctx.Done():
		return nil, ctxError(ctx.Err())
	case <-t.C:
		return nil, lockTimeout(context.DeadlineExceeded)
	}
}
