//go:build darwin || linux

package localstore

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// crossProcessLock reports that writes are protected by flock(2) across processes.
const crossProcessLock = true

const lockPoll = 25 * time.Millisecond

// openNoFollow makes open fail on a symlink as the final path element.
const openNoFollow = syscall.O_NOFOLLOW

// acquireLock takes an exclusive flock on path, creating the file (0600) if needed. flock is tied to
// the open file description, so a fresh open per call also excludes goroutines in this process. The
// kernel drops the lock if the process dies. It polls without blocking so ctx and timeout apply.
func acquireLock(ctx context.Context, path string, timeout time.Duration) (release func(), err error) {
	if fi, lerr := os.Lstat(path); lerr == nil && !fi.Mode().IsRegular() {
		return nil, ioError(draft.CodeDataDirUnavailable, "the lock file is not a regular file", errors.New("lock path is not a regular file"))
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|openNoFollow, 0o600)
	if err != nil {
		return nil, ioError(draft.CodeIOError, "cannot open the draft lock file", err)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(lockPoll)
	defer tick.Stop()
	for {
		ferr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if ferr == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if ferr != syscall.EWOULDBLOCK && ferr != syscall.EINTR {
			_ = f.Close()
			return nil, ioError(draft.CodeIOError, "cannot lock the draft directory", ferr)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctxError(ctx.Err())
		case <-deadline.C:
			_ = f.Close()
			return nil, lockTimeout(context.DeadlineExceeded)
		case <-tick.C:
		}
	}
}
