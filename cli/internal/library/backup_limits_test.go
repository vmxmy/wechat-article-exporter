package library

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

func TestVerifyBackupRejectsCompressedBombBeforeManifestProcessing(t *testing.T) {
	archivePath := writeLimitTestArchive(t, map[string][]byte{
		backupManifestPath: bytes.Repeat([]byte("0"), 1<<20),
	})
	verification, err := VerifyBackup(context.Background(), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Valid || !containsFailure(verification.Failures, "compression ratio") {
		t.Fatalf("verification=%#v, want compression-ratio failure", verification)
	}
}

func TestVerifyAndRestoreRejectUnsafeArchiveNamesBeforeMutation(t *testing.T) {
	archivePath := writeLimitTestArchive(t, map[string][]byte{"../outside.sqlite3": []byte("not a backup")})
	verification, err := VerifyBackup(context.Background(), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Valid || !containsFailure(verification.Failures, "unsafe archive path") {
		t.Fatalf("verification=%#v, want unsafe-path failure", verification)
	}

	databasePath := filepath.Join(t.TempDir(), "live.sqlite")
	if err := os.WriteFile(databasePath, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(context.Background(), RestoreOptions{ArchivePath: archivePath, DatabasePath: databasePath, ObjectStore: store}); err == nil {
		t.Fatal("RestoreBackup() error = nil")
	}
	contents, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "unchanged" {
		t.Fatalf("live database changed to %q", contents)
	}
}

func TestVerifyAndRestoreRejectSymlinkArchiveEntryBeforeMutation(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "symlink.wab")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	header := &zip.FileHeader{Name: "objects/sha256/aa/aa/linked-object", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("../../outside")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	verification, err := VerifyBackup(context.Background(), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Valid || !containsFailure(verification.Failures, "unsupported archive entry") {
		t.Fatalf("verification=%#v, want symlink-entry rejection", verification)
	}

	databasePath := filepath.Join(t.TempDir(), "live.sqlite")
	if err := os.WriteFile(databasePath, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(context.Background(), RestoreOptions{ArchivePath: archivePath, DatabasePath: databasePath, ObjectStore: store}); err == nil {
		t.Fatal("RestoreBackup() error = nil")
	}
	contents, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "unchanged" {
		t.Fatalf("live database changed to %q", contents)
	}
}

func TestIndexBackupEntriesAppliesEntryCountAndSizeLimits(t *testing.T) {
	archivePath := writeLimitTestArchive(t, map[string][]byte{"one": []byte("one"), "two": []byte("two")})
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	_, failures := indexBackupEntries(reader.File, BackupArchiveLimits{MaximumEntries: 1, MaximumEntryBytes: 2, MaximumTotalBytes: 3, MaximumCompressionRatio: 100})
	for _, expected := range []string{"entries", "exceeds 2 bytes", "total uncompressed"} {
		if !containsFailure(failures, expected) {
			t.Fatalf("failures=%v, want %q", failures, expected)
		}
	}
}

func writeLimitTestArchive(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.wab")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, contents := range entries {
		if err := writeZipBytes(writer, name, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsFailure(failures []string, expected string) bool {
	for _, failure := range failures {
		if strings.Contains(failure, expected) {
			return true
		}
	}
	return false
}
