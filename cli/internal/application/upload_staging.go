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
	MaximumBytes      int64
	MaximumUploads    int
	MaximumTotalBytes int64
	TTL               time.Duration
}

var DefaultUploadStagingLimits = UploadStagingLimits{
	MaximumBytes:      2 << 30,
	MaximumUploads:    1,
	MaximumTotalBytes: 2 << 30,
	TTL:               15 * time.Minute,
}

func (limits UploadStagingLimits) normalized() UploadStagingLimits {
	if limits.MaximumBytes <= 0 {
		limits.MaximumBytes = DefaultUploadStagingLimits.MaximumBytes
	}
	if limits.MaximumUploads <= 0 {
		limits.MaximumUploads = DefaultUploadStagingLimits.MaximumUploads
	}
	if limits.MaximumTotalBytes <= 0 {
		limits.MaximumTotalBytes = DefaultUploadStagingLimits.MaximumTotalBytes
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
	sizeBytes int64
}

type pendingDeletion struct {
	object    UploadStagedObject
	sizeBytes int64
	inFlight  bool
}

// UploadStagingService manages bounded, expiring, one-shot staged uploads.
// The service owns capability validation and lifecycle; an app adapter owns
// the concrete temporary-file or object-store backend.
type UploadStagingService struct {
	backend UploadStagingBackend
	limits  UploadStagingLimits
	now     func() time.Time
	newID   func() (UploadHandle, error)

	mu        sync.Mutex
	uploads   map[UploadHandle]stagedUpload
	deletes   map[uint64]pendingDeletion
	nextID    uint64
	used      uploadStagingUsage
	pending   uploadStagingUsage
	closed    bool
	stages    int
	stageDone chan struct{}
	closeDone chan struct{}
	closeErr  error
}

type uploadStagingUsage struct {
	uploads int
	bytes   int64
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
	limits := options.Limits.normalized()
	if limits.MaximumTotalBytes < limits.MaximumBytes {
		return nil, errors.New("upload staging total byte limit must cover one upload")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	newID := options.NewID
	if newID == nil {
		newID = newUploadHandle
	}
	return &UploadStagingService{backend: options.Backend, limits: limits, now: now, newID: newID, uploads: make(map[UploadHandle]stagedUpload), deletes: make(map[uint64]pendingDeletion)}, nil
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
	if !service.beginStage() {
		return UploadReceipt{}, errors.New("upload staging is unavailable")
	}
	defer service.endStage()
	if err := service.PurgeExpired(ctx); err != nil {
		return UploadReceipt{}, err
	}
	// The declared size is client-controlled and is not a resource bound. Hold
	// the complete per-upload allowance until the streaming copy proves less.
	reservedBytes := service.limits.MaximumBytes
	if !service.reserve(reservedBytes) {
		return UploadReceipt{}, errors.New("upload staging capacity is exhausted")
	}
	hash := sha256.New()
	limited := &uploadSizeReader{source: source, maximum: service.limits.MaximumBytes}
	object, err := service.backend.Stage(ctx, io.TeeReader(limited, hash), declaredSize)
	if err != nil {
		service.releaseReservation(reservedBytes)
		if errors.Is(err, errUploadTooLarge) {
			return UploadReceipt{}, fmt.Errorf("upload exceeds %d bytes", service.limits.MaximumBytes)
		}
		return UploadReceipt{}, uploadStagingFailure("stage upload", err)
	}
	written := limited.written
	if declaredSize >= 0 && written != declaredSize {
		if err := service.deleteAfterReservation(context.Background(), object, reservedBytes, written); err != nil {
			return UploadReceipt{}, err
		}
		return UploadReceipt{}, errors.New("upload size does not match declared size")
	}
	handle, err := service.newID()
	if err != nil || !validUploadHandle(handle) {
		if cleanupErr := service.deleteAfterReservation(context.Background(), object, reservedBytes, written); cleanupErr != nil {
			return UploadReceipt{}, cleanupErr
		}
		return UploadReceipt{}, errors.New("create upload handle failed")
	}
	now := service.now()
	receipt := UploadReceipt{Handle: handle, SizeBytes: written, SHA256: hex.EncodeToString(hash.Sum(nil)), ExpiresAt: now.Add(service.limits.TTL)}
	service.mu.Lock()
	_, duplicate := service.uploads[handle]
	if service.closed || duplicate {
		service.pending.uploads--
		service.pending.bytes -= reservedBytes
		service.used.uploads++
		service.used.bytes += written
		service.enqueueDeletionLocked(object, written)
		deletions := service.takeReadyDeletionsLocked()
		service.mu.Unlock()
		_ = service.deleteObjects(context.Background(), deletions)
		return UploadReceipt{}, errors.New("create upload handle failed")
	}
	service.pending.uploads--
	service.pending.bytes -= reservedBytes
	service.used.uploads++
	service.used.bytes += written
	service.uploads[handle] = stagedUpload{object: object, receipt: receipt, expiresAt: receipt.ExpiresAt, sizeBytes: written}
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
	var deletions []uploadDeletion
	if exists && !service.now().Before(upload.expiresAt) {
		service.enqueueDeletionLocked(upload.object, upload.sizeBytes)
		deletions = service.takeReadyDeletionsLocked()
	}
	service.mu.Unlock()
	if !exists {
		return nil, UploadReceipt{}, errors.New("upload handle is unavailable")
	}
	if !service.now().Before(upload.expiresAt) {
		_ = service.deleteObjects(context.Background(), deletions)
		return nil, UploadReceipt{}, errors.New("upload handle has expired")
	}
	reader, err := service.backend.Open(ctx, upload.object)
	if err != nil {
		service.queueDeletion(context.Background(), upload.object, upload.sizeBytes)
		return nil, UploadReceipt{}, uploadStagingFailure("open upload", err)
	}
	return &consumedUpload{ReadCloser: reader, cleanup: func() { service.queueDeletion(context.Background(), upload.object, upload.sizeBytes) }}, upload.receipt, nil
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
		service.enqueueDeletionLocked(upload.object, upload.sizeBytes)
	}
	deletions := service.takeReadyDeletionsLocked()
	service.mu.Unlock()
	if expired {
		_ = service.deleteObjects(context.Background(), deletions)
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
		service.enqueueDeletionLocked(upload.object, upload.sizeBytes)
	}
	deletions := service.takeReadyDeletionsLocked()
	service.mu.Unlock()
	if !exists {
		return nil
	}
	if err := service.deleteObjects(ctx, deletions); err != nil {
		return uploadStagingFailure("discard upload", err)
	}
	return nil
}

// PurgeExpired removes retained uploads whose TTL has elapsed.
func (service *UploadStagingService) PurgeExpired(ctx context.Context) error {
	if service == nil || service.backend == nil {
		return errors.New("upload staging is unavailable")
	}
	service.mu.Lock()
	service.detachExpiredLocked(service.now())
	deletions := service.takeReadyDeletionsLocked()
	service.mu.Unlock()
	if err := service.deleteObjects(ctx, deletions); err != nil {
		return uploadStagingFailure("purge expired upload", err)
	}
	return nil
}

// Close removes every unconsumed staged upload. It is path-free so a server
// can invoke it during shutdown without learning any backend location.
func (service *UploadStagingService) Close(ctx context.Context) error {
	if service == nil || service.backend == nil {
		return errors.New("upload staging is unavailable")
	}
	service.mu.Lock()
	if service.closeDone != nil {
		done := service.closeDone
		service.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return uploadStagingFailure("close upload staging", ctx.Err())
		}
		service.mu.Lock()
		err := service.closeErr
		service.mu.Unlock()
		return err
	}
	service.closed = true
	service.closeDone = make(chan struct{})
	done := service.closeDone
	if service.stages > 0 {
		service.stageDone = make(chan struct{})
	}
	service.mu.Unlock()
	if service.stageDone != nil {
		select {
		case <-service.stageDone:
		case <-ctx.Done():
			service.finishClose(uploadStagingFailure("close upload staging", ctx.Err()), done)
			return uploadStagingFailure("close upload staging", ctx.Err())
		}
	}
	service.mu.Lock()
	for handle, upload := range service.uploads {
		delete(service.uploads, handle)
		service.enqueueDeletionLocked(upload.object, upload.sizeBytes)
	}
	deletions := service.takeReadyDeletionsLocked()
	service.mu.Unlock()
	if err := service.deleteObjects(ctx, deletions); err != nil {
		err = uploadStagingFailure("close upload staging", err)
		service.finishClose(err, done)
		return err
	}
	service.finishClose(nil, done)
	return nil
}

