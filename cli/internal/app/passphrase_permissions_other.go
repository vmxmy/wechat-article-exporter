//go:build !unix && !windows

package app

import (
	"errors"
	"os"
)

func validatePassphraseFilePermissions(string, *os.File, os.FileInfo) error {
	return errors.New("secure passphrase-file permission validation is unsupported on this platform")
}
