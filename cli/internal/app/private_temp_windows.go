//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
)

func createPrivateTemp(directory, pattern string) (*os.File, error) {
	securityDescriptor, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;OW)(A;;GA;;;SY)(A;;GA;;;BA)")
	if err != nil {
		return nil, fmt.Errorf("build private temporary file security descriptor: %w", err)
	}
	attributes := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: securityDescriptor,
	}
	for attempts := 0; attempts < 100; attempts++ {
		name := privateTemporaryName(pattern, uuid.NewString())
		path := windowsExtendedPath(filepath.Join(directory, name))
		pathUTF16, conversionErr := windows.UTF16PtrFromString(path)
		if conversionErr != nil {
			return nil, conversionErr
		}
		handle, createErr := windows.CreateFile(pathUTF16, windows.GENERIC_READ|windows.GENERIC_WRITE,
			windows.FILE_SHARE_READ, attributes, windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_TEMPORARY|windows.FILE_FLAG_WRITE_THROUGH, 0)
		if createErr == nil {
			return os.NewFile(uintptr(handle), path), nil
		}
		if createErr != windows.ERROR_FILE_EXISTS && createErr != windows.ERROR_ALREADY_EXISTS {
			return nil, createErr
		}
	}
	return nil, errors.New("create private temporary file: too many name collisions")
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

func privateTemporaryName(pattern, random string) string {
	if index := strings.LastIndex(pattern, "*"); index >= 0 {
		return pattern[:index] + random + pattern[index+1:]
	}
	return pattern + random
}
