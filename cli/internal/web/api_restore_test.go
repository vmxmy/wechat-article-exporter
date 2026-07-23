package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestRestoreAPIMultipartProtectionAndExactlyOneArchive(t *testing.T) {
	server, client, restore := startRestoreServer(t, &restoreWebCoordinator{})
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	for name, request := range map[string]*http.Request{
		"wrong origin": multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "archive", []byte("archive"), map[string]string{"Origin": "http://evil.example", "X-CSRF-Token": csrf}),
		"wrong csrf":   multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "archive", []byte("archive"), map[string]string{"Origin": base, "X-CSRF-Token": "wrong"}),
		"wrong part":   multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "other", []byte("archive"), map[string]string{"Origin": base, "X-CSRF-Token": csrf}),
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
	missingSession := multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "archive", []byte("archive"), map[string]string{"Origin": base, "X-CSRF-Token": csrf})
	response, err := (&http.Client{}).Do(missingSession)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing session status=%d", response.StatusCode)
	}

	request := multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "archive", []byte("archive"), map[string]string{"Origin": base, "X-CSRF-Token": csrf})
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var receipt application.UploadReceipt
	decodeAPIData(t, response, &receipt)
	if receipt.Handle == "" || strings.Contains(string(receipt.Handle), "archive") {
		t.Fatalf("receipt = %#v", receipt)
	}

	request = multipartTwoPartRestoreRequest(t, base+"/api/v1/maintenance/restore/upload", csrf)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("two-part status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if _, err := restore.Prepare(context.Background(), application.RestorePrepareRequest{UploadHandle: receipt.Handle, ConflictPolicy: application.RestoreRefuseConflicts}); err != nil {
		t.Fatalf("first staged receipt became unavailable: %v", err)
	}
}

func TestRestoreAPIEnforcesIndependentUploadLimit(t *testing.T) {
	server, client, _ := startRestoreServerWithLimit(t, &restoreWebCoordinator{}, 4)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	request := multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "archive", []byte("five!"), map[string]string{"Origin": base, "X-CSRF-Token": csrf})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("large upload status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
}

func TestRestoreAPIPrepareIsBoundAndCommitShutsDownOnlyOnSuccess(t *testing.T) {
	coordinator := &restoreWebCoordinator{}
	server, client, _ := startRestoreServer(t, coordinator)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	receipt := restoreUpload(t, client, base, csrf, []byte("archive"))

	prepared := restorePrepare(t, client, base, csrf, receipt.Handle, application.RestoreRenameConflicts)
	wrong := jsonRestoreRequest(t, base+"/api/v1/maintenance/restore/commit", `{"preparationId":"`+prepared.ID+`","confirmation":"wrong"}`, csrf)
	response, err := client.Do(wrong)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong confirmation status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	if response = get(t, client, base+"/api/v1/status"); response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("failure shut down server: %d", response.StatusCode)
	}
	response.Body.Close()

	commit := jsonRestoreRequest(t, base+"/api/v1/maintenance/restore/commit", `{"preparationId":"`+prepared.ID+`","confirmation":"`+prepared.Confirmation+`"}`, csrf)
	response, err = client.Do(commit)
	if err != nil {
		t.Fatal(err)
	}
	var completion application.RestoreCompletion
	decodeAPIData(t, response, &completion)
	if completion.RestoredFiles != 3 || coordinator.archive != "archive" || coordinator.policy != application.RestoreRenameConflicts {
		t.Fatalf("completion=%#v coordinator=%#v", completion, coordinator)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err = client.Get(base + "/api/v1/status")
		if err != nil || response.StatusCode != http.StatusOK {
			if response != nil {
				response.Body.Close()
			}
			return
		}
		response.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server remained available after successful restore")
}

func TestRestoreAPIFailureDoesNotShutdown(t *testing.T) {
	coordinator := &restoreWebCoordinator{err: errors.New("restore failed")}
	server, client, _ := startRestoreServer(t, coordinator)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	receipt := restoreUpload(t, client, base, csrf, []byte("archive"))
	prepared := restorePrepare(t, client, base, csrf, receipt.Handle, application.RestoreRefuseConflicts)
	response, err := client.Do(jsonRestoreRequest(t, base+"/api/v1/maintenance/restore/commit", `{"preparationId":"`+prepared.ID+`","confirmation":"`+prepared.Confirmation+`"}`, csrf))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("failed restore status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response = get(t, client, base+"/api/v1/status")
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("failed restore shut down server: %d", response.StatusCode)
	}
	response.Body.Close()
}

