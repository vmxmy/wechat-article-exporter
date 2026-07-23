package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	runtimeenv "github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
)

type localExportRuntime struct {
	version    string
	profile    domain.ProfileID
	library    *library.Database
	objects    *objects.FileStore
	store      *library.JobStore
	recordFile func(context.Context, library.ExportFileRecord) error
	publish    func(jobs.CheckpointFunc, exportOutputIntent) error
	now        func() time.Time
	browser    runtimeenv.BrowserDiscovery
	runner     exporter.ProcessRunner
	scheduler  *jobs.Scheduler
	admit      func(context.Context, func(context.Context) error) error
}

type exportJobItem struct {
	Version             int                               `json:"version"`
	ProducerVersion     string                            `json:"producerVersion,omitempty"`
	ExportID            domain.ExportID                   `json:"exportId"`
	ArticleID           domain.ArticleID                  `json:"articleId"`
	ArticleIDs          []domain.ArticleID                `json:"articleIds,omitempty"`
	Directories         []string                          `json:"directories,omitempty"`
	SourceDigest        string                            `json:"sourceDigest,omitempty"`
	SourceDigests       map[domain.ArticleID]string       `json:"sourceDigests,omitempty"`
	SnapshotDigest      string                            `json:"snapshotDigest,omitempty"`
	SnapshotDigests     map[domain.ArticleID]string       `json:"snapshotDigests,omitempty"`
	PinnedDigests       []string                          `json:"pinnedDigests,omitempty"`
	Format              string                            `json:"format"`
	Output              string                            `json:"output"`
	OutputRelativePath  string                            `json:"outputRelativePath,omitempty"`
	Options             domain.ExportOptions              `json:"options"`
	Selection           exporter.SelectionManifest        `json:"selection"`
	OutputAuthorization *domain.ExportOutputAuthorization `json:"outputAuthorization,omitempty"`
}

type exportInputSnapshot struct {
	Version    int                 `json:"version"`
	ArticleID  domain.ArticleID    `json:"articleId"`
	Article    domain.Article      `json:"article"`
	HTMLDigest string              `json:"htmlDigest"`
	Comments   []processor.Comment `json:"comments"`
	Resources  []exportResourceRef `json:"resources"`
}

type exportResourceRef struct {
	URL       string `json:"url"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Role      string `json:"role,omitempty"`
	Ordinal   int    `json:"ordinal,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Status    string `json:"status"`
}

type capturedExportSnapshot struct {
	Digest string
	Pinned []string
	Object objects.Object
}

var errLegacyProvenanceUnavailable = errors.New("legacy export provenance is unavailable")

func newLocalExportRuntime(
	runtime *ProfileRuntime,
	clock runtimeenv.Clock,
	browser runtimeenv.BrowserDiscovery,
	options ...any,
) *localExportRuntime {
	if runtime == nil || runtime.Library == nil || runtime.Objects == nil || runtime.Jobs == nil {
		return nil
	}
	now := time.Now
	if clock != nil {
		now = clock.Now
	}
	var pdfRunner exporter.ProcessRunner
	var scheduler *jobs.Scheduler
	for _, option := range options {
		switch value := option.(type) {
		case exporter.ProcessRunner:
			pdfRunner = value
		case *jobs.Scheduler:
			scheduler = value
		}
	}
	return &localExportRuntime{
		version: Version, profile: runtime.Profile.ID, library: runtime.Library, objects: runtime.Objects,
		store: runtime.Jobs, recordFile: runtime.Library.UpsertExportFile,
		publish: checkpointExportOutputIntent,
		now:     now, browser: browser, runner: pdfRunner, scheduler: scheduler,
	}
}

func (runtime *localExportRuntime) Start(ctx context.Context, request domain.ExportRequest) (domain.Job, error) {
	if runtime == nil || runtime.library == nil || runtime.store == nil {
		return domain.Job{}, fmt.Errorf("export runtime: %w", application.ErrUnavailable)
	}
	if runtime.admit == nil {
		return runtime.startAdmitted(ctx, request)
	}
	var job domain.Job
	err := runtime.admit(ctx, func(admittedCtx context.Context) error {
		var startErr error
		job, startErr = runtime.startAdmitted(admittedCtx, request)
		return startErr
	})
	return job, err
}

