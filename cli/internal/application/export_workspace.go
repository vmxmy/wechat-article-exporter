package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

const workspaceDefaultExportDirectory = "wechat-article-exports"

// WorkspaceExportService is the filesystem-safe export boundary for local
// presentation adapters. Directory handles are opaque server-issued
// capabilities: callers provide neither host paths nor execution-time output
// authorizations.
type WorkspaceExportService interface {
	DefaultExportDirectory(context.Context) (WorkspaceExportDirectory, error)
	CreateExportDirectory(context.Context, WorkspaceCreateExportDirectoryRequest) (WorkspaceExportDirectory, error)
	StartExport(context.Context, WorkspaceStartExportRequest) (WorkspaceExportJob, error)
	ExportRecords(context.Context, WorkspacePageRequest) (WorkspacePage[WorkspaceExportRecord], error)
	ExportManifest(context.Context, string) (WorkspaceExportManifest, error)
	VerifyExport(context.Context, string) (WorkspaceExportVerification, error)
	DownloadArtifact(context.Context, WorkspaceDownloadArtifactRequest) (WorkspaceDownloadArtifact, error)
}

// WorkspaceExportDirectory carries a token only. Its host path and filesystem
// identity remain private to the application process.
type WorkspaceExportDirectory struct {
	Token       WorkspaceDirectoryHandle `json:"token"`
	Label       string                   `json:"label"`
	IsDefault   bool                     `json:"isDefault,omitempty"`
	CreatedAt   time.Time                `json:"createdAt"`
	Description string                   `json:"description,omitempty"`
}

type WorkspaceCreateExportDirectoryRequest struct {
	ParentToken WorkspaceDirectoryHandle `json:"parentToken,omitempty"`
	Name        string                   `json:"name"`
}

type WorkspaceStartExportRequest struct {
	DirectoryToken WorkspaceDirectoryHandle `json:"directoryToken"`
	Subdirectory   string                   `json:"subdirectory,omitempty"`
	Selection      domain.ExportSelection   `json:"selection"`
	Format         string                   `json:"format"`
	Options        domain.ExportOptions     `json:"options,omitempty"`
}

type WorkspaceExportJob struct {
	ID        domain.JobID    `json:"id"`
	State     domain.JobState `json:"state"`
	ExportID  string          `json:"exportId,omitempty"`
	Format    string          `json:"format"`
	QueuedAt  time.Time       `json:"queuedAt"`
	Directory string          `json:"directory"`
}

type WorkspaceExportRecord struct {
	ID                   string       `json:"id"`
	JobID                domain.JobID `json:"jobId"`
	Format               string       `json:"format"`
	State                string       `json:"state"`
	CreatedAt            time.Time    `json:"createdAt"`
	CompletedAt          *time.Time   `json:"completedAt,omitempty"`
	ProvenanceState      string       `json:"provenanceState,omitempty"`
	ProvenanceGeneration int64        `json:"provenanceGeneration"`
	OutputDirectory      string       `json:"outputDirectory"`
}

type WorkspaceExportFile struct {
	ArticleID string `json:"articleId,omitempty"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType,omitempty"`
	Status    string `json:"status"`
}

type WorkspaceExportManifest struct {
	ExportID             string                `json:"exportId"`
	Format               string                `json:"format"`
	State                string                `json:"state"`
	ProvenanceState      string                `json:"provenanceState,omitempty"`
	ProvenanceGeneration int64                 `json:"provenanceGeneration"`
	Files                []WorkspaceExportFile `json:"files"`
}

type WorkspaceExportVerification struct {
	ExportID           string                       `json:"exportId"`
	Valid              bool                         `json:"valid"`
	VerifiedOutputs    int                          `json:"verifiedOutputs"`
	Issues             []exporter.VerificationIssue `json:"issues"`
	AffectedArticleIDs []string                     `json:"affectedArticleIds,omitempty"`
}

type WorkspaceDownloadArtifactRequest struct {
	ExportID string `json:"exportId"`
	Path     string `json:"path"`
}

