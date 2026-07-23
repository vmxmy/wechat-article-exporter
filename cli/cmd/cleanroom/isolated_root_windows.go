//go:build windows

package main

import (
	"errors"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

type isolatedRoot struct {
	handle windows.Handle
}

func openIsolatedRoot(path string) (*isolatedRoot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent, err := openWindowsDirectory(0, windowsNTPath(filepath.Dir(absolute)), windows.FILE_OPEN)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(parent)
	handle, err := openWindowsDirectory(parent, filepath.Base(absolute), windows.FILE_OPEN_IF)
	if err != nil {
		return nil, err
	}
	return &isolatedRoot{handle: handle}, nil
}

func openWindowsDirectory(parent windows.Handle, name string, disposition uint32) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	attributes.Length = uint32(unsafe.Sizeof(*attributes))
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE, attributes,
		&windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, disposition,
		windows.FILE_DIRECTORY_FILE|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	return handle, err
}

func (root *isolatedRoot) EnsureDirectory(relative string) error {
	if root == nil || root.handle == 0 {
		return errors.New("isolated root is closed")
	}
	current, err := duplicateWindowsHandle(root.handle)
	if err != nil {
		return err
	}
	components, err := isolatedRootComponents(relative)
	if err != nil {
		_ = windows.CloseHandle(current)
		return err
	}
	for _, component := range components {
		next, openErr := openWindowsDirectory(current, component, windows.FILE_OPEN_IF)
		_ = windows.CloseHandle(current)
		if openErr != nil {
			return openErr
		}
		current = next
	}
	return windows.CloseHandle(current)
}

func duplicateWindowsHandle(handle windows.Handle) (windows.Handle, error) {
	process := windows.CurrentProcess()
	var duplicate windows.Handle
	err := windows.DuplicateHandle(process, handle, process, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
	return duplicate, err
}

func (root *isolatedRoot) Close() error {
	if root == nil || root.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(root.handle)
	root.handle = 0
	return err
}
