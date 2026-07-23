//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openRegularFileNoFollow(path string) (*os.File, os.FileInfo, error) {
	components, err := unixNoFollowPathComponents(path)
	if err != nil {
		return nil, nil, err
	}
	if len(components) == 0 {
		return nil, nil, errors.New("file path names a filesystem root")
	}
	directoryFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, component := range components[:len(components)-1] {
		nextFD, openErr := unix.Openat(directoryFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		_ = unix.Close(directoryFD)
		if openErr != nil {
			return nil, nil, fmt.Errorf("open regular-file ancestor %q: %w", component, openErr)
		}
		directoryFD = nextFD
	}
	fd, err := unix.Openat(directoryFD, components[len(components)-1], unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	_ = unix.Close(directoryFD)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, nil, errors.New("file must not be a symbolic link")
		}
		return nil, nil, fmt.Errorf("open regular file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, errors.New("file must be a regular non-symlink file")
	}
	return file, info, nil
}

func unixNoFollowPathComponents(path string) ([]string, error) {
	if path != string(filepath.Separator) && strings.HasSuffix(path, string(filepath.Separator)) {
		return nil, errors.New("file path must not end with a separator")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	components := strings.FieldsFunc(strings.TrimPrefix(absolute, string(filepath.Separator)), func(value rune) bool {
		return value == filepath.Separator
	})
	if len(components) == 0 {
		return nil, nil
	}
	first := filepath.Join(string(filepath.Separator), components[0])
	info, err := os.Lstat(first)
	if len(components) > 1 && err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(first)
		if resolveErr != nil {
			return nil, resolveErr
		}
		absolute = filepath.Join(append([]string{resolved}, components[1:]...)...)
		components = strings.FieldsFunc(strings.TrimPrefix(absolute, string(filepath.Separator)), func(value rune) bool {
			return value == filepath.Separator
		})
	}
	return components, nil
}
