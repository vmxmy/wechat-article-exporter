package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

type workspaceExportJobs struct {
	request domain.ExportRequest
	job     domain.Job
}

func (jobs *workspaceExportJobs) Start(_ context.Context, request domain.ExportRequest) (domain.Job, error) {
	jobs.request = request
	return jobs.job, nil
}
func (*workspaceExportJobs) Run(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (*workspaceExportJobs) Recover(context.Context) (int64, error) { return 0, nil }

type workspaceExportStarter struct{}

func (workspaceExportStarter) Start(context.Context, domain.Job) error { return nil }

type workspaceExportRecords struct {
	page   domain.Page[library.ExportRecord]
	record library.ExportRecord
	files  []library.ExportFileRecord
}

func (records workspaceExportRecords) QueryExportRecords(context.Context, int, int) (domain.Page[library.ExportRecord], error) {
	return records.page, nil
}
func (records workspaceExportRecords) GetExport(context.Context, domain.ExportID) (library.ExportRecord, error) {
	return records.record, nil
}
func (records workspaceExportRecords) ListExportFiles(context.Context, domain.ExportID) ([]library.ExportFileRecord, error) {
	return append([]library.ExportFileRecord(nil), records.files...), nil
}

func TestWorkspaceExportsIssuesOpaqueDirectoriesAndAuthorizesOnlyChildren(t *testing.T) {
	temporary := t.TempDir()
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	exports := &workspaceExportJobs{job: domain.Job{ID: "job-1", State: domain.JobQueued, CreatedAt: now}}
	service := NewWorkspaceExports(New(Options{Exports: exports, Starter: workspaceExportStarter{}}), nil)
	service.home = func() (string, error) { return temporary, nil }
	service.now = func() time.Time { return now }

	root, err := service.DefaultExportDirectory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if root.Token == "" || root.Label != workspaceDefaultExportDirectory || !root.IsDefault || root.CreatedAt != now {
		t.Fatalf("default directory = %#v", root)
	}
	if filepath.IsAbs(string(root.Token)) || string(root.Token) == filepath.Join(temporary, "Downloads", workspaceDefaultExportDirectory) {
		t.Fatalf("directory token exposed a host path: %q", root.Token)
	}

	child, err := service.CreateExportDirectory(context.Background(), WorkspaceCreateExportDirectoryRequest{ParentToken: root.Token, Name: "July"})
	if err != nil {
		t.Fatal(err)
	}
	if child.Token == root.Token || child.Label != "July" {
		t.Fatalf("child directory = %#v", child)
	}

	queued, err := service.StartExport(context.Background(), WorkspaceStartExportRequest{
		DirectoryToken: child.Token, Subdirectory: "ready", Format: "markdown",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(temporary, "Downloads", workspaceDefaultExportDirectory, "July", "ready")
	if exports.request.OutputRoot != wantRoot || exports.request.OutputAuthorization == nil ||
		exports.request.OutputAuthorization.Root != wantRoot || exports.request.OutputAuthorization.RelativePath != "" ||
		exports.request.OutputAuthorization.Device == 0 || exports.request.OutputAuthorization.Inode == 0 {
		t.Fatalf("authorized request = %#v, want concrete authorized root %q", exports.request, wantRoot)
	}
	if queued.ID != "job-1" || queued.Directory != string(child.Token) || filepath.IsAbs(queued.Directory) {
		t.Fatalf("queued export = %#v", queued)
	}

	for _, subdirectory := range []string{"../escape", "/tmp/escape", `C:\\escape`} {
		_, err := service.StartExport(context.Background(), WorkspaceStartExportRequest{DirectoryToken: child.Token, Subdirectory: subdirectory, Format: "markdown"})
		var workspaceErr *WorkspaceError
		if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInvalidArgument {
			t.Fatalf("StartExport(%q) error = %#v", subdirectory, err)
		}
	}
}

func TestWorkspaceExportsRejectsUnknownTokensAndSymlinkDirectories(t *testing.T) {
	temporary := t.TempDir()
	service := NewWorkspaceExports(nil, nil)
	service.home = func() (string, error) { return temporary, nil }

	_, err := service.CreateExportDirectory(context.Background(), WorkspaceCreateExportDirectoryRequest{ParentToken: "dir_unknown", Name: "child"})
	var workspaceErr *WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorNotFound {
		t.Fatalf("unknown token error = %#v", err)
	}

	path := filepath.Join(temporary, "Downloads", workspaceDefaultExportDirectory)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err = service.DefaultExportDirectory(context.Background())
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInternal {
		t.Fatalf("symlink default directory error = %#v", err)
	}
}

func TestWorkspaceExportsReturnSafeManifestAndArtifactMetadata(t *testing.T) {
	created := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	completed := created.Add(time.Minute)
	record := library.ExportRecord{ID: "export-1", JobID: "job-1", Format: "markdown", State: "completed", CreatedAt: created,
		CompletedAt: completed, OutputRoot: "/private/export-root", ProvenancePath: "export-manifest.json", ProvenanceState: "ready", ProvenanceGeneration: 2}
	files := []library.ExportFileRecord{{ExportID: "export-1", ArticleID: "article-1", RelativePath: "article.md", SizeBytes: 42,
		SHA256: "abcdef", MediaType: "text/markdown", Status: "written"}}
	service := NewWorkspaceExports(nil, workspaceExportRecords{page: domain.Page[library.ExportRecord]{Items: []library.ExportRecord{record}, Total: 1, Limit: 1}, record: record, files: files})

	page, err := service.ExportRecords(context.Background(), WorkspacePageRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].OutputDirectory != "local export directory" {
		t.Fatalf("ExportRecords() = %#v, %v", page, err)
	}
	if reflect.ValueOf(page.Items[0]).FieldByName("OutputRoot").IsValid() {
		t.Fatalf("export record exposed output root: %#v", page.Items[0])
	}

	manifest, err := service.ExportManifest(context.Background(), "export-1")
	if err != nil || len(manifest.Files) != 1 || manifest.Files[0].Path != "article.md" {
		t.Fatalf("ExportManifest() = %#v, %v", manifest, err)
	}
	artifact, err := service.DownloadArtifact(context.Background(), WorkspaceDownloadArtifactRequest{ExportID: "export-1", Path: "article.md"})
	if err != nil || artifact.Path != "article.md" || artifact.Name != "article.md" {
		t.Fatalf("DownloadArtifact() = %#v, %v", artifact, err)
	}
	if reflect.ValueOf(artifact).FieldByName("AbsolutePath").IsValid() {
		t.Fatalf("artifact exposed absolute path: %#v", artifact)
	}
	_, err = service.DownloadArtifact(context.Background(), WorkspaceDownloadArtifactRequest{ExportID: "export-1", Path: "../article.md"})
	var workspaceErr *WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInvalidArgument {
		t.Fatalf("unsafe artifact error = %#v", err)
	}
}
