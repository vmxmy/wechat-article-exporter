package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebBackupArtifactConsumesHandleExpiresAndCleansUp(t *testing.T) {
	directory := t.TempDir()
	archivePath := filepath.Join(directory, "backup.zip")
	if err := os.WriteFile(archivePath, []byte("backup archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := &webMaintenanceStorageAdapter{backups: map[string]webBackupHandle{
		"backup-1": {path: archivePath, expiresAt: time.Now().Add(time.Minute)},
	}}
	archive, err := adapter.OpenBackup(context.Background(), "backup-1")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(archive)
	if err != nil || string(contents) != "backup archive" {
		t.Fatalf("archive = %q, %v", contents, err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(archivePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive retained after stream close: %v", err)
	}
	if _, err := adapter.OpenBackup(context.Background(), "backup-1"); err == nil {
		t.Fatal("replayed handle opened an archive")
	}

	expiredPath := filepath.Join(directory, "expired.zip")
	if err := os.WriteFile(expiredPath, []byte("expired"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter.backups["backup-expired"] = webBackupHandle{path: expiredPath, expiresAt: time.Now()}
	if _, err := adapter.OpenBackup(context.Background(), "backup-expired"); err == nil {
		t.Fatal("expired handle opened an archive")
	}
	if _, err := os.Stat(expiredPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired archive was not removed: %v", err)
	}

	pendingPath := filepath.Join(directory, "pending.zip")
	if err := os.WriteFile(pendingPath, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter.backups["backup-pending"] = webBackupHandle{path: pendingPath, expiresAt: time.Now().Add(time.Minute)}
	if err := adapter.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pendingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending archive was not removed on close: %v", err)
	}
}
