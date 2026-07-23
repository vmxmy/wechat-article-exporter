package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRestoreStagesPreparesAndCommitsOnce(t *testing.T) {
	backend := &memoryUploadBackend{}
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	uploads, err := NewUploadStaging(UploadStagingOptions{Backend: backend, Now: func() time.Time { return now }, NewID: func() (UploadHandle, error) { return "upload-restore-1", nil }})
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &restoreCoordinatorFixture{}
	restore, err := NewRestore(RestoreOptions{Uploads: uploads, Coordinator: coordinator, Now: func() time.Time { return now }, NewID: func() (string, error) { return "restore-plan-1", nil }, NewConfirmation: func() (string, error) { return "restore-proof-1", nil }})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := restore.Stage(context.Background(), strings.NewReader("archive"), int64(len("archive")))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := restore.Prepare(context.Background(), RestorePrepareRequest{UploadHandle: receipt.Handle, ConflictPolicy: RestoreRenameConflicts})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ID != "restore-plan-1" || prepared.Confirmation != "restore-proof-1" || prepared.ConflictPolicy != RestoreRenameConflicts {
		t.Fatalf("prepared = %#v", prepared)
	}
	if _, err := restore.Prepare(context.Background(), RestorePrepareRequest{UploadHandle: receipt.Handle, ConflictPolicy: RestoreRenameConflicts}); !errors.Is(err, ErrRestoreConfirmationRequired) {
		t.Fatalf("second prepare error = %v", err)
	}
	completion, err := restore.Commit(context.Background(), RestoreCommitRequest{PreparationID: prepared.ID, Confirmation: prepared.Confirmation})
	if err != nil {
		t.Fatal(err)
	}
	if completion.RestoredFiles != 2 || coordinator.archive != "archive" || coordinator.policy != RestoreRenameConflicts {
		t.Fatalf("completion=%#v coordinator=%#v", completion, coordinator)
	}
	if _, err := restore.Commit(context.Background(), RestoreCommitRequest{PreparationID: prepared.ID, Confirmation: prepared.Confirmation}); !errors.Is(err, ErrRestoreConfirmationRequired) {
		t.Fatalf("replayed commit error = %v", err)
	}
	if backend.deleteCount != 1 {
		t.Fatalf("upload cleanup count=%d, want 1", backend.deleteCount)
	}
}

func TestRestoreExpiresPreparationDiscardsUploadAndConsumesBadProof(t *testing.T) {
	backend := &memoryUploadBackend{}
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	uploads, err := NewUploadStaging(UploadStagingOptions{Backend: backend, Now: func() time.Time { return now }, NewID: func() (UploadHandle, error) { return "upload-restore-2", nil }})
	if err != nil {
		t.Fatal(err)
	}
	restore, err := NewRestore(RestoreOptions{Uploads: uploads, Coordinator: &restoreCoordinatorFixture{}, ConfirmationTTL: time.Minute, Now: func() time.Time { return now }, NewID: func() (string, error) { return "restore-plan-2", nil }, NewConfirmation: func() (string, error) { return "restore-proof-2", nil }})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := restore.Stage(context.Background(), strings.NewReader("archive"), -1)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := restore.Prepare(context.Background(), RestorePrepareRequest{UploadHandle: receipt.Handle, ConflictPolicy: RestoreRefuseConflicts})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restore.Commit(context.Background(), RestoreCommitRequest{PreparationID: prepared.ID, Confirmation: "wrong-proof"}); !errors.Is(err, ErrRestoreConfirmationRequired) {
		t.Fatalf("wrong proof error = %v", err)
	}
	// Incorrect proof does not consume a valid confirmation, but a later clock
	// advance does; a subsequent request also purges and deletes its upload.
	now = now.Add(2 * time.Minute)
	if _, err := restore.Prepare(context.Background(), RestorePrepareRequest{UploadHandle: receipt.Handle, ConflictPolicy: RestoreRefuseConflicts}); !errors.Is(err, ErrRestoreConfirmationRequired) {
		t.Fatalf("expired prepare error = %v", err)
	}
	if backend.deleteCount != 1 {
		t.Fatalf("expired preparation cleanup count=%d, want 1", backend.deleteCount)
	}
}

type restoreCoordinatorFixture struct {
	archive string
	policy  RestoreConflictPolicy
}

func (fixture *restoreCoordinatorFixture) Restore(_ context.Context, archive io.Reader, policy RestoreConflictPolicy) (RestoreCompletion, error) {
	contents, err := io.ReadAll(archive)
	if err != nil {
		return RestoreCompletion{}, err
	}
	fixture.archive, fixture.policy = string(contents), policy
	return RestoreCompletion{RestoredFiles: 2, RestoredBytes: int64(len(contents)), Profiles: 1}, nil
}
