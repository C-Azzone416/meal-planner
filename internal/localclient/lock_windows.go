//go:build windows

package localclient

import (
	"context"
	"os"
	"sync"
)

var windowsLedgerLock sync.Mutex

func acquireLedgerLock(ctx context.Context, _ *os.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	windowsLedgerLock.Lock()
	return nil
}

func releaseLedgerLock(_ *os.File) { windowsLedgerLock.Unlock() }
