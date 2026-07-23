package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeBackupMaintenance struct {
	receipt      BackupReceipt
	verification BackupVerification
	verifiedID   string
	err          error
}

func (fake *fakeBackupMaintenance) CreateBackup(context.Context) (BackupReceipt, error) {
	return fake.receipt, fake.err
}
func (fake *fakeBackupMaintenance) VerifyBackup(_ context.Context, id string) (BackupVerification, error) {
	fake.verifiedID = id
	return fake.verification, fake.err
}

type fakeIntegrityMaintenance struct{ report IntegrityReport }

func (fake fakeIntegrityMaintenance) CheckIntegrity(context.Context) (IntegrityReport, error) {
	return fake.report, nil
}

type fakeGarbageCollectionMaintenance struct {
	plan     GarbageCollectionPlan
	result   GarbageCollectionResult
	applied  []GarbageCollectionApplyRequest
	planErr  error
	applyErr error
}

func (fake *fakeGarbageCollectionMaintenance) PlanGarbageCollection(context.Context) (GarbageCollectionPlan, error) {
	return fake.plan, fake.planErr
}
func (fake *fakeGarbageCollectionMaintenance) ApplyGarbageCollection(_ context.Context, id, confirmation string) (GarbageCollectionResult, error) {
	fake.applied = append(fake.applied, GarbageCollectionApplyRequest{PlanID: id, Confirmation: confirmation})
	return fake.result, fake.applyErr
}

type fakeDiagnosticsMaintenance struct{ report DiagnosticsReport }

func (fake fakeDiagnosticsMaintenance) CollectDiagnostics(context.Context) (DiagnosticsReport, error) {
	return fake.report, nil
}

func TestMaintenanceStorageBackupVerificationUsesOpaqueHandleAndRedactsOutput(t *testing.T) {
	backups := &fakeBackupMaintenance{
		receipt:      BackupReceipt{ID: "backup-7", SHA256: "digest", Omitted: []string{"Cookie: sid=backup-secret at /Users/fixture/archive.zip"}},
		verification: BackupVerification{Valid: false, Failures: []string{"cannot read /Users/fixture/archive.zip access_token=verify-secret"}},
	}
	service := NewMaintenanceStorage(MaintenanceStorageOptions{Backups: backups})
	receipt, err := service.CreateBackup(context.Background())
	if err != nil || receipt.ID != "backup-7" {
		t.Fatalf("CreateBackup() = %#v, %v", receipt, err)
	}
	verification, err := service.VerifyBackup(context.Background(), " backup-7 ")
	if err != nil || backups.verifiedID != "backup-7" || verification.BackupID != "backup-7" {
		t.Fatalf("VerifyBackup() = %#v, %v; id = %q", verification, err, backups.verifiedID)
	}
	encoded, err := json.Marshal(struct {
		Receipt      BackupReceipt      `json:"receipt"`
		Verification BackupVerification `json:"verification"`
	}{receipt, verification})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"backup-secret", "verify-secret", "/Users/fixture/archive.zip"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("storage response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestMaintenanceStorageIntegrityAndDiagnosticsRedactPathsAndSecrets(t *testing.T) {
	service := NewMaintenanceStorage(MaintenanceStorageOptions{
		Integrity: fakeIntegrityMaintenance{report: IntegrityReport{Issues: []IntegrityIssue{{
			Kind: "missing-object", Message: "missing /private/var/db.sqlite Cookie: sid=integrity-secret",
			Recommendation: `inspect path="C:\Users\fixture\objects"`, Repairable: true,
		}}}},
		Diagnostics: fakeDiagnosticsMaintenance{report: DiagnosticsReport{Checks: []DiagnosticCheck{{
			Name: "storage", Status: "degraded", Summary: `root="/Users/fixture/data" appmsg_token=diagnostic-secret`,
		}}}},
	})
	integrity, err := service.CheckIntegrity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := service.Diagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(struct {
		Integrity   IntegrityReport   `json:"integrity"`
		Diagnostics DiagnosticsReport `json:"diagnostics"`
	}{integrity, diagnostics})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/private/var/db.sqlite", `C:\Users\fixture\objects`, "/Users/fixture/data", "integrity-secret", "diagnostic-secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("maintenance output leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestMaintenanceStorageGarbageCollectionRequiresIssuedExactPlanBoundConfirmation(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	garbage := &fakeGarbageCollectionMaintenance{plan: GarbageCollectionPlan{
		ID: "gc-plan-1", Confirmation: "garbage-collect:fixture-proof", ExpiresAt: now.Add(time.Minute),
		Unreferenced: StorageReclaimable{Count: 2, Bytes: 42},
	}}
	service := NewMaintenanceStorage(MaintenanceStorageOptions{Garbage: garbage, Now: func() time.Time { return now }})
	if _, err := service.ApplyGarbageCollection(context.Background(), GarbageCollectionApplyRequest{PlanID: "gc-plan-1", Confirmation: garbage.plan.Confirmation}); !errors.Is(err, ErrMaintenanceConfirmationRequired) || len(garbage.applied) != 0 {
		t.Fatalf("ApplyGarbageCollection() before plan error = %v, calls = %#v", err, garbage.applied)
	}
	plan, err := service.PlanGarbageCollection(context.Background())
	if err != nil || plan.ID != "gc-plan-1" {
		t.Fatalf("PlanGarbageCollection() = %#v, %v", plan, err)
	}
	for _, request := range []GarbageCollectionApplyRequest{
		{PlanID: plan.ID, Confirmation: ""},
		{PlanID: plan.ID, Confirmation: plan.Confirmation + " "},
		{PlanID: "other-plan", Confirmation: plan.Confirmation},
	} {
		if _, err := service.ApplyGarbageCollection(context.Background(), request); !errors.Is(err, ErrMaintenanceConfirmationRequired) || len(garbage.applied) != 0 {
			t.Fatalf("ApplyGarbageCollection(%#v) error = %v, calls = %#v", request, err, garbage.applied)
		}
	}
	result, err := service.ApplyGarbageCollection(context.Background(), GarbageCollectionApplyRequest{PlanID: plan.ID, Confirmation: plan.Confirmation})
	if err != nil || len(garbage.applied) != 1 || garbage.applied[0].PlanID != plan.ID || result.DeletedObjects.Count != 0 {
		t.Fatalf("ApplyGarbageCollection() = %#v, %v; calls = %#v", result, err, garbage.applied)
	}
	if _, err := service.ApplyGarbageCollection(context.Background(), GarbageCollectionApplyRequest{PlanID: plan.ID, Confirmation: plan.Confirmation}); !errors.Is(err, ErrMaintenanceConfirmationRequired) || len(garbage.applied) != 1 {
		t.Fatalf("replayed ApplyGarbageCollection() error = %v, calls = %#v", err, garbage.applied)
	}
}

