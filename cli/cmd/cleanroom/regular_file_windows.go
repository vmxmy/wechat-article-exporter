//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openRegularFileNoFollow(path string) (*os.File, os.FileInfo, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, err
	}
	volume := filepath.VolumeName(absolute)
	if strings.Contains(strings.TrimPrefix(absolute, volume), ":") {
		return nil, nil, errors.New("file path must not name an alternate data stream")
	}
	objectName, err := windows.NewNTUnicodeString(windowsNTPath(absolute))
	if err != nil {
		return nil, nil, err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{ObjectName: objectName, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ, attributes, &windows.IO_STATUS_BLOCK{}, nil,
		windows.FILE_ATTRIBUTE_NORMAL, windows.FILE_SHARE_READ, windows.FILE_OPEN,
		windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_NON_DIRECTORY_FILE, 0, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open regular file: %w", err)
	}
	file := os.NewFile(uintptr(handle), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = file.Close()
		return nil, nil, errors.New("file must not be a reparse point")
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, errors.New("file must be a regular non-reparse file")
	}
	return file, info, nil
}

func windowsNTPath(path string) string {
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, `\??\`) {
		return clean
	}
	if strings.HasPrefix(clean, `\\?\UNC\`) {
		return `\??\UNC\` + strings.TrimPrefix(clean, `\\?\UNC\`)
	}
	if strings.HasPrefix(clean, `\\?\`) {
		return `\??\` + strings.TrimPrefix(clean, `\\?\`)
	}
	if strings.HasPrefix(clean, `\\`) {
		return `\??\UNC\` + strings.TrimPrefix(clean, `\\`)
	}
	return `\??\` + clean
}
