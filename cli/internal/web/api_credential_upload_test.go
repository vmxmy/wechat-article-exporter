package web

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestCredentialUploadAPIStreamsOneFileAndNeverEchoesSecrets(t *testing.T) {
	maintenance := &webCredentialMaintenance{imported: application.CredentialMetadata{ID: "credential-1", AccountID: "account-1", Kind: "wechat-article", Status: "valid"}}
	server, client := startCredentialUploadServer(t, maintenance)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	response, err := client.Do(credentialUploadRequest(t, base, csrf, base, "credential", validCredentialUploadJSON))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	for _, forbidden := range []string{"biz-secret", "key-secret", "cookie-secret", "credential.json"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	if maintenance.request.Cookie != "cookie-secret" || maintenance.request.AppMsgToken != "token-secret" {
		t.Fatalf("maintenance request=%#v", maintenance.request)
	}
}

func TestCredentialUploadAPIRejectsProtectedMalformedAndMultiFileBodies(t *testing.T) {
	server, client := startCredentialUploadServer(t, &webCredentialMaintenance{})
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	requests := []*http.Request{
		credentialUploadRequest(t, base, "wrong", base, "credential", validCredentialUploadJSON),
		credentialUploadRequest(t, base, csrf, "http://evil.example", "credential", validCredentialUploadJSON),
		credentialUploadRequest(t, base, csrf, base, "other", validCredentialUploadJSON),
		credentialUploadRequest(t, base, csrf, base, "credential", `{"biz":"missing-fields"}`),
		credentialUploadTwoPartRequest(t, base, csrf),
	}
	for _, request := range requests {
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode < 400 {
			t.Fatalf("invalid upload accepted: status=%d body=%s", response.StatusCode, readResponse(t, response))
		}
		response.Body.Close()
	}
}

const validCredentialUploadJSON = `{"biz":"biz-secret","uin":"uin-secret","key":"key-secret","pass_ticket":"ticket-secret","wap_sid2":"sid-secret","appmsg_token":"token-secret","cookie":"cookie-secret"}`

func startCredentialUploadServer(t *testing.T, credentials *webCredentialMaintenance) (*Server, *http.Client) {
	t.Helper()
	maintenance := application.NewMaintenance(application.MaintenanceOptions{Credentials: credentials})
	server, err := New(Options{Application: &apiApplication{}, Maintenance: maintenance, CredentialUploads: application.NewCredentialUpload(maintenance)})
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

func credentialUploadRequest(t *testing.T, base, csrf, origin, field, contents string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "credential.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/settings/credentials/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", origin)
	request.Header.Set("X-CSRF-Token", csrf)
	return request
}

func credentialUploadTwoPartRequest(t *testing.T, base, csrf string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for range 2 {
		part, err := writer.CreateFormFile("credential", "credential.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, validCredentialUploadJSON); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/settings/credentials/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", base)
	request.Header.Set("X-CSRF-Token", csrf)
	return request
}
