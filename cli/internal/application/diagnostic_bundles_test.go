package application

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeDiagnosticBundleMaintenance struct {
	artifact  DiagnosticBundleArtifact
	contents  []byte
	opened    []string
	discarded []string
	err       error
}

func (fake *fakeDiagnosticBundleMaintenance) CreateDiagnosticBundle(context.Context) (DiagnosticBundleArtifact, error) {
	return fake.artifact, fake.err
}

func (fake *fakeDiagnosticBundleMaintenance) OpenDiagnosticBundle(_ context.Context, reference string) (io.ReadCloser, error) {
	fake.opened = append(fake.opened, reference)
	if fake.err != nil {
		return nil, fake.err
	}
	return io.NopCloser(bytes.NewReader(fake.contents)), nil
}

func (fake *fakeDiagnosticBundleMaintenance) DiscardDiagnosticBundle(_ context.Context, reference string) error {
	fake.discarded = append(fake.discarded, reference)
	return nil
}

func TestDiagnosticBundleServiceIssuesOpaqueOneShotExpiringHandles(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	backend := &fakeDiagnosticBundleMaintenance{artifact: DiagnosticBundleArtifact{Reference: "/private/staging/diagnostics.zip", CreatedAt: now, SHA256: "digest", SizeBytes: 9}, contents: []byte("redacted")}
	service := NewDiagnosticBundleService(DiagnosticBundleOptions{
		Maintenance: backend, TTL: time.Minute, Now: func() time.Time { return now },
		Random: func(buffer []byte) (int, error) { return len(buffer), nil },
	})
	receipt, err := service.Create(context.Background())
	if err != nil || !strings.HasPrefix(receipt.Handle, "diagnostic_") || strings.Contains(receipt.Handle, "/private") || receipt.ExpiresAt != now.Add(time.Minute) {
		t.Fatalf("Create() = %#v, %v", receipt, err)
	}
	reader, opened, err := service.Open(context.Background(), receipt.Handle)
	if err != nil || opened.Handle != receipt.Handle || opened.SizeBytes != 9 {
		t.Fatalf("Open() = %#v, %#v, %v", reader, opened, err)
	}
	contents, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err != nil || closeErr != nil || string(contents) != "redacted" {
		t.Fatalf("download = %q, %v, %v", contents, err, closeErr)
	}
	if len(backend.opened) != 1 || backend.opened[0] != backend.artifact.Reference || len(backend.discarded) != 1 || backend.discarded[0] != backend.artifact.Reference {
		t.Fatalf("backend lifecycle opened=%#v discarded=%#v", backend.opened, backend.discarded)
	}
	if _, _, err := service.Open(context.Background(), receipt.Handle); err == nil {
		t.Fatal("Open() replay succeeded")
	}

	expiring, err := service.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, _, err := service.Open(context.Background(), expiring.Handle); err == nil || len(backend.discarded) != 2 {
		t.Fatalf("expired Open() error=%v discarded=%#v", err, backend.discarded)
	}
}

func TestDiagnosticBundleServiceFailsClosedWithoutLeakingBackendErrors(t *testing.T) {
	service := NewDiagnosticBundleService(DiagnosticBundleOptions{Maintenance: &fakeDiagnosticBundleMaintenance{err: errors.New("write /private/staging Cookie: sid=secret")}})
	if _, err := service.Create(context.Background()); err == nil || strings.Contains(err.Error(), "/private") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("Create() leaked backend error: %v", err)
	}
	if _, _, err := service.Open(context.Background(), "/private/staging"); err == nil {
		t.Fatal("Open() accepted a path as a handle")
	}
}

var _ DiagnosticBundleMaintenance = (*fakeDiagnosticBundleMaintenance)(nil)
