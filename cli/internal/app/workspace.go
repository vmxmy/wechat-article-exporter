package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
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

// DefaultExportRoot uses the configured root when present. Otherwise the TUI
// offers a visible local Downloads path that needs no setup.
func (extensions *workspaceExtensions) DefaultExportRoot(context.Context) (string, error) {
	active, err := extensions.active()
	if err != nil {
		return "", err
	}
	configuration, _, err := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
	if err != nil {
		return "", err
	}
	if root := strings.TrimSpace(configuration.Preferences.Export.Root); root != "" {
		return root, nil
	}
	return "~/Downloads/wechat-article-exports", nil
}

func (extensions *workspaceExtensions) Panel(ctx context.Context, area tui.Area) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	switch area {
	case tui.AreaExports:
		page, err := active.Library.QueryExportRecords(ctx, 0, 10)
		if err != nil {
			return tui.OperationResult{}, err
		}
		lines := make([]string, 0, len(page.Items))
		for _, record := range page.Items {
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

func (extensions *workspaceExtensions) QueryExports(ctx context.Context, offset, limit int) (domain.Page[tui.ExportSummary], error) {
	active, err := extensions.active()
	if err != nil {
		return domain.Page[tui.ExportSummary]{}, err
	}
	page, err := active.Library.QueryExportRecords(ctx, offset, limit)
	if err != nil {
		return domain.Page[tui.ExportSummary]{}, err
	}
	items := make([]tui.ExportSummary, 0, len(page.Items))
	for _, record := range page.Items {
		var completedAt *time.Time
		if !record.CompletedAt.IsZero() {
			value := record.CompletedAt
			completedAt = &value
		}
		items = append(items, tui.ExportSummary{
			ID: record.ID, Format: record.Format, State: record.State, OutputRoot: record.OutputRoot,
			ProvenanceState: record.ProvenanceState, ProvenancePath: record.ProvenancePath,
			ProvenanceGeneration: record.ProvenanceGeneration, CreatedAt: record.CreatedAt, CompletedAt: completedAt,
		})
	}
	return domain.Page[tui.ExportSummary]{Items: items, Total: page.Total, Offset: page.Offset, Limit: page.Limit}, nil
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

// RenderArticlePreview keeps object-store reads and HTML rendering in the
// local runtime. The web adapter receives only the completed self-contained
// document through application.WorkspaceArticlePreviewRenderer.
func (extensions *workspaceExtensions) RenderArticlePreview(ctx context.Context, articleID domain.ArticleID) (application.WorkspaceRenderedArticlePreview, error) {
	active, err := extensions.active()
	if err != nil {
		return application.WorkspaceRenderedArticlePreview{}, err
	}
	article, normalized, comments, assets, err := loadWorkspaceArticle(ctx, active, articleID)
	if err != nil {
		return application.WorkspaceRenderedArticlePreview{}, err
	}
	rendered, err := processor.Render(normalized, processor.RenderOptions{
		ResourceMap: dataResourceMap(assets), ResourcePolicy: processor.ResourceRewriteStrict,
		IncludeComments: true, Comments: comments,
	})
	if err != nil {
		return application.WorkspaceRenderedArticlePreview{}, err
	}
	return application.WorkspaceRenderedArticlePreview{ArticleID: article.ID, HTML: []byte(stripPreviewStyleElements(rendered.HTML))}, nil
}

// The preview endpoint uses a CSP that disallows inline styles. Processor's
// general export HTML includes a convenience stylesheet, so remove it for the
// browser handoff rather than weakening the document policy.
func stripPreviewStyleElements(document string) string {
	for {
		start := strings.Index(strings.ToLower(document), "<style")
		if start < 0 {
			return document
		}
		openEnd := strings.Index(document[start:], ">")
		if openEnd < 0 {
			return document[:start]
		}
		endStart := strings.Index(strings.ToLower(document[start+openEnd+1:]), "</style>")
		if endStart < 0 {
			return document[:start] + document[start+openEnd+1:]
		}
		end := start + openEnd + 1 + endStart + len("</style>")
		document = document[:start] + document[end:]
	}
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
		return extensions.importAccounts(ctx, request.Parameters["path"])
	case tui.OperationAccountExport:
		manifest, err := extensions.app.core.ExportAccounts(ctx, domain.AccountQuery{})
		if err != nil {
			return tui.OperationResult{}, err
		}
		path := strings.TrimSpace(request.Parameters["path"])
		if path == "" {
			return tui.OperationResult{}, errors.New("account manifest output path is required")
		}
		if err := writePrivateJSONFile(path, manifest); err != nil {
			return tui.OperationResult{}, err
		}
		return tui.OperationResult{Title: "Account export manifest", Fields: map[string]string{
			"schema version": fmt.Sprint(manifest.SchemaVersion), "accounts": fmt.Sprint(len(manifest.Accounts)),
			"exported at": manifest.ExportedAt.Local().Format(time.RFC3339), "path": path,
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
		return extensions.jobOperation(ctx, request.IDs, nil, "queued for retry")
	case tui.OperationRouteHealth:
		return extensions.routeHealth(ctx)
	case tui.OperationExportManifest:
		return extensions.exportManifest(ctx, request.IDs)
	case tui.OperationExportVerify:
		return extensions.verifyExport(ctx, request.IDs)
	case tui.OperationExportConfig:
		return extensions.exportConfiguration(ctx)
	case tui.OperationExportStart:
		return extensions.startExport(ctx, request)
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
	case tui.OperationCredentialImport:
		return extensions.importCredential(ctx, request.Parameters["path"])
	case tui.OperationCredentialCheck:
		return extensions.validateCredential(ctx, request.Parameters["id"])
	case tui.OperationCredentialRemove:
		return extensions.removeCredential(ctx, request.Parameters["id"])
	case tui.OperationProxies:
		return extensions.routeHealth(ctx)
	case tui.OperationProxyAdd:
		return extensions.addProxy(ctx, request.Parameters)
	case tui.OperationProxyEnable:
		return extensions.proxyOperation(ctx, request.Parameters["id"], active.Network.Enable, "enabled")
	case tui.OperationProxyDisable:
		return extensions.proxyOperation(ctx, request.Parameters["id"], active.Network.Disable, "disabled")
	case tui.OperationProxyTest:
		return extensions.testProxy(ctx, request.Parameters["id"])
	case tui.OperationProxyRemove:
		return extensions.proxyOperation(ctx, request.Parameters["id"], active.Network.Remove, "removed")
	case tui.OperationPreferences:
		return extensions.preferences(ctx)
	case tui.OperationPreferenceSet:
		return extensions.setPreference(ctx, request.Parameters["key"], request.Parameters["value"])
	case tui.OperationBackup:
		return extensions.backup(ctx)
	case tui.OperationRestore:
		return extensions.restore(ctx, request.Parameters["path"], request.Parameters["conflict"])
	case tui.OperationIntegrity:
		report, err := active.Library.CheckIntegrity(ctx, active.Objects)
		if err != nil {
			return tui.OperationResult{}, err
		}
		return tui.OperationResult{Title: "Integrity check", Message: fmt.Sprintf("%d issue(s)", len(report.Issues)),
			Fields: map[string]string{"checked at": report.CheckedAt.Local().Format(time.RFC3339), "valid": fmt.Sprint(len(report.Issues) == 0)}}, nil
	case tui.OperationGarbageCollect:
		return extensions.garbageCollect(ctx, request.Parameters)
	case tui.OperationDiagnostics:
		return extensions.diagnostics(ctx)
	case tui.OperationDiagnosticBundle:
		return extensions.diagnosticBundle(ctx, request.Parameters["path"])
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

func (extensions *workspaceExtensions) credentialService() (*credentials.Service, error) {
	if extensions == nil || extensions.app == nil {
		return nil, errors.New("credential service is unavailable")
	}
	return extensions.app.credentialService()
}

func (extensions *workspaceExtensions) importCredential(ctx context.Context, path string) (tui.OperationResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return tui.OperationResult{}, errors.New("credential JSON path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return tui.OperationResult{}, err
	}
	defer file.Close()
	record, err := credentials.ParseJSON(file)
	if err != nil {
		return tui.OperationResult{}, err
	}
	service, err := extensions.credentialService()
	if err != nil {
		return tui.OperationResult{}, err
	}
	metadata, err := service.Import(ctx, record)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Credential imported", Fields: map[string]string{
		"id": metadata.ID, "account": string(metadata.AccountID), "kind": metadata.Kind, "status": string(metadata.Status),
	}}, nil
}

func (extensions *workspaceExtensions) validateCredential(ctx context.Context, id string) (tui.OperationResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tui.OperationResult{}, errors.New("credential ID is required")
	}
	service, err := extensions.credentialService()
	if err != nil {
		return tui.OperationResult{}, err
	}
	metadata, err := service.Validate(ctx, id)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Credential validated", Fields: map[string]string{
		"id": metadata.ID, "status": string(metadata.Status), "validated at": formatOptionalTime(metadata.ValidatedAt),
	}}, nil
}

func (extensions *workspaceExtensions) removeCredential(ctx context.Context, id string) (tui.OperationResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tui.OperationResult{}, errors.New("credential ID is required")
	}
	service, err := extensions.credentialService()
	if err != nil {
		return tui.OperationResult{}, err
	}
	if err := service.Remove(ctx, id); err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Credential removed", Fields: map[string]string{"id": id}}, nil
}

