//go:build !unix && !windows

package profiles

import (
	"context"
	"errors"
	"os"
)

func lockProfileFile(context.Context, *os.File, bool) error {
	return errors.New("profile maintenance locking is unsupported on this platform")
}

func unlockProfileFile(*os.File) error { return nil }
