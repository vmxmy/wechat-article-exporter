package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
)

const (
	webGarbageCollectionPlanTTL = 5 * time.Minute
	webBackupHandleTTL          = 30 * time.Minute
	webMaximumBackupHandles     = 32
	webObjectRetention          = 24 * time.Hour
	webTemporaryRetention       = 24 * time.Hour
	webDebugRetention           = 30 * 24 * time.Hour
	webJobLogRetention          = 30 * 24 * time.Hour
)

// newWebMaintenance constructs the local-runtime adapters that implement the
// application maintenance boundaries for the browser workspace. The adapters
// retain access to filesystem paths, stores, and secrets; the web package only
// receives the typed application facades.
func newWebMaintenance(app *App) (*application.MaintenanceService, *application.MaintenanceStorageService) {
	maintenance := &webMaintenanceAdapter{app: app}
	storage := &webMaintenanceStorageAdapter{app: app, backups: make(map[string]webBackupHandle), gcPlans: make(map[string]webGarbageCollectionPlan)}
	return application.NewMaintenance(application.MaintenanceOptions{
			Credentials: maintenance,
			Proxies:     maintenance,
			Preferences: maintenance,
		}), application.NewMaintenanceStorage(application.MaintenanceStorageOptions{
			Backups:     storage,
			Integrity:   storage,
			Garbage:     storage,
			Diagnostics: storage,
		})
}

type webMaintenanceAdapter struct{ app *App }

func (adapter *webMaintenanceAdapter) ListCredentialMetadata(ctx context.Context) ([]application.CredentialMetadata, error) {
	service, err := adapter.credentialService()
	if err != nil {
		return nil, err
	}
	items, err := service.Status(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]application.CredentialMetadata, len(items))
	for index, item := range items {
		result[index] = credentialMetadata(item)
	}
	return result, nil
}

func (adapter *webMaintenanceAdapter) ImportCredential(ctx context.Context, request application.CredentialImportRequest) (application.CredentialMetadata, error) {
	service, err := adapter.credentialService()
	if err != nil {
		return application.CredentialMetadata{}, err
	}
	metadata, err := service.Import(ctx, credentials.Record{
		Nickname: request.Nickname, Biz: request.Biz, UIN: request.UIN, Key: request.Key,
		PassTicket: request.PassTicket, WapSID2: request.WapSID2, AppMsgToken: request.AppMsgToken,
		Cookie: request.Cookie, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		return application.CredentialMetadata{}, err
	}
	return credentialMetadata(metadata), nil
}

func (adapter *webMaintenanceAdapter) RemoveCredential(ctx context.Context, id string) error {
	service, err := adapter.credentialService()
	if err != nil {
		return err
	}
	return service.Remove(ctx, id)
}

func (adapter *webMaintenanceAdapter) ListProxies(ctx context.Context) ([]application.ProxyRoute, error) {
	active, err := adapter.active()
	if err != nil {
		return nil, err
	}
	routes, err := active.Network.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]application.ProxyRoute, len(routes))
	for index, route := range routes {
		result[index] = proxyRoute(route)
	}
	return result, nil
}

func (adapter *webMaintenanceAdapter) AddProxy(ctx context.Context, request application.ProxyAddRequest) (application.ProxyRoute, error) {
	active, err := adapter.active()
	if err != nil {
		return application.ProxyRoute{}, err
	}
	route, err := active.Network.Add(ctx, network.AddProxyRequest{
		Name: request.Name, Endpoint: request.Endpoint, Authorization: request.Authorization,
		Trust: network.TrustLevel(request.Trust), Classes: proxyRequestClasses(request.Classes), Priority: request.Priority,
	})
	if err != nil {
		return application.ProxyRoute{}, err
	}
	return proxyRoute(route), nil
}

func (adapter *webMaintenanceAdapter) RemoveProxy(ctx context.Context, id string) (application.ProxyRoute, error) {
	active, err := adapter.active()
	if err != nil {
		return application.ProxyRoute{}, err
	}
	route, err := active.Network.Remove(ctx, id)
	return proxyRoute(route), err
}

