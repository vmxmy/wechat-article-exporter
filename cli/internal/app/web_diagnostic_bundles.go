package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

// newWebDiagnosticBundles keeps the diagnostic archive path wholly inside the
// local runtime. The returned application facade exposes only opaque handles.
func newWebDiagnosticBundles(app *App) *application.DiagnosticBundleService {
	return application.NewDiagnosticBundleService(application.DiagnosticBundleOptions{Maintenance: &webDiagnosticBundleAdapter{app: app, bundles: make(map[string]string)}})
}

type webDiagnosticBundleAdapter struct {
	app *App

	mu      sync.Mutex
	bundles map[string]string
}

func (adapter *webDiagnosticBundleAdapter) CreateDiagnosticBundle(ctx context.Context) (application.DiagnosticBundleArtifact, error) {
	if adapter == nil || adapter.app == nil {
		return application.DiagnosticBundleArtifact{}, errors.New("diagnostic bundle runtime is unavailable")
	}
	active, err := adapter.app.requireActiveStorage()
	if err != nil {
		return application.DiagnosticBundleArtifact{}, err
	}
	id, err := webMaintenanceID("diagnostic")
	if err != nil {
		return application.DiagnosticBundleArtifact{}, err
	}
	directory := filepath.Join(active.Profile.Paths.Data, "diagnostics", "web-staging")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return application.DiagnosticBundleArtifact{}, err
	}
	path := filepath.Join(directory, id+".zip")
	report, err := adapter.app.createDiagnosticBundle(ctx, path)
	if err != nil {
		return application.DiagnosticBundleArtifact{}, err
	}
	adapter.mu.Lock()
	adapter.bundles[id] = path
	adapter.mu.Unlock()
	return application.DiagnosticBundleArtifact{Reference: id, CreatedAt: time.Now(), SHA256: report.SHA256, SizeBytes: report.Bytes}, nil
}

func (adapter *webDiagnosticBundleAdapter) OpenDiagnosticBundle(_ context.Context, reference string) (io.ReadCloser, error) {
	path, found := adapter.path(reference)
	if !found {
		return nil, errors.New("diagnostic bundle archive is unavailable")
	}
	return os.Open(path)
}

func (adapter *webDiagnosticBundleAdapter) DiscardDiagnosticBundle(_ context.Context, reference string) error {
	adapter.mu.Lock()
	path, found := adapter.bundles[reference]
	if found {
		delete(adapter.bundles, reference)
	}
	adapter.mu.Unlock()
	if !found {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (adapter *webDiagnosticBundleAdapter) path(reference string) (string, bool) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	path, found := adapter.bundles[reference]
	return path, found
}

var _ application.DiagnosticBundleMaintenance = (*webDiagnosticBundleAdapter)(nil)
