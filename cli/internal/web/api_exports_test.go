package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

type exportAPIService struct {
	directory application.WorkspaceExportDirectory
	page      application.WorkspacePage[application.WorkspaceExportRecord]
	manifest  application.WorkspaceExportManifest
	verify    application.WorkspaceExportVerification
	artifact  application.WorkspaceDownloadArtifact
	create    application.WorkspaceCreateExportDirectoryRequest
	start     application.WorkspaceStartExportRequest
	openID    string
}

func (service *exportAPIService) DefaultExportDirectory(context.Context) (application.WorkspaceExportDirectory, error) {
	return service.directory, nil
}
func (service *exportAPIService) CreateExportDirectory(_ context.Context, request application.WorkspaceCreateExportDirectoryRequest) (application.WorkspaceExportDirectory, error) {
	service.create = request
	return application.WorkspaceExportDirectory{Token: "dir_child", Label: request.Name}, nil
}
func (service *exportAPIService) StartExport(_ context.Context, request application.WorkspaceStartExportRequest) (application.WorkspaceExportJob, error) {
	service.start = request
	return application.WorkspaceExportJob{ID: "11111111-1111-1111-1111-111111111111", State: domain.JobQueued}, nil
}
func (service *exportAPIService) ExportRecords(context.Context, application.WorkspacePageRequest) (application.WorkspacePage[application.WorkspaceExportRecord], error) {
	return service.page, nil
}
func (service *exportAPIService) ExportManifest(context.Context, string) (application.WorkspaceExportManifest, error) {
	return service.manifest, nil
}
func (service *exportAPIService) VerifyExport(context.Context, string) (application.WorkspaceExportVerification, error) {
	return service.verify, nil
}
func (service *exportAPIService) DownloadArtifact(context.Context, application.WorkspaceDownloadArtifactRequest) (application.WorkspaceDownloadArtifact, error) {
	return service.artifact, nil
}
func (service *exportAPIService) OpenExportOutput(_ context.Context, exportID string) error {
	service.openID = exportID
	return nil
}

type exportListRecords struct {
	page     domain.Page[library.ExportRecord]
	requests []application.WorkspacePageRequest
}

func (records *exportListRecords) QueryExportRecords(_ context.Context, offset, limit int) (domain.Page[library.ExportRecord], error) {
	records.requests = append(records.requests, application.WorkspacePageRequest{Offset: offset, Limit: limit})
	return records.page, nil
}

func (*exportListRecords) GetExport(context.Context, domain.ExportID) (library.ExportRecord, error) {
	return library.ExportRecord{}, nil
}

func (*exportListRecords) ListExportFiles(context.Context, domain.ExportID) ([]library.ExportFileRecord, error) {
	return nil, nil
}

func TestExportListAPIUsesAuthenticatedSafePagedWorkspaceDTO(t *testing.T) {
	completed := time.Date(2026, 7, 24, 10, 30, 0, 0, time.UTC)
	records := &exportListRecords{page: domain.Page[library.ExportRecord]{
		Items: []library.ExportRecord{{
			ID:                   "export-1",
			JobID:                "11111111-1111-1111-1111-111111111111",
			Format:               "markdown",
			State:                "completed",
			CreatedAt:            completed.Add(-time.Minute),
			CompletedAt:          completed,
			OutputRoot:           "/private/export-root/local-temp-path",
			ProvenancePath:       "/private/export-root/export-manifest.json",
			ProvenanceSHA256:     "digest-secret",
			ProvenanceState:      "ready",
			ProvenanceGeneration: 2,
		}},
		Total: 7, Offset: 6, Limit: 3,
	}}
	server, client := startExportAPIServer(t, application.NewWorkspaceExports(nil, records))
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/exports?offset=6&limit=3")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var page struct {
		APIVersion string            `json:"apiVersion"`
		Data       []json.RawMessage `json:"data"`
		Total      int               `json:"total"`
		Offset     int               `json:"offset"`
		Limit      int               `json:"limit"`
	}
	body := readResponse(t, response)
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	if page.APIVersion != apiVersion || page.Total != 7 || page.Offset != 6 || page.Limit != 3 || len(page.Data) != 1 {
		t.Fatalf("list envelope=%#v", page)
	}
	if !reflect.DeepEqual(records.requests, []application.WorkspacePageRequest{{Offset: 6, Limit: 3}}) {
		t.Fatalf("list request=%#v", records.requests)
	}
	var record application.WorkspaceExportRecord
	if err := json.Unmarshal(page.Data[0], &record); err != nil {
		t.Fatal(err)
	}
	if got := record; got.OutputDirectory != "local export directory" {
		t.Fatalf("unsafe output directory=%q", got.OutputDirectory)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(page.Data[0], &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		"id", "jobId", "format", "state", "createdAt", "completedAt", "provenanceState", "provenanceGeneration", "outputDirectory",
	} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("export list DTO omitted safe field %q: %s", field, page.Data[0])
		}
	}
	if len(fields) != 9 {
		t.Fatalf("export list DTO has unsafe extra fields: %s", page.Data[0])
	}
	for _, forbidden := range []string{"/private/export-root", "local-temp-path", "digest", "secret"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("list response leaked %q: %s", forbidden, body)
		}
	}

	response = get(t, client, base+"/api/v1/exports?limit=101")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
	response = get(t, client, base+"/api/v1/exports?offset=0&page=1")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("mixed pagination status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
	if len(records.requests) != 1 {
		t.Fatalf("invalid queries reached export service: %#v", records.requests)
	}

}

