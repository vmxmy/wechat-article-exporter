package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestMultipartUploadsRejectUnsafeFilenamesBeforeStagingOrMutation(t *testing.T) {
	filenames := []string{
		"../../outside.json",
		`..\\..\\outside.json`,
		`C:\\Windows\\outside.json`,
		`\\\\server\\share\\outside.json`,
		"name\x00with-control.json",
		"name\x1fwith-control.json",
		strings.Repeat("a", 256) + ".json",
		"",
	}

	for _, filename := range filenames {
		t.Run(fmt.Sprintf("%q", filename), func(t *testing.T) {
			assertUnsafeAccountManifestFilename(t, filename)
			assertUnsafeCredentialFilename(t, filename)
			assertUnsafeRestoreFilename(t, filename)
		})
	}
}

func TestMultipartUploadsRejectEncodedUnsafeRawFilenames(t *testing.T) {
	for _, disposition := range []string{
		`form-data; name="%s"; filename="safe.json"; filename*=utf-8''..%2Foutside.json`,
		`form-data; name="%s"; filename*=utf-8''..%5Coutside.json`,
	} {
		t.Run(disposition, func(t *testing.T) {
			assertUnsafeAccountManifestDisposition(t, disposition)
			assertUnsafeCredentialDisposition(t, disposition)
			assertUnsafeRestoreDisposition(t, disposition)
		})
	}
}

func TestMultipartUploadsAcceptBenignSingleFileNames(t *testing.T) {
	assertBenignAccountManifestFilename(t)
	assertBenignCredentialFilename(t)
	assertBenignRestoreFilename(t)
}

func assertBenignAccountManifestFilename(t *testing.T) {
	t.Helper()
	backend := &countingUploadBackend{}
	server, client := startAccountManifestServerWithBackend(t, &accountManifestApplication{}, backend, 1024)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/accounts/manifest/upload", accountManifestFormField, `form-data; name="%s"; filename="accounts.json"`, []byte(`{"schemaVersion":1,"accounts":[]}`), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedMultipartFilename(t, response)
	if backend.stages != 1 {
		t.Fatalf("benign account manifest stages=%d, want 1", backend.stages)
	}
}

func assertBenignCredentialFilename(t *testing.T) {
	t.Helper()
	maintenance := &webCredentialMaintenance{imported: application.CredentialMetadata{ID: "credential-1"}}
	server, client := startCredentialUploadServer(t, maintenance)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/settings/credentials/upload", credentialUploadFormField, `form-data; name="%s"; filename="credential.json"`, []byte(validCredentialUploadJSON), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedMultipartFilename(t, response)
	if maintenance.request.Cookie != "cookie-secret" {
		t.Fatalf("benign credential filename did not import: %#v", maintenance.request)
	}
}

func assertBenignRestoreFilename(t *testing.T) {
	t.Helper()
	backend := &countingUploadBackend{}
	server, client, _ := startRestoreServerWithBackend(t, &restoreWebCoordinator{}, backend)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/maintenance/restore/upload", restoreArchiveFormField, `form-data; name="%s"; filename="restore.wab"`, []byte("archive"), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedMultipartFilename(t, response)
	if backend.stages != 1 {
		t.Fatalf("benign restore stages=%d, want 1", backend.stages)
	}
}

func assertUnsafeAccountManifestFilename(t *testing.T, filename string) {
	t.Helper()
	assertUnsafeAccountManifestDisposition(t, `form-data; name="%s"; filename="`+filename+`"`)
}

func assertUnsafeAccountManifestDisposition(t *testing.T, disposition string) {
	t.Helper()
	app := &accountManifestApplication{}
	backend := &countingUploadBackend{}
	server, client := startAccountManifestServerWithBackend(t, app, backend, 1024)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/accounts/manifest/upload", accountManifestFormField, disposition, []byte(`{"schemaVersion":1,"accounts":[]}`), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedMultipartFilename(t, response)
	if backend.stages != 0 || app.imported.SchemaVersion != 0 || len(app.imported.Accounts) != 0 {
		t.Fatalf("unsafe filename staged or mutated account manifest: stages=%d imported=%#v", backend.stages, app.imported)
	}
}

