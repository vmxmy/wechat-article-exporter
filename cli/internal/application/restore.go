package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var ErrRestoreConfirmationRequired = errors.New("restore confirmation is invalid or expired")

// RestoreConflictPolicy is intentionally a small application DTO rather than
// exposing the storage package's restore implementation to presentation code.
type RestoreConflictPolicy string

const (
	RestoreRefuseConflicts RestoreConflictPolicy = "refuse"
	RestoreRenameConflicts RestoreConflictPolicy = "rename"
)

type RestorePrepareRequest struct {
	UploadHandle   UploadHandle          `json:"uploadHandle"`
	ConflictPolicy RestoreConflictPolicy `json:"conflictPolicy"`
}

// RestorePreparation is a short-lived, one-time proof that binds a staged
// upload to its selected conflict policy. Neither field reveals a host path.
type RestorePreparation struct {
	ID             string                `json:"id"`
	Confirmation   string                `json:"confirmation"`
	ConflictPolicy RestoreConflictPolicy `json:"conflictPolicy"`
	ExpiresAt      time.Time             `json:"expiresAt"`
}

type RestoreCommitRequest struct {
	PreparationID string `json:"preparationId"`
	Confirmation  string `json:"confirmation"`
}

type RestoreCompletion struct {
	RestoredFiles int   `json:"restoredFiles"`
	RestoredBytes int64 `json:"restoredBytes"`
	Profiles      int   `json:"profiles"`
}

// RestoreCoordinator is the only application seam allowed to turn a consumed
// archive stream into a restore. Its app adapter owns all runtime replacement
// and filesystem details.
type RestoreCoordinator interface {
	Restore(context.Context, io.Reader, RestoreConflictPolicy) (RestoreCompletion, error)
}

type RestoreOptions struct {
	Uploads         *UploadStagingService
	Coordinator     RestoreCoordinator
	ConfirmationTTL time.Duration
	Now             func() time.Time
	NewID           func() (string, error)
	NewConfirmation func() (string, error)
}

type issuedRestorePreparation struct {
	uploadHandle UploadHandle
	confirmation string
	policy       RestoreConflictPolicy
	expiresAt    time.Time
}

// RestoreService coordinates an opaque staged upload and an exact one-time
// confirmation. A successful commit always consumes the upload before calling
// the coordinator, so it cannot be replayed after an ambiguous failure.
type RestoreService struct {
	uploads         *UploadStagingService
	coordinator     RestoreCoordinator
	confirmationTTL time.Duration
	now             func() time.Time
	newID           func() (string, error)
	newConfirmation func() (string, error)

	mu           sync.Mutex
	preparations map[string]issuedRestorePreparation
	reserved     map[UploadHandle]string
	closed       bool
}

func NewRestore(options RestoreOptions) (*RestoreService, error) {
	if options.Uploads == nil {
		return nil, errors.New("restore upload staging is required")
	}
	if options.Coordinator == nil {
		return nil, errors.New("restore coordinator is required")
	}
	if options.ConfirmationTTL <= 0 {
		options.ConfirmationTTL = 5 * time.Minute
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = newRestoreToken
	}
	if options.NewConfirmation == nil {
		options.NewConfirmation = newRestoreToken
	}
	return &RestoreService{uploads: options.Uploads, coordinator: options.Coordinator, confirmationTTL: options.ConfirmationTTL, now: options.Now,
		newID: options.NewID, newConfirmation: options.NewConfirmation, preparations: make(map[string]issuedRestorePreparation), reserved: make(map[UploadHandle]string)}, nil
}

func (service *RestoreService) Stage(ctx context.Context, source io.Reader, declaredSize int64) (UploadReceipt, error) {
	if service == nil || service.uploads == nil {
		return UploadReceipt{}, ErrUnavailable
	}
	return service.uploads.Stage(ctx, source, declaredSize)
}

