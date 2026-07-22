package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	syncrunner "github.com/wechat-article/wechat-article-exporter/cli/internal/sync"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type workspaceExtensions struct {
	app *App
}

func newWorkspaceExtensions(application *App) tui.WorkspaceExtensions {
	return &workspaceExtensions{app: application}
}

func (extensions *workspaceExtensions) active() (*ProfileRuntime, error) {
	if extensions == nil || extensions.app == nil || extensions.app.active == nil {
		return nil, errors.New("active profile runtime is unavailable")
	}
	return extensions.app.active, nil
}

func (extensions *workspaceExtensions) Panel(ctx context.Context, area tui.Area) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	switch area {
	case tui.AreaExports:
		page, err := active.Library.QueryExports(ctx, 0, 10)
		if err != nil {
			return tui.OperationResult{}, err
		}
		lines := make([]string, 0, len(page.Items))
		for _, id := range page.Items {
			record, recordErr := active.Library.GetExport(ctx, id)
			if recordErr != nil {
				return tui.OperationResult{}, recordErr
			}
			lines = append(lines, fmt.Sprintf("%s · %s · %s · %s", record.ID, record.Format, record.State, record.OutputRoot))
		}
		return tui.OperationResult{Title: "Exports", Message: fmt.Sprintf("%d persistent export records", page.Total), Lines: lines}, nil
	case tui.AreaSettings:
		configuration, _, err := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
		if err != nil {
			return tui.OperationResult{}, err
		}
		credentials, credentialErr := active.Library.ListCredentials(ctx)
		if credentialErr != nil {
			return tui.OperationResult{}, credentialErr
		}
		routes, routeErr := active.Network.List(ctx)
		if routeErr != nil {
			return tui.OperationResult{}, routeErr
		}
		return tui.OperationResult{Title: "Settings", Fields: map[string]string{
			"profile": string(active.Profile.ID), "configuration schema": fmt.Sprint(configuration.SchemaVersion),
			"credentials": fmt.Sprint(len(credentials)), "proxies": fmt.Sprint(len(routes)),
			"download concurrency": fmt.Sprint(configuration.Preferences.Download.Concurrency),
			"sync range":           configuration.Preferences.Sync.Range, "sync page delay": configuration.Preferences.Sync.PageDelay.String(),
			"export root": fallbackValue(configuration.Preferences.Export.Root, "not set"),
			"display":     displayPreferenceSummary(configuration.Preferences.Display),
		}}, nil
	case tui.AreaStorage:
		status, err := extensions.app.core.StorageStatus(ctx)
		if err != nil {
			return tui.OperationResult{}, err
		}
		return tui.OperationResult{Title: "Storage", Fields: map[string]string{
			"database": active.Library.Path(), "objects": active.Objects.Root(),
			"database available": fmt.Sprint(status.DatabaseAvailable), "object store ready": fmt.Sprint(status.ObjectStoreReady),
			"accounts": fmt.Sprint(status.Accounts), "articles": fmt.Sprint(status.Articles),
			"albums": fmt.Sprint(status.Albums), "jobs": fmt.Sprint(status.Jobs),
			"objects stored": fmt.Sprint(status.Objects), "object bytes": fmt.Sprint(status.ObjectBytes),
		}}, nil
	case tui.AreaDiagnostics:
		return extensions.diagnostics(ctx)
	default:
		return tui.OperationResult{}, nil
	}
}

func (extensions *workspaceExtensions) PreviewArticle(ctx context.Context, articleID domain.ArticleID) (tui.PreviewDocument, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.PreviewDocument{}, err
	}
	article, normalized, comments, assets, err := loadWorkspaceArticle(ctx, active, articleID)
	if err != nil {
		return tui.PreviewDocument{}, err
	}
	rendered, err := processor.Render(normalized, processor.RenderOptions{
		ResourceMap: dataResourceMap(assets), ResourcePolicy: processor.ResourceRewriteBestEffort,
		IncludeComments: true, Comments: comments,
	})
	if err != nil {
		return tui.PreviewDocument{}, err
	}
	return tui.PreviewDocument{Title: article.Title, Format: "markdown", Text: rendered.Markdown}, nil
}