// WorkspaceDownloadArtifact describes a verified file that a local adapter
// may stream. It intentionally contains no absolute filename.
type WorkspaceDownloadArtifact struct {
	ExportID  string `json:"exportId"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType,omitempty"`
}

type workspaceExportDirectoryCapability struct {
	path      string
	root      *os.Root
	device    uint64
	inode     uint64
	createdAt time.Time
	label     string
	isDefault bool
}

// WorkspaceExports owns ephemeral directory and download capabilities while
// delegating durable export work to Application and its local library seam.
type WorkspaceExports struct {
	application Application
	library     workspaceExportLibrary
	directories map[WorkspaceDirectoryHandle]workspaceExportDirectoryCapability
	mu          sync.Mutex
	now         func() time.Time
	home        func() (string, error)
	random      func([]byte) (int, error)
}

type workspaceExportLibrary interface {
	QueryExportRecords(context.Context, int, int) (domain.Page[library.ExportRecord], error)
	GetExport(context.Context, domain.ExportID) (library.ExportRecord, error)
	ListExportFiles(context.Context, domain.ExportID) ([]library.ExportFileRecord, error)
}

func NewWorkspaceExports(application Application, records workspaceExportLibrary) *WorkspaceExports {
	return &WorkspaceExports{
		application: application,
		library:     records,
		directories: make(map[WorkspaceDirectoryHandle]workspaceExportDirectoryCapability),
		now:         time.Now,
		home:        os.UserHomeDir,
		random:      rand.Read,
	}
}

func (service *WorkspaceExports) DefaultExportDirectory(ctx context.Context) (WorkspaceExportDirectory, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceExportDirectory{}, workspaceError(err)
	}
	home, err := service.home()
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("resolve Downloads directory: %w", err))
	}
	homeRoot, err := os.OpenRoot(home)
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("open home directory: %w", err))
	}
	const relative = "Downloads/" + workspaceDefaultExportDirectory
	if err := homeRoot.MkdirAll(relative, 0o700); err != nil {
		homeRoot.Close()
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("create default export directory: %w", err))
	}
	info, err := homeRoot.Lstat(relative)
	if err != nil {
		homeRoot.Close()
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("inspect default export directory: %w", err))
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		homeRoot.Close()
		return WorkspaceExportDirectory{}, workspaceError(errors.New("default export directory must be a non-symlink directory"))
	}
	root, err := homeRoot.OpenRoot(relative)
	homeRoot.Close()
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("open default export directory: %w", err))
	}
	return service.issueOpenedDirectory(filepath.Join(home, filepath.FromSlash(relative)), root, workspaceDefaultExportDirectory, true)
}

func (service *WorkspaceExports) CreateExportDirectory(ctx context.Context, request WorkspaceCreateExportDirectoryRequest) (WorkspaceExportDirectory, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceExportDirectory{}, workspaceError(err)
	}
	name, err := workspaceDirectoryName(request.Name)
	if err != nil {
		return WorkspaceExportDirectory{}, err
	}
	parent, err := service.directory(request.ParentToken)
	if err != nil {
		return WorkspaceExportDirectory{}, err
	}
	if err := parent.root.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("create export directory: %w", err))
	}
	info, err := parent.root.Lstat(name)
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return WorkspaceExportDirectory{}, workspaceError(errors.New("export directory must be a non-symlink directory"))
	}
	child, err := parent.root.OpenRoot(name)
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("open export directory: %w", err))
	}
	return service.issueOpenedDirectory(filepath.Join(parent.path, name), child, name, false)
}

