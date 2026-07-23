//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func platformCommitFileNoReplace(temporary, destination string) error {
	from, err := windows.UTF16PtrFromString(windowsExtendedPath(temporary))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(windowsExtendedPath(destination))
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return os.ErrExist
	}
	return err
}

// Windows MoveFileEx with WRITE_THROUGH flushes the move before returning.
func syncParentDirectory(string) error { return nil }
