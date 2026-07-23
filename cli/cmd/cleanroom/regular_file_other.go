//go:build !unix && !windows

package main

import (
	"errors"
	"os"
)

func openRegularFileNoFollow(string) (*os.File, os.FileInfo, error) {
	return nil, nil, errors.New("secure no-follow file opens are unsupported on this platform")
}