func (service *WorkspaceExports) StartExport(ctx context.Context, request WorkspaceStartExportRequest) (WorkspaceExportJob, error) {
	if service == nil || service.application == nil {
		return WorkspaceExportJob{}, workspaceError(fmt.Errorf("start export: %w", ErrUnavailable))
	}
	directory, err := service.directory(request.DirectoryToken)
	if err != nil {
		return WorkspaceExportJob{}, err
	}
	relative, err := workspaceSafeRelativeDirectory(request.Subdirectory)
	if err != nil {
		return WorkspaceExportJob{}, err
	}
	outputRoot, err := service.openExportChildDirectory(directory, relative)
	if err != nil {
		return WorkspaceExportJob{}, workspaceError(err)
	}
	job, err := service.application.StartExport(ctx, domain.ExportRequest{
		Selection: request.Selection, Format: strings.TrimSpace(request.Format), Options: request.Options,
		OutputRoot: outputRoot.Root, OutputAuthorization: outputRoot,
	})
	if err != nil {
		return WorkspaceExportJob{}, workspaceError(err)
	}
	return WorkspaceExportJob{ID: job.ID, State: job.State, Format: strings.TrimSpace(request.Format), QueuedAt: job.CreatedAt,
		Directory: string(request.DirectoryToken)}, nil
}

func (service *WorkspaceExports) ExportRecords(ctx context.Context, request WorkspacePageRequest) (WorkspacePage[WorkspaceExportRecord], error) {
	page, err := request.normalize()
	if err != nil {
		return WorkspacePage[WorkspaceExportRecord]{}, err
	}
	if service == nil || service.library == nil {
		return WorkspacePage[WorkspaceExportRecord]{}, workspaceError(fmt.Errorf("list exports: %w", ErrUnavailable))
	}
	records, err := service.library.QueryExportRecords(ctx, page.Offset, page.Limit)
	if err != nil {
		return WorkspacePage[WorkspaceExportRecord]{}, workspaceError(err)
	}
	items := make([]WorkspaceExportRecord, 0, len(records.Items))
	for _, record := range records.Items {
		items = append(items, workspaceExportRecord(record))
	}
	return WorkspacePage[WorkspaceExportRecord]{Items: items, Total: records.Total, Offset: records.Offset, Limit: records.Limit}, nil
}

func (service *WorkspaceExports) ExportManifest(ctx context.Context, exportID string) (WorkspaceExportManifest, error) {
	record, files, err := service.exportRecordAndFiles(ctx, exportID)
	if err != nil {
		return WorkspaceExportManifest{}, err
	}
	return WorkspaceExportManifest{ExportID: string(record.ID), Format: record.Format, State: record.State,
		ProvenanceState: record.ProvenanceState, ProvenanceGeneration: record.ProvenanceGeneration,
		Files: workspaceExportFiles(files)}, nil
}

func (service *WorkspaceExports) VerifyExport(ctx context.Context, exportID string) (WorkspaceExportVerification, error) {
	record, _, err := service.exportRecordAndFiles(ctx, exportID)
	if err != nil {
		return WorkspaceExportVerification{}, err
	}
	if strings.TrimSpace(record.ProvenancePath) == "" || record.OutputAuthorization == nil {
		return WorkspaceExportVerification{}, workspaceError(errors.New("export provenance is not ready for verification"))
	}
	root, err := workspaceAuthorizedOutputRoot(record.OutputAuthorization)
	if err != nil {
		return WorkspaceExportVerification{}, workspaceError(err)
	}
	report, err := exporter.VerifyProvenanceManifest(ctx, root, record.ProvenancePath)
	if err != nil {
		return WorkspaceExportVerification{}, workspaceError(err)
	}
	affected := make([]string, len(report.AffectedArticleIDs))
	for index, id := range report.AffectedArticleIDs {
		affected[index] = string(id)
	}
	return WorkspaceExportVerification{ExportID: string(record.ID), Valid: report.Valid, VerifiedOutputs: report.VerifiedOutputs,
		Issues: append([]exporter.VerificationIssue(nil), report.Issues...), AffectedArticleIDs: affected}, nil
}

