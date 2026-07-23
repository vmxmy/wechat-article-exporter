//go:build darwin

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePassphraseFilePermissionsRejectsDarwinExtendedACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "passphrase.txt")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		t.Skipf("extended ACLs unavailable: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("chmod", "-N", path).Run() })
	file, info, err := openProtectedPassphraseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed unexpectedly: %o", info.Mode().Perm())
	}
	err = validatePassphraseFilePermissions(path, file, info)
	if err == nil || !strings.Contains(err.Error(), "extended access-control list") {
		t.Fatalf("permission validation error=%v", err)
	}
}
