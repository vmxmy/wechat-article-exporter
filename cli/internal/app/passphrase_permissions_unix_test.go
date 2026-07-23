//go:build unix

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePassphraseFilePermissionsAcceptsPrivateOwnerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passphrase.txt")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, info, err := openProtectedPassphraseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := validatePassphraseFilePermissions(path, file, info); err != nil {
		t.Fatalf("private owner file rejected: %v", err)
	}
}

func TestValidatePassphraseFilePermissionsRejectsGroupOrOtherAccess(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o604} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "passphrase.txt")
			if err := os.WriteFile(path, []byte("secret\n"), mode); err != nil {
				t.Fatal(err)
			}
			file, info, err := openProtectedPassphraseFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := validatePassphraseFilePermissions(path, file, info); err == nil {
				t.Fatalf("mode %o accepted", mode)
			}
		})
	}
}