func TestExportListAPIIsUnavailableWithoutExportService(t *testing.T) {
	server, client := startExportAPIServer(t, nil)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/exports")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("list status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "unavailable")
}

func TestExportAPIUsesOpaqueCapabilitiesAndFacadeOnly(t *testing.T) {
	service := &exportAPIService{
		directory: application.WorkspaceExportDirectory{Token: "dir_root", Label: "exports", IsDefault: true},
		page:      application.WorkspacePage[application.WorkspaceExportRecord]{Items: []application.WorkspaceExportRecord{{ID: "export-1", OutputDirectory: "local export directory"}}, Total: 1, Limit: 50},
		manifest:  application.WorkspaceExportManifest{ExportID: "export-1", Files: []application.WorkspaceExportFile{{Path: "article.md"}}},
		verify:    application.WorkspaceExportVerification{ExportID: "export-1", Valid: true, VerifiedOutputs: 1},
	}
	server, client := startExportAPIServer(t, service)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	mutate := func(path, body string) *http.Response {
		t.Helper()
		request := requestWith(t, http.MethodPost, base+path, strings.NewReader(body), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}

	response := mutate("/api/v1/export-directories/authorize", `{"confirm":"authorize-default-export-directory"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("authorize status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	if strings.Contains(body, "/") || !strings.Contains(body, "dir_root") {
		t.Fatalf("directory authorization exposed an unsafe path or missed token: %s", body)
	}

	response = mutate("/api/v1/export-directories", `{"parentToken":"dir_root","name":"July","confirm":"create-export-directory:dir_root:July"}`)
	if response.StatusCode != http.StatusCreated || service.create.ParentToken != "dir_root" || service.create.Name != "July" {
		t.Fatalf("create status=%d request=%#v", response.StatusCode, service.create)
	}
	response.Body.Close()

	response = mutate("/api/v1/exports/start", `{"directoryToken":"dir_child","subdirectory":"batch","selection":{"kind":"explicit_ids","articleIds":["article-1"]},"format":"markdown","options":{"formatOptions":{"metadata":false,"comments":true}},"confirm":"start-export:dir_child"}`)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var start struct {
		Data struct {
			JobID string `json:"jobId"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&start); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if start.Data.JobID != "11111111-1111-1111-1111-111111111111" || service.start.DirectoryToken != "dir_child" || service.start.Subdirectory != "batch" {
		t.Fatalf("start response=%#v request=%#v", start, service.start)
	}
	if !reflect.DeepEqual(service.start.Options.FormatOptions, map[string]any{"metadata": false, "comments": true}) {
		t.Fatalf("start format options = %#v", service.start.Options.FormatOptions)
	}

	response = mutate("/api/v1/exports/start", `{"directoryToken":"dir_child","selection":{"kind":"album_ids","albumIds":["album-1","album-2"]},"format":"markdown","confirm":"start-export:dir_child"}`)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("album batch start status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
	if service.start.Selection.Kind != domain.ExportSelectionAlbumIDs || !reflect.DeepEqual(service.start.Selection.AlbumIDs, []domain.AlbumID{"album-1", "album-2"}) {
		t.Fatalf("album batch start selection = %#v", service.start.Selection)
	}

	for _, path := range []string{"/api/v1/exports", "/api/v1/exports/export-1", "/api/v1/exports/export-1/manifest"} {
		response = get(t, client, base+path)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.StatusCode, readResponse(t, response))
		}
		response.Body.Close()
	}
	response = mutate("/api/v1/exports/export-1/verify", `{"confirm":"verify-export:export-1"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	response.Body.Close()
}

func TestExportAPINeverReturnsConfiguredExportRootPath(t *testing.T) {
	configured := filepath.Join(t.TempDir(), "private-configured-export-root")
	service := application.NewWorkspaceExports(nil, nil, application.WorkspaceExportsOptions{
		ConfiguredRoot: func(context.Context) (string, error) { return configured, nil },
	})
	server, client := startExportAPIServer(t, service)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	request := requestWith(t, http.MethodPost, base+"/api/v1/export-directories/authorize", strings.NewReader(`{"confirm":"authorize-default-export-directory"}`), map[string]string{
		"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf,
	})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("authorize status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	body := readResponse(t, response)
	if strings.Contains(body, configured) || strings.Contains(body, filepath.Base(configured)) || !strings.Contains(body, `"token":"dir_`) {
		t.Fatalf("configured root leaked or token missing from API response: %s", body)
	}
}