func (service *WorkspaceExports) DownloadArtifact(ctx context.Context, request WorkspaceDownloadArtifactRequest) (WorkspaceDownloadArtifact, error) {
	_, files, err := service.exportRecordAndFiles(ctx, request.ExportID)
	if err != nil {
		return WorkspaceDownloadArtifact{}, err
	}
	path, err := workspaceSafeArtifactPath(request.Path)
	if err != nil {
		return WorkspaceDownloadArtifact{}, err
	}
	for _, file := range files {
		if file.RelativePath != path {
			continue
		}
		artifact := WorkspaceDownloadArtifact{ExportID: string(file.ExportID), Path: file.RelativePath, Name: filepath.Base(file.RelativePath),
			SizeBytes: file.SizeBytes, SHA256: file.SHA256, MediaType: file.MediaType}
		return artifact, nil
	}
	return WorkspaceDownloadArtifact{}, workspaceError(fmt.Errorf("export artifact %q: %w", path, fs.ErrNotExist))
}

func (service *WorkspaceExports) exportRecordAndFiles(ctx context.Context, exportID string) (library.ExportRecord, []library.ExportFileRecord, error) {
	if service == nil || service.library == nil {
		return library.ExportRecord{}, nil, workspaceError(fmt.Errorf("read export: %w", ErrUnavailable))
	}
	id := domain.ExportID(strings.TrimSpace(exportID))
	if id == "" {
		return library.ExportRecord{}, nil, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export ID is required"}
	}
	record, err := service.library.GetExport(ctx, id)
	if err != nil {
		return library.ExportRecord{}, nil, workspaceError(err)
	}
	files, err := service.library.ListExportFiles(ctx, id)
	if err != nil {
		return library.ExportRecord{}, nil, workspaceError(err)
	}
	return record, files, nil
}

func workspaceExportFiles(files []library.ExportFileRecord) []WorkspaceExportFile {
	items := make([]WorkspaceExportFile, 0, len(files))
	for _, file := range files {
		item := WorkspaceExportFile{ArticleID: string(file.ArticleID), Path: file.RelativePath, SizeBytes: file.SizeBytes,
			SHA256: file.SHA256, MediaType: file.MediaType, Status: file.Status}
		items = append(items, item)
	}
	return items
}

func (service *WorkspaceExports) issueOpenedDirectory(path string, root *os.Root, label string, isDefault bool) (WorkspaceExportDirectory, error) {
	identity, err := root.Open(".")
	if err != nil {
		root.Close()
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("inspect export directory identity: %w", err))
	}
	device, inode, identityErr := workspaceExportRootIdentityFromFile(identity)
	closeErr := identity.Close()
	if err := errors.Join(identityErr, closeErr); err != nil {
		root.Close()
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("identify export directory: %w", err))
	}
	token, err := service.token("dir")
	if err != nil {
		root.Close()
		return WorkspaceExportDirectory{}, workspaceError(err)
	}
	createdAt := service.now().UTC()
	service.mu.Lock()
	service.directories[token] = workspaceExportDirectoryCapability{path: path, root: root, device: device, inode: inode,
		label: label, isDefault: isDefault, createdAt: createdAt}
	service.mu.Unlock()
	return WorkspaceExportDirectory{Token: token, Label: label, IsDefault: isDefault, CreatedAt: createdAt}, nil
}

func (service *WorkspaceExports) directory(token WorkspaceDirectoryHandle) (workspaceExportDirectoryCapability, error) {
	if strings.TrimSpace(string(token)) == "" {
		return workspaceExportDirectoryCapability{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export directory token is required"}
	}
	service.mu.Lock()
	directory, ok := service.directories[token]
	service.mu.Unlock()
	if !ok {
		return workspaceExportDirectoryCapability{}, &WorkspaceError{Code: WorkspaceErrorNotFound, Message: "export directory token was not found"}
	}
	return directory, nil
}

func (service *WorkspaceExports) token(prefix string) (WorkspaceDirectoryHandle, error) {
	buffer := make([]byte, 18)
	if _, err := service.random(buffer); err != nil {
		return "", fmt.Errorf("issue workspace capability: %w", err)
	}
	return WorkspaceDirectoryHandle(prefix + "_" + hex.EncodeToString(buffer)), nil
}

func workspaceExportRecord(record library.ExportRecord) WorkspaceExportRecord {
	var completed *time.Time
	if !record.CompletedAt.IsZero() {
		value := record.CompletedAt
		completed = &value
	}
	return WorkspaceExportRecord{ID: string(record.ID), JobID: record.JobID, Format: record.Format, State: record.State,
		CreatedAt: record.CreatedAt, CompletedAt: completed, ProvenanceState: record.ProvenanceState,
		ProvenanceGeneration: record.ProvenanceGeneration, OutputDirectory: "local export directory"}
}

func workspaceDirectoryName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) {
		return "", &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export directory name must be a single path component"}
	}
	return value, nil
}

func workspaceSafeRelativeDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." {
		return "", nil
	}
	if strings.Contains(value, `\`) {
		return "", &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export subdirectory must stay within its authorized directory"}
	}
	value = filepath.FromSlash(value)
	clean := filepath.Clean(value)
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" || clean != value || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export subdirectory must stay within its authorized directory"}
	}
	return filepath.ToSlash(clean), nil
}

func workspaceSafeArtifactPath(value string) (string, error) {
	path, err := workspaceSafeRelativeDirectory(value)
	if err != nil || path == "" {
		if err != nil {
			return "", err
		}
		return "", &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export artifact path is required"}
	}
	return path, nil
}

func workspaceAuthorizedOutputRoot(authorization *domain.ExportOutputAuthorization) (string, error) {
	if authorization == nil {
		return "", errors.New("export output authorization is unavailable")
	}
	relative, err := workspaceSafeRelativeDirectory(authorization.RelativePath)
	if err != nil {
		return "", err
	}
	if relative != "" {
		return "", errors.New("workspace export authorization must identify a concrete output directory")
	}
	info, err := os.Lstat(authorization.Root)
	if err != nil {
		return "", fmt.Errorf("inspect authorized export directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("authorized export directory changed type")
	}
	file, err := os.Open(authorization.Root)
	if err != nil {
		return "", fmt.Errorf("open authorized export directory: %w", err)
	}
	device, inode, identityErr := workspaceExportRootIdentityFromFile(file)
	closeErr := file.Close()
	if err := errors.Join(identityErr, closeErr); err != nil {
		return "", fmt.Errorf("identify authorized export directory: %w", err)
	}
	if device != authorization.Device || inode != authorization.Inode {
		return "", errors.New("authorized export directory was replaced")
	}
	return authorization.Root, nil
}

func (service *WorkspaceExports) openExportChildDirectory(directory workspaceExportDirectoryCapability, relative string) (*domain.ExportOutputAuthorization, error) {
	root := directory.root
	if root == nil {
		return nil, errors.New("export directory capability is unavailable")
	}
	target := root
	path := directory.path
	for _, component := range strings.Split(relative, "/") {
		if component == "" {
			continue
		}
		if err := target.Mkdir(component, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("create export subdirectory: %w", err)
		}
		info, err := target.Lstat(component)
		if err != nil {
			return nil, fmt.Errorf("inspect export subdirectory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return nil, errors.New("export subdirectory must be a non-symlink directory")
		}
		next, err := target.OpenRoot(component)
		if err != nil {
			return nil, fmt.Errorf("open export subdirectory: %w", err)
		}
		if target != root {
			_ = target.Close()
		}
		target = next
		path = filepath.Join(path, component)
	}
	identity, err := target.Open(".")
	if err != nil {
		if target != root {
			_ = target.Close()
		}
		return nil, fmt.Errorf("inspect export output identity: %w", err)
	}
	device, inode, identityErr := workspaceExportRootIdentityFromFile(identity)
	closeErr := identity.Close()
	if target != root {
		_ = target.Close()
	}
	if err := errors.Join(identityErr, closeErr); err != nil {
		return nil, fmt.Errorf("identify export output: %w", err)
	}
	return &domain.ExportOutputAuthorization{Root: path, Device: device, Inode: inode}, nil
}

var _ WorkspaceExportService = (*WorkspaceExports)(nil)