func (runtime *localExportRuntime) startAdmitted(ctx context.Context, request domain.ExportRequest) (domain.Job, error) {
	if strings.TrimSpace(request.OutputRoot) == "" {
		return domain.Job{}, errors.New("export output root is required")
	}
	if request.OutputAuthorization == nil {
		authorization, err := authorizeExportOutputRoot(request.OutputRoot)
		if err != nil {
			return domain.Job{}, err
		}
		request.OutputAuthorization = authorization
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if !supportedLocalExportFormat(format) {
		return domain.Job{}, fmt.Errorf("unsupported export format %q", request.Format)
	}
	manifest, err := exporter.BuildSelectionManifest(ctx, runtime.library, request, runtime.now())
	if err != nil {
		return domain.Job{}, err
	}
	articles := make([]domain.Article, 0, len(manifest.ArticleIDs))
	sourceDigests := make(map[domain.ArticleID]string, len(manifest.ArticleIDs))
	snapshotDigests := make(map[domain.ArticleID]string, len(manifest.ArticleIDs))
	articlePinnedDigests := make(map[domain.ArticleID][]string, len(manifest.ArticleIDs))
	registeredSnapshots := make([]library.RegisteredJobObject, 0, len(manifest.ArticleIDs))
	pinnedDigests := make([]string, 0, len(manifest.ArticleIDs)*2)
	for _, articleID := range manifest.ArticleIDs {
		snapshot, err := runtime.library.ReadExportSnapshot(ctx, articleID)
		if err != nil {
			return domain.Job{}, fmt.Errorf("load selected article %s export snapshot: %w", articleID, err)
		}
		articles = append(articles, snapshot.Article)
		if _, err := runtime.objects.Stat(snapshot.Content.ObjectDigest); err != nil {
			return domain.Job{}, fmt.Errorf("inspect article %s HTML object: %w", articleID, err)
		}
		sourceDigests[articleID] = snapshot.Content.ObjectDigest
		captured, err := runtime.captureExportSnapshot(ctx, snapshot)
		if err != nil {
			return domain.Job{}, fmt.Errorf("capture article %s export snapshot: %w", articleID, err)
		}
		snapshotDigests[articleID] = captured.Digest
		articlePinnedDigests[articleID] = captured.Pinned
		pinnedDigests = append(pinnedDigests, captured.Pinned...)
		registeredSnapshots = append(registeredSnapshots, library.RegisteredJobObject{
			Object: captured.Object, CreatedAt: runtime.now(),
		})
	}
	pinnedDigests = uniqueSortedStrings(pinnedDigests)
	names, err := planExportNames(format, request.Options, articles)
	if err != nil {
		return domain.Job{}, err
	}
	exportID := domain.ExportID(uuid.NewString())
	batchArchive := strings.TrimSpace(optionString(request.Options.FormatOptions, "htmlBatchArchive", ""))
	if batchArchive != "" {
		if format != "html" {
			return domain.Job{}, errors.New("HTML batch archive is only valid with the html format")
		}
		if err := validateHTMLBatchArchiveName(batchArchive); err != nil {
			return domain.Job{}, err
		}
	}
	if _, err := htmlResourcePolicy(request.Options.FormatOptions); err != nil {
		return domain.Job{}, err
	}
	items := make([]string, 0, len(articles))
	if batchArchive != "" {
		directories := make([]string, len(names))
		articleIDs := make([]domain.ArticleID, len(articles))
		for index := range articles {
			directories[index] = names[index].Path
			articleIDs[index] = articles[index].ID
		}
		envelope := exportJobItem{
			Version: 4, ProducerVersion: runtime.version, ExportID: exportID, ArticleIDs: articleIDs, Directories: directories,
			SourceDigests: sourceDigests, SnapshotDigests: snapshotDigests, PinnedDigests: pinnedDigests, Format: format,
			Output: filepath.Join(request.OutputRoot, batchArchive), OutputRelativePath: batchArchive,
			Options: request.Options, Selection: manifest,
			OutputAuthorization: request.OutputAuthorization,
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return domain.Job{}, fmt.Errorf("encode export job item: %w", err)
		}
		items = append(items, string(encoded))
	} else {
		for index, article := range articles {
			envelope := exportJobItem{
				Version: 4, ProducerVersion: runtime.version, ExportID: exportID, ArticleID: article.ID,
				SourceDigest: sourceDigests[article.ID], SnapshotDigest: snapshotDigests[article.ID],
				PinnedDigests: append([]string(nil), articlePinnedDigests[article.ID]...), Format: format,
				Output: filepath.Join(request.OutputRoot, names[index].Path), OutputRelativePath: names[index].Path,
				Options: request.Options, Selection: manifest,
				OutputAuthorization: request.OutputAuthorization,
			}
			encoded, err := json.Marshal(envelope)
			if err != nil {
				return domain.Job{}, fmt.Errorf("encode export job item: %w", err)
			}
			items = append(items, string(encoded))
		}
	}
	job, err := runtime.store.CreateWithItemsAndObjects(ctx, jobs.Spec{
		Kind: "export", Profile: runtime.profile,
		Payload: map[string]any{"exportId": exportID, "format": format, "outputRoot": request.OutputRoot, "selection": manifest},
	}, items, registeredSnapshots)
	if err != nil {
		return domain.Job{}, err
	}
	if err := runtime.library.UpsertExport(ctx, library.ExportRecord{
		ID: exportID, JobID: job.ID, Format: format, Manifest: manifest, OutputRoot: request.OutputRoot,
		State: string(domain.JobQueued), CreatedAt: runtime.now(), OutputAuthorization: request.OutputAuthorization,
	}); err != nil {
		_, _ = runtime.store.Cancel(context.Background(), job.ID)
		return domain.Job{}, err
	}
	return job, nil
}

func (runtime *localExportRuntime) Run(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if runtime == nil || runtime.store == nil {
		return domain.Job{}, fmt.Errorf("export runtime: %w", application.ErrUnavailable)
	}
	record, err := runtime.exportRecordForJob(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	generation := record.ProvenanceGeneration
	engine, err := jobs.NewEngine(runtime.store, jobs.EngineOptions{
		Owner: "local-export-worker", Scheduler: runtime.scheduler,
		Metadata: func(jobs.Item) jobs.WorkMetadata {
			return jobs.WorkMetadata{Operation: "export", Host: "local"}
		},
	})
	if err != nil {
		return domain.Job{}, err
	}
	job, runErr := engine.Run(ctx, id, runtime.execute)
	if job.ID == "" {
		return job, runErr
	}
	background := context.Background()
	updateErr := runtime.library.UpdateExportStateByJob(background, id, generation, string(job.State), runtime.now())
	if updateErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("update export state: %w", updateErr))
	}
	if updateErr == nil && exportTerminalState(job.State) {
		if manifestErr := runtime.finalizeProvenanceGeneration(background, job, generation); manifestErr != nil {
			if job.State == domain.JobCompleted || job.State == domain.JobPartial {
				runErr = errors.Join(runErr, fmt.Errorf("finalize export provenance: %w", manifestErr))
			} else if logErr := runtime.store.AppendLog(background, job.ID, "", "warning",
				"export provenance was not written after unsuccessful completion", map[string]any{"error": manifestErr.Error()}); logErr != nil {
				runErr = errors.Join(runErr, fmt.Errorf("record provenance warning: %w", logErr))
			}
		}
	}
	return job, runErr
}

func exportTerminalState(state domain.JobState) bool {
	switch state {
	case domain.JobCompleted, domain.JobPartial, domain.JobFailed, domain.JobCancelled:
		return true
	default:
		return false
	}
}

func exportCompletionStatus(state domain.JobState) (exporter.ExportCompletionStatus, error) {
	switch state {
	case domain.JobCompleted:
		return exporter.ExportCompleted, nil
	case domain.JobPartial:
		return exporter.ExportPartial, nil
	case domain.JobFailed:
		return exporter.ExportFailed, nil
	case domain.JobCancelled:
		return exporter.ExportCancelled, nil
	default:
		return "", fmt.Errorf("job state %q is not terminal", state)
	}
}

func (runtime *localExportRuntime) finalizeProvenance(ctx context.Context, job domain.Job) (resultErr error) {
	record, err := runtime.exportRecordForJob(ctx, job.ID)
	if err != nil {
		return err
	}
	return runtime.finalizeProvenanceRecord(ctx, job, record, record.ProvenanceGeneration)
}

func (runtime *localExportRuntime) finalizeProvenanceGeneration(
	ctx context.Context,
	job domain.Job,
	expectedGeneration int64,
) (resultErr error) {
	record, err := runtime.exportRecordForJob(ctx, job.ID)
	if err != nil {
		return err
	}
	if record.ProvenanceGeneration != expectedGeneration {
		return jobs.ErrStateChanged
	}
	return runtime.finalizeProvenanceRecord(ctx, job, record, expectedGeneration)
}