func TestExportAPIStreamsOpaqueArtifactsAndProtectsDesktopOpening(t *testing.T) {
	service := &exportAPIService{
		artifact: application.WorkspaceDownloadArtifact{ExportID: "export-1", Path: "private/article.md", Name: "article.md", SizeBytes: 7,
			MediaType: "text/markdown", Reader: io.NopCloser(strings.NewReader("content"))},
	}
	server, client := startExportAPIServer(t, service)
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/exports/export-1/artifact?artifactId=artifact_opaque")
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/markdown" || response.Header.Get("Content-Disposition") != `attachment; filename="article.md"` {
		t.Fatalf("artifact status=%d headers=%v body=%s", response.StatusCode, response.Header, readResponse(t, response))
	}
	if body := readResponse(t, response); body != "content" {
		t.Fatalf("artifact body=%q", body)
	}

	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	request := requestWith(t, http.MethodPost, base+"/api/v1/exports/export-1/open", strings.NewReader(`{"confirm":"open-export-output:export-1"}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || service.openID != "export-1" {
		t.Fatalf("open status=%d id=%q body=%s", response.StatusCode, service.openID, readResponse(t, response))
	}
	response.Body.Close()

	request = requestWith(t, http.MethodPost, base+"/api/v1/exports/export-1/open", strings.NewReader(`{"confirm":"open-export-output:export-1"}`), map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "X-CSRF-Token": csrf})
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin open status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "forbidden")
}

func TestExportAPIRejectsEscapesBadConfirmationAndMissingMutationCredentials(t *testing.T) {
	service := &exportAPIService{directory: application.WorkspaceExportDirectory{Token: "dir_root"}}
	server, client := startExportAPIServer(t, service)
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	mutate := func(path, body string, headers map[string]string) *http.Response {
		t.Helper()
		if headers == nil {
			headers = map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf}
		}
		request := requestWith(t, http.MethodPost, base+path, strings.NewReader(body), headers)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	for _, input := range []struct{ path, body string }{
		{"/api/v1/export-directories", `{"parentToken":"dir_root","name":"../escape","confirm":"create-export-directory:dir_root:../escape"}`},
		{"/api/v1/exports/start", `{"directoryToken":"/tmp/escape","format":"markdown","confirm":"start-export:/tmp/escape"}`},
		{"/api/v1/exports/start", `{"directoryToken":"dir_root","format":"markdown","confirm":"wrong"}`},
	} {
		response := mutate(input.path, input.body, nil)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s status=%d body=%s", input.path, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
	response := mutate("/api/v1/exports/export-1/verify", `{"confirm":"verify-export:export-1"}`, map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "X-CSRF-Token": csrf})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin verification status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "forbidden")
	response = get(t, client, base+"/api/v1/exports/export-1/artifact?path=../secret")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("artifact capability status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestExportVerificationIsRateLimited(t *testing.T) {
	server, client := startExportAPIServer(t, &exportAPIService{})
	base := authorizeAPI(t, client, server.URL())
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	for attempt := 0; attempt < maximumExportVerifications; attempt++ {
		request := requestWith(t, http.MethodPost, base+"/api/v1/exports/export-1/verify", strings.NewReader(`{"confirm":"verify-export:export-1"}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.StatusCode, readResponse(t, response))
		}
		response.Body.Close()
	}
	request := requestWith(t, http.MethodPost, base+"/api/v1/exports/export-1/verify", strings.NewReader(`{"confirm":"verify-export:export-1"}`), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "60" {
		t.Fatalf("rate limit status=%d retry-after=%q body=%s", response.StatusCode, response.Header.Get("Retry-After"), readResponse(t, response))
	}
	assertAPIError(t, response, "rate_limited")
}

func startExportAPIServer(t *testing.T, exports application.WorkspaceExportService) (*Server, *http.Client) {
	t.Helper()
	server, err := New(Options{Application: testApplication{}, Exports: exports, Now: time.Now})
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
			t.Errorf("server stopped with error: %v", err)
		}
	})
	return server, newTestClient(t)
}