func (extensions *workspaceExtensions) addProxy(ctx context.Context, parameters map[string]string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	route, err := active.Network.Add(ctx, network.AddProxyRequest{
		Name: strings.TrimSpace(parameters["name"]), Endpoint: strings.TrimSpace(parameters["endpoint"]),
		Authorization: parameters["authorization"], Trust: network.TrustPublicOnly,
		Classes: []network.RequestClass{network.PublicContent}, Priority: 100,
	})
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Proxy added", Fields: map[string]string{
		"id": route.ID, "name": route.Name, "endpoint": route.Endpoint, "trust": string(route.Trust),
		"authorization configured": fmt.Sprint(route.AuthorizationConfigured),
	}}, nil
}

func (extensions *workspaceExtensions) proxyOperation(
	ctx context.Context,
	id string,
	operation func(context.Context, string) (network.RouteConfig, error),
	verb string,
) (tui.OperationResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tui.OperationResult{}, errors.New("proxy name or ID is required")
	}
	route, err := operation(ctx, id)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Proxy " + verb, Fields: map[string]string{
		"id": route.ID, "name": route.Name, "enabled": fmt.Sprint(route.Enabled),
	}}, nil
}

func (extensions *workspaceExtensions) testProxy(ctx context.Context, id string) (tui.OperationResult, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return tui.OperationResult{}, errors.New("proxy name or ID is required")
	}
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	probe, err := active.Network.Test(ctx, id)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Proxy test", Fields: map[string]string{
		"id": probe.Route.ID, "name": probe.Route.Name, "valid": fmt.Sprint(probe.ResponseValid),
		"latency": probe.Latency.String(), "status": fmt.Sprint(probe.StatusCode),
	}}, nil
}