func (runtime *localExportRuntime) finalizeProvenanceRecord(
	ctx context.Context,
	job domain.Job,
	record library.ExportRecord,
	expectedGeneration int64,
) (resultErr error) {
	if record.ProvenanceState == "ready" {
		return nil
	}
	generation, claimed, err := runtime.library.ClaimExportProvenance(ctx, record.ID, expectedGeneration,
		runtime.now().Add(-5*time.Minute))
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	succeeded := false
	defer func() {
		if !succeeded {
			message := "provenance finalization did not complete"
			if resultErr != nil {
				message = resultErr.Error()
			}
			markFailure := runtime.library.FailExportProvenance
			if errors.Is(resultErr, errLegacyProvenanceUnavailable) {
				markFailure = runtime.library.MarkExportProvenanceUnavailable
			}
			if failErr := markFailure(context.Background(), record.ID, generation, message); failErr != nil &&
				!errors.Is(failErr, jobs.ErrStateChanged) {
				resultErr = errors.Join(resultErr, failErr)
			}
		}
	}()
	selection, err := decodeSelectionManifest(record.Manifest)
	if err != nil {
		return err
	}
	status, err := exportCompletionStatus(job.State)
	if err != nil {
		return err
	}
	items, err := runtime.store.ListItems(ctx, job.ID)
	if err != nil {
		return err
	}
	producerVersion, sourceDigests, snapshotDigests, reconstructed, err := exportProvenanceInputs(
		items, selection, record.ID, record.Format,
	)
	if err != nil {
		return err
	}
	if producerVersion == "" {
		return fmt.Errorf("%w: producer version is missing and cannot be reconstructed without inventing metadata",
			errLegacyProvenanceUnavailable)
	}
	builder := exporter.NewProvenanceBuilder(producerVersion, record.ID, record.Format, selection, record.CreatedAt)
	for _, articleID := range selection.ArticleIDs {
		digest := sourceDigests[articleID]
		if digest == "" {
			return fmt.Errorf("%w: source digest for article %s is missing and cannot be reconstructed from current content",
				errLegacyProvenanceUnavailable, articleID)
		}
		if err := builder.AddSource(exporter.SourceArticle{
			ArticleID: articleID, SHA256: digest, SnapshotSHA256: snapshotDigests[articleID],
		}); err != nil {
			return err
		}
	}
	if reconstructed {
		builder.Warn("source_digest_reconstructed", "source digest was reconstructed for an export created by an older job-item version")
	}
	files, err := runtime.library.ListExportFiles(ctx, record.ID)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.RelativePath == exportProvenancePath(record.ID) {
			continue
		}
		output := exporter.OutputFile{
			ArticleID: file.ArticleID, Path: file.RelativePath, Size: file.SizeBytes,
			SHA256: file.SHA256, Status: exporter.OutputStatus(file.Status),
		}
		if file.ArticleID == "" && len(files) == 1 && len(selection.ArticleIDs) > 1 {
			output.ArticleIDs = append([]domain.ArticleID(nil), selection.ArticleIDs...)
		}
		if err := builder.AddOutput(output); err != nil {
			return err
		}
	}
	logs, err := runtime.store.ListAllLogs(ctx, job.ID)
	if err != nil {
		return err
	}
	for _, log := range logs {
		if log.Fields["kind"] == "missing_resource" {
			articleID, _ := log.Fields["articleId"].(string)
			resource, _ := log.Fields["resource"].(string)
			reason, _ := log.Fields["reason"].(string)
			if strings.TrimSpace(resource) != "" {
				builder.AddMissingResource(exporter.MissingResource{
					ArticleID: domain.ArticleID(articleID), Resource: resource, Reason: reason,
				})
			}
		}
		if log.Level == "warning" || log.Level == "error" {
			builder.Warn("job_"+log.Level, log.Message)
		}
	}
	completedAt := record.CompletedAt
	if completedAt.IsZero() {
		completedAt = runtime.now()
	}
	manifest, err := builder.Complete(status, completedAt)
	if err != nil {
		return err
	}
	if job.State == domain.JobFailed || job.State == domain.JobCancelled {
		if err := runtime.library.CompleteExportProvenance(ctx, record.ID, generation, manifest, "", ""); err != nil {
			return err
		}
		succeeded = true
		return nil
	}
	manager, err := exportOutputManager(record.OutputAuthorization, record.OutputRoot)
	if err != nil {
		return err
	}
	defer manager.Close()
	manifestPath := exportProvenanceGenerationPath(record.ID, generation)
	manifestOutput, err := exporter.WriteProvenanceManifest(ctx, manager, manifestPath, manifest, exporter.CollisionReplace)
	if err != nil {
		return err
	}
	if err := runtime.library.CompleteExportProvenance(ctx, record.ID, generation, manifest, manifestOutput.Path, manifestOutput.SHA256); err != nil {
		return err
	}
	succeeded = true
	return nil
}

func exportProvenanceInputs(
	items []library.JobItem,
	selection exporter.SelectionManifest,
	exportID domain.ExportID,
	format string,
) (string, map[domain.ArticleID]string, map[domain.ArticleID]string, bool, error) {
	if err := exporter.ValidateSelectionManifest(selection); err != nil {
		return "", nil, nil, false, err
	}
	if exportID == "" || format == "" || selection.Format != format {
		return "", nil, nil, false, errors.New("export record identity does not match the stored selection")
	}
	selectionSet := make(map[domain.ArticleID]struct{}, len(selection.ArticleIDs))
	for _, articleID := range selection.ArticleIDs {
		selectionSet[articleID] = struct{}{}
	}
	producerVersion := ""
	digests := make(map[domain.ArticleID]string, len(selection.ArticleIDs))
	snapshots := make(map[domain.ArticleID]string, len(selection.ArticleIDs))
	reconstructed := false
	for _, item := range items {
		var envelope exportJobItem
		if err := json.Unmarshal([]byte(item.Key), &envelope); err != nil {
			return "", nil, nil, false, fmt.Errorf("decode export item for provenance: %w", err)
		}
		if envelope.ExportID != exportID || envelope.Format != format ||
			envelope.Selection.ID != selection.ID || envelope.Selection.DigestSHA256 != selection.DigestSHA256 {
			return "", nil, nil, false, errors.New("export item identity does not match the stored selection")
		}
		if err := exporter.ValidateSelectionManifest(envelope.Selection); err != nil {
			return "", nil, nil, false, fmt.Errorf("validate export item selection: %w", err)
		}
		if envelope.ProducerVersion != "" {
			if producerVersion != "" && producerVersion != envelope.ProducerVersion {
				return "", nil, nil, false, errors.New("export items have conflicting producer versions")
			}
			producerVersion = envelope.ProducerVersion
		} else {
			reconstructed = true
		}
		if envelope.ArticleID != "" && envelope.SourceDigest != "" {
			if _, selected := selectionSet[envelope.ArticleID]; !selected {
				return "", nil, nil, false, fmt.Errorf("export item article %s is outside the selection", envelope.ArticleID)
			}
			if existing := digests[envelope.ArticleID]; existing != "" && existing != envelope.SourceDigest {
				return "", nil, nil, false, fmt.Errorf("export items have conflicting source digests for article %s", envelope.ArticleID)
			}
			digests[envelope.ArticleID] = envelope.SourceDigest
		}
		for articleID, digest := range envelope.SourceDigests {
			if _, selected := selectionSet[articleID]; !selected {
				return "", nil, nil, false, fmt.Errorf("export item article %s is outside the selection", articleID)
			}
			if existing := digests[articleID]; existing != "" && existing != digest {
				return "", nil, nil, false, fmt.Errorf("export items have conflicting source digests for article %s", articleID)
			}
			digests[articleID] = digest
		}
		if envelope.ArticleID != "" && envelope.SnapshotDigest != "" {
			if existing := snapshots[envelope.ArticleID]; existing != "" && existing != envelope.SnapshotDigest {
				return "", nil, nil, false, fmt.Errorf("export items have conflicting snapshot digests for article %s", envelope.ArticleID)
			}
			snapshots[envelope.ArticleID] = envelope.SnapshotDigest
		}
		for articleID, digest := range envelope.SnapshotDigests {
			if _, selected := selectionSet[articleID]; !selected {
				return "", nil, nil, false, fmt.Errorf("export item snapshot article %s is outside the selection", articleID)
			}
			if existing := snapshots[articleID]; existing != "" && existing != digest {
				return "", nil, nil, false, fmt.Errorf("export items have conflicting snapshot digests for article %s", articleID)
			}
			snapshots[articleID] = digest
		}
	}
	return producerVersion, digests, snapshots, reconstructed, nil
}

func (runtime *localExportRuntime) exportRecordForJob(ctx context.Context, jobID domain.JobID) (library.ExportRecord, error) {
	return runtime.library.GetExportByJob(ctx, jobID)
}

func decodeSelectionManifest(value any) (exporter.SelectionManifest, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return exporter.SelectionManifest{}, err
	}
	var manifest exporter.SelectionManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return exporter.SelectionManifest{}, err
	}
	if manifest.SchemaVersion != exporter.SelectionManifestVersion || len(manifest.ArticleIDs) == 0 {
		return exporter.SelectionManifest{}, errors.New("export selection manifest is missing or unsupported")
	}
	return manifest, nil
}

func exportProvenancePath(id domain.ExportID) string {
	return "export-" + string(id) + "-manifest.json"
}

func exportProvenanceGenerationPath(id domain.ExportID, generation int64) string {
	if generation <= 1 {
		return exportProvenancePath(id)
	}
	return fmt.Sprintf("export-%s-manifest-g%d.json", id, generation)
}

