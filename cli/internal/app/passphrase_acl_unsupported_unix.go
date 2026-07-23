//go:build aix || dragonfly || freebsd || hurd || illumos || ios || netbsd || openbsd || solaris

package app

import (
	"errors"
	"os"
)

func validatePassphraseExtendedACL(*os.File) error {
	return errors.New("secure passphrase-file ACL validation is unsupported on this platform")
}
