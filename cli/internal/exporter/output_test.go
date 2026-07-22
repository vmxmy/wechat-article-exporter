package exporter

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutputManagerCollisionPoliciesAndChecksums(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "article.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.WriteFile(context.Background(), "article.txt", CollisionFail, writeString("new")); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("fail policy error = %v", err)
	}
	skipped, err := manager.WriteFile(context.Background(), "article.txt", CollisionSkip, writeString("new"))
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Status != OutputSkipped || skipped.SHA256 != "cba06b5736faf67e54b07b561eae94395e774c517a7d910a54369e1263ccfbd4" {
		t.Fatalf("skipped output = %#v", skipped)
	}
	replaced, err := manager.WriteFile(context.Background(), "article.txt", CollisionReplace, writeString("new"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Status != OutputReplaced || replaced.Size != 3 || replaced.SHA256 != "11507a0e2f5e69d5dfa40a62a1bd7b6ee57e6bcd85c67c9b8431b36fff21c437" {
		t.Fatalf("replaced output = %#v", replaced)
	}
	contents, err := os.ReadFile(filepath.Join(root, "article.txt"))
	if err != nil || string(contents) != "new" {
		t.Fatalf("destination = %q, %v", contents, err)
	}
	created, err := manager.WriteFile(context.Background(), "nested/new.txt", CollisionFail, writeString("content"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != OutputWritten || created.Path != "nested/new.txt" {
		t.Fatalf("created output = %#v", created)
	}
}

func TestOutputManagerInterruptionPreservesDestination(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "article.txt")
	if err := os.WriteFile(destination, []byte("complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("renderer interrupted")
	_, err = manager.WriteFile(context.Background(), "article.txt", CollisionReplace, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, "partial"); err != nil {
			return err
		}
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("write error = %v", err)
	}
	contents, readErr := os.ReadFile(destination)
	if readErr != nil || string(contents) != "complete" {
		t.Fatalf("destination after interruption = %q, %v", contents, readErr)
	}
	matches, err := filepath.Glob(filepath.Join(root, temporaryOutputPrefix+"*"+temporaryOutputSuffix))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary outputs = %#v, %v", matches, err)
	}
}

func TestOutputManagerRejectsReplacedOutputRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "export")
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := manager.WriteFile(context.Background(), "article.txt", CollisionFail, writeString("unsafe")); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("replaced root error = %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("replacement root changed: %#v, %v", entries, err)
	}
}

func TestOutputManagerRejectsTraversalAbsoluteAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, path := range []string{"../outside.txt", "/absolute.txt", `C:\\outside.txt`, "escape/outside.txt", "safe/../../outside.txt"} {
		if _, err := manager.WriteFile(context.Background(), path, CollisionFail, writeString("unsafe")); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("path %q error = %v", path, err)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: %#v, %v", entries, err)
	}
}

func TestCleanupAbandonedOutputsRemovesOnlyOldRegularStagingPaths(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, temporaryOutputPrefix+"old"+temporaryOutputSuffix)
	freshPath := filepath.Join(root, temporaryOutputPrefix+"fresh"+temporaryOutputSuffix)
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(freshPath, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, old, old); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, temporaryOutputPrefix+"link"+temporaryOutputSuffix)
	if err := os.Symlink(oldPath, linkPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	report, err := manager.CleanupAbandoned(context.Background(), time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Removed) != 1 || report.Removed[0] != filepath.Base(oldPath) {
		t.Fatalf("cleanup report = %#v", report)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old staging path still exists: %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh staging path removed: %v", err)
	}
	if info, err := os.Lstat(linkPath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("staging symlink changed: %v, %#v", err, info)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "symlink") {
		t.Fatalf("cleanup warnings = %#v", report.Warnings)
	}
}

func writeString(value string) func(io.Writer) error {
	return func(writer io.Writer) error {
		_, err := io.WriteString(writer, value)
		return err
	}
}
