package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

var (
	absoluteMaintenancePath = regexp.MustCompile(`(^|[\s(\[{"'=,:])(?:/[^\s,;:)\]}\]"']+|[A-Za-z]:[\\/][^\s,;:)\]}\]"']+|\\\\[^\s,;:)\]}\]"']+)`)
	opaqueMaintenanceToken  = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._:-]{0,255}\z`)
)

// MaintenanceStorageService is the application-only facade for local storage
// maintenance. It accepts and returns only the typed DTOs below: profile
// runtimes, database connections, object stores, secret stores, and host paths
// cannot cross this boundary.
//
// Restore deliberately has no method here. It requires coordinated replacement
// of the active App runtime and is intentionally reserved for a later contract.
type MaintenanceStorageService struct {
	backups     BackupMaintenance
	integrity   IntegrityMaintenance
	garbage     GarbageCollectionMaintenance
	diagnostics DiagnosticsMaintenance
	now         func() time.Time

	mu            sync.Mutex
	issuedGCPlans map[string]issuedGarbageCollectionPlan
}

type MaintenanceStorageOptions struct {
	Backups     BackupMaintenance
	Integrity   IntegrityMaintenance
	Garbage     GarbageCollectionMaintenance
	Diagnostics DiagnosticsMaintenance
	Now         func() time.Time
}

func NewMaintenanceStorage(options MaintenanceStorageOptions) *MaintenanceStorageService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &MaintenanceStorageService{
		backups: options.Backups, integrity: options.Integrity, garbage: options.Garbage, diagnostics: options.Diagnostics,
		now: now, issuedGCPlans: make(map[string]issuedGarbageCollectionPlan),
	}
}

// BackupReceipt describes a completed backup without disclosing its location.
// ID is an opaque application-owned handle, not a filename or host path.
type BackupReceipt struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	SHA256    string    `json:"sha256"`
	Bytes     int64     `json:"bytes"`
	Objects   int       `json:"objects"`
	Omitted   []string  `json:"omitted,omitempty"`
}

type BackupVerification struct {
	BackupID string   `json:"backupId"`
	Valid    bool     `json:"valid"`
	SHA256   string   `json:"sha256,omitempty"`
	Failures []string `json:"failures,omitempty"`
}

// BackupMaintenance owns backup placement and archive access. The facade sees
// only opaque handles, never destinations or archive paths.
type BackupMaintenance interface {
	CreateBackup(context.Context) (BackupReceipt, error)
	VerifyBackup(context.Context, string) (BackupVerification, error)
	// OpenBackup consumes the opaque backup handle and returns its archive. The
	// implementation owns the archive location and enforces its short lifetime.
	OpenBackup(context.Context, string) (io.ReadCloser, error)
}

// backupMaintenanceCleanup is intentionally private to the application
// boundary. Local browser servers use it to remove unconsumed archive files on
// shutdown without exposing runtime paths to presentation adapters.
type backupMaintenanceCleanup interface {
	Close(context.Context) error
}

func (service *MaintenanceStorageService) CreateBackup(ctx context.Context) (BackupReceipt, error) {
	if service.backups == nil {
		return BackupReceipt{}, unavailableStorageMaintenance("create backup")
	}
	receipt, err := service.backups.CreateBackup(ctx)
	if err != nil {
		return BackupReceipt{}, storageMaintenanceFailure("create backup", err)
	}
	if strings.TrimSpace(receipt.ID) == "" {
		return BackupReceipt{}, errors.New("create backup returned no backup handle")
	}
	if !isOpaqueMaintenanceToken(receipt.ID) {
		return BackupReceipt{}, errors.New("create backup returned an unsafe backup handle")
	}
	return sanitizeBackupReceipt(receipt), nil
}

func (service *MaintenanceStorageService) VerifyBackup(ctx context.Context, backupID string) (BackupVerification, error) {
	if service.backups == nil {
		return BackupVerification{}, unavailableStorageMaintenance("verify backup")
	}
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return BackupVerification{}, errors.New("backup handle is required")
	}
	if !isOpaqueMaintenanceToken(backupID) {
		return BackupVerification{}, errors.New("backup handle is invalid")
	}
	verification, err := service.backups.VerifyBackup(ctx, backupID)
	if err != nil {
		return BackupVerification{}, storageMaintenanceFailure("verify backup", err)
	}
	verification.BackupID = backupID
	return sanitizeBackupVerification(verification), nil
}

// OpenBackup opens a one-shot archive through its opaque backup handle. The
// browser adapter receives only a stream; archive paths cannot cross this
// boundary. Implementations must consume the handle before returning the
// stream, so a retry cannot retrieve the same archive twice.
func (service *MaintenanceStorageService) OpenBackup(ctx context.Context, backupID string) (io.ReadCloser, error) {
	if service.backups == nil {
		return nil, unavailableStorageMaintenance("open backup archive")
	}
	backupID = strings.TrimSpace(backupID)
	if backupID == "" || !isOpaqueMaintenanceToken(backupID) {
		return nil, errors.New("backup handle is invalid")
	}
	archive, err := service.backups.OpenBackup(ctx, backupID)
	if err != nil {
		return nil, storageMaintenanceFailure("open backup archive", err)
	}
	if archive == nil {
		return nil, errors.New("open backup archive returned no archive")
	}
	return archive, nil
}

// Close discards any unconsumed backup archive artifacts held by the local
// maintenance implementation. It is a lifecycle hook, not a browser API.
func (service *MaintenanceStorageService) Close(ctx context.Context) error {
	if service == nil || service.backups == nil {
		return nil
	}
	cleanup, ok := service.backups.(backupMaintenanceCleanup)
	if !ok {
		return nil
	}
	if err := cleanup.Close(ctx); err != nil {
		return storageMaintenanceFailure("close backup archives", err)
	}
	return nil
}

type IntegrityIssue struct {
	Kind           string `json:"kind"`
	ArticleID      string `json:"articleId,omitempty"`
	ResourceID     string `json:"resourceId,omitempty"`
	ObjectDigest   string `json:"objectDigest,omitempty"`
	Message        string `json:"message"`
	Repairable     bool   `json:"repairable"`
	Recommendation string `json:"recommendation,omitempty"`
}

type IntegrityReport struct {
	CheckedAt time.Time        `json:"checkedAt"`
	Issues    []IntegrityIssue `json:"issues"`
}

type IntegrityMaintenance interface {
	CheckIntegrity(context.Context) (IntegrityReport, error)
}

func (service *MaintenanceStorageService) CheckIntegrity(ctx context.Context) (IntegrityReport, error) {
	if service.integrity == nil {
		return IntegrityReport{}, unavailableStorageMaintenance("check integrity")
	}
	report, err := service.integrity.CheckIntegrity(ctx)
	if err != nil {
		return IntegrityReport{}, storageMaintenanceFailure("check integrity", err)
	}
	return sanitizeIntegrityReport(report), nil
}

type StorageReclaimable struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// GarbageCollectionPlan intentionally contains aggregate counts only. Its ID
// and confirmation are opaque, operation-specific proof; candidate filenames
// and object-store paths never leave the maintenance implementation.
type GarbageCollectionPlan struct {
	ID               string             `json:"id"`
	GeneratedAt      time.Time          `json:"generatedAt"`
	ExpiresAt        time.Time          `json:"expiresAt,omitempty"`
	Unreferenced     StorageReclaimable `json:"unreferencedObjects"`
	Temporary        StorageReclaimable `json:"temporaryFiles"`
	ExpiredDebug     StorageReclaimable `json:"expiredDebugCaptures"`
	CompletedJobLogs StorageReclaimable `json:"completedJobLogs"`
	Confirmation     string             `json:"confirmation"`
}

type GarbageCollectionApplyRequest struct {
	PlanID       string `json:"planId"`
	Confirmation string `json:"confirmation"`
}

type GarbageCollectionResult struct {
	DeletedObjects       StorageReclaimable `json:"deletedObjects"`
	DeletedTemporary     StorageReclaimable `json:"deletedTemporaryFiles"`
	DeletedDebug         StorageReclaimable `json:"deletedDebugCaptures"`
	DeletedCompletedLogs StorageReclaimable `json:"deletedCompletedJobLogs"`
	Skipped              int                `json:"skipped"`
}

// GarbageCollectionMaintenance retains the sensitive plan candidates. Apply
// receives the opaque plan ID and its exact confirmation, allowing the
// implementation to bind deletion to the plan it generated.
type GarbageCollectionMaintenance interface {
	PlanGarbageCollection(context.Context) (GarbageCollectionPlan, error)
	ApplyGarbageCollection(context.Context, string, string) (GarbageCollectionResult, error)
}

type issuedGarbageCollectionPlan struct {
	confirmation string
	expiresAt    time.Time
}

func (service *MaintenanceStorageService) PlanGarbageCollection(ctx context.Context) (GarbageCollectionPlan, error) {
	if service.garbage == nil {
		return GarbageCollectionPlan{}, unavailableStorageMaintenance("plan garbage collection")
	}
	plan, err := service.garbage.PlanGarbageCollection(ctx)
	if err != nil {
		return GarbageCollectionPlan{}, storageMaintenanceFailure("plan garbage collection", err)
	}
	if strings.TrimSpace(plan.ID) == "" || plan.Confirmation == "" {
		return GarbageCollectionPlan{}, errors.New("garbage collection plan is missing its bound confirmation")
	}
	if !isOpaqueMaintenanceToken(plan.ID) || !isOpaqueMaintenanceToken(plan.Confirmation) {
		return GarbageCollectionPlan{}, errors.New("garbage collection plan contains an unsafe handle or confirmation")
	}
	if plan.ExpiresAt.IsZero() || !plan.ExpiresAt.After(service.now()) {
		return GarbageCollectionPlan{}, errors.New("garbage collection plan is expired or has no expiry")
	}
	service.mu.Lock()
	service.discardExpiredGCPlansLocked()
	service.issuedGCPlans[plan.ID] = issuedGarbageCollectionPlan{confirmation: plan.Confirmation, expiresAt: plan.ExpiresAt}
	service.mu.Unlock()
	return plan, nil
}

func (service *MaintenanceStorageService) ApplyGarbageCollection(ctx context.Context, request GarbageCollectionApplyRequest) (GarbageCollectionResult, error) {
	if service.garbage == nil {
		return GarbageCollectionResult{}, unavailableStorageMaintenance("apply garbage collection")
	}
	if !isOpaqueMaintenanceToken(request.PlanID) || !isOpaqueMaintenanceToken(request.Confirmation) {
		return GarbageCollectionResult{}, ErrMaintenanceConfirmationRequired
	}
	service.mu.Lock()
	issued, found := service.issuedGCPlans[request.PlanID]
	if found && !issued.expiresAt.After(service.now()) {
		delete(service.issuedGCPlans, request.PlanID)
		found = false
	}
	if found && request.Confirmation == issued.confirmation {
		// Consume before calling the destructive collaborator. A backend error can
		// be ambiguous or partial, so retries require a newly issued plan.
		delete(service.issuedGCPlans, request.PlanID)
	} else {
		found = false
	}
	service.mu.Unlock()
	if !found {
		return GarbageCollectionResult{}, ErrMaintenanceConfirmationRequired
	}
	result, err := service.garbage.ApplyGarbageCollection(ctx, request.PlanID, request.Confirmation)
	if err != nil {
		return GarbageCollectionResult{}, storageMaintenanceFailure("apply garbage collection", err)
	}
	return result, nil
}

func (service *MaintenanceStorageService) discardExpiredGCPlansLocked() {
	now := service.now()
	for id, issued := range service.issuedGCPlans {
		if !issued.expiresAt.After(now) {
			delete(service.issuedGCPlans, id)
		}
	}
}

type DiagnosticCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type DiagnosticsReport struct {
	CollectedAt time.Time         `json:"collectedAt"`
	Checks      []DiagnosticCheck `json:"checks"`
}

type DiagnosticsMaintenance interface {
	CollectDiagnostics(context.Context) (DiagnosticsReport, error)
}

func (service *MaintenanceStorageService) Diagnostics(ctx context.Context) (DiagnosticsReport, error) {
	if service.diagnostics == nil {
		return DiagnosticsReport{}, unavailableStorageMaintenance("collect diagnostics")
	}
	report, err := service.diagnostics.CollectDiagnostics(ctx)
	if err != nil {
		return DiagnosticsReport{}, storageMaintenanceFailure("collect diagnostics", err)
	}
	return sanitizeDiagnosticsReport(report), nil
}

func unavailableStorageMaintenance(operation string) error {
	return fmt.Errorf("%s: %w", operation, ErrUnavailable)
}

// storageMaintenanceFailure intentionally discards the backend cause: the
// maintenance boundary cannot expose a path- or secret-bearing error chain.
func storageMaintenanceFailure(operation string, cause error) error {
	_ = cause
	return maintenanceStorageError{operation: operation}
}

type maintenanceStorageError struct {
	operation string
}

func (err maintenanceStorageError) Error() string { return err.operation + " failed" }

func sanitizeBackupReceipt(receipt BackupReceipt) BackupReceipt {
	receipt.ID = sanitizeMaintenanceText(receipt.ID)
	receipt.SHA256 = sanitizeMaintenanceText(receipt.SHA256)
	receipt.Omitted = sanitizeMaintenanceStrings(receipt.Omitted)
	return receipt
}

func sanitizeBackupVerification(verification BackupVerification) BackupVerification {
	verification.BackupID = sanitizeMaintenanceText(verification.BackupID)
	verification.SHA256 = sanitizeMaintenanceText(verification.SHA256)
	verification.Failures = sanitizeMaintenanceStrings(verification.Failures)
	return verification
}

func sanitizeIntegrityReport(report IntegrityReport) IntegrityReport {
	issues := make([]IntegrityIssue, len(report.Issues))
	for index, issue := range report.Issues {
		issue.Kind = sanitizeMaintenanceText(issue.Kind)
		issue.ArticleID = sanitizeMaintenanceText(issue.ArticleID)
		issue.ResourceID = sanitizeMaintenanceText(issue.ResourceID)
		issue.ObjectDigest = sanitizeMaintenanceText(issue.ObjectDigest)
		issue.Message = sanitizeMaintenanceText(issue.Message)
		issue.Recommendation = sanitizeMaintenanceText(issue.Recommendation)
		issues[index] = issue
	}
	report.Issues = issues
	return report
}

func sanitizeDiagnosticsReport(report DiagnosticsReport) DiagnosticsReport {
	checks := make([]DiagnosticCheck, len(report.Checks))
	for index, check := range report.Checks {
		checks[index] = DiagnosticCheck{Name: sanitizeMaintenanceText(check.Name), Status: sanitizeMaintenanceText(check.Status), Summary: sanitizeMaintenanceText(check.Summary)}
	}
	report.Checks = checks
	return report
}

func sanitizeMaintenanceStrings(values []string) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = sanitizeMaintenanceText(value)
	}
	return result
}

func sanitizeMaintenanceText(value string) string {
	value = safety.RedactText(value)
	return absoluteMaintenancePath.ReplaceAllStringFunc(value, func(match string) string {
		prefix := match[:1]
		if prefix == " " || prefix == "(" || prefix == "\n" || prefix == "\t" {
			return prefix + "[REDACTED]"
		}
		return "[REDACTED]"
	})
}

func isOpaqueMaintenanceToken(value string) bool {
	return opaqueMaintenanceToken.MatchString(value)
}
