package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

const webRestoreMaximumUploadBytes = 2 << 30

func newWebRestore(app *App) (*application.RestoreService, error) {
	staging, err := application.NewUploadStaging(application.UploadStagingOptions{
		Backend: applicationUploadFileStaging{app: app},
		Limits:  application.UploadStagingLimits{MaximumBytes: webRestoreMaximumUploadBytes},
	})
	if err != nil {
		return nil, err
	}
	return application.NewRestore(application.RestoreOptions{Uploads: staging, Coordinator: appRestoreCoordinator{app: app}})
}

// applicationUploadFileStaging retains all filesystem access in app. The
// application service exposes only opaque UploadHandle values to web.
type applicationUploadFileStaging struct{ app *App }

func (backend applicationUploadFileStaging) Stage(ctx context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	if backend.app == nil || backend.app.active == nil {
		return application.UploadStagedObject{}, fmt.Errorf("active profile storage is unavailable")
	}
	directory := filepath.Join(backend.app.active.Profile.Paths.Cache, "web-restore")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return application.UploadStagedObject{}, err
	}
	file, err := createPrivateTemp(directory, ".restore-upload-*.wab")
	if err != nil {
		return application.UploadStagedObject{}, err
	}
	path := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(path)
		}
	}()
	if _, err := io.Copy(file, source); err != nil {
		return application.UploadStagedObject{}, err
	}
	if err := ctx.Err(); err != nil {
		return application.UploadStagedObject{}, err
	}
	if err := file.Sync(); err != nil {
		return application.UploadStagedObject{}, err
	}
	if err := file.Close(); err != nil {
		return application.UploadStagedObject{}, err
	}
	committed = true
	return application.UploadStagedObject{Reference: path}, nil
}

func (backend applicationUploadFileStaging) Open(ctx context.Context, object application.UploadStagedObject) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(object.Reference)
}

func (backend applicationUploadFileStaging) Delete(_ context.Context, object application.UploadStagedObject) error {
	if object.Reference == "" {
		return nil
	}
	if err := os.Remove(object.Reference); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type appRestoreCoordinator struct{ app *App }

func (coordinator appRestoreCoordinator) Restore(ctx context.Context, archive io.Reader, policy application.RestoreConflictPolicy) (application.RestoreCompletion, error) {
	if coordinator.app == nil || coordinator.app.active == nil {
		return application.RestoreCompletion{}, fmt.Errorf("active profile storage is unavailable")
	}
	directory := filepath.Join(coordinator.app.active.Profile.Paths.Cache, "web-restore")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return application.RestoreCompletion{}, err
	}
	file, err := createPrivateTemp(directory, ".restore-commit-*.wab")
	if err != nil {
		return application.RestoreCompletion{}, err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := io.Copy(file, archive); err != nil {
		_ = file.Close()
		return application.RestoreCompletion{}, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return application.RestoreCompletion{}, err
	}
	if err := file.Close(); err != nil {
		return application.RestoreCompletion{}, err
	}
	conflict := library.RestoreConflictPolicy(policy)
	report, err := coordinator.app.restoreActiveProfile(ctx, path, conflict)
	if err != nil {
		return application.RestoreCompletion{}, err
	}
	return application.RestoreCompletion{RestoredFiles: report.RestoredFiles, RestoredBytes: report.RestoredBytes, Profiles: len(report.Profiles)}, nil
}

var _ application.UploadStagingBackend = applicationUploadFileStaging{}
var _ application.RestoreCoordinator = appRestoreCoordinator{}
