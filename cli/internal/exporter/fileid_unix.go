//go:build !windows

package exporter

import (
	"errors"
	"os"
	"syscall"
)

func fileIdentityFromInfo(info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("filesystem identity is unavailable")
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}

func fileIdentityFromFile(file *os.File) (uint64, uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, 0, err
	}
	return fileIdentityFromInfo(info)
}