func (adapter *webMaintenanceAdapter) SetProxyEnabled(ctx context.Context, id string, enabled bool) (application.ProxyRoute, error) {
	active, err := adapter.active()
	if err != nil {
		return application.ProxyRoute{}, err
	}
	var route network.RouteConfig
	if enabled {
		route, err = active.Network.Enable(ctx, id)
	} else {
		route, err = active.Network.Disable(ctx, id)
	}
	return proxyRoute(route), err
}

func (adapter *webMaintenanceAdapter) TestProxy(ctx context.Context, id string) (application.ProxyProbeResult, error) {
	active, err := adapter.active()
	if err != nil {
		return application.ProxyProbeResult{}, err
	}
	result, err := active.Network.Test(ctx, id)
	return application.ProxyProbeResult{
		Route: proxyRoute(result.Route), Latency: result.Latency, StatusCode: result.StatusCode,
		ResponseValid: result.ResponseValid, CredentialEligible: result.CredentialEligible, ErrorClass: result.ErrorClass,
	}, err
}

func (adapter *webMaintenanceAdapter) Preferences(ctx context.Context) (application.Preferences, error) {
	if err := ctx.Err(); err != nil {
		return application.Preferences{}, err
	}
	active, err := adapter.active()
	if err != nil {
		return application.Preferences{}, err
	}
	configuration, _, err := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
	if err != nil {
		return application.Preferences{}, err
	}
	return maintenancePreferences(configuration.Preferences), nil
}

func (adapter *webMaintenanceAdapter) PatchPreferences(ctx context.Context, patch application.PreferencesPatch) (application.Preferences, error) {
	if err := ctx.Err(); err != nil {
		return application.Preferences{}, err
	}
	active, err := adapter.active()
	if err != nil {
		return application.Preferences{}, err
	}
	effective, err := profiles.NewConfigStore(active.Profile.Paths.Config).Update(func(configuration *profiles.ProfileConfig) error {
		applyMaintenancePreferencesPatch(&configuration.Preferences, patch)
		return nil
	})
	if err != nil {
		return application.Preferences{}, err
	}
	return maintenancePreferences(effective.Preferences), nil
}

func (adapter *webMaintenanceAdapter) active() (*ProfileRuntime, error) {
	if adapter == nil || adapter.app == nil || adapter.app.active == nil || adapter.app.active.Network == nil {
		return nil, errors.New("active profile maintenance runtime is unavailable")
	}
	return adapter.app.active, nil
}

func (adapter *webMaintenanceAdapter) credentialService() (*credentials.Service, error) {
	if adapter == nil || adapter.app == nil {
		return nil, errors.New("active profile credential storage is unavailable")
	}
	return adapter.app.credentialService()
}

