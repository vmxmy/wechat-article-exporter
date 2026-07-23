package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// UploadHandle is an opaque, server-issued capability for one staged upload.
// It is not a filename, filesystem path, or client-selected identifier.
type UploadHandle string

// UploadStagingLimits bound a browser-supplied archive before it can reach a
// restore adapter. A zero value uses the conservative service defaults.
type UploadStagingLimits struct {
	MaximumBytes int64
	TTL          time.Duration
}

var DefaultUploadStagingLimits = UploadStagingLimits{
	MaximumBytes: 2 << 30,
	TTL:          15 * time.Minute,
}

func (limits UploadStagingLimits) normalized() UploadStagingLimits {
	if limits.MaximumBytes <= 0 {
		limits.MaximumBytes = DefaultUploadStagingLimits.MaximumBytes
	}
	if limits.TTL <= 0 {
		limits.TTL = DefaultUploadStagingLimits.TTL
	}
	return limits
}

// UploadStagingBackend owns private upload bytes. Stage must consume the full
// reader before it returns successfully. Its location and any backend-specific
// token are intentionally never returned by this package.
type UploadStagingBackend interface {
	Stage(context.Context, io.Reader, int64) (UploadStagedObject, error)
	Open(context.Context, UploadStagedObject) (io.ReadCloser, error)
	Delete(context.Context, UploadStagedObject) error
}

// UploadStagedObject is private to the application/backend seam. Adapters
// receive only UploadHandle and UploadReceipt.
type UploadStagedObject struct {
	Reference string
}

