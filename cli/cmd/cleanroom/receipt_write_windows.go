//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func commitReceiptFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(windowsExtendedPath(source))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(windowsExtendedPath(destination))
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: source, New: destination, Err: err}
	}
	return nil
}

func windowsExtendedPath(path string) string {
	if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\??\`) {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil || len(absolute) < 248 {
		return path
	}
	if strings.HasPrefix(absolute, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(absolute, `\\`)
	}
	return `\\?\` + absolute
}
