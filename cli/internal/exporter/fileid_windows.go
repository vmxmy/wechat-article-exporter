//go:build windows

package exporter

import (
	"errors"
	"os"
	"syscall"
)

func fileIdentity(info os.FileInfo) (uint64, uint64, error) {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0, 0, errors.New("filesystem identity is unavailable")
	}
	return uint64(data.CreationTime.HighDateTime), uint64(data.CreationTime.LowDateTime), nil
}
