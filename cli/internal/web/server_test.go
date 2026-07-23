package web

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	runtimeenv "github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestWorkspaceBindsOnlyRandomIPv4Loopback(t *testing.T) {
	server := newTestServer(t, time.Now)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if got := server.listener.Addr().Network(); got != "tcp" {
		t.Fatalf("listener network = %q", got)
	}
	address := server.listener.Addr().String()
	if !strings.HasPrefix(address, "127.0.0.1:") || strings.HasSuffix(address, ":0") {
		t.Fatalf("listener address = %q; want random IPv4 loopback", address)
	}
	if !strings.HasPrefix(server.URL(), "http://127.0.0.1:") || !strings.Contains(server.URL(), "?token=") {
		t.Fatalf("workspace URL = %q", server.URL())
	}
}

func TestBootstrapTokenCreatesOneSessionAndClearsURL(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	bootstrapURL := server.URL()
	response, err := client.Get(bootstrapURL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || strings.Contains(response.Request.URL.String(), "token=") || !strings.Contains(string(body), "id=\"root\"") {
		t.Fatalf("bootstrap result status=%d url=%q body=%q", response.StatusCode, response.Request.URL, body)
	}
	if cookie := cookieFor(t, client, response.Request.URL, sessionCookieName); cookie.Value == "" {
		t.Fatalf("session cookie = %#v; want non-empty", cookie)
	}
	if got := get(t, client, bootstrapURL).StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("reused bootstrap token status = %d; want 401", got)
	}
}

func TestAuthenticatedWorkspaceServesEmbeddedAssetsAndSPAFallback(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	manifest := mustEmbeddedManifest(t)
	entrypoint := manifest["index.html"]

	for _, target := range []string{"/", "/articles", "/" + entrypoint.File} {
		if got := get(t, client, base+target).StatusCode; got != http.StatusUnauthorized {
			t.Fatalf("unauthorized GET %s status = %d; want 401", target, got)
		}
	}

	authorize(t, client, server.URL())
	index := get(t, client, base+"/")
	if index.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d; want 200", index.StatusCode)
	}
	indexBody := readResponse(t, index)
	if !strings.Contains(indexBody, "/"+entrypoint.File) || !strings.Contains(indexBody, "/"+entrypoint.CSS[0]) {
		t.Fatalf("embedded index did not reference manifest entrypoint")
	}

	asset := get(t, client, base+"/"+entrypoint.File)
	if asset.StatusCode != http.StatusOK || !strings.HasPrefix(asset.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("GET entrypoint status=%d content-type=%q", asset.StatusCode, asset.Header.Get("Content-Type"))
	}
	if body := readResponse(t, asset); body == "" {
		t.Fatal("embedded entrypoint was empty")
	}

	fallback := get(t, client, base+"/articles")
	if fallback.StatusCode != http.StatusOK {
		t.Fatalf("GET SPA route status = %d; want 200", fallback.StatusCode)
	}
	if body := readResponse(t, fallback); body != indexBody {
		t.Fatal("SPA fallback did not serve the embedded application shell")
	}
}

func TestStatusRequiresSessionAndHasSecurityHeaders(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	unauthorized := get(t, client, base+"/api/v1/status")
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}
	assertSecurityHeaders(t, unauthorized.Header)

	authorize(t, client, server.URL())
	response := get(t, client, base+"/api/v1/status")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized status = %d", response.StatusCode)
	}
	assertSecurityHeaders(t, response.Header)
	body := readResponse(t, response)
	if !strings.Contains(body, `"runtime"`) {
		t.Fatalf("status body was missing safe runtime data")
	}
}

func TestHostOriginCSRFLimitsAndLogout(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	bootstrapURL := server.URL()
	authorize(t, client, bootstrapURL)
	base := strings.TrimSuffix(strings.Split(bootstrapURL, "?")[0], "/")
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value

	for name, request := range map[string]*http.Request{
		"host":   requestWith(t, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("{}"), map[string]string{"Host": "localhost", "Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf}),
		"origin": requestWith(t, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("{}"), map[string]string{"Origin": "http://evil.example", "Content-Type": "application/json", "X-CSRF-Token": csrf}),
		"csrf":   requestWith(t, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("{}"), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": "wrong"}),
		"type":   requestWith(t, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("x"), map[string]string{"Origin": base, "Content-Type": "text/plain", "X-CSRF-Token": csrf}),
	} {
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s request: %v", name, err)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatalf("%s mutation status = %d; want rejection", name, response.StatusCode)
		}
	}

	logout := requestWith(t, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("{}"), map[string]string{"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf})
	response, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d", response.StatusCode)
	}
	if response := get(t, client, base+"/api/v1/status"); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d", response.StatusCode)
	}
}

func TestSessionExpiryAndCancellationInvalidateCredentials(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	server := newTestServer(t, func() time.Time { return now })
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil }}
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	authorize(t, client, server.URL())
	now = now.Add(defaultSessionTTL + time.Second)
	if got := get(t, client, base+"/api/v1/status").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve cancellation = %v", err)
	}
	if server.URL() != "" {
		t.Fatal("workspace URL remained available after shutdown")
	}
}

