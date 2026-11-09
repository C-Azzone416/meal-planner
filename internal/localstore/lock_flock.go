//go:build darwin || linux

package localstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/C-Azzone416/meal-planner/internal/core/draft"
)

// crossProcessLock reports that writes are protected by flock(2) across processes.
const crossProcessLock = true

const lockPoll = 25 * time.Millisecond

// openNoFollow makes open fail on a symlink as the final path element.
const openNoFollow = syscall.O_NOFOLLOW
const openNonblock = syscall.O_NONBLOCK

// acquireLock takes an exclusive flock on path, creating the file (0600) if needed. flock is tied to
// the open file description, so a fresh open per call also excludes goroutines in this process. The
// kernel drops the lock if the process dies. It polls without blocking so ctx and timeout apply.
func acquireLock(ctx context.Context, path string, timeout time.Duration) (release func(), err error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return acquireRootLock(ctx, root, timeout)
}

func acquireRootLock(ctx context.Context, root *os.Root, timeout time.Duration) (release func(), err error) {
	if fi, lerr := root.Lstat(lockName); lerr == nil && !fi.Mode().IsRegular() {
		return nil, ioError(draft.CodeDataDirUnavailable, "the lock file is not a regular file", errors.New("lock path is not a regular file"))
	}
	// Exclusive creation avoids the macOS concurrent O_CREAT open race. If
	// another process won, open its stable lock inode without creation.
	f, err := root.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL|openNoFollow|openNonblock, 0o600)
	if errors.Is(err, os.ErrExist) {
		f, err = root.OpenFile(lockName, os.O_RDWR|openNoFollow|openNonblock, 0)
	}
	if err != nil {
		return nil, ioError(draft.CodeIOError, "cannot open the draft lock file", err)
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, ioError(draft.CodeDataDirUnavailable, "the lock file is not a regular file", err)
	}
	current, err := root.Lstat(lockName)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(current, fi) {
		f.Close()
		return nil, ioError(draft.CodeDataDirUnavailable, "the lock file changed while opening", err)
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