func (extensions *workspaceExtensions) setPreference(ctx context.Context, key, value string) (tui.OperationResult, error) {
	if err := ctx.Err(); err != nil {
		return tui.OperationResult{}, err
	}
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if key == "" {
		return tui.OperationResult{}, errors.New("preference key is required")
	}
	effective, err := profiles.NewConfigStore(active.Profile.Paths.Config).Update(func(configuration *profiles.ProfileConfig) error {
		return updateWorkspacePreference(configuration, key, value)
	})
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Preference saved", Fields: map[string]string{
		"key": key, "value": value, "configuration": effective.Path,
	}, Message: "The saved value applies to new operations; restart the workspace for display/runtime composition changes."}, nil
}

func updateWorkspacePreference(configuration *profiles.ProfileConfig, key, value string) error {
	parseBool := func() (bool, error) {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s must be true or false", key)
		}
		return parsed, nil
	}
	switch key {
	case "export.root":
		configuration.Preferences.Export.Root = value
	case "export.naming-template":
		if value == "" {
			return errors.New("export.naming-template must not be empty")
		}
		configuration.Preferences.Export.NamingTemplate = value
	case "export.collision-policy":
		if value != "fail" && value != "suffix" {
			return errors.New("export.collision-policy must be fail or suffix")
		}
		configuration.Preferences.Export.CollisionPolicy = value
	case "download.concurrency":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 64 {
			return errors.New("download.concurrency must be an integer between 1 and 64")
		}
		configuration.Preferences.Download.Concurrency = parsed
	case "display.no-color":
		parsed, err := parseBool()
		if err != nil {
			return err
		}
		configuration.Preferences.Display.NoColor = parsed
	case "display.ascii":
		parsed, err := parseBool()
		if err != nil {
			return err
		}
		configuration.Preferences.Display.ASCII = parsed
	case "display.plain":
		parsed, err := parseBool()
		if err != nil {
			return err
		}
		configuration.Preferences.Display.Plain = parsed
	case "display.language":
		if value != "en" && value != "zh-CN" {
			return errors.New("display.language must be en or zh-CN")
		}
		configuration.Preferences.Display.Language = value
	case "proxy.direct-first":
		parsed, err := parseBool()
		if err != nil {
			return err
		}
		configuration.Preferences.Proxy.DirectFirst = parsed
	case "proxy.fallback-enabled":
		parsed, err := parseBool()
		if err != nil {
			return err
		}
		configuration.Preferences.Proxy.FallbackEnabled = parsed
	case "sync.page-delay":
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed < 3*time.Second {
			return errors.New("sync.page-delay must be at least 3s in the workspace; use Cobra with explicit risk confirmation for lower values")
		}
		configuration.Preferences.Sync.PageDelay = parsed
	default:
		return fmt.Errorf("unsupported workspace preference %q", key)
	}
	return nil
}

