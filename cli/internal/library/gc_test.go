package library

import (
	"archive/zip"
	"context"
	"encoding/json"
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

func TestGarbageCollectionConfirmationBindsExactCandidateIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	base := GarbageCollectionPlan{
		GeneratedAt: now,
		Objects: objects.GCPlan{Candidates: []objects.GCCandidate{{
			Kind: "temporary", Path: "/objects/tmp/a", Size: 4, ModifiedAt: now.Add(-time.Hour),
		}}},
		debugIDs:  []string{"debug-a"},
		jobLogIDs: []int64{1},
	}
	base.Objects.Temporary.Count = 1
	base.Metadata.ExpiredDebug.Count = 1
	base.Metadata.CompletedJobLogs.Count = 1
	changed := base
	changed.Objects.Candidates = append([]objects.GCCandidate(nil), base.Objects.Candidates...)
	changed.Objects.Candidates[0].Path = "/objects/tmp/b"
	if garbageCollectionConfirmation(base) == garbageCollectionConfirmation(changed) {
		t.Fatal("confirmation did not bind the exact candidate path")
	}
}

func TestQueuedJobPinnedObjectsSurviveGarbageCollectionAndEnterBackup(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pinned, err := store.Put(ctx, strings.NewReader("queued immutable export snapshot"), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	unreferenced, err := store.Put(ctx, strings.NewReader("ordinary unreferenced object"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	old := now.Add(-60 * 24 * time.Hour)
	for _, object := range []objects.Object{pinned, unreferenced} {
		path := filepath.Join(store.Root(), "sha256", object.Digest[:2], object.Digest[2:4], object.Digest)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	itemKey, err := json.Marshal(map[string]any{"version": 4, "pinnedDigests": []string{pinned.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, media_type, created_at) VALUES(?, ?, ?, ?), (?, ?, ?, ?)`,
		pinned.Digest, pinned.Size, pinned.MediaType, old.UnixMilli(),
		unreferenced.Digest, unreferenced.Size, unreferenced.MediaType, old.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO jobs(id, profile_id, kind, state, created_at, updated_at)
VALUES('job-pinned', 'profile-a', 'export', 'queued', ?, ?)`, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO job_items(id, job_id, item_key, state, created_at, updated_at)
VALUES('item-pinned', 'job-pinned', ?, 'queued', ?, ?)`, string(itemKey), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	referenced, err := database.referencedObjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := referenced[pinned.Digest]; !ok {
		var stored string
		if err := database.db.QueryRow(`SELECT item_key FROM job_items WHERE id='item-pinned'`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("pinned digest was not resolved from job item: key=%s referenced=%#v", stored, referenced)
	}
	plan, err := database.PlanGarbageCollection(ctx, GarbageCollectionOptions{
		ObjectStore: store, ObjectRetention: 24 * time.Hour, TemporaryRetention: 24 * time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Objects.Unreferenced.Count != 1 {
		t.Fatalf("garbage collection plan = %#v", plan.Objects.Unreferenced)
	}
	result, err := database.ApplyGarbageCollection(ctx, GarbageCollectionOptions{ObjectStore: store}, plan, plan.Confirmation)
	if err != nil {
		t.Fatal(err)
	}
	if result.Objects.DeletedObjects.Count != 1 {
		t.Fatalf("garbage collection result = %#v", result.Objects.DeletedObjects)
	}
	if err := store.Validate(ctx, pinned.Digest); err != nil {
		t.Fatalf("queued pinned object was deleted: %v", err)
	}
	if err := store.Validate(ctx, unreferenced.Digest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary unreferenced object was retained: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "pinned.wab")
	manifest, err := database.CreateBackup(ctx, BackupOptions{Destination: archivePath, ObjectStore: store, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Objects) != 1 || manifest.Objects[0].Digest != pinned.Digest {
		t.Fatalf("backup manifest objects = %#v", manifest.Objects)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	found := false
	for _, file := range reader.File {
		if file.Name == backupObjectPath(pinned.Digest) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("backup omitted queued pinned object %s", pinned.Digest)
	}
}

func TestCompletedJobItemPinBecomesGarbageCollectionCandidate(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pinned, err := store.Put(ctx, strings.NewReader("completed export snapshot"), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	old := now.Add(-60 * 24 * time.Hour)
	path := filepath.Join(store.Root(), "sha256", pinned.Digest[:2], pinned.Digest[2:4], pinned.Digest)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	itemKey, err := json.Marshal(map[string]any{"version": 4, "pinnedDigests": []string{pinned.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, media_type, created_at) VALUES(?, ?, ?, ?);
INSERT INTO jobs(id, profile_id, kind, state, created_at, updated_at, completed_at)
VALUES('job-completed-pin', 'profile-a', 'export', 'completed', ?, ?, ?);
INSERT INTO job_items(id, job_id, item_key, state, created_at, updated_at, completed_at)
VALUES('item-completed-pin', 'job-completed-pin', ?, 'completed', ?, ?, ?)`,
		pinned.Digest, pinned.Size, pinned.MediaType, old.UnixMilli(),
		now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), string(itemKey),
		now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	referenced, err := database.referencedObjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, retained := referenced[pinned.Digest]; retained {
		t.Fatalf("completed job pin remained reachable: %#v", referenced)
	}
	plan, err := database.PlanGarbageCollection(ctx, GarbageCollectionOptions{
		ObjectStore: store, ObjectRetention: 24 * time.Hour, TemporaryRetention: 24 * time.Hour, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Objects.Unreferenced.Count != 1 {
		t.Fatalf("garbage collection plan=%#v", plan.Objects)
	}
}