func (runtime *localExportRuntime) Recover(ctx context.Context) (int64, error) {
	if runtime == nil || runtime.store == nil {
		return 0, fmt.Errorf("export runtime: %w", application.ErrUnavailable)
	}
	recovered, err := runtime.store.RecoverStale(ctx)
	if err != nil {
		return recovered, err
	}
	var failures []error
	var processed int64
	var after domain.ExportID
	for {
		pending, pageErr := runtime.library.PendingTerminalExportsPage(ctx, after, 100)
		if pageErr != nil {
			return recovered + processed, errors.Join(errors.Join(failures...), pageErr)
		}
		if len(pending) == 0 {
			break
		}
		for _, record := range pending {
			after = record.ID
			job, getErr := runtime.store.Get(ctx, record.JobID)
			if getErr != nil {
				failures = append(failures, fmt.Errorf("export %s: %w", record.ID, getErr))
				continue
			}
			if finalizeErr := runtime.finalizeProvenance(ctx, job); finalizeErr != nil {
				failures = append(failures, fmt.Errorf("export %s: %w", record.ID, finalizeErr))
				continue
			}
			processed++
		}
	}
	return recovered + processed, errors.Join(failures...)
}

func (runtime *localExportRuntime) execute(ctx context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
	var envelope exportJobItem
	decoder := json.NewDecoder(strings.NewReader(item.Key))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode export job item: %w", err)}
	}
	if envelope.Version != 1 && envelope.Version != 2 && envelope.Version != 3 && envelope.Version != 4 {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode export job item: unsupported version %d", envelope.Version)}
	}
	if len(envelope.ArticleIDs) > 0 {
		if envelope.Format != "html" || len(envelope.Directories) != len(envelope.ArticleIDs) || envelope.ArticleID != "" {
			return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: errors.New("decode HTML batch export job item: invalid article set")}
		}
		if recovered, err := runtime.recoverExportOutputs(ctx, item.JobID, item.ID, item.Checkpoint); recovered || err != nil {
			return err
		}
		return runtime.executeHTMLBatch(ctx, item.JobID, item.ID, envelope, checkpoint)
	}
	if envelope.ArticleID == "" {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: errors.New("decode export job item: article ID is required")}
	}
	if recovered, err := runtime.recoverExportOutputs(ctx, item.JobID, item.ID, item.Checkpoint); recovered || err != nil {
		return err
	}
	article, normalized, comments, assets, err := runtime.loadExportArticleSnapshot(ctx, envelope.ArticleID,
		envelope.SourceDigest, envelope.SnapshotDigest)
	if err != nil {
		return err
	}
	outputDirectory := filepath.Dir(envelope.Output)
	manager, err := exportOutputManager(envelope.OutputAuthorization, outputDirectory)
	if err != nil {
		return err
	}
	defer manager.Close()
	policy := exportCollisionPolicy(envelope.Options.CollisionPolicy)
	outputName := exportOutputRelativePath(envelope)
	if strings.EqualFold(strings.TrimSpace(envelope.Options.CollisionPolicy), "suffix") {
		outputName, err = availableExportOutputName(manager.Root(), outputName, envelope.ArticleID,
			envelope.Options.MaximumNameBytes)
		if err != nil {
			return err
		}
		policy = exporter.CollisionFail
	}
	includeComments := optionBool(envelope.Options.FormatOptions, "comments", false)
	includeContent := optionBool(envelope.Options.FormatOptions, "content", true)
	includeMetadata := optionBool(envelope.Options.FormatOptions, "metadata", true)
	var stagedOutputs []exporter.StagedOutput
	var diagnostics []exportOutputDiagnostic
	switch envelope.Format {
	case "html":
		resourcePolicy, policyErr := htmlResourcePolicy(envelope.Options.FormatOptions)
		if policyErr != nil {
			return policyErr
		}
		result, staged, exportErr := exporter.StageHTMLArticle(ctx, manager, exporter.HTMLArticleInput{
			ArticleID: article.ID, Directory: strings.TrimSuffix(outputName, filepath.Ext(outputName)), Article: normalized,
			Assets: assets, Comments: comments,
		}, exporter.HTMLOptions{ResourcePolicy: resourcePolicy, IncludeComments: includeComments}, policy)
		if exportErr != nil {
			return exportErr
		}
		stagedOutputs = staged
		diagnostics = exportHTMLDiagnostics(result.ArticleID, result.MissingResources, result.Warnings)
	case "markdown":
		var data []byte
		data, err = exporter.RenderMarkdown(article.ID, normalized,
			exporter.MarkdownOptions{IncludeFrontMatter: includeMetadata, IncludeComments: includeComments, Comments: comments})
		if err == nil {
			var staged exporter.StagedOutput
			staged, err = manager.StageFile(ctx, outputName, policy, writeExportBytes(data))
			staged.Output.ArticleID = article.ID
			stagedOutputs = []exporter.StagedOutput{staged}
		}
	case "text":
		var data []byte
		data, err = exporter.RenderText(article.ID, normalized,
			exporter.TextOptions{IncludeMetadataHeader: includeMetadata, IncludeComments: includeComments, Comments: comments})
		if err == nil {
			var staged exporter.StagedOutput
			staged, err = manager.StageFile(ctx, outputName, policy, writeExportBytes(data))
			staged.Output.ArticleID = article.ID
			stagedOutputs = []exporter.StagedOutput{staged}
		}
	case "json":
		_, data, marshalErr := exporter.MarshalJSONExport(exporter.JSONExportInput{
			ArticleID: article.ID, Article: normalized, Comments: comments, ExportedAt: runtime.now(),
		}, exporter.JSONOptions{IncludeContent: includeContent, IncludeMetrics: includeMetadata, IncludeComments: includeComments,
			IncludeReplies: includeComments, IncludeAlbums: includeMetadata})
		err = marshalErr
		if err == nil {
			var staged exporter.StagedOutput
			staged, err = manager.StageFile(ctx, outputName, policy, writeExportBytes(data))
			staged.Output.ArticleID = article.ID
			stagedOutputs = []exporter.StagedOutput{staged}
		}
	case "xlsx":
		var staged exporter.StagedOutput
		staged, err = manager.StageFile(ctx, outputName, policy, func(writer io.Writer) error {
			_, writeErr := exporter.WriteXLSX(ctx, writer, &singleXLSXSource{row: xlsxRow(article, normalized, includeContent)},
				exporter.XLSXOptions{IncludeContent: includeContent, SheetName: "Articles"})
			return writeErr
		})
		staged.Output.ArticleID = article.ID
		stagedOutputs = []exporter.StagedOutput{staged}
	case "docx":
		var staged exporter.StagedOutput
		staged, err = manager.StageFile(ctx, outputName, policy, func(writer io.Writer) error {
			_, writeErr := exporter.WriteDOCX(ctx, writer, docxDocument(normalized, comments, assets),
				exporter.DOCXOptions{IncludeComments: includeComments})
			return writeErr
		})
		staged.Output.ArticleID = article.ID
		stagedOutputs = []exporter.StagedOutput{staged}
	case "pdf":
		browserPath := ""
		if runtime.browser != nil {
			browser, browserErr := runtime.browser.FindChromium(ctx)
			if browserErr != nil {
				return browserErr
			}
			browserPath = browser.Path
		}
		rendered, renderErr := processor.Render(normalized, processor.RenderOptions{
			ResourceMap: dataResourceMap(assets), ResourcePolicy: processor.ResourceRewriteBestEffort,
			IncludeComments: includeComments, Comments: comments,
		})
		if renderErr != nil {
			return renderErr
		}
		var staged exporter.StagedOutput
		staged, err = manager.StageFile(ctx, outputName, policy, func(writer io.Writer) error {
			_, writeErr := exporter.RenderPDF(ctx, writer, rendered.HTML, exporter.PDFOptions{
				BrowserPath: browserPath, Runner: runtime.runner,
			})
			return writeErr
		})
		staged.Output.ArticleID = article.ID
		stagedOutputs = []exporter.StagedOutput{staged}
	}
	if err != nil {
		return err
	}
	intent := newExportOutputIntent(envelope.ExportID, envelope.Format, stagedOutputs, diagnostics)
	if err := runtime.publishExportOutputs(checkpoint, intent); err != nil {
		return err
	}
	return runtime.commitExportOutputIntent(ctx, item.JobID, item.ID, intent, true)
}