func (extensions *workspaceExtensions) OpenHTMLPreview(ctx context.Context, articleID domain.ArticleID) error {
	active, err := extensions.active()
	if err != nil {
		return err
	}
	_, normalized, comments, assets, err := loadWorkspaceArticle(ctx, active, articleID)
	if err != nil {
		return err
	}
	rendered, err := processor.Render(normalized, processor.RenderOptions{
		ResourceMap: dataResourceMap(assets), ResourcePolicy: processor.ResourceRewriteBestEffort,
		IncludeComments: true, Comments: comments,
	})
	if err != nil {
		return err
	}
	directory := filepath.Join(active.Profile.Paths.Cache, "previews")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create preview directory: %w", err)
	}
	path := filepath.Join(directory, string(articleID)+".html")
	if err := writePrivateTextFile(path, rendered.HTML); err != nil {
		return err
	}
	return extensions.app.openBrowser(ctx, "file://"+filepath.ToSlash(path))
}

func (extensions *workspaceExtensions) Operate(ctx context.Context, request tui.OperationRequest) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	switch request.Kind {
	case tui.OperationAccountImport:
		return tui.OperationResult{Title: "Import accounts", Message: "Use `wechat-article account import --file <manifest>` so the source path and merge report are explicit."}, nil
	case tui.OperationAccountExport:
		manifest, err := extensions.app.core.ExportAccounts(ctx, domain.AccountQuery{})
		if err != nil {
			return tui.OperationResult{}, err
		}
		return tui.OperationResult{Title: "Account export manifest", Fields: map[string]string{
			"schema version": fmt.Sprint(manifest.SchemaVersion), "accounts": fmt.Sprint(len(manifest.Accounts)),
			"exported at": manifest.ExportedAt.Local().Format(time.RFC3339),
		}}, nil
	case tui.OperationAlbumTraverse:
		return extensions.albumTraverse(ctx, request)
	case tui.OperationJobLogs:
		return extensions.jobDetails(ctx, request.IDs)
	case tui.OperationJobPause:
		return extensions.jobOperation(ctx, request.IDs, active.Jobs.Pause, "paused")
	case tui.OperationJobResume:
		return extensions.jobOperation(ctx, request.IDs, active.Jobs.Resume, "queued for resume")
	case tui.OperationJobRetry:
		return extensions.jobOperation(ctx, request.IDs, active.Jobs.Retry, "queued for retry")
	case tui.OperationRouteHealth:
		return extensions.routeHealth(ctx)
	case tui.OperationExportManifest:
		return extensions.exportManifest(ctx, request.IDs)
	case tui.OperationExportConfig:
		return extensions.exportConfiguration(ctx)
	case tui.OperationOpenExport:
		return extensions.openExport(ctx, request.IDs)
	case tui.OperationCredentials:
		items, err := active.Library.ListCredentials(ctx)
		if err != nil {
			return tui.OperationResult{}, err
		}
		lines := make([]string, 0, len(items))
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("%s · account %s · %s · %s", item.ID, item.AccountID, item.Kind, item.Status))
		}
		return tui.OperationResult{Title: "Credentials", Message: fmt.Sprintf("%d non-secret credential records", len(items)), Lines: lines}, nil
	case tui.OperationProxies:
		return extensions.routeHealth(ctx)
	case tui.OperationPreferences:
		return extensions.preferences(ctx)
	case tui.OperationBackup:
		return extensions.backup(ctx)
	case tui.OperationRestore:
		return tui.OperationResult{Title: "Restore backup", Message: "Use `wechat-article db restore <archive> --confirm restore-backup:<archive>` so the archive and conflict policy are explicit."}, nil
	case tui.OperationIntegrity:
		report, err := active.Library.CheckIntegrity(ctx, active.Objects)
		if err != nil {
			return tui.OperationResult{}, err
		}
		return tui.OperationResult{Title: "Integrity check", Message: fmt.Sprintf("%d issue(s)", len(report.Issues)),
			Fields: map[string]string{"checked at": report.CheckedAt.Local().Format(time.RFC3339), "valid": fmt.Sprint(len(report.Issues) == 0)}}, nil
	case tui.OperationGarbageCollect:
		plan, err := active.Library.PlanGarbageCollection(ctx, library.GarbageCollectionOptions{
			ObjectStore: active.Objects, ObjectRetention: 24 * time.Hour, TemporaryRetention: 24 * time.Hour,
			DebugRetention: 30 * 24 * time.Hour, CompletedJobRetention: 30 * 24 * time.Hour,
		})
		if err != nil {
			return tui.OperationResult{}, err
		}
		return tui.OperationResult{Title: "Garbage collection dry run", Fields: map[string]string{
			"unreferenced objects":   fmt.Sprint(plan.Objects.Unreferenced.Count),
			"temporary files":        fmt.Sprint(plan.Objects.Temporary.Count),
			"expired debug captures": fmt.Sprint(plan.Metadata.ExpiredDebug.Count),
			"completed logs":         fmt.Sprint(plan.Metadata.CompletedJobLogs.Count),
			"CLI confirmation":       plan.Confirmation,
		}}, nil
	case tui.OperationDiagnostics:
		return extensions.diagnostics(ctx)
	case tui.OperationArticleComments:
		return extensions.articleComments(ctx, request.IDs)
	case tui.OperationArticleMetrics:
		return extensions.articleMetrics(ctx, request.IDs)
	case tui.OperationArticleResources:
		return extensions.articleResources(ctx, request.IDs)
	default:
		return tui.OperationResult{}, fmt.Errorf("unsupported workspace operation %q", request.Kind)
	}
}