func TestServerShutdownClearsOutstandingStagedRestoreUploads(t *testing.T) {
	backend := &shutdownUploadBackend{}
	uploads, err := application.NewUploadStaging(application.UploadStagingOptions{
		Backend: backend,
		NewID:   func() (application.UploadHandle, error) { return "upload-shutdown-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	restore, err := application.NewRestore(application.RestoreOptions{Uploads: uploads, Coordinator: shutdownRestoreCoordinator{}})
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Application: testApplication{}, Restore: restore})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restore.Stage(context.Background(), strings.NewReader("archive"), int64(len("archive"))); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if backend.deletes != 1 {
		t.Fatalf("shutdown deleted %d staged uploads, want 1", backend.deletes)
	}
}

func newTestServer(t *testing.T, now func() time.Time) *Server {
	t.Helper()
	server, err := New(Options{Application: testApplication{}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func startTestServer(t *testing.T, now func() time.Time) (*Server, *http.Client) {
	t.Helper()
	server := newTestServer(t, now)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	t.Cleanup(func() {
		_ = server.Close()
		if err := <-serveDone; err != nil {
			t.Errorf("test server stopped with error: %v", err)
		}
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return server, &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil }}
}

func authorize(t *testing.T, client *http.Client, target string) {
	t.Helper()
	noRedirect := *client
	noRedirect.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := noRedirect.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("bootstrap status = %d; want 303", response.StatusCode)
	}
}

func get(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func requestWith(t *testing.T, method, target string, body io.Reader, headers map[string]string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		if key == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(key, value)
	}
	return request
}

func cookieFor(t *testing.T, client *http.Client, target *url.URL, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range client.Jar.Cookies(target) {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q was not set", name)
	return nil
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	for key, want := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff",
		"X-Frame-Options": "DENY", "Cache-Control": "no-store",
	} {
		if got := header.Get(key); !strings.Contains(got, want) {
			t.Fatalf("%s = %q; want %q", key, got, want)
		}
	}
}

func readResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

type testApplication struct{}

type shutdownUploadBackend struct{ deletes int }

func (backend *shutdownUploadBackend) Stage(_ context.Context, source io.Reader, _ int64) (application.UploadStagedObject, error) {
	if _, err := io.ReadAll(source); err != nil {
		return application.UploadStagedObject{}, err
	}
	return application.UploadStagedObject{Reference: "private"}, nil
}
func (backend *shutdownUploadBackend) Open(context.Context, application.UploadStagedObject) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (backend *shutdownUploadBackend) Delete(context.Context, application.UploadStagedObject) error {
	backend.deletes++
	return nil
}

type shutdownRestoreCoordinator struct{}

func (shutdownRestoreCoordinator) Restore(context.Context, io.Reader, application.RestoreConflictPolicy) (application.RestoreCompletion, error) {
	return application.RestoreCompletion{}, nil
}

func (testApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return domain.RuntimeStatus{Profile: "test"}, nil
}
func (testApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{State: wechat.SessionMissing}, nil
}
func (testApplication) BeginLogin(context.Context, string) (wechat.LoginFlow, error) {
	return wechat.LoginFlow{}, nil
}
func (testApplication) PollLogin(context.Context) (wechat.PollResult, error) {
	return wechat.PollResult{}, nil
}
func (testApplication) CompleteLogin(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (testApplication) ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error) {
	return nil, nil
}
func (testApplication) SwitchAccount(context.Context, string) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (testApplication) Logout(context.Context) error { return nil }
func (testApplication) SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (testApplication) ResolveAccountName(context.Context, string) (string, error) { return "", nil }
func (testApplication) ResolveAccountFromArticle(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (testApplication) AccountDetails(context.Context, string) (wechat.AccountDetails, error) {
	return wechat.AccountDetails{}, nil
}
func (testApplication) AuthorInfo(context.Context, string) (wechat.AuthorInfo, error) {
	return wechat.AuthorInfo{}, nil
}
func (testApplication) ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	return wechat.ArticlePage{}, nil
}
func (testApplication) SaveAccount(context.Context, domain.Account) (domain.Account, error) {
	return domain.Account{}, nil
}
func (testApplication) UpdateAccount(context.Context, domain.Account) (domain.Account, error) {
	return domain.Account{}, nil
}
func (testApplication) GetAccount(context.Context, domain.AccountID) (domain.Account, error) {
	return domain.Account{}, nil
}
func (testApplication) GetAccountByFakeID(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (testApplication) QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (testApplication) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return domain.AccountManifest{}, nil
}
func (testApplication) ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error) {
	return domain.AccountImportReport{}, nil
}
func (testApplication) DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error) {
	return domain.AccountDeleteReport{}, nil
}
func (testApplication) QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error) {
	return domain.Page[domain.Article]{}, nil
}
func (testApplication) SaveArticleQuery(context.Context, string, domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	return domain.SavedArticleQuery{}, nil
}
func (testApplication) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return nil, nil
}
func (testApplication) DeleteSavedArticleQuery(context.Context, string) (bool, error) {
	return false, nil
}
func (testApplication) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return domain.Page[domain.Album]{}, nil
}
func (testApplication) SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (testApplication) SynchronizeAlbum(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (testApplication) StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (testApplication) StartExport(context.Context, domain.ExportRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (testApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (testApplication) QueryJobs(context.Context, domain.JobQuery) (domain.Page[domain.Job], error) {
	return domain.Page[domain.Job]{}, nil
}
func (testApplication) CancelJob(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (testApplication) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return domain.StorageStatus{}, nil
}
func (testApplication) DiscoverBrowser(context.Context) (runtimeenv.Browser, error) {
	return runtimeenv.Browser{}, nil
}
func (testApplication) ProcessSignals() <-chan os.Signal { return nil }

var _ application.Application = testApplication{}