type uploadDeletion struct {
	id     uint64
	object UploadStagedObject
}

func (service *UploadStagingService) reserve(bytes int64) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed || service.used.uploads+service.pending.uploads >= service.limits.MaximumUploads || bytes > service.limits.MaximumTotalBytes-service.used.bytes-service.pending.bytes {
		return false
	}
	service.pending.uploads++
	service.pending.bytes += bytes
	return true
}

func (service *UploadStagingService) beginStage() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.closed {
		return false
	}
	service.stages++
	return true
}

func (service *UploadStagingService) endStage() {
	service.mu.Lock()
	service.stages--
	if service.closed && service.stages == 0 && service.stageDone != nil {
		close(service.stageDone)
	}
	service.mu.Unlock()
}

func (service *UploadStagingService) finishClose(err error, done chan struct{}) {
	service.mu.Lock()
	service.closeErr = err
	close(done)
	service.mu.Unlock()
}

func (service *UploadStagingService) releaseReservation(bytes int64) {
	service.mu.Lock()
	service.pending.uploads--
	service.pending.bytes -= bytes
	service.mu.Unlock()
}

func (service *UploadStagingService) deleteAfterReservation(ctx context.Context, object UploadStagedObject, reservedBytes, objectBytes int64) error {
	service.mu.Lock()
	service.pending.uploads--
	service.pending.bytes -= reservedBytes
	service.used.uploads++
	service.used.bytes += objectBytes
	service.enqueueDeletionLocked(object, objectBytes)
	deletions := service.takeReadyDeletionsLocked()
	service.mu.Unlock()
	return service.deleteObjects(ctx, deletions)
}

