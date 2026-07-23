//go:build unix

package profiles

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockProfileFile(ctx context.Context, file *os.File, shared bool) error {
	operation := unix.LOCK_EX | unix.LOCK_NB
	if shared {
		operation = unix.LOCK_SH | unix.LOCK_NB
	}
	for {
		if err := unix.Flock(int(file.Fd()), operation); err == nil {
			return nil
		} else if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(ErrProfileBusy, ctx.Err())
		default:
			return ErrProfileBusy
		}
	}
}

func unlockProfileFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