func (extensions *workspaceExtensions) garbageCollect(ctx context.Context, parameters map[string]string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	gate, err := profiles.AcquireMaintenanceGate(ctx, active.Profile.Paths)
	if err != nil {
		return tui.OperationResult{}, err
	}
	defer gate.Close()
	blockers, err := active.Jobs.RestoreBlockers(ctx)
	if err != nil {
		return tui.OperationResult{}, fmt.Errorf("check garbage-collection blockers: %w", err)
	}
	if len(blockers) > 0 {
		return tui.OperationResult{}, fmt.Errorf("garbage collection blocked by %d running job or active lease: %w", len(blockers), profiles.ErrProfileBusy)
	}
	options := library.GarbageCollectionOptions{
		ObjectStore: active.Objects, ObjectRetention: 24 * time.Hour, TemporaryRetention: 24 * time.Hour,
		DebugRetention: 30 * 24 * time.Hour, CompletedJobRetention: 30 * 24 * time.Hour,
	}
	plan, err := active.Library.PlanGarbageCollection(ctx, options)
	if err != nil {
		return tui.OperationResult{}, err
	}
	fields := map[string]string{
		"unreferenced objects": fmt.Sprint(plan.Objects.Unreferenced.Count), "temporary files": fmt.Sprint(plan.Objects.Temporary.Count),
		"expired debug captures": fmt.Sprint(plan.Metadata.ExpiredDebug.Count), "completed logs": fmt.Sprint(plan.Metadata.CompletedJobLogs.Count),
		"confirmation": plan.Confirmation,
	}
	if strings.TrimSpace(parameters["mode"]) != "apply" {
		return tui.OperationResult{Title: "Garbage collection dry run", Fields: fields,
			Message: "Copy the exact confirmation into Apply garbage collection to delete this plan."}, nil
	}
	confirmation := strings.TrimSpace(parameters["confirm"])
	if confirmation != plan.Confirmation {
		return tui.OperationResult{}, fmt.Errorf("garbage collection plan changed or confirmation mismatched; generate a new plan and use %q", plan.Confirmation)
	}
	result, err := active.Library.ApplyGarbageCollection(ctx, options, plan, confirmation)
	if err != nil {
		return tui.OperationResult{}, err
	}
	fields["deleted objects"] = fmt.Sprint(result.Objects.DeletedObjects.Count)
	fields["deleted temporary files"] = fmt.Sprint(result.Objects.DeletedTemporary.Count)
	fields["deleted debug captures"] = fmt.Sprint(result.DeletedDebug.Count)
	fields["deleted completed logs"] = fmt.Sprint(result.DeletedCompletedLogs.Count)
	return tui.OperationResult{Title: "Garbage collection complete", Fields: fields}, nil
}