// UploadReceipt is safe for adapter DTOs: it exposes an opaque handle and a
// checksum, never a host path, original file name, or backend reference.
type UploadReceipt struct {
	Handle    UploadHandle `json:"handle"`
	SizeBytes int64        `json:"sizeBytes"`
	SHA256    string       `json:"sha256"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

type stagedUpload struct {
	object    UploadStagedObject
	receipt   UploadReceipt
	expiresAt time.Time
}

// UploadStagingService manages bounded, expiring, one-shot staged uploads.
// The service owns capability validation and lifecycle; an app adapter owns
// the concrete temporary-file or object-store backend.
type UploadStagingService struct {
	backend UploadStagingBackend
	limits  UploadStagingLimits
	now     func() time.Time
	newID   func() (UploadHandle, error)

	mu      sync.Mutex
	uploads map[UploadHandle]stagedUpload
}

type UploadStagingOptions struct {
	Backend UploadStagingBackend
	Limits  UploadStagingLimits
	Now     func() time.Time
	NewID   func() (UploadHandle, error)
}

func NewUploadStaging(options UploadStagingOptions) (*UploadStagingService, error) {
	if options.Backend == nil {
		return nil, errors.New("upload staging backend is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	newID := options.NewID
	if newID == nil {
		newID = newUploadHandle
	}
	return &UploadStagingService{backend: options.Backend, limits: options.Limits.normalized(), now: now, newID: newID, uploads: make(map[UploadHandle]stagedUpload)}, nil
}

// Stage copies exactly one bounded archive into the private backend and emits
// an opaque receipt. The declared size is optional; when present it must match
// the stream exactly.
func (service *UploadStagingService) Stage(ctx context.Context, source io.Reader, declaredSize int64) (UploadReceipt, error) {
	if service == nil || service.backend == nil {
		return UploadReceipt{}, errors.New("upload staging is unavailable")
	}
	if source == nil {
		return UploadReceipt{}, errors.New("upload source is required")
	}
	if declaredSize < -1 {
		return UploadReceipt{}, errors.New("upload size is invalid")
	}
	if declaredSize > service.limits.MaximumBytes {
		return UploadReceipt{}, fmt.Errorf("upload exceeds %d bytes", service.limits.MaximumBytes)
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: source, N: service.limits.MaximumBytes + 1}
	object, err := service.backend.Stage(ctx, io.TeeReader(limited, hash), declaredSize)
	if err != nil {
		return UploadReceipt{}, uploadStagingFailure("stage upload", err)
	}
	written := service.limits.MaximumBytes + 1 - limited.N
	if written > service.limits.MaximumBytes || (declaredSize >= 0 && written != declaredSize) {
		_ = service.backend.Delete(context.Background(), object)
		if written > service.limits.MaximumBytes {
			return UploadReceipt{}, fmt.Errorf("upload exceeds %d bytes", service.limits.MaximumBytes)
		}
		return UploadReceipt{}, errors.New("upload size does not match declared size")
	}
	handle, err := service.newID()
	if err != nil || !validUploadHandle(handle) {
		_ = service.backend.Delete(context.Background(), object)
		return UploadReceipt{}, errors.New("create upload handle failed")
	}
	now := service.now()
	receipt := UploadReceipt{Handle: handle, SizeBytes: written, SHA256: hex.EncodeToString(hash.Sum(nil)), ExpiresAt: now.Add(service.limits.TTL)}
	service.mu.Lock()
	service.uploads[handle] = stagedUpload{object: object, receipt: receipt, expiresAt: receipt.ExpiresAt}
	service.mu.Unlock()
	return receipt, nil
}

// Consume opens a staged archive once. Whether it succeeds, expires, or is
// invalid, the capability is removed so it cannot be replayed.
func (service *UploadStagingService) Consume(ctx context.Context, handle UploadHandle) (io.ReadCloser, UploadReceipt, error) {
	if service == nil || service.backend == nil {
		return nil, UploadReceipt{}, errors.New("upload staging is unavailable")
	}
	if !validUploadHandle(handle) {
		return nil, UploadReceipt{}, errors.New("upload handle is invalid")
	}
	service.mu.Lock()
	upload, exists := service.uploads[handle]
	if exists {
		delete(service.uploads, handle)
	}
	service.mu.Unlock()
	if !exists {
		return nil, UploadReceipt{}, errors.New("upload handle is unavailable")
	}
	if !service.now().Before(upload.expiresAt) {
		_ = service.backend.Delete(context.Background(), upload.object)
		return nil, UploadReceipt{}, errors.New("upload handle has expired")
	}
	reader, err := service.backend.Open(ctx, upload.object)
	if err != nil {
		_ = service.backend.Delete(context.Background(), upload.object)
		return nil, UploadReceipt{}, uploadStagingFailure("open upload", err)
	}
	return &consumedUpload{ReadCloser: reader, cleanup: func() { _ = service.backend.Delete(context.Background(), upload.object) }}, upload.receipt, nil
}

// Receipt validates that a staged upload is still available without consuming
// it. It is used only to bind a later confirmation; callers still need Consume
// to access bytes.
func (service *UploadStagingService) Receipt(_ context.Context, handle UploadHandle) (UploadReceipt, error) {
	if service == nil || service.backend == nil {
		return UploadReceipt{}, errors.New("upload staging is unavailable")
	}
	if !validUploadHandle(handle) {
		return UploadReceipt{}, errors.New("upload handle is invalid")
	}
	service.mu.Lock()
	upload, exists := service.uploads[handle]
	expired := exists && !service.now().Before(upload.expiresAt)
	if expired {
		delete(service.uploads, handle)
	}
	service.mu.Unlock()
	if expired {
		_ = service.backend.Delete(context.Background(), upload.object)
		exists = false
	}
	if !exists {
		return UploadReceipt{}, errors.New("upload handle is unavailable")
	}
	return upload.receipt, nil
}

// Discard removes an unconsumed upload. Unknown and already-consumed handles
// are deliberately idempotent to simplify adapter cleanup paths.
func (service *UploadStagingService) Discard(ctx context.Context, handle UploadHandle) error {
	if service == nil || service.backend == nil {
		return errors.New("upload staging is unavailable")
	}
	if !validUploadHandle(handle) {
		return errors.New("upload handle is invalid")
	}
	service.mu.Lock()
	upload, exists := service.uploads[handle]
	if exists {
		delete(service.uploads, handle)
	}
	service.mu.Unlock()
	if !exists {
		return nil
	}
	if err := service.backend.Delete(ctx, upload.object); err != nil {
		return uploadStagingFailure("discard upload", err)
	}
	return nil
}

// PurgeExpired removes retained uploads whose TTL has elapsed.
func (service *UploadStagingService) PurgeExpired(ctx context.Context) error {
	if service == nil || service.backend == nil {
		return errors.New("upload staging is unavailable")
	}
	now := service.now()
	service.mu.Lock()
	expired := make([]stagedUpload, 0)
	for handle, upload := range service.uploads {
		if !now.Before(upload.expiresAt) {
			delete(service.uploads, handle)
			expired = append(expired, upload)
		}
	}
	service.mu.Unlock()
	for _, upload := range expired {
		if err := service.backend.Delete(ctx, upload.object); err != nil {
			return uploadStagingFailure("purge expired upload", err)
		}
	}
	return nil
}

type consumedUpload struct {
	io.ReadCloser
	once    sync.Once
	cleanup func()
}

func (reader *consumedUpload) Close() error {
	err := reader.ReadCloser.Close()
	reader.once.Do(reader.cleanup)
	return err
}

func newUploadHandle() (UploadHandle, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return UploadHandle("upload-" + hex.EncodeToString(token[:])), nil
}

func validUploadHandle(handle UploadHandle) bool {
	value := string(handle)
	return strings.HasPrefix(value, "upload-") && len(value) <= 128 && isOpaqueMaintenanceToken(value)
}

func uploadStagingFailure(operation string, cause error) error {
	_ = cause
	return fmt.Errorf("%s failed", operation)
}