func (runtime *localExportRuntime) executeHTMLBatch(
	ctx context.Context,
	jobID domain.JobID,
	itemID string,
	envelope exportJobItem,
	checkpoint jobs.CheckpointFunc,
) error {
	outputDirectory := filepath.Dir(envelope.Output)
	manager, err := exportOutputManager(envelope.OutputAuthorization, outputDirectory)
	if err != nil {
		return err
	}
	defer manager.Close()
	outputName := exportOutputRelativePath(envelope)
	policy := exportCollisionPolicy(envelope.Options.CollisionPolicy)
	if strings.EqualFold(strings.TrimSpace(envelope.Options.CollisionPolicy), "suffix") {
		outputName, err = availableExportOutputName(manager.Root(), outputName, domain.ArticleID(envelope.ExportID),
			envelope.Options.MaximumNameBytes)
		if err != nil {
			return err
		}
		policy = exporter.CollisionFail
	}
	resourcePolicy, err := htmlResourcePolicy(envelope.Options.FormatOptions)
	if err != nil {
		return err
	}
	inputs := make([]exporter.HTMLArticleInput, 0, len(envelope.ArticleIDs))
	for index, articleID := range envelope.ArticleIDs {
		_, normalized, comments, assets, loadErr := runtime.loadExportArticleSnapshot(ctx, articleID,
			envelope.SourceDigests[articleID], envelope.SnapshotDigests[articleID])
		if loadErr != nil {
			return loadErr
		}
		inputs = append(inputs, exporter.HTMLArticleInput{
			ArticleID: articleID, Directory: envelope.Directories[index], Article: normalized,
			Assets: assets, Comments: comments,
		})
	}
	result, staged, err := exporter.StageHTMLBatchArchive(ctx, manager, outputName, inputs, exporter.HTMLOptions{
		ResourcePolicy: resourcePolicy, IncludeComments: optionBool(envelope.Options.FormatOptions, "comments", false),
	}, policy)
	if err != nil {
		return err
	}
	result.Output.ArticleIDs = append([]domain.ArticleID(nil), envelope.ArticleIDs...)
	diagnostics := make([]exportOutputDiagnostic, 0)
	for _, article := range result.Articles {
		diagnostics = append(diagnostics, exportHTMLDiagnostics(article.ArticleID, article.MissingResources, article.Warnings)...)
	}
	intent := newExportOutputIntent(envelope.ExportID, envelope.Format, []exporter.StagedOutput{staged}, diagnostics)
	if err := runtime.publishExportOutputs(checkpoint, intent); err != nil {
		return err
	}
	return runtime.commitExportOutputIntent(ctx, jobID, itemID, intent, true)
}

type exportOutputIntent struct {
	Version     int                      `json:"version,omitempty"`
	ExportID    domain.ExportID          `json:"exportId"`
	Format      string                   `json:"format"`
	Outputs     []exportOutputIntentFile `json:"outputs"`
	Diagnostics []exportOutputDiagnostic `json:"diagnostics,omitempty"`
}

type exportOutputIntentFile struct {
	ArticleID     domain.ArticleID         `json:"articleId,omitempty"`
	ArticleIDs    []domain.ArticleID       `json:"articleIds,omitempty"`
	Path          string                   `json:"path"`
	Size          int64                    `json:"size"`
	SHA256        string                   `json:"sha256"`
	MediaType     string                   `json:"mediaType"`
	Status        exporter.OutputStatus    `json:"status"`
	TemporaryPath string                   `json:"temporaryPath,omitempty"`
	Policy        exporter.CollisionPolicy `json:"policy,omitempty"`
}

type exportOutputDiagnostic struct {
	Level     string           `json:"level"`
	Message   string           `json:"message"`
	Kind      string           `json:"kind"`
	ArticleID domain.ArticleID `json:"articleId,omitempty"`
	Resource  string           `json:"resource,omitempty"`
	Reason    string           `json:"reason,omitempty"`
}

func newExportOutputIntent(
	exportID domain.ExportID,
	format string,
	outputs []exporter.StagedOutput,
	diagnostics []exportOutputDiagnostic,
) exportOutputIntent {
	files := make([]exportOutputIntentFile, len(outputs))
	for index, staged := range outputs {
		output := staged.Output
		files[index] = exportOutputIntentFile{
			ArticleID: output.ArticleID, ArticleIDs: append([]domain.ArticleID(nil), output.ArticleIDs...),
			Path: output.Path, Size: output.Size, SHA256: output.SHA256,
			MediaType: exportMediaType(format, output.Path), Status: output.Status,
			TemporaryPath: staged.TemporaryPath, Policy: staged.Policy,
		}
	}
	return exportOutputIntent{
		Version: 2, ExportID: exportID, Format: format, Outputs: files,
		Diagnostics: append([]exportOutputDiagnostic(nil), diagnostics...),
	}
}

func checkpointExportOutputIntent(checkpoint jobs.CheckpointFunc, intent exportOutputIntent) error {
	return checkpoint(intent)
}

func writeExportBytes(data []byte) func(io.Writer) error {
	return func(writer io.Writer) error {
		_, err := writer.Write(data)
		return err
	}
}

func (runtime *localExportRuntime) publishExportOutputs(checkpoint jobs.CheckpointFunc, intent exportOutputIntent) error {
	if runtime.publish != nil {
		return runtime.publish(checkpoint, intent)
	}
	return checkpointExportOutputIntent(checkpoint, intent)
}

func (runtime *localExportRuntime) recoverExportOutputs(
	ctx context.Context,
	jobID domain.JobID,
	itemID string,
	raw json.RawMessage,
) (bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
		return false, nil
	}
	var intent exportOutputIntent
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil || intent.ExportID == "" || intent.Format == "" || len(intent.Outputs) == 0 {
		return false, nil
	}
	if intent.Version != 0 && intent.Version != 2 {
		return true, fmt.Errorf("unsupported export output checkpoint version %d", intent.Version)
	}
	if err := runtime.commitExportOutputIntent(ctx, jobID, itemID, intent, true); err != nil {
		return true, err
	}
	return true, nil
}

func (runtime *localExportRuntime) commitExportOutputIntent(
	ctx context.Context,
	jobID domain.JobID,
	itemID string,
	intent exportOutputIntent,
	verify bool,
) error {
	records := make([]library.ExportFileRecord, 0, len(intent.Outputs))
	for _, output := range intent.Outputs {
		if output.MediaType == "" && intent.Version == 0 {
			output.MediaType = exportMediaType(intent.Format, output.Path)
		}
		if output.Path == "" || output.SHA256 == "" || output.Size < 0 || output.MediaType == "" {
			return errors.New("export output checkpoint is incomplete")
		}
		if verify {
			root, err := runtime.exportRoot(ctx, intent.ExportID)
			if err != nil {
				return err
			}
			manager, err := exportOutputManager(root.authorization, root.path)
			if err != nil {
				return err
			}
			policy := output.Policy
			if policy == "" {
				// Version 0 checkpoints were written after publication and did not
				// carry a private staging path or collision policy.
				policy = exporter.CollisionFail
			}
			committed, commitErr := manager.CommitStaged(ctx, exporter.StagedOutput{
				Output: exporter.OutputFile{
					ArticleID: output.ArticleID, ArticleIDs: append([]domain.ArticleID(nil), output.ArticleIDs...),
					Path: output.Path, Size: output.Size, SHA256: output.SHA256, Status: output.Status,
				},
				TemporaryPath: output.TemporaryPath,
				Policy:        policy,
			})
			closeErr := manager.Close()
			if commitErr != nil || closeErr != nil {
				return errors.Join(commitErr, closeErr)
			}
			if committed.SHA256 != output.SHA256 || committed.Size != output.Size {
				return fmt.Errorf("exported file %s no longer matches its committed checkpoint", output.Path)
			}
		}
		records = append(records, library.ExportFileRecord{
			ExportID: intent.ExportID, ArticleID: output.ArticleID, RelativePath: output.Path,
			SizeBytes: output.Size, SHA256: output.SHA256,
			MediaType: output.MediaType, Status: string(output.Status),
		})
	}
	for _, record := range records {
		if err := runtime.recordExportFile(ctx, record); err != nil {
			return fmt.Errorf("record exported file %s: %w", record.RelativePath, err)
		}
	}
	for index, diagnostic := range intent.Diagnostics {
		if err := runtime.recordExportDiagnostic(ctx, jobID, itemID, intent.ExportID, index, diagnostic); err != nil {
			return err
		}
	}
	return nil
}

