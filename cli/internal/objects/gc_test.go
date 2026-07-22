package objects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGarbageCollectionPlansConfirmsRechecksAndDeletesSafely(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	referenced, err := store.Put(ctx, strings.NewReader("referenced"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	unreferenced, err := store.Put(ctx, strings.NewReader("unreferenced"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	young, err := store.Put(ctx, strings.NewReader("young"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	old := now.Add(-48 * time.Hour)
	for _, digest := range []string{referenced.Digest, unreferenced.Digest} {
		path, pathErr := store.pathForDigest(digest)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	temporaryDirectory := filepath.Join(store.Root(), "tmp")
	if err := os.MkdirAll(temporaryDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	temporaryPath := filepath.Join(temporaryDirectory, ".object-abandoned.tmp")
	if err := os.WriteFile(temporaryPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(temporaryPath, old, old); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(temporaryDirectory, ".object-link.tmp")
	if err := os.Symlink(temporaryPath, symlinkPath); err != nil {
		t.Fatal(err)
	}

	plan, err := store.PlanGarbageCollection(ctx, map[string]struct{}{referenced.Digest: {}}, RetentionPolicy{
		UnreferencedObjects: 24 * time.Hour,
		TemporaryFiles:      24 * time.Hour,
		Now:                 now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Unreferenced.Count != 1 || plan.Temporary.Count != 1 || plan.Confirmation == "" {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := store.ApplyGarbageCollection(ctx, plan, "wrong", nil); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("ApplyGarbageCollection(wrong confirmation) error = %v", err)
	}

	result, err := store.ApplyGarbageCollection(ctx, plan, plan.Confirmation, func(_ context.Context, digest string) (bool, error) {
		return digest == unreferenced.Digest, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedTemporary.Count != 1 || result.DeletedObjects.Count != 0 || len(result.Skipped) != 1 {
		t.Fatalf("first result = %#v", result)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary path still exists: %v", err)
	}
	if _, err := os.Lstat(symlinkPath); err != nil {
		t.Fatalf("symlink should not be followed or deleted: %v", err)
	}

	secondPlan, err := store.PlanGarbageCollection(ctx, map[string]struct{}{referenced.Digest: {}}, RetentionPolicy{
		UnreferencedObjects: 24 * time.Hour,
		TemporaryFiles:      24 * time.Hour,
		Now:                 now,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = store.ApplyGarbageCollection(ctx, secondPlan, secondPlan.Confirmation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedObjects.Count != 1 {
		t.Fatalf("second result = %#v", result)
	}
	if err := store.Validate(ctx, referenced.Digest); err != nil {
		t.Fatalf("referenced object was removed: %v", err)
	}
	if err := store.Validate(ctx, young.Digest); err != nil {
		t.Fatalf("young unreferenced object was removed: %v", err)
	}
}

func TestGarbageCollectionSkipsCandidateChangedAfterDryRun(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.Put(context.Background(), strings.NewReader("old"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.pathForDigest(object.Digest)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	old := now.Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanGarbageCollection(context.Background(), nil, RetentionPolicy{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyGarbageCollection(context.Background(), plan, plan.Confirmation, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeletedObjects.Count != 0 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v", result)
	}
}
