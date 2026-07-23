package application

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUploadStagingStagesOpaqueReceiptAndConsumesOnce(t *testing.T) {
	backend := &memoryUploadBackend{}
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	service, err := NewUploadStaging(UploadStagingOptions{Backend: backend, Now: func() time.Time { return now }, NewID: func() (UploadHandle, error) { return "upload-opaque-1", nil }})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Stage(context.Background(), strings.NewReader("archive bytes"), int64(len("archive bytes")))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Handle != "upload-opaque-1" || receipt.SizeBytes != int64(len("archive bytes")) || len(receipt.SHA256) != 64 || !receipt.ExpiresAt.Equal(now.Add(DefaultUploadStagingLimits.TTL)) {
		t.Fatalf("receipt=%#v", receipt)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/private/staging/archive.wab", "upload-secret", "originalName", "reference"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("receipt leaked %q: %s", forbidden, encoded)
		}
	}
	reader, consumed, err := service.Consume(context.Background(), receipt.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != receipt {
		t.Fatalf("consumed receipt=%#v, want %#v", consumed, receipt)
	}
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "archive bytes" {
		t.Fatalf("contents=%q", contents)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Consume(context.Background(), receipt.Handle); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("second Consume() error=%v", err)
	}
	if backend.deleteCount != 1 {
		t.Fatalf("delete count=%d, want 1", backend.deleteCount)
	}
}

func TestUploadStagingEnforcesLimitsAndExpiresWithoutLeakingBackendErrors(t *testing.T) {
	backend := &memoryUploadBackend{}
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	service, err := NewUploadStaging(UploadStagingOptions{Backend: backend, Limits: UploadStagingLimits{MaximumBytes: 3, TTL: time.Minute}, Now: func() time.Time { return now }, NewID: func() (UploadHandle, error) { return "upload-opaque-2", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Stage(context.Background(), strings.NewReader("four"), -1); err == nil || !strings.Contains(err.Error(), "exceeds 3 bytes") {
		t.Fatalf("oversized Stage() error=%v", err)
	}
	backend.stageErr = errors.New("write /private/staging/upload-secret.wab failed")
	if _, err := service.Stage(context.Background(), strings.NewReader("ok"), 2); err == nil || strings.Contains(err.Error(), "/private/") || strings.Contains(err.Error(), "upload-secret") {
		t.Fatalf("backend Stage() error=%v", err)
	}

	backend.stageErr = nil
	receipt, err := service.Stage(context.Background(), strings.NewReader("ok"), 2)
	if err != nil {
		t.Fatal(err)
	}
	// The preceding oversize input was staged through the bounded reader and
	// then cleaned up. Measure expiry cleanup independently.
	backend.deleteCount = 0
	now = now.Add(time.Minute)
	if _, _, err := service.Consume(context.Background(), receipt.Handle); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired Consume() error=%v", err)
	}
	if backend.deleteCount != 1 {
		t.Fatalf("expired delete count=%d, want 1", backend.deleteCount)
	}
}

func TestUploadStagingPreStageReclaimsExpiryAndHonorsAggregateQuota(t *testing.T) {
	backend := &memoryUploadBackend{}
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	ids := []UploadHandle{"upload-quota-1", "upload-quota-2"}
	service, err := NewUploadStaging(UploadStagingOptions{
		Backend: backend,
		Limits:  UploadStagingLimits{MaximumBytes: 4, MaximumUploads: 1, MaximumTotalBytes: 4, TTL: time.Minute},
		Now:     func() time.Time { return now },
		NewID: func() (UploadHandle, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Stage(context.Background(), strings.NewReader("four"), 4); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Stage(context.Background(), strings.NewReader("next"), 4); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("aggregate quota Stage() error=%v", err)
	}
	now = now.Add(time.Minute)
	if _, err := service.Stage(context.Background(), strings.NewReader("next"), 4); err != nil {
		t.Fatalf("Stage() after pre-stage expiry reclamation: %v", err)
	}
	if backend.deleteCount != 1 {
		t.Fatalf("expired upload delete count=%d, want 1", backend.deleteCount)
	}
}

func TestUploadStagingCloseDeletesAllUploadsWithoutHoldingLock(t *testing.T) {
	backend := &memoryUploadBackend{deleteStarted: make(chan struct{}), allowDelete: make(chan struct{})}
	service, err := NewUploadStaging(UploadStagingOptions{
		Backend: backend,
		Limits:  UploadStagingLimits{MaximumBytes: 4, MaximumUploads: 2, MaximumTotalBytes: 8},
		NewID:   uploadIDs("upload-close-1", "upload-close-2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, archive := range []string{"one", "two"} {
		if _, err := service.Stage(context.Background(), strings.NewReader(archive), int64(len(archive))); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- service.Close(context.Background()) }()
	<-backend.deleteStarted
	// The cleanup call is blocked in backend deletion. A concurrent operation
	// must still acquire the staging lock rather than being serialized behind I/O.
	receiptDone := make(chan struct{})
	go func() {
		_, _ = service.Receipt(context.Background(), "upload-close-1")
		close(receiptDone)
	}()
	select {
	case <-receiptDone:
	case <-time.After(time.Second):
		t.Fatal("Receipt blocked behind backend deletion")
	}
	close(backend.allowDelete)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if backend.deleteCount != 2 {
		t.Fatalf("close delete count=%d, want 2", backend.deleteCount)
	}
}

func uploadIDs(ids ...UploadHandle) func() (UploadHandle, error) {
	return func() (UploadHandle, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
}

type memoryUploadBackend struct {
	mu            sync.Mutex
	data          map[string][]byte
	stageErr      error
	deleteCount   int
	deleteStarted chan struct{}
	allowDelete   chan struct{}
}

func (backend *memoryUploadBackend) Stage(_ context.Context, source io.Reader, _ int64) (UploadStagedObject, error) {
	if backend.stageErr != nil {
		return UploadStagedObject{}, backend.stageErr
	}
	contents, err := io.ReadAll(source)
	if err != nil {
		return UploadStagedObject{}, err
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.data == nil {
		backend.data = map[string][]byte{}
	}
	backend.data["/private/staging/archive.wab"] = contents
	return UploadStagedObject{Reference: "/private/staging/archive.wab?token=upload-secret"}, nil
}

func (backend *memoryUploadBackend) Open(_ context.Context, object UploadStagedObject) (io.ReadCloser, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return io.NopCloser(bytes.NewReader(backend.data["/private/staging/archive.wab"])), nil
}

func (backend *memoryUploadBackend) Delete(_ context.Context, object UploadStagedObject) error {
	if backend.deleteStarted != nil {
		select {
		case backend.deleteStarted <- struct{}{}:
		default:
		}
		<-backend.allowDelete
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	delete(backend.data, "/private/staging/archive.wab")
	backend.deleteCount++
	return nil
}