type exportOutputRoot struct {
	path          string
	authorization *domain.ExportOutputAuthorization
}

func (runtime *localExportRuntime) exportRoot(ctx context.Context, exportID domain.ExportID) (exportOutputRoot, error) {
	record, err := runtime.library.GetExport(ctx, exportID)
	if err != nil {
		return exportOutputRoot{}, err
	}
	return exportOutputRoot{path: record.OutputRoot, authorization: record.OutputAuthorization}, nil
}

func (runtime *localExportRuntime) recordExportFile(ctx context.Context, record library.ExportFileRecord) error {
	if runtime.recordFile != nil {
		return runtime.recordFile(ctx, record)
	}
	return runtime.library.UpsertExportFile(ctx, record)
}

func exportHTMLDiagnostics(
	articleID domain.ArticleID,
	missing []processor.Resource,
	warnings []exporter.Warning,
) []exportOutputDiagnostic {
	diagnostics := make([]exportOutputDiagnostic, 0, len(missing)+len(warnings))
	for _, resource := range missing {
		diagnostics = append(diagnostics, exportOutputDiagnostic{
			Level: "warning", Message: "HTML resource was not available locally", Kind: "missing_resource",
			ArticleID: articleID, Resource: resource.URL, Reason: string(resource.Kind),
		})
	}
	for _, warning := range warnings {
		diagnostics = append(diagnostics, exportOutputDiagnostic{
			Level: "warning", Message: warning.Message, Kind: warning.Code, ArticleID: articleID,
		})
	}
	return diagnostics
}

func (runtime *localExportRuntime) recordExportDiagnostic(
	ctx context.Context,
	jobID domain.JobID,
	itemID string,
	exportID domain.ExportID,
	index int,
	diagnostic exportOutputDiagnostic,
) error {
	return runtime.store.AppendLogOnce(ctx, jobID, itemID, diagnostic.Level, diagnostic.Message, map[string]any{
		"kind": diagnostic.Kind, "articleId": diagnostic.ArticleID, "resource": diagnostic.Resource,
		"reason": diagnostic.Reason,
	}, fmt.Sprintf("export-output:%s:%s:%d", exportID, itemID, index))
}

func validateHTMLBatchArchiveName(value string) error {
	if filepath.Base(value) != value || strings.ContainsAny(value, `/\\`) || !strings.EqualFold(filepath.Ext(value), ".zip") {
		return errors.New("HTML batch archive must be a single .zip file name")
	}
	return nil
}

func htmlResourcePolicy(options map[string]any) (processor.ResourceRewritePolicy, error) {
	value := strings.ToLower(strings.TrimSpace(optionString(options, "htmlResourcePolicy", "best_effort")))
	switch value {
	case "best_effort", "best-effort", "":
		return processor.ResourceRewriteBestEffort, nil
	case "strict":
		return processor.ResourceRewriteStrict, nil
	default:
		return "", errors.New("HTML resource policy must be best-effort or strict")
	}
}

func openAuthorizedExportRoot(
	authorization *domain.ExportOutputAuthorization,
	outputDirectory string,
) (string, string, *os.Root, error) {
	if authorization == nil || strings.TrimSpace(authorization.Root) == "" {
		return "", "", nil, errors.New("export output authorization is incomplete")
	}
	root := filepath.Clean(authorization.Root)
	info, err := os.Lstat(root)
	if err != nil {
		return "", "", nil, fmt.Errorf("inspect authorized export root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", "", nil, errors.New("authorized export root changed type")
	}
	relative := filepath.FromSlash(strings.TrimSpace(authorization.RelativePath))
	if relative == "." {
		relative = ""
	}
	cleanRelative := filepath.Clean(relative)
	if relative == "" {
		cleanRelative = ""
	}
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || cleanRelative != relative ||
		relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", nil, errors.New("authorized export path is unsafe")
	}
	expected := filepath.Clean(filepath.Join(root, relative))
	if filepath.Clean(outputDirectory) != expected {
		return "", "", nil, errors.New("export output path no longer matches its authorization")
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return "", "", nil, fmt.Errorf("open authorized export root: %w", err)
	}
	identityFile, err := handle.Open(".")
	if err != nil {
		handle.Close()
		return "", "", nil, fmt.Errorf("open authorized export root identity handle: %w", err)
	}
	device, inode, identityErr := exportRootIdentityFromFile(identityFile)
	identityCloseErr := identityFile.Close()
	err = errors.Join(identityErr, identityCloseErr)
	if err != nil || device != authorization.Device || inode != authorization.Inode {
		handle.Close()
		if err != nil {
			return "", "", nil, fmt.Errorf("identify opened authorized export root: %w", err)
		}
		return "", "", nil, errors.New("authorized export root was replaced after the job was queued")
	}
	return expected, filepath.ToSlash(relative), handle, nil
}

func exportOutputManager(authorization *domain.ExportOutputAuthorization, outputDirectory string) (*exporter.OutputManager, error) {
	if authorization == nil {
		return exporter.NewOutputManager(outputDirectory)
	}
	_, relative, root, err := openAuthorizedExportRoot(authorization, outputDirectory)
	if err != nil {
		return nil, &jobs.ClassifiedError{Class: jobs.FailureStorage, Err: err}
	}
	manager, err := exporter.NewOutputManagerFromRoot(root, relative)
	closeErr := root.Close()
	if err != nil || closeErr != nil {
		if manager != nil {
			_ = manager.Close()
		}
		return nil, errors.Join(err, closeErr)
	}
	return manager, nil
}

func authorizeExportOutputRoot(outputRoot string) (*domain.ExportOutputAuthorization, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(outputRoot))
	if err != nil {
		return nil, fmt.Errorf("resolve export output root: %w", err)
	}
	absolute = filepath.Clean(absolute)
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create export output root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect export output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("export output root must be a non-symlink directory")
	}
	identityFile, err := os.Open(absolute)
	if err != nil {
		return nil, fmt.Errorf("open export output root: %w", err)
	}
	device, inode, identityErr := exportRootIdentityFromFile(identityFile)
	closeErr := identityFile.Close()
	if err := errors.Join(identityErr, closeErr); err != nil {
		return nil, fmt.Errorf("identify export output root: %w", err)
	}
	return &domain.ExportOutputAuthorization{Root: absolute, Device: device, Inode: inode}, nil
}

