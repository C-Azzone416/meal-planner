//go:build !windows

package localclient

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

func acquireLedgerLock(ctx context.Context, file *os.File) error {
	for {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return ErrUnavailable
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func releaseLedgerLock(file *os.File) { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
