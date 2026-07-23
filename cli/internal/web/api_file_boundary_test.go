package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestMultipartUploadsKeepDangerousFilenamesPrivateAndInert(t *testing.T) {
	filenames := []string{
		"../../outside.json",
		"name\x00with-control.json",
		strings.Repeat("a", 16<<10) + ".json",
	}

	for _, filename := range filenames {
		t.Run(fmt.Sprintf("%q", filename), func(t *testing.T) {
			assertDangerousAccountManifestFilename(t, filename)
			assertDangerousCredentialFilename(t, filename)
			assertDangerousRestoreFilename(t, filename)
		})
	}
}

func assertDangerousAccountManifestFilename(t *testing.T, filename string) {
	t.Helper()
	server, client := startAccountManifestServer(t, &accountManifestApplication{}, 1024)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/accounts/manifest/upload", accountManifestFormField, filename, []byte(`{"schemaVersion":1,"accounts":[]}`), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertUnobservableMultipartFilename(t, response, filename)
}

func assertDangerousCredentialFilename(t *testing.T, filename string) {
	t.Helper()
	server, client := startCredentialUploadServer(t, &webCredentialMaintenance{})
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/settings/credentials/upload", credentialUploadFormField, filename, []byte(validCredentialUploadJSON), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertUnobservableMultipartFilename(t, response, filename)
}

func assertDangerousRestoreFilename(t *testing.T, filename string) {
	t.Helper()
	server, client, _ := startRestoreServer(t, &restoreWebCoordinator{})
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(multipartUploadRequest(t, base+"/api/v1/maintenance/restore/upload", restoreArchiveFormField, filename, []byte("archive"), base, csrf))
	if err != nil {
		t.Fatal(err)
	}
	assertUnobservableMultipartFilename(t, response, filename)
}

func multipartUploadRequest(t *testing.T, target, field, filename string, contents []byte, origin, csrf string) *http.Request {
	t.Helper()
	const boundary = "browser-workspace-file-boundary"
	var body bytes.Buffer
	_, _ = fmt.Fprintf(&body, "--%s\r\nContent-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\nContent-Type: application/octet-stream\r\n\r\n", boundary, field, filename)
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

func assertUnobservableMultipartFilename(t *testing.T, response *http.Response, filename string) {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusInternalServerError {
		t.Fatalf("dangerous multipart filename status=%d body=%s", response.StatusCode, body)
	}
	for _, forbidden := range []string{filename, "outside.json", "with-control.json", "private", "/"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("response leaked multipart filename detail %q: %s", forbidden, body)
		}
	}
	var envelope struct {
		Data application.UploadReceipt `json:"data"`
	}
	if response.StatusCode == http.StatusCreated && strings.Contains(string(body), "uploadHandle") {
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.Handle == "" || strings.Contains(string(envelope.Data.Handle), filename) {
			t.Fatalf("unsafe multipart filename influenced opaque receipt: %#v", envelope.Data)
		}
	}
}