func (extensions *workspaceExtensions) albumTraverse(ctx context.Context, request tui.OperationRequest) (tui.OperationResult, error) {
	if len(request.IDs) == 0 {
		return tui.OperationResult{}, errors.New("select an album first")
	}
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	page, err := active.Library.QueryAlbums(ctx, domain.AlbumQuery{Limit: 500})
	if err != nil {
		return tui.OperationResult{}, err
	}
	var album domain.Album
	for _, candidate := range page.Items {
		if string(candidate.ID) == request.IDs[0] {
			album = candidate
			break
		}
	}
	if album.ID == "" {
		return tui.OperationResult{}, sql.ErrNoRows
	}
	account, err := active.Library.GetAccount(ctx, album.AccountID)
	if err != nil {
		return tui.OperationResult{}, err
	}
	runtime, ok := active.Syncs.(*localSyncRuntime)
	if !ok || runtime == nil {
		return tui.OperationResult{}, errors.New("local album sync runtime is unavailable")
	}
	order := request.Parameters["order"]
	if order == "" {
		order = "forward"
	}
	downloadAfter := request.Parameters["mode"] == "download"
	job, err := runtime.StartAlbum(ctx, syncrunner.AlbumSyncRequest{
		FakeID: account.FakeID, AlbumID: album.UpstreamID, Order: wechat.AlbumOrder(order), PageSize: 20, PageDelay: 5 * time.Second,
	}, downloadAfter)
	if err != nil {
		return tui.OperationResult{}, err
	}
	if extensions.app.runtimes != nil && extensions.app.runtimes.worker != nil {
		starter := persistentJobStarter{executable: extensions.app.runtimes.executable, paths: extensions.app.runtimes.paths,
			profile: active.Profile.ID, launcher: extensions.app.runtimes.worker}
		if err := starter.Start(ctx, job); err != nil {
			return tui.OperationResult{}, err
		}
	}
	return tui.OperationResult{Title: "Album traversal queued", Fields: map[string]string{
		"job": string(job.ID), "album": album.Name, "order": order, "download after traversal": fmt.Sprint(downloadAfter),
	}}, nil
}

func (extensions *workspaceExtensions) exportConfiguration(ctx context.Context) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	configuration, _, err := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
	if err != nil {
		return tui.OperationResult{}, err
	}
	preferences := configuration.Preferences.Export
	return tui.OperationResult{Title: "Export configuration", Fields: map[string]string{
		"output root": fallbackValue(preferences.Root, "not set"), "naming template": preferences.NamingTemplate,
		"maximum name bytes": fmt.Sprint(preferences.MaximumNameBytes), "collision policy": preferences.CollisionPolicy,
		"Excel content": fmt.Sprint(preferences.ExcelIncludeContent), "JSON content": fmt.Sprint(preferences.JSONIncludeContent),
		"JSON comments": fmt.Sprint(preferences.JSONIncludeComments), "HTML comments": fmt.Sprint(preferences.HTMLIncludeComments),
	}, Message: "Select articles or an album before starting an export. Cobra exposes explicit format/output flags for automation."}, nil
}

