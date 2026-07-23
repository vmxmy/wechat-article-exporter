//go:build windows

package profiles

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockProfileFile(ctx context.Context, file *os.File, shared bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if !shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped)
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		select {
		case <-ctx.Done():
			return errors.Join(ErrProfileBusy, ctx.Err())
		default:
			return ErrProfileBusy
		}
	}
	return err
}

func unlockProfileFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
}