func credentialMetadata(item credentials.Metadata) application.CredentialMetadata {
	return application.CredentialMetadata{
		ID: item.ID, AccountID: string(item.AccountID), Kind: item.Kind, Status: string(item.Status),
		ValidatedAt: item.ValidatedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func proxyRoute(route network.RouteConfig) application.ProxyRoute {
	classes := make([]application.ProxyRequestClass, len(route.Classes))
	for index, class := range route.Classes {
		classes[index] = application.ProxyRequestClass(class)
	}
	return application.ProxyRoute{
		ID: route.ID, Name: route.Name, Endpoint: route.Endpoint, AuthorizationConfigured: route.AuthorizationConfigured,
		Trust: application.ProxyTrust(route.Trust), Classes: classes, Priority: route.Priority, Enabled: route.Enabled,
		Health: application.ProxyHealth{
			State: string(route.Health.State), ConsecutiveFailures: route.Health.ConsecutiveFailures,
			CooldownUntil: route.Health.CooldownUntil, LastSampleAt: route.Health.LastSampleAt,
			LastSuccessAt: route.Health.LastSuccessAt, LastLatency: route.Health.LastLatency,
			LastStatusCode: route.Health.LastStatusCode, LastErrorClass: route.Health.LastErrorClass,
		},
		CreatedAt: route.CreatedAt, UpdatedAt: route.UpdatedAt,
	}
}

func proxyRequestClasses(classes []application.ProxyRequestClass) []network.RequestClass {
	result := make([]network.RequestClass, len(classes))
	for index, class := range classes {
		result[index] = network.RequestClass(class)
	}
	return result
}

func maintenancePreferences(value profiles.Preferences) application.Preferences {
	return application.Preferences{
		Sync:     application.SyncPreferences{Range: value.Sync.Range, DatePoint: value.Sync.DatePoint, PageDelay: value.Sync.PageDelay, Jitter: value.Sync.Jitter, PageSize: value.Sync.PageSize, Incremental: value.Sync.Incremental, UnsafePacingSaved: value.Sync.UnsafePacingSaved},
		Download: application.DownloadPreferences{Concurrency: value.Download.Concurrency, ForceContent: value.Download.ForceContent, MetadataOverridesContent: value.Download.MetadataOverridesContent},
		Export:   application.ExportPreferences{NamingTemplate: value.Export.NamingTemplate, MaximumNameBytes: value.Export.MaximumNameBytes, CollisionPolicy: value.Export.CollisionPolicy, ExcelIncludeContent: value.Export.ExcelIncludeContent, JSONIncludeContent: value.Export.JSONIncludeContent, JSONIncludeComments: value.Export.JSONIncludeComments, HTMLIncludeComments: value.Export.HTMLIncludeComments},
		Display:  application.DisplayPreferences{NoColor: value.Display.NoColor, ASCII: value.Display.ASCII, Plain: value.Display.Plain, HideDeleted: value.Display.HideDeleted, Language: value.Display.Language},
		Proxy:    application.ProxyPreferences{DirectFirst: value.Proxy.DirectFirst, FallbackEnabled: value.Proxy.FallbackEnabled},
	}
}

func applyMaintenancePreferencesPatch(value *profiles.Preferences, patch application.PreferencesPatch) {
	if patch.Sync != nil {
		if patch.Sync.Range != nil {
			value.Sync.Range = *patch.Sync.Range
		}
		if patch.Sync.DatePoint != nil {
			value.Sync.DatePoint = *patch.Sync.DatePoint
		}
		if patch.Sync.PageDelay != nil {
			value.Sync.PageDelay = *patch.Sync.PageDelay
		}
		if patch.Sync.Jitter != nil {
			value.Sync.Jitter = *patch.Sync.Jitter
		}
		if patch.Sync.PageSize != nil {
			value.Sync.PageSize = *patch.Sync.PageSize
		}
		if patch.Sync.Incremental != nil {
			value.Sync.Incremental = *patch.Sync.Incremental
		}
		if patch.Sync.UnsafePacingSaved != nil {
			value.Sync.UnsafePacingSaved = *patch.Sync.UnsafePacingSaved
		}
	}
	if patch.Download != nil {
		if patch.Download.Concurrency != nil {
			value.Download.Concurrency = *patch.Download.Concurrency
		}
		if patch.Download.ForceContent != nil {
			value.Download.ForceContent = *patch.Download.ForceContent
		}
		if patch.Download.MetadataOverridesContent != nil {
			value.Download.MetadataOverridesContent = *patch.Download.MetadataOverridesContent
		}
	}
	if patch.Export != nil {
		if patch.Export.NamingTemplate != nil {
			value.Export.NamingTemplate = *patch.Export.NamingTemplate
		}
		if patch.Export.MaximumNameBytes != nil {
			value.Export.MaximumNameBytes = *patch.Export.MaximumNameBytes
		}
		if patch.Export.CollisionPolicy != nil {
			value.Export.CollisionPolicy = *patch.Export.CollisionPolicy
		}
		if patch.Export.ExcelIncludeContent != nil {
			value.Export.ExcelIncludeContent = *patch.Export.ExcelIncludeContent
		}
		if patch.Export.JSONIncludeContent != nil {
			value.Export.JSONIncludeContent = *patch.Export.JSONIncludeContent
		}
		if patch.Export.JSONIncludeComments != nil {
			value.Export.JSONIncludeComments = *patch.Export.JSONIncludeComments
		}
		if patch.Export.HTMLIncludeComments != nil {
			value.Export.HTMLIncludeComments = *patch.Export.HTMLIncludeComments
		}
	}
	if patch.Display != nil {
		if patch.Display.NoColor != nil {
			value.Display.NoColor = *patch.Display.NoColor
		}
		if patch.Display.ASCII != nil {
			value.Display.ASCII = *patch.Display.ASCII
		}
		if patch.Display.Plain != nil {
			value.Display.Plain = *patch.Display.Plain
		}
		if patch.Display.HideDeleted != nil {
			value.Display.HideDeleted = *patch.Display.HideDeleted
		}
		if patch.Display.Language != nil {
			value.Display.Language = *patch.Display.Language
		}
	}
	if patch.Proxy != nil {
		if patch.Proxy.DirectFirst != nil {
			value.Proxy.DirectFirst = *patch.Proxy.DirectFirst
		}
		if patch.Proxy.FallbackEnabled != nil {
			value.Proxy.FallbackEnabled = *patch.Proxy.FallbackEnabled
		}
	}
}

type webMaintenanceStorageAdapter struct {
	app *App

	mu      sync.Mutex
	backups map[string]webBackupHandle
	gcPlans map[string]webGarbageCollectionPlan
}

type webBackupHandle struct {
	path      string
	expiresAt time.Time
}

type webGarbageCollectionPlan struct {
	plan      library.GarbageCollectionPlan
	expiresAt time.Time
}

func (adapter *webMaintenanceStorageAdapter) CreateBackup(ctx context.Context) (application.BackupReceipt, error) {
	active, err := adapter.active()
	if err != nil {
		return application.BackupReceipt{}, err
	}
	id, err := webMaintenanceID("backup")
	if err != nil {
		return application.BackupReceipt{}, err
	}
	directory := filepath.Join(active.Profile.Paths.Data, "backups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return application.BackupReceipt{}, err
	}
	path := filepath.Join(directory, id+".zip")
	manifest, err := active.Library.CreateBackup(ctx, library.BackupOptions{Destination: path, ObjectStore: active.Objects, ConfigPath: active.Profile.Paths.Config})
	if err != nil {
		return application.BackupReceipt{}, err
	}
	verification, err := library.VerifyBackup(ctx, path)
	if err != nil {
		return application.BackupReceipt{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return application.BackupReceipt{}, err
	}
	adapter.mu.Lock()
	adapter.discardExpiredBackupsLocked()
	for len(adapter.backups) >= webMaximumBackupHandles {
		adapter.discardOldestBackupLocked()
	}
	adapter.backups[id] = webBackupHandle{path: path, expiresAt: time.Now().Add(webBackupHandleTTL)}
	adapter.mu.Unlock()
	return application.BackupReceipt{ID: id, CreatedAt: manifest.CreatedAt, SHA256: verification.ArchiveSHA, Bytes: info.Size(), Objects: len(manifest.Objects), Omitted: append([]string(nil), manifest.Omitted...)}, nil
}

func (adapter *webMaintenanceStorageAdapter) VerifyBackup(ctx context.Context, id string) (application.BackupVerification, error) {
	adapter.mu.Lock()
	adapter.discardExpiredBackupsLocked()
	handle, found := adapter.backups[id]
	adapter.mu.Unlock()
	if !found {
		return application.BackupVerification{}, errors.New("backup handle was not created by this workspace")
	}
	verification, err := library.VerifyBackup(ctx, handle.path)
	if err != nil {
		return application.BackupVerification{}, err
	}
	return application.BackupVerification{BackupID: id, Valid: verification.Valid, SHA256: verification.ArchiveSHA, Failures: append([]string(nil), verification.Failures...)}, nil
}

func (adapter *webMaintenanceStorageAdapter) CheckIntegrity(ctx context.Context) (application.IntegrityReport, error) {
	active, err := adapter.active()
	if err != nil {
		return application.IntegrityReport{}, err
	}
	report, err := active.Library.CheckIntegrity(ctx, active.Objects)
	if err != nil {
		return application.IntegrityReport{}, err
	}
	issues := make([]application.IntegrityIssue, len(report.Issues))
	for index, issue := range report.Issues {
		issues[index] = application.IntegrityIssue{Kind: issue.Kind, ArticleID: issue.ArticleID, ResourceID: issue.ResourceID, ObjectDigest: issue.ObjectDigest, Message: issue.Message, Repairable: issue.Repairable, Recommendation: issue.Recommendation}
	}
	return application.IntegrityReport{CheckedAt: report.CheckedAt, Issues: issues}, nil
}

func (adapter *webMaintenanceStorageAdapter) PlanGarbageCollection(ctx context.Context) (application.GarbageCollectionPlan, error) {
	active, err := adapter.active()
	if err != nil {
		return application.GarbageCollectionPlan{}, err
	}
	gate, err := profiles.AcquireMaintenanceGate(ctx, active.Profile.Paths)
	if err != nil {
		return application.GarbageCollectionPlan{}, err
	}
	defer gate.Close()
	blockers, err := active.Jobs.RestoreBlockers(ctx)
	if err != nil {
		return application.GarbageCollectionPlan{}, err
	}
	if len(blockers) > 0 {
		return application.GarbageCollectionPlan{}, fmt.Errorf("garbage collection blocked by active work: %w", profiles.ErrProfileBusy)
	}
	plan, err := active.Library.PlanGarbageCollection(ctx, webGarbageCollectionOptions(active))
	if err != nil {
		return application.GarbageCollectionPlan{}, err
	}
	id, err := webMaintenanceID("gc-plan")
	if err != nil {
		return application.GarbageCollectionPlan{}, err
	}
	expiresAt := time.Now().Add(webGarbageCollectionPlanTTL)
	adapter.mu.Lock()
	adapter.discardExpiredGCPlansLocked()
	adapter.gcPlans[id] = webGarbageCollectionPlan{plan: plan, expiresAt: expiresAt}
	adapter.mu.Unlock()
	return application.GarbageCollectionPlan{
		ID: id, GeneratedAt: plan.GeneratedAt, ExpiresAt: expiresAt, Confirmation: plan.Confirmation,
		Unreferenced:     application.StorageReclaimable{Count: plan.Objects.Unreferenced.Count, Bytes: plan.Objects.Unreferenced.Bytes},
		Temporary:        application.StorageReclaimable{Count: plan.Objects.Temporary.Count, Bytes: plan.Objects.Temporary.Bytes},
		ExpiredDebug:     application.StorageReclaimable{Count: plan.Metadata.ExpiredDebug.Count, Bytes: plan.Metadata.ExpiredDebug.Bytes},
		CompletedJobLogs: application.StorageReclaimable{Count: plan.Metadata.CompletedJobLogs.Count, Bytes: plan.Metadata.CompletedJobLogs.Bytes},
	}, nil
}

func (adapter *webMaintenanceStorageAdapter) ApplyGarbageCollection(ctx context.Context, id, confirmation string) (application.GarbageCollectionResult, error) {
	active, err := adapter.active()
	if err != nil {
		return application.GarbageCollectionResult{}, err
	}
	adapter.mu.Lock()
	stored, found := adapter.gcPlans[id]
	if found && !stored.expiresAt.After(time.Now()) {
		delete(adapter.gcPlans, id)
		found = false
	}
	if found {
		delete(adapter.gcPlans, id)
	}
	adapter.mu.Unlock()
	if !found || confirmation != stored.plan.Confirmation {
		return application.GarbageCollectionResult{}, application.ErrMaintenanceConfirmationRequired
	}
	gate, err := profiles.AcquireMaintenanceGate(ctx, active.Profile.Paths)
	if err != nil {
		return application.GarbageCollectionResult{}, err
	}
	defer gate.Close()
	blockers, err := active.Jobs.RestoreBlockers(ctx)
	if err != nil {
		return application.GarbageCollectionResult{}, err
	}
	if len(blockers) > 0 {
		return application.GarbageCollectionResult{}, fmt.Errorf("garbage collection blocked by active work: %w", profiles.ErrProfileBusy)
	}
	result, err := active.Library.ApplyGarbageCollection(ctx, webGarbageCollectionOptions(active), stored.plan, confirmation)
	if err != nil {
		return application.GarbageCollectionResult{}, err
	}
	return application.GarbageCollectionResult{
		DeletedObjects:       application.StorageReclaimable{Count: result.Objects.DeletedObjects.Count, Bytes: result.Objects.DeletedObjects.Bytes},
		DeletedTemporary:     application.StorageReclaimable{Count: result.Objects.DeletedTemporary.Count, Bytes: result.Objects.DeletedTemporary.Bytes},
		DeletedDebug:         application.StorageReclaimable{Count: result.DeletedDebug.Count, Bytes: result.DeletedDebug.Bytes},
		DeletedCompletedLogs: application.StorageReclaimable{Count: result.DeletedCompletedLogs.Count, Bytes: result.DeletedCompletedLogs.Bytes},
		Skipped:              len(result.Objects.Skipped),
	}, nil
}

func (adapter *webMaintenanceStorageAdapter) CollectDiagnostics(ctx context.Context) (application.DiagnosticsReport, error) {
	active, err := adapter.active()
	if err != nil {
		return application.DiagnosticsReport{}, err
	}
	checks := make([]application.DiagnosticCheck, 0, 3)
	if _, err := adapter.app.core.RuntimeStatus(ctx); err != nil {
		checks = append(checks, application.DiagnosticCheck{Name: "runtime", Status: "degraded", Summary: "runtime status is unavailable"})
	} else {
		checks = append(checks, application.DiagnosticCheck{Name: "runtime", Status: "ok", Summary: "runtime status is available"})
	}
	if _, err := adapter.app.core.SessionStatus(ctx); err != nil {
		checks = append(checks, application.DiagnosticCheck{Name: "session", Status: "degraded", Summary: "session status is unavailable"})
	} else {
		checks = append(checks, application.DiagnosticCheck{Name: "session", Status: "ok", Summary: "session status is available"})
	}
	report, integrityErr := active.Library.CheckIntegrity(ctx, active.Objects)
	if integrityErr != nil {
		checks = append(checks, application.DiagnosticCheck{Name: "storage", Status: "degraded", Summary: "storage integrity check failed"})
	} else if len(report.Issues) > 0 {
		checks = append(checks, application.DiagnosticCheck{Name: "storage", Status: "degraded", Summary: "storage integrity issues were found"})
	} else {
		checks = append(checks, application.DiagnosticCheck{Name: "storage", Status: "ok", Summary: "storage integrity check passed"})
	}
	return application.DiagnosticsReport{CollectedAt: time.Now(), Checks: checks}, nil
}

func (adapter *webMaintenanceStorageAdapter) active() (*ProfileRuntime, error) {
	if adapter == nil || adapter.app == nil || adapter.app.active == nil || adapter.app.active.Library == nil || adapter.app.active.Objects == nil || adapter.app.active.Jobs == nil {
		return nil, errors.New("active profile storage is unavailable")
	}
	return adapter.app.active, nil
}

func (adapter *webMaintenanceStorageAdapter) discardExpiredGCPlansLocked() {
	now := time.Now()
	for id, plan := range adapter.gcPlans {
		if !plan.expiresAt.After(now) {
			delete(adapter.gcPlans, id)
		}
	}
}

func (adapter *webMaintenanceStorageAdapter) discardExpiredBackupsLocked() {
	now := time.Now()
	for id, handle := range adapter.backups {
		if !handle.expiresAt.After(now) {
			delete(adapter.backups, id)
		}
	}
}

func (adapter *webMaintenanceStorageAdapter) discardOldestBackupLocked() {
	var oldestID string
	var oldestExpiry time.Time
	for id, handle := range adapter.backups {
		if oldestID == "" || handle.expiresAt.Before(oldestExpiry) {
			oldestID, oldestExpiry = id, handle.expiresAt
		}
	}
	if oldestID != "" {
		delete(adapter.backups, oldestID)
	}
}

func webGarbageCollectionOptions(active *ProfileRuntime) library.GarbageCollectionOptions {
	return library.GarbageCollectionOptions{
		ObjectStore: active.Objects, ObjectRetention: webObjectRetention, TemporaryRetention: webTemporaryRetention,
		DebugRetention: webDebugRetention, CompletedJobRetention: webJobLogRetention,
	}
}

func webMaintenanceID(prefix string) (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate maintenance handle: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(buffer), nil
}

var (
	_ application.CredentialMaintenance        = (*webMaintenanceAdapter)(nil)
	_ application.ProxyMaintenance             = (*webMaintenanceAdapter)(nil)
	_ application.PreferencesMaintenance       = (*webMaintenanceAdapter)(nil)
	_ application.BackupMaintenance            = (*webMaintenanceStorageAdapter)(nil)
	_ application.IntegrityMaintenance         = (*webMaintenanceStorageAdapter)(nil)
	_ application.GarbageCollectionMaintenance = (*webMaintenanceStorageAdapter)(nil)
	_ application.DiagnosticsMaintenance       = (*webMaintenanceStorageAdapter)(nil)
)
