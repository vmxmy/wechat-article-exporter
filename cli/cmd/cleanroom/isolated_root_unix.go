//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type isolatedRoot struct {
	fd int
}

func openIsolatedRoot(path string) (*isolatedRoot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(absolute)
	name := filepath.Base(absolute)
	parentFile, _, err := openDirectoryNoFollow(parent)
	if err != nil {
		return nil, err
	}
	defer parentFile.Close()
	parentFD := int(parentFile.Fd())
	if err := validateIsolatedRootParent(parentFD); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if errors.Is(err, unix.ENOENT) {
		if mkdirErr := unix.Mkdirat(parentFD, name, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return nil, mkdirErr
		}
		fd, err = unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	}
	if err != nil {
		return nil, err
	}
	if err := validateIsolatedRootDirectory(fd); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return &isolatedRoot{fd: fd}, nil
}

func validateIsolatedRootParent(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Uid != uint32(unix.Geteuid()) {
		return errors.New("isolated root parent is not owned by the current user")
	}
	if stat.Mode&0o022 != 0 {
		return errors.New("isolated root parent must not be group or world writable")
	}
	return nil
}

func validateIsolatedRootDirectory(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("isolated root is not a directory")
	}
	if stat.Uid != uint32(unix.Geteuid()) {
		return errors.New("isolated root is not owned by the current user")
	}
	return nil
}

func openDirectoryNoFollow(path string) (*os.File, os.FileInfo, error) {
	components, err := unixNoFollowPathComponents(path)
	if err != nil {
		return nil, nil, err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, component := range components {
		nextFD, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, nil, fmt.Errorf("open directory ancestor %q: %w", component, openErr)
		}
		fd = nextFD
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

func (root *isolatedRoot) EnsureDirectory(relative string) error {
	if root == nil || root.fd < 0 {
		return errors.New("isolated root is closed")
	}
	fd, err := unix.Openat(root.fd, ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	components, err := isolatedRootComponents(relative)
	if err != nil {
		_ = unix.Close(fd)
		return err
	}
	for _, component := range components {
		nextFD, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(fd, component, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(fd)
				return mkdirErr
			}
			nextFD, openErr = unix.Openat(fd, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		}
		_ = unix.Close(fd)
		if openErr != nil {
			return openErr
		}
		fd = nextFD
	}
	err = unix.Fchmod(fd, 0o700)
	return errors.Join(err, unix.Close(fd))
}

func (root *isolatedRoot) Close() error {
	if root == nil || root.fd < 0 {
		return nil
	}
	err := unix.Close(root.fd)
	root.fd = -1
	return err
}
