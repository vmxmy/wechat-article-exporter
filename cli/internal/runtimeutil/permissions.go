package runtimeutil

import (
	"os"
	"runtime"
	"testing"
)

// AssertPrivatePermissions verifies POSIX permission bits where the platform
// exposes them. Windows enforces privacy through ACLs and os.FileMode reports
// synthesized 0666/0777 bits that cannot prove or disprove the ACL.
func AssertPrivatePermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
