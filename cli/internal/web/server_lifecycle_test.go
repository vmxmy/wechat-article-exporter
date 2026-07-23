package web

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestServerCloseCleansOneShotArtifactsExactlyOnce(t *testing.T) {
	restoreBackend := &lifecycleUploadBackend{}
	restore := newLifecycleRestore(t, restoreBackend)
	if _, err := restore.Stage(context.Background(), strings.NewReader("restore archive"), int64(len("restore archive"))); err != nil {
		t.Fatal(err)
	}

	manifestBackend := &lifecycleUploadBackend{}
	manifests := newLifecycleAccountManifests(t, manifestBackend)
	if _, err := manifests.Stage(context.Background(), strings.NewReader("manifest"), int64(len("manifest"))); err != nil {
		t.Fatal(err)
	}

	backups := &lifecycleBackupMaintenance{}
	storage := application.NewMaintenanceStorage(application.MaintenanceStorageOptions{Backups: backups})
	server, err := New(Options{
		Application:        testApplication{},
		Restore:            restore,
		AccountManifests:   manifests,
		StorageMaintenance: storage,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	if restoreBackend.deletes != 1 {
		t.Fatalf("restore cleanup deleted %d uploads, want 1", restoreBackend.deletes)
	}
	if manifestBackend.deletes != 1 {
		t.Fatalf("account manifest cleanup deleted %d uploads, want 1", manifestBackend.deletes)
	}
	if backups.closes != 1 {
		t.Fatalf("backup artifact cleanup closed %d times, want 1", backups.closes)
	}
}

func newLifecycleRestore(t *testing.T, backend application.UploadStagingBackend) *application.RestoreService {
	t.Helper()
	uploads, err := application.NewUploadStaging(application.UploadStagingOptions{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	restore, err := application.NewRestore(application.RestoreOptions{Uploads: uploads, Coordinator: shutdownRestoreCoordinator{}})
	if err != nil {
		t.Fatal(err)
	}
	return restore
}

func newLifecycleAccountManifests(t *testing.T, backend application.UploadStagingBackend) *application.AccountManifestService {
	t.Helper()
	uploads, err := application.NewUploadStaging(application.UploadStagingOptions{Backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := application.NewAccountManifestService(testApplication{}, uploads)
	if err != nil {
		t.Fatal(err)
	}
	return manifests
}

type lifecycleUploadBackend struct {
	deletes int
}

func (backend *lifecycleUploadBackend) Stage(_ context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	if _, err := io.ReadAll(source); err != nil {
		return application.UploadStagedObject{}, err
	}
	return application.UploadStagedObject{Reference: "private"}, nil
}

func (*lifecycleUploadBackend) Open(context.Context, application.UploadStagedObject) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (backend *lifecycleUploadBackend) Delete(context.Context, application.UploadStagedObject) error {
	backend.deletes++
	return nil
}

type lifecycleBackupMaintenance struct {
	closes int
}

func (*lifecycleBackupMaintenance) CreateBackup(context.Context) (application.BackupReceipt, error) {
	return application.BackupReceipt{}, nil
}

func (*lifecycleBackupMaintenance) VerifyBackup(context.Context, string) (application.BackupVerification, error) {
	return application.BackupVerification{}, nil
}

func (*lifecycleBackupMaintenance) OpenBackup(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (backups *lifecycleBackupMaintenance) Close(context.Context) error {
	backups.closes++
	return nil
}