func loadWorkspaceArticle(
	ctx context.Context,
	active *ProfileRuntime,
	articleID domain.ArticleID,
) (domain.Article, processor.Article, []processor.Comment, []exporter.HTMLAsset, error) {
	if active.Exports == nil {
		return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("article preview: %w", application.ErrUnavailable)
	}
	runtime, ok := active.Exports.(*localExportRuntime)
	if !ok {
		return domain.Article{}, processor.Article{}, nil, nil, errors.New("article preview requires the local export runtime")
	}
	article, normalized, comments, assets, err := runtime.loadExportArticle(ctx, articleID)
	return article, normalized, comments, assets, err
}

func (extensions *workspaceExtensions) preferences(ctx context.Context) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	configuration, backup, err := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
	if err != nil {
		return tui.OperationResult{}, err
	}
	preferences := configuration.Preferences
	return tui.OperationResult{Title: "Saved and effective preferences", Fields: map[string]string{
		"configuration": active.Profile.Paths.Config, "schema": fmt.Sprint(configuration.SchemaVersion),
		"migration backup": fallbackValue(backup, "none"), "sync range": preferences.Sync.Range,
		"sync point": formatOptionalTime(preferences.Sync.DatePoint), "page delay": preferences.Sync.PageDelay.String(),
		"jitter": preferences.Sync.Jitter.String(), "page size": fmt.Sprint(preferences.Sync.PageSize),
		"incremental": fmt.Sprint(preferences.Sync.Incremental), "unsafe pacing confirmed": fmt.Sprint(preferences.Sync.UnsafePacingSaved),
		"download concurrency": fmt.Sprint(preferences.Download.Concurrency), "force content": fmt.Sprint(preferences.Download.ForceContent),
		"metadata overrides content": fmt.Sprint(preferences.Download.MetadataOverridesContent),
		"export root":                fallbackValue(preferences.Export.Root, "not set"), "naming template": preferences.Export.NamingTemplate,
		"maximum name bytes": fmt.Sprint(preferences.Export.MaximumNameBytes), "collision policy": preferences.Export.CollisionPolicy,
		"display":      displayPreferenceSummary(preferences.Display),
		"direct first": fmt.Sprint(preferences.Proxy.DirectFirst), "proxy fallback": fmt.Sprint(preferences.Proxy.FallbackEnabled),
	}, Message: "Edit non-secret values with the versioned profile configuration; values below 3s require unsafePacingSaved confirmation."}, nil
}

func (extensions *workspaceExtensions) routeHealth(ctx context.Context) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	routes, err := active.Network.List(ctx)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := []string{"direct route · enabled · credential eligible"}
	for _, route := range routes {
		lines = append(lines, fmt.Sprintf("%s · enabled=%t · trust=%s · health=%s · latency=%s", route.Name, route.Enabled,
			route.Trust, route.Health.State, route.Health.LastLatency))
	}
	return tui.OperationResult{Title: "Route health", Message: fmt.Sprintf("direct plus %d configured proxy route(s)", len(routes)), Lines: lines}, nil
}

func (extensions *workspaceExtensions) diagnostics(ctx context.Context) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	runtimeStatus, err := extensions.app.core.RuntimeStatus(ctx)
	if err != nil {
		return tui.OperationResult{}, err
	}
	session, err := extensions.app.core.SessionStatus(ctx)
	if err != nil {
		return tui.OperationResult{}, err
	}
	jobsPage, jobsErr := extensions.app.core.QueryJobs(ctx, domain.JobQuery{Limit: 10})
	if jobsErr != nil {
		return tui.OperationResult{}, jobsErr
	}
	routes, routesErr := active.Network.List(ctx)
	if routesErr != nil {
		return tui.OperationResult{}, routesErr
	}
	return tui.OperationResult{Title: "Diagnostics", Fields: map[string]string{
		"version": runtimeStatus.Version, "profile": string(runtimeStatus.Profile), "portable": fmt.Sprint(runtimeStatus.Portable),
		"offline ready": fmt.Sprint(runtimeStatus.OfflineReady), "session": string(session.State),
		"recent jobs": fmt.Sprint(jobsPage.Total), "configured proxies": fmt.Sprint(len(routes)),
		"database": active.Library.Path(), "objects": active.Objects.Root(),
	}}, nil
}

