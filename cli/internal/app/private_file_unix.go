//go:build !windows

package app

import "os"

func securePrivateFile(file *os.File) error {
	return file.Chmod(0o600)
}
