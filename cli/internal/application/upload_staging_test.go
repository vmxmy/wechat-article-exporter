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
	now = now.Add(time.Minute)
	if _, _, err := service.Consume(context.Background(), receipt.Handle); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired Consume() error=%v", err)
	}
	if backend.deleteCount != 2 {
		t.Fatalf("expired delete count=%d, want 2", backend.deleteCount)
	}
}

type memoryUploadBackend struct {
	mu          sync.Mutex
	data        map[string][]byte
	stageErr    error
	deleteCount int
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
	backend.mu.Lock()
	defer backend.mu.Unlock()
	delete(backend.data, "/private/staging/archive.wab")
	backend.deleteCount++
	return nil
}
