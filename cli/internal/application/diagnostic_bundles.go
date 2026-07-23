package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	defaultDiagnosticBundleTTL     = 15 * time.Minute
	maximumDiagnosticBundleHandles = 8
)

// DiagnosticBundleMaintenance retains the archive location and creates the
// redacted archive. The opaque reference never crosses into a web response.
type DiagnosticBundleMaintenance interface {
	CreateDiagnosticBundle(context.Context) (DiagnosticBundleArtifact, error)
	OpenDiagnosticBundle(context.Context, string) (io.ReadCloser, error)
	DiscardDiagnosticBundle(context.Context, string) error
}

// DiagnosticBundleArtifact is private collaboration data between the
// application facade and its local runtime adapter. Reference is deliberately
// excluded from serialization and must not be a browser-visible identifier.
type DiagnosticBundleArtifact struct {
	Reference string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"sizeBytes"`
}

// DiagnosticBundleReceipt is safe for presentation adapters. Handle is an
// expiring, single-use capability, not an archive name or filesystem path.
type DiagnosticBundleReceipt struct {
	Handle    string    `json:"handle"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"sizeBytes"`
}

type issuedDiagnosticBundle struct {
	artifact  DiagnosticBundleArtifact
	expiresAt time.Time
}

// DiagnosticBundleService owns browser-facing archive capabilities. Its
// collaborator remains responsible for private staging and archive contents.
type DiagnosticBundleService struct {
	maintenance DiagnosticBundleMaintenance
	ttl         time.Duration
	now         func() time.Time
	random      func([]byte) (int, error)

	mu      sync.Mutex
	bundles map[string]issuedDiagnosticBundle
}

type DiagnosticBundleOptions struct {
	Maintenance DiagnosticBundleMaintenance
	TTL         time.Duration
	Now         func() time.Time
	Random      func([]byte) (int, error)
}

func NewDiagnosticBundleService(options DiagnosticBundleOptions) *DiagnosticBundleService {
	ttl := options.TTL
	if ttl <= 0 {
		ttl = defaultDiagnosticBundleTTL
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Read
	}
	return &DiagnosticBundleService{maintenance: options.Maintenance, ttl: ttl, now: now, random: randomSource, bundles: make(map[string]issuedDiagnosticBundle)}
}

func (service *DiagnosticBundleService) Create(ctx context.Context) (DiagnosticBundleReceipt, error) {
	if service == nil || service.maintenance == nil {
		return DiagnosticBundleReceipt{}, unavailableDiagnosticBundle("create diagnostic bundle")
	}
	service.discardExpired(ctx)
	artifact, err := service.maintenance.CreateDiagnosticBundle(ctx)
	if err != nil {
		return DiagnosticBundleReceipt{}, diagnosticBundleFailure("create diagnostic bundle", err)
	}
	if strings.TrimSpace(artifact.Reference) == "" || artifact.SizeBytes < 0 || strings.TrimSpace(artifact.SHA256) == "" {
		_ = service.maintenance.DiscardDiagnosticBundle(context.Background(), artifact.Reference)
		return DiagnosticBundleReceipt{}, errors.New("create diagnostic bundle returned an incomplete archive")
	}
	handle, err := service.newHandle()
	if err != nil {
		_ = service.maintenance.DiscardDiagnosticBundle(context.Background(), artifact.Reference)
		return DiagnosticBundleReceipt{}, errors.New("create diagnostic bundle handle failed")
	}
	now := service.now()
	receipt := DiagnosticBundleReceipt{Handle: handle, CreatedAt: artifact.CreatedAt, ExpiresAt: now.Add(service.ttl), SHA256: sanitizeMaintenanceText(artifact.SHA256), SizeBytes: artifact.SizeBytes}
	service.mu.Lock()
	if len(service.bundles) >= maximumDiagnosticBundleHandles {
		oldestHandle, oldest := service.oldestLocked()
		delete(service.bundles, oldestHandle)
		service.mu.Unlock()
		_ = service.maintenance.DiscardDiagnosticBundle(context.Background(), oldest.artifact.Reference)
		service.mu.Lock()
	}
	service.bundles[handle] = issuedDiagnosticBundle{artifact: artifact, expiresAt: receipt.ExpiresAt}
	service.mu.Unlock()
	return receipt, nil
}

// Open consumes a handle before opening its file. The returned reader removes
// private staged bytes on Close, including after a partial client download.
func (service *DiagnosticBundleService) Open(ctx context.Context, handle string) (io.ReadCloser, DiagnosticBundleReceipt, error) {
	if service == nil || service.maintenance == nil {
		return nil, DiagnosticBundleReceipt{}, unavailableDiagnosticBundle("open diagnostic bundle")
	}
	if !isOpaqueMaintenanceToken(handle) || !strings.HasPrefix(handle, "diagnostic_") {
		return nil, DiagnosticBundleReceipt{}, errors.New("diagnostic bundle handle is invalid")
	}
	service.mu.Lock()
	bundle, found := service.bundles[handle]
	if found {
		delete(service.bundles, handle)
	}
	service.mu.Unlock()
	if !found {
		return nil, DiagnosticBundleReceipt{}, errors.New("diagnostic bundle handle is unavailable")
	}
	receipt := DiagnosticBundleReceipt{Handle: handle, CreatedAt: bundle.artifact.CreatedAt, ExpiresAt: bundle.expiresAt, SHA256: sanitizeMaintenanceText(bundle.artifact.SHA256), SizeBytes: bundle.artifact.SizeBytes}
	if !service.now().Before(bundle.expiresAt) {
		_ = service.maintenance.DiscardDiagnosticBundle(context.Background(), bundle.artifact.Reference)
		return nil, DiagnosticBundleReceipt{}, errors.New("diagnostic bundle handle has expired")
	}
	reader, err := service.maintenance.OpenDiagnosticBundle(ctx, bundle.artifact.Reference)
	if err != nil {
		_ = service.maintenance.DiscardDiagnosticBundle(context.Background(), bundle.artifact.Reference)
		return nil, DiagnosticBundleReceipt{}, diagnosticBundleFailure("open diagnostic bundle", err)
	}
	return &discardedDiagnosticBundle{ReadCloser: reader, discard: func() {
		_ = service.maintenance.DiscardDiagnosticBundle(context.Background(), bundle.artifact.Reference)
	}}, receipt, nil
}

func (service *DiagnosticBundleService) discardExpired(ctx context.Context) {
	if service == nil || service.maintenance == nil {
		return
	}
	now := service.now()
	service.mu.Lock()
	expired := make([]issuedDiagnosticBundle, 0)
	for handle, bundle := range service.bundles {
		if !now.Before(bundle.expiresAt) {
			delete(service.bundles, handle)
			expired = append(expired, bundle)
		}
	}
	service.mu.Unlock()
	for _, bundle := range expired {
		_ = service.maintenance.DiscardDiagnosticBundle(ctx, bundle.artifact.Reference)
	}
}

func (service *DiagnosticBundleService) oldestLocked() (string, issuedDiagnosticBundle) {
	var oldestHandle string
	var oldest issuedDiagnosticBundle
	for handle, bundle := range service.bundles {
		if oldestHandle == "" || bundle.expiresAt.Before(oldest.expiresAt) {
			oldestHandle, oldest = handle, bundle
		}
	}
	return oldestHandle, oldest
}

func (service *DiagnosticBundleService) newHandle() (string, error) {
	buffer := make([]byte, 24)
	if _, err := service.random(buffer); err != nil {
		return "", fmt.Errorf("read diagnostic bundle randomness: %w", err)
	}
	return "diagnostic_" + hex.EncodeToString(buffer), nil
}

func unavailableDiagnosticBundle(operation string) error {
	return fmt.Errorf("%s: %w", operation, ErrUnavailable)
}

func diagnosticBundleFailure(operation string, cause error) error {
	_ = cause
	return maintenanceStorageError{operation: operation}
}

type discardedDiagnosticBundle struct {
	io.ReadCloser
	once    sync.Once
	discard func()
}

func (reader *discardedDiagnosticBundle) Close() error {
	err := reader.ReadCloser.Close()
	reader.once.Do(reader.discard)
	return err
}