func availableExportOutputName(
	root, plannedName string,
	articleID domain.ArticleID,
	maximumBytes int,
) (string, error) {
	if maximumBytes == 0 {
		maximumBytes = exporter.DefaultMaximumBytes
	}
	name := plannedName
	for attempt := 0; ; attempt++ {
		candidatePath := filepath.Join(root, name)
		_, err := os.Lstat(candidatePath)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect export destination %s: %w", candidatePath, err)
		}
		suffixID := articleID
		if attempt > 0 {
			suffixID = domain.ArticleID(string(articleID) + "-" + strconv.Itoa(attempt+1))
		}
		name, err = exporter.AddCollisionSuffix(plannedName, suffixID, exporter.NamingOptions{
			MaximumBytes: maximumBytes, Platform: exporter.PlatformPortable,
		})
		if err != nil {
			return "", err
		}
	}
}

func exportOutputRelativePath(envelope exportJobItem) string {
	if strings.TrimSpace(envelope.OutputRelativePath) != "" {
		return filepath.ToSlash(filepath.Clean(filepath.FromSlash(envelope.OutputRelativePath)))
	}
	return filepath.Base(envelope.Output)
}

func exportMediaType(format, path string) string {
	if format == "html" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".html", ".htm":
			return "text/html"
		case ".css":
			return "text/css"
		case ".zip":
			return "application/zip"
		}
		return "application/octet-stream"
	}
	return map[string]string{
		"markdown": "text/markdown", "text": "text/plain", "json": "application/json",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"pdf":  "application/pdf",
	}[format]
}

func (runtime *localExportRuntime) loadExportArticle(
	ctx context.Context,
	articleID domain.ArticleID,
) (domain.Article, processor.Article, []processor.Comment, []exporter.HTMLAsset, error) {
	return runtime.loadExportArticleAtDigest(ctx, articleID, "")
}

func (runtime *localExportRuntime) captureExportSnapshot(
	ctx context.Context,
	input library.ExportSnapshotRecord,
) (capturedExportSnapshot, error) {
	comments := processorComments(input.Comments)
	resources := make([]exportResourceRef, 0, len(input.Resources))
	pinned := []string{input.Content.ObjectDigest}
	for _, mapping := range input.Resources {
		resource := exportResourceRef{
			URL: mapping.OriginalURL, Name: exportResourceName(mapping.OriginalURL),
			Role: mapping.Role, Ordinal: mapping.Ordinal, Status: mapping.Status, MediaType: mapping.MediaType,
		}
		if mapping.Status == "available" && mapping.ObjectDigest != "" {
			if _, statErr := runtime.objects.Stat(mapping.ObjectDigest); statErr != nil {
				return capturedExportSnapshot{}, fmt.Errorf("inspect article resource %s: %w", mapping.OriginalURL, statErr)
			}
			resource.Digest = mapping.ObjectDigest
			pinned = append(pinned, mapping.ObjectDigest)
		}
		resources = append(resources, resource)
	}
	sort.Slice(resources, func(left, right int) bool {
		if resources[left].Role != resources[right].Role {
			return resources[left].Role < resources[right].Role
		}
		if resources[left].Ordinal != resources[right].Ordinal {
			return resources[left].Ordinal < resources[right].Ordinal
		}
		return resources[left].URL < resources[right].URL
	})
	snapshot := exportInputSnapshot{
		Version: 1, ArticleID: input.Article.ID, Article: input.Article, HTMLDigest: input.Content.ObjectDigest,
		Comments: comments, Resources: resources,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return capturedExportSnapshot{}, fmt.Errorf("encode export input snapshot: %w", err)
	}
	object, err := runtime.objects.Put(ctx, bytes.NewReader(encoded), "application/vnd.wechat-article.export-input+json")
	if err != nil {
		return capturedExportSnapshot{}, err
	}
	pinned = append(pinned, object.Digest)
	return capturedExportSnapshot{Digest: object.Digest, Pinned: uniqueSortedStrings(pinned), Object: object}, nil
}

func processorComments(records []library.CommentRecord) []processor.Comment {
	comments := make([]processor.Comment, 0, len(records))
	for _, record := range records {
		created := record.CreatedAt
		comment := processor.Comment{ID: record.UpstreamID, Author: record.AuthorName, Content: record.Content, Likes: int64(record.LikeCount)}
		if !created.IsZero() {
			comment.CreatedAt = &created
		}
		for _, reply := range record.EmbeddedReplies {
			replyCreated := reply.CreatedAt
			value := processor.Reply{ID: reply.UpstreamID, Author: reply.AuthorName, Content: reply.Content, Likes: int64(reply.LikeCount)}
			if !replyCreated.IsZero() {
				value.CreatedAt = &replyCreated
			}
			comment.Replies = append(comment.Replies, value)
		}
		comments = append(comments, comment)
	}
	return comments
}

func (runtime *localExportRuntime) loadExportArticleSnapshot(
	ctx context.Context,
	articleID domain.ArticleID,
	htmlDigest string,
	snapshotDigest string,
) (domain.Article, processor.Article, []processor.Comment, []exporter.HTMLAsset, error) {
	if snapshotDigest == "" {
		return runtime.loadExportArticleAtDigest(ctx, articleID, htmlDigest)
	}
	var snapshot exportInputSnapshot
	if err := runtime.readVerifiedJSON(ctx, snapshotDigest, &snapshot); err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("read article %s export snapshot: %w", articleID, err)
	}
	if snapshot.Version != 1 || snapshot.ArticleID != articleID || snapshot.Article.ID != articleID ||
		snapshot.HTMLDigest == "" || (htmlDigest != "" && snapshot.HTMLDigest != htmlDigest) {
		return domain.Article{}, processor.Article{}, nil, nil, errors.New("export input snapshot does not match the queued article")
	}
	parsed, err := runtime.processVerifiedHTML(ctx, snapshot.HTMLDigest)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("process article %s snapshot HTML: %w", articleID, err)
	}
	assets := make([]exporter.HTMLAsset, 0, len(snapshot.Resources))
	for _, resource := range snapshot.Resources {
		if resource.Status != "available" || resource.Digest == "" {
			continue
		}
		data, err := runtime.readVerifiedObject(ctx, resource.Digest)
		if err != nil {
			return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("read article resource %s: %w", resource.URL, err)
		}
		assets = append(assets, exporter.HTMLAsset{
			URL: resource.URL, Name: resource.Name, MediaType: resource.MediaType, Data: data,
		})
	}
	return snapshot.Article, parsed, cloneProcessorComments(snapshot.Comments), assets, nil
}

func (runtime *localExportRuntime) loadExportArticleAtDigest(
	ctx context.Context,
	articleID domain.ArticleID,
	digest string,
) (domain.Article, processor.Article, []processor.Comment, []exporter.HTMLAsset, error) {
	article, err := runtime.library.GetArticle(ctx, articleID)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	if digest == "" {
		content, contentErr := runtime.library.CurrentContent(ctx, articleID, "html")
		if contentErr != nil {
			return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("article %s has no downloaded HTML content: %w", articleID, contentErr)
		}
		digest = content.ObjectDigest
	}
	parsed, err := runtime.processVerifiedHTML(ctx, digest)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("process article %s HTML object: %w", articleID, err)
	}
	comments, err := runtime.loadComments(ctx, articleID)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	assets, err := runtime.loadAssets(ctx, articleID)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	return article, parsed, comments, assets, nil
}

func (runtime *localExportRuntime) loadComments(ctx context.Context, articleID domain.ArticleID) ([]processor.Comment, error) {
	records, err := runtime.library.CommentsForArticle(ctx, articleID)
	if err != nil {
		return nil, err
	}
	return processorComments(records), nil
}