func TestMaintenanceStorageRejectsUnsafeOpaqueHandlesAndConsumesPlanBeforeFailure(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	garbage := &fakeGarbageCollectionMaintenance{
		plan:     GarbageCollectionPlan{ID: "gc-plan", Confirmation: "proof", ExpiresAt: now.Add(time.Minute)},
		applyErr: errors.New("delete /private/objects Cookie: sid=backend-secret"),
	}
	service := NewMaintenanceStorage(MaintenanceStorageOptions{Garbage: garbage, Now: func() time.Time { return now }})
	plan, err := service.PlanGarbageCollection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyGarbageCollection(context.Background(), GarbageCollectionApplyRequest{PlanID: plan.ID, Confirmation: plan.Confirmation}); err == nil || errors.Unwrap(err) != nil || strings.Contains(err.Error(), "backend-secret") {
		t.Fatalf("ApplyGarbageCollection() leaked backend failure: %v", err)
	}
	if _, err := service.ApplyGarbageCollection(context.Background(), GarbageCollectionApplyRequest{PlanID: plan.ID, Confirmation: plan.Confirmation}); !errors.Is(err, ErrMaintenanceConfirmationRequired) || len(garbage.applied) != 1 {
		t.Fatalf("failed plan was reusable: %v, calls=%#v", err, garbage.applied)
	}
	garbage.plan = GarbageCollectionPlan{ID: "/private/gc-plan", Confirmation: "proof", ExpiresAt: now.Add(time.Minute)}
	if _, err := service.PlanGarbageCollection(context.Background()); err == nil {
		t.Fatal("PlanGarbageCollection() accepted a path-bearing plan ID")
	}
	garbage.plan = GarbageCollectionPlan{ID: "gc-plan-2", Confirmation: "Cookie: sid=secret", ExpiresAt: now.Add(time.Minute)}
	if _, err := service.PlanGarbageCollection(context.Background()); err == nil {
		t.Fatal("PlanGarbageCollection() accepted a secret-bearing confirmation")
	}
}

func TestMaintenanceStorageFailsClosedForMissingCapabilitiesExpiredPlansAndUnsafeFailures(t *testing.T) {
	service := NewMaintenanceStorage(MaintenanceStorageOptions{})
	if _, err := service.CreateBackup(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CreateBackup() missing capability error = %v", err)
	}
	if _, err := service.CheckIntegrity(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("CheckIntegrity() missing capability error = %v", err)
	}
	if _, err := service.Diagnostics(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Diagnostics() missing capability error = %v", err)
	}
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	garbage := &fakeGarbageCollectionMaintenance{plan: GarbageCollectionPlan{ID: "expired", Confirmation: "proof", ExpiresAt: now}}
	service = NewMaintenanceStorage(MaintenanceStorageOptions{Garbage: garbage, Now: func() time.Time { return now }})
	if _, err := service.PlanGarbageCollection(context.Background()); err == nil || len(garbage.applied) != 0 {
		t.Fatalf("PlanGarbageCollection() accepted expired plan: %v", err)
	}
	backups := &fakeBackupMaintenance{err: errors.New("create /Users/fixture/backup.zip Cookie: sid=error-secret")}
	service = NewMaintenanceStorage(MaintenanceStorageOptions{Backups: backups})
	if _, err := service.CreateBackup(context.Background()); err == nil || strings.Contains(err.Error(), "/Users/fixture/backup.zip") || strings.Contains(err.Error(), "error-secret") {
		t.Fatalf("CreateBackup() leaked backend failure: %v", err)
	}
}
