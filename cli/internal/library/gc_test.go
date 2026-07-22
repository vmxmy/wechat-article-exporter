package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

func TestLibraryGarbageCollectionDryRunConfirmationAndRetention(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	referenced, err := store.Put(context.Background(), strings.NewReader("referenced"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	unreferenced, err := store.Put(context.Background(), strings.NewReader("unreferenced"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	old := now.Add(-60 * 24 * time.Hour)
	for _, digest := range []string{referenced.Digest, unreferenced.Digest} {
		path := filepath.Join(store.Root(), "sha256", digest[:2], digest[2:4], digest)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	temporaryPath := filepath.Join(store.Root(), "tmp", ".object-stale.tmp")
	if err := os.MkdirAll(filepath.Dir(temporaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporaryPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(temporaryPath, old, old); err != nil {
		t.Fatal(err)
	}
	nowMillis := now.UnixMilli()
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, ?, ?);
INSERT INTO debug_incidents(id, profile_id, operation, object_digest, summary, created_at, expires_at)
VALUES('debug-old', 'profile-a', 'download', ?, 'expired', ?, ?);
INSERT INTO jobs(id, profile_id, kind, state, created_at, updated_at, completed_at)
VALUES('job-old', 'profile-a', 'download', 'completed', ?, ?, ?);
INSERT INTO job_logs(job_id, level, message, fields_json, created_at) VALUES('job-old', 'info', 'old log', '{}', ?)`,
		referenced.Digest, referenced.Size, old.UnixMilli(),
		referenced.Digest, old.UnixMilli(), old.UnixMilli(),
		old.UnixMilli(), old.UnixMilli(), old.UnixMilli(), old.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	plan, err := database.PlanGarbageCollection(context.Background(), GarbageCollectionOptions{
		ObjectStore: store, ObjectRetention: 24 * time.Hour, TemporaryRetention: 24 * time.Hour,
		DebugRetention: 30 * 24 * time.Hour, CompletedJobRetention: 30 * 24 * time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Objects.Unreferenced.Count != 1 || plan.Objects.Temporary.Count != 1 ||
		plan.Metadata.ExpiredDebug.Count != 1 || plan.Metadata.CompletedJobLogs.Count != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := database.ApplyGarbageCollection(context.Background(), GarbageCollectionOptions{ObjectStore: store}, plan, ""); err == nil {
		t.Fatal("ApplyGarbageCollection(without confirmation) error = nil")
	}
	if err := store.Validate(context.Background(), unreferenced.Digest); err != nil {
		t.Fatalf("dry run deleted object: %v", err)
	}
	result, err := database.ApplyGarbageCollection(context.Background(), GarbageCollectionOptions{ObjectStore: store}, plan, plan.Confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if result.Objects.DeletedObjects.Count != 1 || result.Objects.DeletedTemporary.Count != 1 ||
		result.DeletedDebug.Count != 1 || result.DeletedCompletedLogs.Count != 1 {
		t.Fatalf("result = %#v", result)
	}
	if err := store.Validate(context.Background(), referenced.Digest); err != nil {
		t.Fatalf("referenced object deleted: %v", err)
	}
	if err := store.Validate(context.Background(), unreferenced.Digest); !errors.Is(err, os.ErrNotExist) && err == nil {
		t.Fatalf("unreferenced object still exists: %v", err)
	}
	var debugCount, logCount int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM debug_incidents").Scan(&debugCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM job_logs").Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if debugCount != 0 || logCount != 0 {
		t.Fatalf("metadata remains: debug=%d logs=%d now=%d", debugCount, logCount, nowMillis)
	}
}
