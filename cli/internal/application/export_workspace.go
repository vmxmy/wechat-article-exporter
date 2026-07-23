package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

const workspaceConfiguredExportDirectoryLabel = "configured export directory"

const (
	workspaceExportOptionContent            = "content"
	workspaceExportOptionMetadata           = "metadata"
	workspaceExportOptionComments           = "comments"
	workspaceExportOptionHTMLResourcePolicy = "htmlResourcePolicy"
	workspaceExportOptionHTMLBatchArchive   = "htmlBatchArchive"
)

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
	OpenExportOutput(context.Context, string) error
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
	ArtifactID string `json:"artifactId"`
	ArticleID  string `json:"articleId,omitempty"`
	Path       string `json:"path"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA256     string `json:"sha256"`
	MediaType  string `json:"mediaType,omitempty"`
	Status     string `json:"status"`
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
	ExportID   string `json:"exportId"`
	ArtifactID string `json:"artifactId"`
}

// WorkspaceDownloadArtifact describes a verified file that a local adapter
// may stream. It intentionally contains no absolute filename.
type WorkspaceDownloadArtifact struct {
	ExportID  string        `json:"exportId"`
	Path      string        `json:"path"`
	Name      string        `json:"name"`
	SizeBytes int64         `json:"sizeBytes"`
	SHA256    string        `json:"sha256"`
	MediaType string        `json:"mediaType,omitempty"`
	Reader    io.ReadCloser `json:"-"`
}

type workspaceArtifactCapability struct {
	exportID domain.ExportID
	path     string
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

// WorkspaceExportRootProvider is a narrow, trusted presentation-adapter seam
// for the active profile's configured export root. It must never be exposed to
// browser callers: WorkspaceExports turns the resulting directory into an
// opaque capability before returning it to an adapter.
type WorkspaceExportRootProvider func(context.Context) (string, error)

// WorkspaceExportsOptions configures trusted local integration seams. A nil
// ConfiguredRoot preserves the Downloads fallback.
type WorkspaceExportsOptions struct {
	ConfiguredRoot WorkspaceExportRootProvider
}

// WorkspaceExports owns ephemeral directory and download capabilities while
// delegating durable export work to Application and its local library seam.
type WorkspaceExports struct {
	application    Application
	library        workspaceExportLibrary
	directories    map[WorkspaceDirectoryHandle]workspaceExportDirectoryCapability
	artifacts      map[string]workspaceArtifactCapability
	mu             sync.Mutex
	now            func() time.Time
	home           func() (string, error)
	random         func([]byte) (int, error)
	openOutput     func(context.Context, string) error
	configuredRoot WorkspaceExportRootProvider
}

type workspaceExportLibrary interface {
	QueryExportRecords(context.Context, int, int) (domain.Page[library.ExportRecord], error)
	GetExport(context.Context, domain.ExportID) (library.ExportRecord, error)
	ListExportFiles(context.Context, domain.ExportID) ([]library.ExportFileRecord, error)
}

func NewWorkspaceExports(application Application, records workspaceExportLibrary, options ...WorkspaceExportsOptions) *WorkspaceExports {
	service := &WorkspaceExports{
		application: application,
		library:     records,
		directories: make(map[WorkspaceDirectoryHandle]workspaceExportDirectoryCapability),
		artifacts:   make(map[string]workspaceArtifactCapability),
		now:         time.Now,
		home:        os.UserHomeDir,
		random:      rand.Read,
		openOutput:  workspaceOpenOutput,
	}
	if len(options) > 0 {
		service.configuredRoot = options[0].ConfiguredRoot
	}
	return service
}

func (service *WorkspaceExports) DefaultExportDirectory(ctx context.Context) (WorkspaceExportDirectory, error) {
	if err := ctx.Err(); err != nil {
		return WorkspaceExportDirectory{}, workspaceError(err)
	}
	if service.configuredRoot != nil {
		configured, err := service.configuredRoot(ctx)
		if err != nil {
			return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("resolve configured export directory: %w", err))
		}
		if strings.TrimSpace(configured) != "" {
			return service.openConfiguredExportDirectory(configured)
		}
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

func (service *WorkspaceExports) openConfiguredExportDirectory(configured string) (WorkspaceExportDirectory, error) {
	rootPath, err := service.workspaceExportRootPath(configured)
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(err)
	}
	root, err := workspaceOpenRootNoSymlink(rootPath, true)
	if err != nil {
		return WorkspaceExportDirectory{}, workspaceError(fmt.Errorf("open configured export directory: %w", err))
	}
	return service.issueOpenedDirectory(rootPath, root, workspaceConfiguredExportDirectoryLabel, true)
}

func (service *WorkspaceExports) workspaceExportRootPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("configured export directory is empty")
	}
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := service.home()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for configured export: %w", err)
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, value[2:])
		}
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve configured export directory: %w", err)
	}
	return workspaceCanonicalSystemDirectory(filepath.Clean(root)), nil
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
	if err := service.validateExportDirectory(parent); err != nil {
		return WorkspaceExportDirectory{}, workspaceError(err)
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
	format, options, err := workspaceExportOptions(request.Format, request.Options)
	if err != nil {
		return WorkspaceExportJob{}, err
	}
	directory, err := service.directory(request.DirectoryToken)
	if err != nil {
		return WorkspaceExportJob{}, err
	}
	if err := service.validateExportDirectory(directory); err != nil {
		return WorkspaceExportJob{}, workspaceError(err)
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
		Selection: request.Selection, Format: format, Options: options,
		OutputRoot: outputRoot.Root, OutputAuthorization: outputRoot,
	})
	if err != nil {
		return WorkspaceExportJob{}, workspaceError(err)
	}
	return WorkspaceExportJob{ID: job.ID, State: job.State, Format: format, QueuedAt: job.CreatedAt,
		Directory: string(request.DirectoryToken)}, nil
}

// workspaceExportOptions narrows the untyped domain option map at the local
// browser boundary. It preserves the existing application/CLI option contract
// while preventing browser callers from smuggling future, internal, or
// cross-format settings into durable jobs.
func workspaceExportOptions(format string, options domain.ExportOptions) (string, domain.ExportOptions, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if !workspaceExportFormat(format) {
		return "", domain.ExportOptions{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export format is unsupported"}
	}
	if options.FormatOptions == nil {
		return format, options, nil
	}
	allowed := map[string]struct{}{
		workspaceExportOptionContent:  {},
		workspaceExportOptionMetadata: {},
		workspaceExportOptionComments: {},
	}
	if format == "html" {
		allowed[workspaceExportOptionHTMLResourcePolicy] = struct{}{}
		allowed[workspaceExportOptionHTMLBatchArchive] = struct{}{}
	}
	normalized := make(map[string]any, len(options.FormatOptions))
	for key, value := range options.FormatOptions {
		if _, ok := allowed[key]; !ok {
			return "", domain.ExportOptions{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export option is unsupported for this format"}
		}
		if !workspaceExportOptionValueValid(key, value) {
			return "", domain.ExportOptions{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export option value is invalid"}
		}
		normalized[key] = value
	}
	options.FormatOptions = normalized
	return format, options, nil
}

func workspaceExportFormat(format string) bool {
	switch format {
	case "html", "markdown", "text", "json", "xlsx", "docx", "pdf":
		return true
	default:
		return false
	}
}

func workspaceExportOptionValueValid(key string, value any) bool {
	switch key {
	case workspaceExportOptionContent, workspaceExportOptionMetadata, workspaceExportOptionComments:
		_, ok := value.(bool)
		return ok
	case workspaceExportOptionHTMLResourcePolicy:
		policy, ok := value.(string)
		return ok && (policy == "best-effort" || policy == "strict")
	case workspaceExportOptionHTMLBatchArchive:
		name, ok := value.(string)
		return ok && workspaceExportArchiveName(name)
	default:
		return false
	}
}

func workspaceExportArchiveName(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`) && strings.EqualFold(filepath.Ext(value), ".zip")
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
	workspaceFiles, err := service.workspaceExportFiles(files)
	if err != nil {
		return WorkspaceExportManifest{}, workspaceError(err)
	}
	return WorkspaceExportManifest{ExportID: string(record.ID), Format: record.Format, State: record.State,
		ProvenanceState: record.ProvenanceState, ProvenanceGeneration: record.ProvenanceGeneration, Files: workspaceFiles}, nil
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
	if err := ctx.Err(); err != nil {
		return WorkspaceDownloadArtifact{}, workspaceError(err)
	}
	capability, err := service.artifact(request.ExportID, request.ArtifactID)
	if err != nil {
		return WorkspaceDownloadArtifact{}, err
	}
	record, files, err := service.exportRecordAndFiles(ctx, string(capability.exportID))
	if err != nil {
		return WorkspaceDownloadArtifact{}, err
	}
	for _, file := range files {
		if file.RelativePath != capability.path {
			continue
		}
		reader, err := workspaceOpenExportArtifact(record.OutputAuthorization, file)
		if err != nil {
			return WorkspaceDownloadArtifact{}, workspaceError(err)
		}
		artifact := WorkspaceDownloadArtifact{ExportID: string(file.ExportID), Path: file.RelativePath, Name: filepath.Base(file.RelativePath),
			SizeBytes: file.SizeBytes, SHA256: file.SHA256, MediaType: file.MediaType, Reader: reader}
		return artifact, nil
	}
	return WorkspaceDownloadArtifact{}, workspaceError(fmt.Errorf("export artifact %q: %w", capability.path, fs.ErrNotExist))
}