func (extensions *workspaceExtensions) albumTraverse(ctx context.Context, request tui.OperationRequest) (tui.OperationResult, error) {
	if len(request.IDs) == 0 {
		return tui.OperationResult{}, errors.New("select an album first")
	}
	if len(request.IDs) > 50 {
		return tui.OperationResult{}, errors.New("select no more than 50 albums")
	}
	active, err := extensions.active()
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
	albumIDs := make([]domain.AlbumID, len(request.IDs))
	for index, id := range request.IDs {
		albumIDs[index] = domain.AlbumID(id)
	}
	job, err := runtime.StartAlbumsByIDWithOrder(ctx, albumIDs, wechat.AlbumOrder(order), downloadAfter)
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
		"job": string(job.ID), "albums": fmt.Sprint(len(albumIDs)), "order": order, "download after traversal": fmt.Sprint(downloadAfter),
	}}, nil
}

func (extensions *workspaceExtensions) importAccounts(ctx context.Context, path string) (tui.OperationResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return tui.OperationResult{}, errors.New("account manifest path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return tui.OperationResult{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest domain.AccountManifest
	if err := decodeSingleJSONValue(decoder, &manifest); err != nil {
		return tui.OperationResult{}, fmt.Errorf("decode account manifest: %w", err)
	}
	report, err := extensions.app.core.ImportAccounts(ctx, manifest)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Account import complete", Fields: map[string]string{
		"path": path, "added": fmt.Sprint(report.Added), "merged": fmt.Sprint(report.Merged), "unchanged": fmt.Sprint(report.Unchanged),
	}}, nil
}

func (extensions *workspaceExtensions) startExport(ctx context.Context, request tui.OperationRequest) (tui.OperationResult, error) {
	format := strings.ToLower(strings.TrimSpace(request.Parameters["format"]))
	outputRoot := strings.TrimSpace(request.Parameters["outputRoot"])
	if format == "" || outputRoot == "" {
		return tui.OperationResult{}, errors.New("export format and output directory are required")
	}
	selection := domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs}
	if request.Area == tui.AreaAlbums {
		selection = domain.ExportSelection{Kind: domain.ExportSelectionAlbumIDs, AlbumIDs: make([]domain.AlbumID, len(request.IDs))}
		for index, id := range request.IDs {
			selection.AlbumIDs[index] = domain.AlbumID(id)
		}
	} else {
		for _, id := range request.IDs {
			selection.ArticleIDs = append(selection.ArticleIDs, domain.ArticleID(id))
		}
	}
	if len(selection.ArticleIDs) == 0 && len(selection.AlbumIDs) == 0 {
		return tui.OperationResult{}, errors.New("select one or more articles or albums before starting an export")
	}
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	configuration, _, err := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
	if err != nil {
		return tui.OperationResult{}, err
	}
	preferences := configuration.Preferences.Export
	includeComments := false
	switch format {
	case "html":
		includeComments = preferences.HTMLIncludeComments
	case "json":
		includeComments = preferences.JSONIncludeComments
	}
	job, err := extensions.app.core.StartExport(ctx, domain.ExportRequest{
		Selection: selection, Format: format, OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: preferences.NamingTemplate, MaximumNameBytes: preferences.MaximumNameBytes,
			CollisionPolicy: preferences.CollisionPolicy, FormatOptions: map[string]any{
				"content": true, "metadata": true, "comments": includeComments,
				"htmlResourcePolicy": fallbackValue(request.Parameters["htmlResourcePolicy"], "best-effort"),
				"htmlBatchArchive":   request.Parameters["htmlBatchArchive"],
			}},
	})
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Export queued", Fields: map[string]string{
		"job": string(job.ID), "format": format, "output": outputRoot, "state": string(job.State),
	}}, nil
}

