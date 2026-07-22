//go:build !(aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris || windows)

package secrets

import (
	"context"
	"errors"
)

func lockKeyring(context.Context, string) (func(), error) {
	return nil, errors.New("cross-process OS credential locking is unsupported on this platform")
}