// OpenExportOutput opens only the output directory authenticated by the export
// record. The host path never crosses the facade boundary.
func (service *WorkspaceExports) OpenExportOutput(ctx context.Context, exportID string) error {
	if err := ctx.Err(); err != nil {
		return workspaceError(err)
	}
	record, _, err := service.exportRecordAndFiles(ctx, exportID)
	if err != nil {
		return err
	}
	root, err := workspaceAuthorizedOutputRoot(record.OutputAuthorization)
	if err != nil {
		return workspaceError(err)
	}
	if service.openOutput == nil {
		return workspaceError(fmt.Errorf("open export output: %w", ErrUnavailable))
	}
	return workspaceError(service.openOutput(ctx, root))
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

func (service *WorkspaceExports) workspaceExportFiles(files []library.ExportFileRecord) ([]WorkspaceExportFile, error) {
	items := make([]WorkspaceExportFile, 0, len(files))
	for _, file := range files {
		artifactID, err := service.issueArtifact(file.ExportID, file.RelativePath)
		if err != nil {
			return nil, err
		}
		item := WorkspaceExportFile{ArtifactID: artifactID, ArticleID: string(file.ArticleID), Path: file.RelativePath, SizeBytes: file.SizeBytes,
			SHA256: file.SHA256, MediaType: file.MediaType, Status: file.Status}
		items = append(items, item)
	}
	return items, nil
}

func (service *WorkspaceExports) issueArtifact(exportID domain.ExportID, path string) (string, error) {
	path, err := workspaceSafeArtifactPath(path)
	if err != nil {
		return "", err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	for id, capability := range service.artifacts {
		if capability.exportID == exportID && capability.path == path {
			return id, nil
		}
	}
	buffer := make([]byte, 18)
	if _, err := service.random(buffer); err != nil {
		return "", fmt.Errorf("issue artifact capability: %w", err)
	}
	id := "artifact_" + hex.EncodeToString(buffer)
	service.artifacts[id] = workspaceArtifactCapability{exportID: exportID, path: path}
	return id, nil
}

func (service *WorkspaceExports) artifact(exportID, artifactID string) (workspaceArtifactCapability, error) {
	if strings.TrimSpace(exportID) == "" || strings.TrimSpace(artifactID) == "" {
		return workspaceArtifactCapability{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "export and artifact identifiers are required"}
	}
	service.mu.Lock()
	capability, ok := service.artifacts[artifactID]
	service.mu.Unlock()
	if !ok || capability.exportID != domain.ExportID(exportID) {
		return workspaceArtifactCapability{}, &WorkspaceError{Code: WorkspaceErrorNotFound, Message: "export artifact was not found"}
	}
	return capability, nil
}

func (service *WorkspaceExports) issueOpenedDirectory(path string, root *os.Root, label string, isDefault bool) (WorkspaceExportDirectory, error) {
	device, inode, err := workspaceOpenedExportRootIdentity(root)
	if err != nil {
		root.Close()
		return WorkspaceExportDirectory{}, workspaceError(err)
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

func (service *WorkspaceExports) validateExportDirectory(directory workspaceExportDirectoryCapability) error {
	if directory.root == nil || strings.TrimSpace(directory.path) == "" {
		return errors.New("export directory capability is unavailable")
	}
	rootPath, err := service.workspaceExportRootPath(directory.path)
	if err != nil {
		return err
	}
	root, err := workspaceOpenRootNoSymlink(rootPath, false)
	if err != nil {
		return fmt.Errorf("open authorized export directory: %w", err)
	}
	device, inode, identityErr := workspaceOpenedExportRootIdentity(root)
	closeErr := root.Close()
	if err := errors.Join(identityErr, closeErr); err != nil {
		return err
	}
	if device != directory.device || inode != directory.inode {
		return errors.New("authorized export directory was replaced")
	}
	return nil
}

// workspaceCanonicalSystemDirectory avoids treating macOS's fixed /var, /tmp,
// and /etc compatibility links as user-selected symlink ancestors. Every
// component below those canonical system directories is still checked without
// following links by workspaceOpenRootNoSymlink.
func workspaceCanonicalSystemDirectory(path string) string {
	if filepath.Separator != '/' {
		return path
	}
	for _, alias := range []string{"/var", "/tmp", "/etc"} {
		if path != alias && !strings.HasPrefix(path, alias+"/") {
			continue
		}
		info, err := os.Lstat(alias)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return path
		}
		canonical, err := filepath.EvalSymlinks(alias)
		if err != nil {
			return path
		}
		return filepath.Join(canonical, strings.TrimPrefix(path, alias+"/"))
	}
	return path
}

// workspaceOpenRootNoSymlink opens an absolute directory from its filesystem
// volume root, validating every component before descending into it. This
// keeps a configured root from inheriting a symlinked ancestor and returns the
// already-validated descriptor used for capability issuance.
func workspaceOpenRootNoSymlink(path string, create bool) (*os.Root, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("export directory must be absolute")
	}
	volumeRoot := filepath.VolumeName(path) + string(filepath.Separator)
	relative, err := filepath.Rel(volumeRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, errors.New("export directory must remain within its filesystem volume")
	}
	current, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, err
	}
	if relative == "." {
		return current, nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if create {
			if err := current.Mkdir(component, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				_ = current.Close()
				return nil, fmt.Errorf("create export directory component: %w", err)
			}
		}
		info, err := current.Lstat(component)
		if err != nil {
			_ = current.Close()
			return nil, fmt.Errorf("inspect export directory component: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			_ = current.Close()
			return nil, errors.New("export directory component must be a non-symlink directory")
		}
		next, err := current.OpenRoot(component)
		_ = current.Close()
		if err != nil {
			return nil, fmt.Errorf("open export directory component: %w", err)
		}
		current = next
	}
	return current, nil
}

func workspaceOpenedExportRootIdentity(root *os.Root) (uint64, uint64, error) {
	identity, err := root.Open(".")
	if err != nil {
		return 0, 0, fmt.Errorf("inspect export directory identity: %w", err)
	}
	device, inode, identityErr := workspaceExportRootIdentityFromFile(identity)
	closeErr := identity.Close()
	if err := errors.Join(identityErr, closeErr); err != nil {
		return 0, 0, fmt.Errorf("identify export directory: %w", err)
	}
	return device, inode, nil
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
