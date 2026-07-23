package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestAccountManifestAPIStreamsExportAndImportsOneBoundedStagedUpload(t *testing.T) {
	app := &accountManifestApplication{manifest: domain.AccountManifest{SchemaVersion: 1, Accounts: []domain.Account{{FakeID: "fixture", Name: "Fixture"}}}}
	server, client := startAccountManifestServer(t, app, 1024)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	export, err := client.Get(base + "/api/v1/accounts/manifest")
	if err != nil {
		t.Fatal(err)
	}
	if export.StatusCode != http.StatusOK || !strings.Contains(export.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export status=%d headers=%v", export.StatusCode, export.Header)
	}
	var exported domain.AccountManifest
	if err := json.NewDecoder(export.Body).Decode(&exported); err != nil {
		export.Body.Close()
		t.Fatal(err)
	}
	export.Body.Close()
	if len(exported.Accounts) != 1 || exported.Accounts[0].FakeID != "fixture" {
		t.Fatalf("exported = %#v", exported)
	}

	body, err := json.Marshal(domain.AccountManifest{SchemaVersion: 1, Accounts: []domain.Account{{FakeID: "imported", Name: "Imported"}}})
	if err != nil {
		t.Fatal(err)
	}
	upload, err := client.Do(accountManifestMultipartRequest(t, base+"/api/v1/accounts/manifest/upload", body, csrf))
	if err != nil {
		t.Fatal(err)
	}
	var receipt application.UploadReceipt
	decodeAPIData(t, upload, &receipt)
	if receipt.Handle == "" || strings.Contains(string(receipt.Handle), "manifest") {
		t.Fatalf("receipt = %#v", receipt)
	}

	importRequest := jsonRestoreRequest(t, base+"/api/v1/accounts/manifest/import", `{"uploadHandle":"`+string(receipt.Handle)+`"}`, csrf)
	response, err := client.Do(importRequest)
	if err != nil {
		t.Fatal(err)
	}
	var result application.AccountManifestImportResult
	decodeAPIData(t, response, &result)
	if result.Report.Added != 1 || len(app.imported.Accounts) != 1 || app.imported.Accounts[0].FakeID != "imported" {
		t.Fatalf("result=%#v imported=%#v", result, app.imported)
	}

	response, err = client.Do(jsonRestoreRequest(t, base+"/api/v1/accounts/manifest/import", `{"uploadHandle":"`+string(receipt.Handle)+`"}`, csrf))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("replayed import status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
}

func TestAccountManifestAPIRejectsCrossOriginExtraPartsAndOversizedUploads(t *testing.T) {
	app := &accountManifestApplication{}
	server, client := startAccountManifestServer(t, app, 4)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	for name, request := range map[string]*http.Request{
		"cross origin":  accountManifestMultipartRequestWithParts(t, base+"/api/v1/accounts/manifest/upload", csrf, "http://evil.example", [][]byte{[]byte("{}")}),
		"two manifests": accountManifestMultipartRequestWithParts(t, base+"/api/v1/accounts/manifest/upload", csrf, base, [][]byte{[]byte("{}"), []byte("{}")}),
		"oversized":     accountManifestMultipartRequest(t, base+"/api/v1/accounts/manifest/upload", []byte("12345"), csrf),
	} {
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatalf("%s status=%d", name, response.StatusCode)
		}
	}
}

func startAccountManifestServer(t *testing.T, app application.Application, maximum int64) (*Server, *http.Client) {
	t.Helper()
	backend := &accountManifestUploadBackend{}
	uploads, err := application.NewUploadStaging(application.UploadStagingOptions{Backend: backend, Limits: application.UploadStagingLimits{MaximumBytes: maximum, MaximumTotalBytes: maximum}})
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := application.NewAccountManifestService(app, uploads)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Application: app, AccountManifests: manifests})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background()) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-done; err != nil {
			t.Errorf("server close: %v", err)
		}
	})
	return server, newTestClient(t)
}

func accountManifestMultipartRequest(t *testing.T, target string, contents []byte, csrf string) *http.Request {
	t.Helper()
	return accountManifestMultipartRequestWithParts(t, target, csrf, strings.TrimSuffix(strings.Split(target, "/api/")[0], "/"), [][]byte{contents})
}

func accountManifestMultipartRequestWithParts(t *testing.T, target, csrf, origin string, values [][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, value := range values {
		part, err := writer.CreateFormFile(accountManifestFormField, "accounts.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", origin)
	request.Header.Set("X-CSRF-Token", csrf)
	return request
}

type accountManifestApplication struct {
	testApplication
	manifest domain.AccountManifest
	imported domain.AccountManifest
}

func (app *accountManifestApplication) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return app.manifest, nil
}
func (app *accountManifestApplication) ImportAccounts(_ context.Context, manifest domain.AccountManifest) (domain.AccountImportReport, error) {
	app.imported = manifest
	return domain.AccountImportReport{Added: len(manifest.Accounts)}, nil
}

type accountManifestUploadBackend struct{ contents []byte }

func (backend *accountManifestUploadBackend) Stage(_ context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	contents, err := io.ReadAll(source)
	if err != nil {
		return application.UploadStagedObject{}, err
	}
	backend.contents = append([]byte(nil), contents...)
	return application.UploadStagedObject{Reference: "private"}, nil
}
func (backend *accountManifestUploadBackend) Open(context.Context, application.UploadStagedObject) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(backend.contents)), nil
}
func (*accountManifestUploadBackend) Delete(context.Context, application.UploadStagedObject) error {
	return nil
}