func (extensions *workspaceExtensions) restore(ctx context.Context, archivePath, conflict string) (tui.OperationResult, error) {
	archivePath = strings.TrimSpace(archivePath)
	if archivePath == "" {
		return tui.OperationResult{}, errors.New("backup archive path is required")
	}
	if conflict == "" {
		conflict = string(library.RestoreRefuseConflicts)
	}
	if conflict != string(library.RestoreRefuseConflicts) && conflict != string(library.RestoreRenameConflicts) {
		return tui.OperationResult{}, errors.New("restore conflict policy must be refuse or rename")
	}
	report, err := extensions.app.restoreActiveProfile(ctx, archivePath, library.RestoreConflictPolicy(conflict))
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{Title: "Restore complete", Fields: map[string]string{
		"archive": archivePath, "files": fmt.Sprint(report.RestoredFiles), "bytes": fmt.Sprint(report.RestoredBytes),
		"profiles": fmt.Sprint(len(report.Profiles)), "conflict policy": conflict,
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
	}, Message: "Select articles or albums before starting an export. Cobra exposes explicit format/output flags for automation."}, nil
}

func (extensions *workspaceExtensions) verifyExport(ctx context.Context, ids []string) (tui.OperationResult, error) {
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	record, err := selectExportRecord(ctx, active, ids)
	if err != nil {
		return tui.OperationResult{}, err
	}
	if strings.TrimSpace(record.ProvenancePath) == "" {
		return tui.OperationResult{}, fmt.Errorf("export %s has no ready provenance manifest", record.ID)
	}
	report, err := exporter.VerifyProvenanceManifest(ctx, record.OutputRoot, record.ProvenancePath)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := make([]string, 0, len(report.Issues))
	for _, issue := range report.Issues {
		article := fallbackValue(string(issue.ArticleID), "batch")
		line := fmt.Sprintf("%s · %s · article=%s", issue.Kind, fallbackValue(issue.Path, "manifest"), article)
		if issue.Expected != "" || issue.Actual != "" {
			line += fmt.Sprintf(" · expected=%s · actual=%s", fallbackValue(issue.Expected, "n/a"), fallbackValue(issue.Actual, "n/a"))
		}
		if issue.Message != "" {
			line += " · " + issue.Message
		}
		lines = append(lines, line)
	}
	if len(report.AffectedArticleIDs) > 0 {
		ids := make([]string, len(report.AffectedArticleIDs))
		for index, id := range report.AffectedArticleIDs {
			ids[index] = string(id)
		}
		lines = append(lines, "affected article IDs · "+strings.Join(ids, ", "))
	}
	return tui.OperationResult{Title: "Export verification", Fields: map[string]string{
		"export": string(record.ID), "valid": fmt.Sprint(report.Valid),
		"verified outputs": fmt.Sprint(report.VerifiedOutputs), "issues": fmt.Sprint(len(report.Issues)),
		"affected articles": fmt.Sprint(len(report.AffectedArticleIDs)),
		"manifest":          filepath.Join(record.OutputRoot, record.ProvenancePath),
	}, Lines: lines}, nil
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

func (extensions *workspaceExtensions) diagnosticBundle(ctx context.Context, path string) (tui.OperationResult, error) {
	report, err := extensions.app.createDiagnosticBundle(ctx, path)
	if err != nil {
		return tui.OperationResult{}, err
	}
	return tui.OperationResult{
		Title:   "Diagnostic bundle created",
		Message: "Private archive created without article bodies or secret-store bytes.",
		Fields: map[string]string{
			"path": report.Path, "sha256": report.SHA256, "bytes": fmt.Sprint(report.Bytes),
		},
		Lines: []string{
			"Included: " + strings.Join(report.Included, ", "),
			"Omitted: " + strings.Join(report.Omitted, ", "),
		},
	}, nil
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
	logs, err := active.Jobs.ListLogs(ctx, id, 100)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lease, err := active.Jobs.Lease(ctx, id)
	if err != nil {
		return tui.OperationResult{}, err
	}
	lines := make([]string, 0, len(items)+len(logs))
	for _, item := range items {
		line := fmt.Sprintf("%s · %s · attempts=%d", item.ID, item.State, item.AttemptCount)
		if item.ErrorClass != "" || item.ErrorMessage != "" {
			line += " · " + string(item.ErrorClass) + " · " + item.ErrorMessage
		}
		lines = append(lines, line)
	}
	for _, entry := range logs {
		lines = append(lines, fmt.Sprintf("log · %s · %s · %s", entry.CreatedAt.Local().Format(time.RFC3339), entry.Level, entry.Message))
	}
	leaseSummary := "none"
	if lease.Owner != "" {
		leaseSummary = fmt.Sprintf("owner=%s expires=%s active=%t", lease.Owner, formatOptionalTime(lease.ExpiresAt), lease.Active)
	}
	return tui.OperationResult{Title: "Job " + string(job.ID), Fields: map[string]string{
		"kind": job.Kind, "state": string(job.State), "updated": job.UpdatedAt.Local().Format(time.RFC3339),
		"items": fmt.Sprint(len(items)), "logs": fmt.Sprint(len(logs)), "execution lease": leaseSummary,
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
	active, err := extensions.active()
	if err != nil {
		return tui.OperationResult{}, err
	}
	current, err := active.Jobs.Get(ctx, domain.JobID(ids[0]))
	if err != nil {
		return tui.OperationResult{}, err
	}
	lease, err := active.Jobs.Lease(ctx, current.ID)
	if err != nil {
		return tui.OperationResult{}, err
	}
	if lease.Active {
		return tui.OperationResult{}, fmt.Errorf("job %s is owned by %s until %s; control is unavailable while the execution lease is active",
			current.ID, lease.Owner, lease.ExpiresAt.Local().Format(time.RFC3339))
	}
	var job domain.Job
	if operation == nil {
		if current.Kind == "export" {
			job, err = active.Jobs.RetryExport(ctx, current.ID)
		} else {
			job, err = active.Jobs.Retry(ctx, current.ID)
		}
	} else {
		job, err = operation(ctx, current.ID)
	}
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
		"provenance state":      fallbackValue(record.ProvenanceState, "pending"),
		"provenance path":       fallbackValue(record.ProvenancePath, "not written"),
		"provenance sha256":     fallbackValue(record.ProvenanceSHA256, "not available"),
		"provenance error":      fallbackValue(record.ProvenanceError, "none"),
		"provenance generation": fmt.Sprint(record.ProvenanceGeneration),
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
	if len(ids) == 0 || strings.TrimSpace(ids[0]) == "" {
		return library.ExportRecord{}, errors.New("select an export record first")
	}
	return active.Library.GetExport(ctx, domain.ExportID(ids[0]))
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create local preview directory: %w", err)
	}
	temporaryFile, err := createPrivateTemp(filepath.Dir(path), ".preview-*.tmp")
	if err != nil {
		return fmt.Errorf("create local preview: %w", err)
	}
	temporary := temporaryFile.Name()
	committed := false
	defer func() {
		_ = temporaryFile.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := temporaryFile.WriteString(value); err != nil {
		return fmt.Errorf("write local preview: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		return fmt.Errorf("sync local preview: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close local preview: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("commit local preview: %w", err)
	}
	committed = true
	return nil
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
	if preferences.Language != "" {
		values = append(values, "language="+preferences.Language)
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
