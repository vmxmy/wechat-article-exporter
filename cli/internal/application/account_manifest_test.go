package application

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestAccountManifestServiceExportsAndImportsThroughApplication(t *testing.T) {
	backend := &memoryUploadBackend{}
	uploads, err := NewUploadStaging(UploadStagingOptions{
		Backend: backend,
		NewID:   uploadIDs("upload-account-manifest-1"),
		Limits:  UploadStagingLimits{MaximumBytes: 1024, MaximumTotalBytes: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := &accountManifestApplication{manifest: domain.AccountManifest{SchemaVersion: 1, Accounts: []domain.Account{{FakeID: "fixture", Name: "Fixture"}}}}
	service, err := NewAccountManifestService(app, uploads)
	if err != nil {
		t.Fatal(err)
	}

	exported, err := service.Export(context.Background(), domain.AccountQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var manifest domain.AccountManifest
	if err := json.NewDecoder(exported).Decode(&manifest); err != nil {
		exported.Close()
		t.Fatal(err)
	}
	_ = exported.Close()
	if len(manifest.Accounts) != 1 || manifest.Accounts[0].FakeID != "fixture" {
		t.Fatalf("exported manifest = %#v", manifest)
	}

	encoded := []byte(`{"schemaVersion":1,"exportedAt":"2026-07-24T00:00:00Z","accounts":[{"fakeid":"imported","name":"Imported"}]}`)
	receipt, err := service.Stage(context.Background(), bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Import(context.Background(), receipt.Handle)
	if err != nil {
		t.Fatal(err)
	}
	if result.Report.Added != 1 || len(app.imported.Accounts) != 1 || app.imported.Accounts[0].FakeID != "imported" {
		t.Fatalf("result=%#v imported=%#v", result, app.imported)
	}
}

func TestAccountManifestServiceConsumesInvalidAndOversizedUploadsWithoutLeakingBackend(t *testing.T) {
	backend := &memoryUploadBackend{}
	uploads, err := NewUploadStaging(UploadStagingOptions{
		Backend: backend,
		NewID:   uploadIDs("upload-account-manifest-2", "upload-account-manifest-3"),
		Limits:  UploadStagingLimits{MaximumBytes: AccountManifestMaximumBytes + 1, MaximumTotalBytes: AccountManifestMaximumBytes + 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAccountManifestService(&accountManifestApplication{}, uploads)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{
		[]byte(`{"schemaVersion":1,"accounts":[]}{}`),
		bytes.Repeat([]byte("x"), int(AccountManifestMaximumBytes)+1),
	} {
		receipt, err := service.Stage(context.Background(), bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Import(context.Background(), receipt.Handle)
		if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "upload-account") {
			t.Fatalf("Import error = %v", err)
		}
	}
}

type accountManifestApplication struct {
	Application
	manifest domain.AccountManifest
	imported domain.AccountManifest
}

func (application *accountManifestApplication) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return application.manifest, nil
}
func (application *accountManifestApplication) ImportAccounts(_ context.Context, manifest domain.AccountManifest) (domain.AccountImportReport, error) {
	application.imported = manifest
	return domain.AccountImportReport{Added: len(manifest.Accounts)}, nil
}

var _ io.Reader = (*bytes.Reader)(nil)