func assertUnsafeCredentialFilename(t *testing.T, filename string) {
	t.Helper()
	assertUnsafeCredentialDisposition(t, `form-data; name="%s"; filename="`+filename+`"`)
}

func assertUnsafeCredentialDisposition(t *testing.T, disposition string) {
	t.Helper()
	maintenance := &webCredentialMaintenance{}
	server, client := startCredentialUploadServer(t, maintenance)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/settings/credentials/upload", credentialUploadFormField, disposition, []byte(validCredentialUploadJSON), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedMultipartFilename(t, response)
	if maintenance.request != (application.CredentialImportRequest{}) {
		t.Fatalf("unsafe filename mutated credentials: %#v", maintenance.request)
	}
}

func assertUnsafeRestoreFilename(t *testing.T, filename string) {
	t.Helper()
	assertUnsafeRestoreDisposition(t, `form-data; name="%s"; filename="`+filename+`"`)
}

func assertUnsafeRestoreDisposition(t *testing.T, disposition string) {
	t.Helper()
	coordinator := &restoreWebCoordinator{}
	backend := &countingUploadBackend{}
	server, client, restore := startRestoreServerWithBackend(t, coordinator, backend)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/maintenance/restore/upload", restoreArchiveFormField, disposition, []byte("archive"), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedMultipartFilename(t, response)
	if coordinator.archive != "" || backend.stages != 0 || restore == nil {
		t.Fatalf("unsafe filename staged or mutated restore: archive=%q stages=%d", coordinator.archive, backend.stages)
	}
}

func multipartUploadRequest(t *testing.T, target, field, disposition string, contents []byte, origin, csrf string) *http.Request {
	t.Helper()
	const boundary = "browser-workspace-file-boundary"
	var body bytes.Buffer
	_, _ = fmt.Fprintf(&body, "--%s\r\nContent-Disposition: "+disposition+"\r\nContent-Type: application/octet-stream\r\n\r\n", boundary, field)
	_, _ = body.Write(contents)
	_, _ = fmt.Fprintf(&body, "\r\n--%s--\r\n", boundary)
	request, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	request.Header.Set("Origin", origin)
	request.Header.Set("X-CSRF-Token", csrf)
	return request
}

func assertRejectedMultipartFilename(t *testing.T, response *http.Response) {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsafe multipart filename status=%d body=%s", response.StatusCode, body)
	}
	for _, forbidden := range []string{"outside.json", "with-control.json", "safe.json", "private", "/", `\\`} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("response leaked multipart filename detail %q: %s", forbidden, body)
		}
	}
}

func assertAcceptedMultipartFilename(t *testing.T, response *http.Response) {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("benign multipart filename status=%d body=%s", response.StatusCode, body)
	}
}

func startAccountManifestServerWithBackend(t *testing.T, app application.Application, backend application.UploadStagingBackend, maximum int64) (*Server, *http.Client) {
	t.Helper()
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

func startRestoreServerWithBackend(t *testing.T, coordinator *restoreWebCoordinator, backend application.UploadStagingBackend) (*Server, *http.Client, *application.RestoreService) {
	t.Helper()
	uploads, err := application.NewUploadStaging(application.UploadStagingOptions{Backend: backend, Limits: application.UploadStagingLimits{MaximumBytes: 1024, MaximumTotalBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	restore, err := application.NewRestore(application.RestoreOptions{Uploads: uploads, Coordinator: coordinator})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Application: testApplication{}, Restore: restore})
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
	return server, newTestClient(t), restore
}

type countingUploadBackend struct{ stages int }

func (backend *countingUploadBackend) Stage(_ context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	backend.stages++
	_, err := io.Copy(io.Discard, source)
	return application.UploadStagedObject{Reference: "private"}, err
}

func (*countingUploadBackend) Open(context.Context, application.UploadStagedObject) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (*countingUploadBackend) Delete(context.Context, application.UploadStagedObject) error {
	return nil
}