func (service *UploadStagingService) queueDeletion(ctx context.Context, object UploadStagedObject, sizeBytes int64) {
	service.mu.Lock()
	service.enqueueDeletionLocked(object, sizeBytes)
	deletions := service.takeReadyDeletionsLocked()
	service.mu.Unlock()
	_ = service.deleteObjects(ctx, deletions)
}

func (service *UploadStagingService) detachExpiredLocked(now time.Time) {
	for handle, upload := range service.uploads {
		if !now.Before(upload.expiresAt) {
			delete(service.uploads, handle)
			service.enqueueDeletionLocked(upload.object, upload.sizeBytes)
		}
	}
}

func (service *UploadStagingService) enqueueDeletionLocked(object UploadStagedObject, sizeBytes int64) {
	service.nextID++
	service.deletes[service.nextID] = pendingDeletion{object: object, sizeBytes: sizeBytes}
}

func (service *UploadStagingService) takeReadyDeletionsLocked() []uploadDeletion {
	deletions := make([]uploadDeletion, 0, len(service.deletes))
	for id, pending := range service.deletes {
		if pending.inFlight {
			continue
		}
		pending.inFlight = true
		service.deletes[id] = pending
		deletions = append(deletions, uploadDeletion{id: id, object: pending.object})
	}
	return deletions
}

func (service *UploadStagingService) deleteObjects(ctx context.Context, deletions []uploadDeletion) error {
	var result error
	for _, deletion := range deletions {
		err := service.backend.Delete(ctx, deletion.object)
		service.mu.Lock()
		pending, exists := service.deletes[deletion.id]
		if exists && err == nil {
			delete(service.deletes, deletion.id)
			service.used.uploads--
			service.used.bytes -= pending.sizeBytes
		} else if exists {
			pending.inFlight = false
			service.deletes[deletion.id] = pending
		}
		service.mu.Unlock()
		if err != nil && result == nil {
			result = err
		}
	}
	return result
}

var errUploadTooLarge = errors.New("upload exceeds configured maximum")

type uploadSizeReader struct {
	source  io.Reader
	maximum int64
	written int64
	checked bool
}

func (reader *uploadSizeReader) Read(buffer []byte) (int, error) {
	if reader.written < reader.maximum {
		remaining := reader.maximum - reader.written
		if int64(len(buffer)) > remaining {
			buffer = buffer[:remaining]
		}
		count, err := reader.source.Read(buffer)
		reader.written += int64(count)
		return count, err
	}
	if reader.checked {
		return 0, io.EOF
	}
	var probe [1]byte
	count, err := reader.source.Read(probe[:])
	if count > 0 {
		return 0, errUploadTooLarge
	}
	if err == nil {
		return 0, nil
	}
	if errors.Is(err, io.EOF) {
		reader.checked = true
	}
	return 0, err
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