func (service *RestoreService) Prepare(ctx context.Context, request RestorePrepareRequest) (RestorePreparation, error) {
	if service == nil || service.uploads == nil || service.coordinator == nil {
		return RestorePreparation{}, ErrUnavailable
	}
	if !validRestorePolicy(request.ConflictPolicy) || !validUploadHandle(request.UploadHandle) {
		return RestorePreparation{}, ErrRestoreConfirmationRequired
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return RestorePreparation{}, ErrUnavailable
	}
	service.discardExpiredLocked(ctx)
	service.mu.Unlock()
	if _, err := service.uploads.Receipt(ctx, request.UploadHandle); err != nil {
		return RestorePreparation{}, ErrRestoreConfirmationRequired
	}
	id, err := service.newID()
	if err != nil || !isOpaqueMaintenanceToken(id) {
		return RestorePreparation{}, errors.New("create restore preparation failed")
	}
	confirmation, err := service.newConfirmation()
	if err != nil || !isOpaqueMaintenanceToken(confirmation) {
		return RestorePreparation{}, errors.New("create restore confirmation failed")
	}
	expiresAt := service.now().Add(service.confirmationTTL)
	service.mu.Lock()
	if service.closed || service.reserved[request.UploadHandle] != "" {
		service.mu.Unlock()
		return RestorePreparation{}, ErrRestoreConfirmationRequired
	}
	service.preparations[id] = issuedRestorePreparation{uploadHandle: request.UploadHandle, confirmation: confirmation, policy: request.ConflictPolicy, expiresAt: expiresAt}
	service.reserved[request.UploadHandle] = id
	service.mu.Unlock()
	return RestorePreparation{ID: id, Confirmation: confirmation, ConflictPolicy: request.ConflictPolicy, ExpiresAt: expiresAt}, nil
}

// Discard removes an unprepared staged upload. It is intentionally exposed to
// presentation adapters solely for malformed multipart cleanup.
func (service *RestoreService) Discard(ctx context.Context, handle UploadHandle) error {
	if service == nil || service.uploads == nil {
		return ErrUnavailable
	}
	service.mu.Lock()
	if _, reserved := service.reserved[handle]; reserved {
		service.mu.Unlock()
		return ErrRestoreConfirmationRequired
	}
	service.mu.Unlock()
	return service.uploads.Discard(ctx, handle)
}

// Close abandons all outstanding restore confirmations and removes every
// unconsumed staged archive without exposing backend paths to its caller.
func (service *RestoreService) Close(ctx context.Context) error {
	if service == nil || service.uploads == nil {
		return ErrUnavailable
	}
	service.mu.Lock()
	service.closed = true
	service.preparations = make(map[string]issuedRestorePreparation)
	service.reserved = make(map[UploadHandle]string)
	service.mu.Unlock()
	return service.uploads.Close(ctx)
}

func (service *RestoreService) Commit(ctx context.Context, request RestoreCommitRequest) (RestoreCompletion, error) {
	if service == nil || service.uploads == nil || service.coordinator == nil {
		return RestoreCompletion{}, ErrUnavailable
	}
	if !isOpaqueMaintenanceToken(request.PreparationID) || !isOpaqueMaintenanceToken(request.Confirmation) {
		return RestoreCompletion{}, ErrRestoreConfirmationRequired
	}
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return RestoreCompletion{}, ErrUnavailable
	}
	issued, found := service.preparations[request.PreparationID]
	if found && (!issued.expiresAt.After(service.now()) || issued.confirmation != request.Confirmation) {
		if !issued.expiresAt.After(service.now()) {
			delete(service.preparations, request.PreparationID)
			delete(service.reserved, issued.uploadHandle)
		}
		found = false
	}
	if found {
		delete(service.preparations, request.PreparationID)
		delete(service.reserved, issued.uploadHandle)
	}
	service.mu.Unlock()
	if !found {
		return RestoreCompletion{}, ErrRestoreConfirmationRequired
	}
	reader, _, err := service.uploads.Consume(ctx, issued.uploadHandle)
	if err != nil {
		return RestoreCompletion{}, ErrRestoreConfirmationRequired
	}
	defer reader.Close()
	completion, err := service.coordinator.Restore(ctx, reader, issued.policy)
	if err != nil {
		return RestoreCompletion{}, fmt.Errorf("restore archive: %w", err)
	}
	return completion, nil
}

func (service *RestoreService) discardExpiredLocked(ctx context.Context) {
	now := service.now()
	for id, issued := range service.preparations {
		if now.Before(issued.expiresAt) {
			continue
		}
		delete(service.preparations, id)
		delete(service.reserved, issued.uploadHandle)
		_ = service.uploads.Discard(ctx, issued.uploadHandle)
	}
}

func validRestorePolicy(policy RestoreConflictPolicy) bool {
	return policy == RestoreRefuseConflicts || policy == RestoreRenameConflicts
}

func newRestoreToken() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "restore-" + hex.EncodeToString(value[:]), nil
}
