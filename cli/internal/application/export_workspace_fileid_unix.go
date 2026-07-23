//go:build !windows

package application

import (
	"errors"
	"os"
	"syscall"
)

func workspaceExportRootIdentityFromFile(file *os.File) (uint64, uint64, error) {
	if file == nil {
		return 0, 0, errors.New("filesystem handle is unavailable")
	}
	info, err := file.Stat()
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("filesystem identity is unavailable")
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}