func (runtime *localExportRuntime) loadAssets(ctx context.Context, articleID domain.ArticleID) ([]exporter.HTMLAsset, error) {
	mappings, err := runtime.library.ListArticleResources(ctx, articleID)
	if err != nil {
		return nil, err
	}
	assets := make([]exporter.HTMLAsset, 0, len(mappings))
	for _, mapping := range mappings {
		record, err := runtime.library.ResourceByURL(ctx, mapping.OriginalURL)
		if err != nil || record.ObjectDigest == "" || record.Status != "available" {
			continue
		}
		data, err := runtime.readVerifiedObject(ctx, record.ObjectDigest)
		if err != nil {
			return nil, fmt.Errorf("read article resource %s: %w", mapping.OriginalURL, err)
		}
		assets = append(assets, exporter.HTMLAsset{
			URL: mapping.OriginalURL, Name: exportResourceName(mapping.OriginalURL),
			MediaType: record.MediaType, Data: data,
		})
	}
	return assets, nil
}

func (runtime *localExportRuntime) processVerifiedHTML(ctx context.Context, digest string) (processor.Article, error) {
	reader, _, err := runtime.objects.Open(ctx, digest)
	if err != nil {
		return processor.Article{}, err
	}
	hash := sha256.New()
	parsed, processErr := processor.New().Process(ctx, io.TeeReader(reader, hash))
	closeErr := reader.Close()
	if processErr != nil || closeErr != nil || parsed.Article == nil {
		return processor.Article{}, errors.Join(processErr, closeErr)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != digest {
		return processor.Article{}, fmt.Errorf("object %s produced digest %s: %w", digest, actual, objects.ErrIntegrity)
	}
	return *parsed.Article, nil
}

func (runtime *localExportRuntime) readVerifiedObject(ctx context.Context, digest string) ([]byte, error) {
	reader, _, err := runtime.objects.Open(ctx, digest)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	data, readErr := io.ReadAll(io.TeeReader(reader, hash))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != digest {
		return nil, fmt.Errorf("object %s produced digest %s: %w", digest, actual, objects.ErrIntegrity)
	}
	return data, nil
}

func (runtime *localExportRuntime) readVerifiedJSON(ctx context.Context, digest string, target any) error {
	data, err := runtime.readVerifiedObject(ctx, digest)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("export input snapshot has trailing data")
	}
	return nil
}

func exportResourceName(rawURL string) string {
	name := filepath.Base(strings.SplitN(rawURL, "?", 2)[0])
	if name == "." || name == string(filepath.Separator) {
		return "resource"
	}
	return name
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneProcessorComments(comments []processor.Comment) []processor.Comment {
	encoded, err := json.Marshal(comments)
	if err != nil {
		return append([]processor.Comment(nil), comments...)
	}
	var clone []processor.Comment
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return append([]processor.Comment(nil), comments...)
	}
	return clone
}

func planExportNames(format string, options domain.ExportOptions, articles []domain.Article) ([]exporter.PlannedName, error) {
	extension := map[string]string{
		"html": "", "markdown": ".md", "text": ".txt", "json": ".json",
		"xlsx": ".xlsx", "docx": ".docx", "pdf": ".pdf",
	}[format]
	items := make([]exporter.NamingData, len(articles))
	for index, article := range articles {
		items[index] = exporter.NamingData{ArticleID: article.ID, AccountID: article.AccountID, Title: article.Title,
			AID: article.Aid, Author: article.Author, PublishedAt: article.PublishedAt, Index: index + 1}
	}
	template := strings.NewReplacer(
		"{title}", "${title}", "{published}", "${YYYY}-${MM}-${DD}", "{author}", "${author}",
		"{articleId}", "${articleId}", "{aid}", "${aid}", "{index}", "${index}",
	).Replace(options.NamingTemplate)
	return exporter.PlanFilenames(exporter.NamingOptions{
		Template: template, Extension: extension, MaximumBytes: options.MaximumNameBytes, Platform: exporter.PlatformPortable,
	}, items)
}

func supportedLocalExportFormat(format string) bool {
	switch format {
	case "html", "markdown", "text", "json", "xlsx", "docx", "pdf":
		return true
	default:
		return false
	}
}

func exportCollisionPolicy(value string) exporter.CollisionPolicy {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "skip":
		return exporter.CollisionSkip
	case "replace":
		return exporter.CollisionReplace
	default:
		return exporter.CollisionFail
	}
}

func optionBool(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key]
	if !ok {
		return fallback
	}
	result, ok := value.(bool)
	if !ok {
		return fallback
	}
	return result
}

func optionString(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok {
		return fallback
	}
	result, ok := value.(string)
	if !ok {
		return fallback
	}
	return result
}

type singleXLSXSource struct {
	row  exporter.XLSXRow
	done bool
}

func (source *singleXLSXSource) Next(context.Context) (exporter.XLSXRow, error) {
	if source.done {
		return exporter.XLSXRow{}, io.EOF
	}
	source.done = true
	return source.row, nil
}

func xlsxRow(article domain.Article, normalized processor.Article, includeContent bool) exporter.XLSXRow {
	row := exporter.XLSXRow{
		Account: normalized.Account.Nickname, ArticleID: string(article.ID), CanonicalURL: article.CanonicalURL,
		Title: article.Title, CoverURL: article.CoverURL, Digest: article.Digest, PublishedAt: article.PublishedAt,
		ReadCount: int64(article.ReadCount), OldLikeCount: int64(article.OldLikeCount), ShareCount: int64(article.ShareCount),
		LikeCount: int64(article.LikeCount), CommentCount: int64(article.CommentCount), Author: article.Author,
		Original: article.Original, MessageType: strconv.Itoa(article.MessageType), State: article.State,
		DownloadState: map[bool]string{true: "available", false: "missing"}[article.HasContent],
	}
	for _, album := range article.Albums {
		row.Albums = append(row.Albums, album.Name)
	}
	if includeContent {
		if rendered, err := processor.Render(normalized, processor.RenderOptions{ResourcePolicy: processor.ResourceRewriteBestEffort}); err == nil {
			row.Content = rendered.Text
		}
	}
	return row
}

func docxDocument(article processor.Article, comments []processor.Comment, assets []exporter.HTMLAsset) exporter.DOCXDocument {
	document := exporter.DOCXDocument{Title: article.Title, Account: article.Account.Nickname, Author: article.Author, HTML: article.Content}
	if article.Timestamps.PublishedAt != nil {
		document.PublishedAt = *article.Timestamps.PublishedAt
	}
	for _, asset := range assets {
		name := filepath.Base(asset.Name)
		if name == "." || name == "" {
			name = filepath.Base(strings.SplitN(asset.URL, "?", 2)[0])
		}
		document.Media = append(document.Media, exporter.DOCXMedia{
			Source: asset.URL, Name: name, ContentType: asset.MediaType, Data: asset.Data,
		})
	}
	for _, comment := range comments {
		value := exporter.DOCXComment{Author: comment.Author, Content: comment.Content}
		if comment.CreatedAt != nil {
			value.CreatedAt = *comment.CreatedAt
		}
		for _, reply := range comment.Replies {
			replyValue := exporter.DOCXReply{Author: reply.Author, Content: reply.Content}
			if reply.CreatedAt != nil {
				replyValue.CreatedAt = *reply.CreatedAt
			}
			value.Replies = append(value.Replies, replyValue)
		}
		document.Comments = append(document.Comments, value)
	}
	return document
}

func dataResourceMap(assets []exporter.HTMLAsset) map[string]string {
	mapping := make(map[string]string, len(assets))
	for _, asset := range assets {
		mapping[asset.URL] = "data:" + asset.MediaType + ";base64," + base64String(asset.Data)
	}
	return mapping
}

func base64String(data []byte) string {
	var output bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &output)
	_, _ = encoder.Write(data)
	_ = encoder.Close()
	return output.String()
}

var _ application.ExportJobs = (*localExportRuntime)(nil)
