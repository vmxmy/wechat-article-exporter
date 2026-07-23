//go:build !unix && !windows

package app

import (
	"errors"
	"os"
)

func openProtectedPassphraseFile(string) (*os.File, os.FileInfo, error) {
	return nil, nil, errors.New("secure passphrase-file opening is unsupported on this platform")
}