func (extensions *workspaceExtensions) jobDetails(ctx context.Context, ids []string) (tui.OperationResult, error) {
	if len(ids) == 0 {
		return tui.OperationResult{}, errors.New("select a job first")
	}
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	id := domain.JobID(ids[0])
	job, err := active.Jobs.Get(ctx, id)
	if err != nil {
		return tui.OperationResult{}, err
	}
	items, err := active.Jobs.ListItems(ctx, id)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := fmt.Sprintf("%s · %s · attempts=%d", item.ID, item.State, item.AttemptCount)
		if item.ErrorClass != "" || item.ErrorMessage != "" {
			line += " · " + string(item.ErrorClass) + " · " + item.ErrorMessage
		}
		lines = append(lines, line)
	}
	return tui.OperationResult{Title: "Job " + string(job.ID), Fields: map[string]string{
		"kind": job.Kind, "state": string(job.State), "updated": job.UpdatedAt.Local().Format(time.RFC3339),
		"items": fmt.Sprint(len(items)), "execution lease": "stored by the persistent job engine; controls reject invalid ownership/state transitions",
	}, Lines: lines}, nil
}

func (extensions *workspaceExtensions) jobOperation(
	ctx context.Context,
	ids []string,
	operation func(context.Context, domain.JobID) (domain.Job, error),
	message string,
) (tui.OperationResult, error) {
	if len(ids) == 0 {
		return tui.OperationResult{}, errors.New("select a job first")
	}
	job, err := operation(ctx, domain.JobID(ids[0]))
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Job " + string(job.ID), Message: message, Fields: map[string]string{"state": string(job.State)}}, nil
}

func (extensions *workspaceExtensions) exportManifest(ctx context.Context, ids []string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	record, err := selectExportRecord(ctx, active, ids)
	if err != nil {
		return tui.OperationResult{}, err
	}
	files, err := active.Library.ListExportFiles(ctx, record.ID)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := make([]string, 0, len(files))
	for _, file := range files {
		lines = append(lines, fmt.Sprintf("%s · %d bytes · sha256:%s", file.RelativePath, file.SizeBytes, file.SHA256))
	}
	return tui.OperationResult{Title: "Export " + string(record.ID), Fields: map[string]string{
		"format": record.Format, "state": record.State, "output root": record.OutputRoot, "files": fmt.Sprint(len(files)),
	}, Lines: lines}, nil
}

func (extensions *workspaceExtensions) openExport(ctx context.Context, ids []string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	record, err := selectExportRecord(ctx, active, ids)
	if err != nil {
		return tui.OperationResult{}, err
	}
	if strings.TrimSpace(record.OutputRoot) == "" {
		return tui.OperationResult{}, errors.New("export output root is empty")
	}
	if err := extensions.app.openBrowser(ctx, "file://"+filepath.ToSlash(record.OutputRoot)); err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Export output opened", Message: record.OutputRoot}, nil
}

func selectExportRecord(ctx context.Context, active *ProfileRuntime, ids []string) (library.ExportRecord, error) {
	if len(ids) > 0 {
		if record, err := active.Library.GetExport(ctx, domain.ExportID(ids[0])); err == nil {
			return record, nil
		}
	}
	page, err := active.Library.QueryExports(ctx, 0, 1)
	if err != nil {
		return library.ExportRecord{}, err
	}
	if len(page.Items) == 0 {
		return library.ExportRecord{}, sql.ErrNoRows
	}
	return active.Library.GetExport(ctx, page.Items[0])
}

