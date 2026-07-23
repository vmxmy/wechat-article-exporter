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
	if err := validateIsolatedRootAncestors(parent); err != nil {
		return nil, err
	}
	parentFile, _, err := openDirectoryNoFollow(parent)
	if err != nil {
		return nil, err
	}
	defer parentFile.Close()
	parentFD := int(parentFile.Fd())
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

func validateIsolatedRootAncestors(path string) error {
	components, err := unixNoFollowPathComponents(path)
	if err != nil {
		return err
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	if err := validateIsolatedRootAncestor(fd, string(filepath.Separator)); err != nil {
		return err
	}
	for _, component := range components {
		nextFD, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		if openErr != nil {
			return fmt.Errorf("open isolated root ancestor %q: %w", component, openErr)
		}
		_ = unix.Close(fd)
		fd = nextFD
		if err := validateIsolatedRootAncestor(fd, component); err != nil {
			return err
		}
	}
	return nil
}

func validateIsolatedRootAncestor(fd int, component string) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("isolated root ancestor %q is not a directory", component)
	}
	uid := uint32(unix.Geteuid())
	if stat.Uid == uid && stat.Mode&0o022 == 0 {
		return nil
	}
	// A root-owned sticky directory (for example /tmp) cannot have an entry
	// owned by this user replaced by another unprivileged user. It is safe only
	// as an ancestor; the next component still undergoes its own validation.
	if stat.Uid == 0 && stat.Mode&unix.S_ISVTX != 0 {
		return nil
	}
	if stat.Uid == 0 && stat.Mode&0o022 == 0 {
		return nil
	}
	return fmt.Errorf("isolated root ancestor %q is not trusted", component)
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
