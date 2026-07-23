package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const webAccountManifestMaximumUploadBytes = application.AccountManifestMaximumBytes

func newWebAccountManifests(app *App) (*application.AccountManifestService, error) {
	staging, err := application.NewUploadStaging(application.UploadStagingOptions{
		Backend: applicationAccountManifestFileStaging{app: app},
		Limits: application.UploadStagingLimits{
			MaximumBytes:      webAccountManifestMaximumUploadBytes,
			MaximumTotalBytes: webAccountManifestMaximumUploadBytes,
		},
	})
	if err != nil {
		return nil, err
	}
	return application.NewAccountManifestService(app.core, staging)
}

// applicationAccountManifestFileStaging owns the private cache file. The web
// adapter sees only the opaque handle returned by application.UploadStaging.
type applicationAccountManifestFileStaging struct{ app *App }

func (backend applicationAccountManifestFileStaging) Stage(ctx context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	if backend.app == nil || backend.app.active == nil {
		return application.UploadStagedObject{}, fmt.Errorf("active profile storage is unavailable")
	}
	directory := filepath.Join(backend.app.active.Profile.Paths.Cache, "web-account-manifests")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return application.UploadStagedObject{}, err
	}
	file, err := createPrivateTemp(directory, ".account-manifest-*.json")
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

func (backend applicationAccountManifestFileStaging) Open(ctx context.Context, object application.UploadStagedObject) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(object.Reference)
}

func (backend applicationAccountManifestFileStaging) Delete(_ context.Context, object application.UploadStagedObject) error {
	if object.Reference == "" {
		return nil
	}
	if err := os.Remove(object.Reference); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var _ application.UploadStagingBackend = applicationAccountManifestFileStaging{}
