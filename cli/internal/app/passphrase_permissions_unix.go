//go:build unix

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func validatePassphraseFilePermissions(path string, file *os.File, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("passphrase file %s must not be accessible by group or other users", filepath.Clean(path))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("passphrase file %s must be owned by the current user", filepath.Clean(path))
	}
	return validatePassphraseExtendedACL(file)
}
