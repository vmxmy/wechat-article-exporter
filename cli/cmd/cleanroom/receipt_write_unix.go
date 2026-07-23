//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
)

func commitReceiptFile(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
