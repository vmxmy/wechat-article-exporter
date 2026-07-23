//go:build unix

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openProtectedPassphraseFile(path string) (*os.File, os.FileInfo, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, nil, errors.New("passphrase file must not be a symbolic link")
		}
		return nil, nil, fmt.Errorf("open passphrase file: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("inspect passphrase file: %w", err)
	}
	return file, info, nil
}