func (extensions *workspaceExtensions) backup(ctx context.Context) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	directory := filepath.Join(active.Profile.Paths.Data, "backups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return tui.OperationResult{}, err
	}
	path := filepath.Join(directory, "library-"+time.Now().UTC().Format("20060102T150405Z")+".zip")
	manifest, err := active.Library.CreateBackup(ctx, library.BackupOptions{
		Destination: path, ObjectStore: active.Objects, ConfigPath: active.Profile.Paths.Config,
	})
	if err != nil {
		return tui.OperationResult{}, err
	}
	verification, err := library.VerifyBackup(ctx, path)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Backup created", Fields: map[string]string{
		"path": path, "valid": fmt.Sprint(verification.Valid), "archive sha256": verification.ArchiveSHA,
		"objects": fmt.Sprint(len(manifest.Objects)), "bytes": fmt.Sprint(manifest.TotalBytes),
	}}, nil
}

func (extensions *workspaceExtensions) articleComments(ctx context.Context, ids []string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	articleIDs, err := requireArticleIDs(ids)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := []string{}
	for _, id := range articleIDs {
		comments, err := active.Library.CommentsForArticle(ctx, id)
		if err != nil {
			return tui.OperationResult{}, err
		}
		for _, comment := range comments {
			lines = append(lines, fmt.Sprintf("%s · %s · %d likes · %d replies", id, comment.AuthorName, comment.LikeCount, len(comment.EmbeddedReplies)))
		}
	}
	return tui.OperationResult{Title: "Local comments", Message: fmt.Sprintf("%d comment record(s)", len(lines)), Lines: lines}, nil
}

func (extensions *workspaceExtensions) articleMetrics(ctx context.Context, ids []string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	articleIDs, err := requireArticleIDs(ids)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := []string{}
	for _, id := range articleIDs {
		snapshot, err := active.Library.LatestMetricSnapshot(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			lines = append(lines, string(id)+" · no local metric snapshot")
			continue
		}
		if err != nil {
			return tui.OperationResult{}, err
		}
		lines = append(lines, fmt.Sprintf("%s · reads=%d likes=%d old-likes=%d shares=%d comments=%d · %s", id,
			snapshot.ReadCount, snapshot.LikeCount, snapshot.OldLikeCount, snapshot.ShareCount, snapshot.CommentCount,
			snapshot.CapturedAt.Local().Format(time.RFC3339)))
	}
	return tui.OperationResult{Title: "Local metrics", Lines: lines}, nil
}

func (extensions *workspaceExtensions) articleResources(ctx context.Context, ids []string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	articleIDs, err := requireArticleIDs(ids)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := []string{}
	for _, id := range articleIDs {
		mappings, err := active.Library.ListArticleResources(ctx, id)
		if err != nil {
			return tui.OperationResult{}, err
		}
		available, missing := 0, 0
		for _, mapping := range mappings {
			record, recordErr := active.Library.ResourceByURL(ctx, mapping.OriginalURL)
			if recordErr == nil && record.Status == "available" && record.ObjectDigest != "" {
				available++
			} else {
				missing++
			}
		}
		lines = append(lines, fmt.Sprintf("%s · available=%d missing=%d total=%d", id, available, missing, len(mappings)))
	}
	return tui.OperationResult{Title: "Resource completeness", Lines: lines}, nil
}

func requireArticleIDs(ids []string) ([]domain.ArticleID, error) {
	if len(ids) == 0 {
		return nil, errors.New("select at least one article")
	}
	articleIDs := make([]domain.ArticleID, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			articleIDs = append(articleIDs, domain.ArticleID(id))
		}
	}
	if len(articleIDs) == 0 {
		return nil, errors.New("select at least one article")
	}
	return articleIDs, nil
}

func writePrivateTextFile(path, value string) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(value), 0o600); err != nil {
		return fmt.Errorf("write local preview: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit local preview: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func displayPreferenceSummary(preferences profiles.DisplayPreferences) string {
	values := []string{}
	if preferences.NoColor {
		values = append(values, "no-color")
	}
	if preferences.ASCII {
		values = append(values, "ASCII")
	}
	if preferences.Plain {
		values = append(values, "plain")
	}
	if preferences.HideDeleted {
		values = append(values, "hide-deleted")
	}
	if len(values) == 0 {
		return "default"
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return "not set"
	}
	return value.Local().Format(time.RFC3339)
}