func startRestoreServer(t *testing.T, coordinator *restoreWebCoordinator) (*Server, *http.Client, *application.RestoreService) {
	t.Helper()
	return startRestoreServerWithLimit(t, coordinator, 1024)
}

func startRestoreServerWithLimit(t *testing.T, coordinator *restoreWebCoordinator, maximum int64) (*Server, *http.Client, *application.RestoreService) {
	t.Helper()
	backend := &restoreUploadBackend{}
	uploads, err := application.NewUploadStaging(application.UploadStagingOptions{Backend: backend, Limits: application.UploadStagingLimits{MaximumBytes: maximum}})
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
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return server, &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil }}, restore
}

func restoreUpload(t *testing.T, client *http.Client, base, csrf string, archive []byte) application.UploadReceipt {
	t.Helper()
	response, err := client.Do(multipartRestoreRequest(t, http.MethodPost, base+"/api/v1/maintenance/restore/upload", "archive", archive, map[string]string{"Origin": base, "X-CSRF-Token": csrf}))
	if err != nil {
		t.Fatal(err)
	}
	var receipt application.UploadReceipt
	decodeAPIData(t, response, &receipt)
	return receipt
}

func restorePrepare(t *testing.T, client *http.Client, base, csrf string, handle application.UploadHandle, policy application.RestoreConflictPolicy) application.RestorePreparation {
	t.Helper()
	body := `{"uploadHandle":"` + string(handle) + `","conflictPolicy":"` + string(policy) + `"}`
	response, err := client.Do(jsonRestoreRequest(t, base+"/api/v1/maintenance/restore/prepare", body, csrf))
	if err != nil {
		t.Fatal(err)
	}
	var prepared application.RestorePreparation
	decodeAPIData(t, response, &prepared)
	return prepared
}

func multipartRestoreRequest(t *testing.T, method, target, field string, archive []byte, headers map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, "restore.wab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return request
}

func multipartTwoPartRestoreRequest(t *testing.T, target, csrf string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, name := range []string{"archive", "archive"} {
		part, err := writer.CreateFormFile(name, "restore.wab")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte("archive"))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, target, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", strings.TrimSuffix(strings.Split(target, "/api/")[0], "/"))
	request.Header.Set("X-CSRF-Token", csrf)
	return request
}

func jsonRestoreRequest(t *testing.T, target, body, csrf string) *http.Request {
	t.Helper()
	return requestWith(t, http.MethodPost, target, strings.NewReader(body), map[string]string{"Origin": strings.TrimSuffix(strings.Split(target, "/api/")[0], "/"), "Content-Type": "application/json", "X-CSRF-Token": csrf})
}

func decodeAPIData(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		contents, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d body=%s", response.StatusCode, contents)
	}
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload.Data, target); err != nil {
		t.Fatal(err)
	}
}

type restoreUploadBackend struct{ contents []byte }

func (backend *restoreUploadBackend) Stage(_ context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	contents, err := io.ReadAll(source)
	if err != nil {
		return application.UploadStagedObject{}, err
	}
	backend.contents = append([]byte(nil), contents...)
	return application.UploadStagedObject{Reference: "private"}, nil
}
func (backend *restoreUploadBackend) Open(context.Context, application.UploadStagedObject) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(backend.contents)), nil
}
func (*restoreUploadBackend) Delete(context.Context, application.UploadStagedObject) error {
	return nil
}

type restoreWebCoordinator struct {
	archive string
	policy  application.RestoreConflictPolicy
	err     error
}

func (coordinator *restoreWebCoordinator) Restore(_ context.Context, archive io.Reader, policy application.RestoreConflictPolicy) (application.RestoreCompletion, error) {
	contents, err := io.ReadAll(archive)
	if err != nil {
		return application.RestoreCompletion{}, err
	}
	coordinator.archive, coordinator.policy = string(contents), policy
	if coordinator.err != nil {
		return application.RestoreCompletion{}, coordinator.err
	}
	return application.RestoreCompletion{RestoredFiles: 3, RestoredBytes: int64(len(contents)), Profiles: 1}, nil
}
