package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
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
	workspaceURL := mustParseURL(t, server.URL())
	if workspaceURL.Host != server.listener.Addr().String() || workspaceURL.Port() == "" || workspaceURL.Port() == "0" || workspaceURL.Query().Get("token") == "" {
		t.Fatalf("workspace URL = %q; want exact random loopback listener and bootstrap credential", workspaceURL)
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

func TestBootstrapRejectsGuessesAndMultipleTokensWithoutLeakingCredentials(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	bootstrapURL := server.URL()
	bootstrap := mustParseURL(t, bootstrapURL)
	secret := bootstrap.Query().Get("token")
	base := strings.TrimSuffix(strings.Split(bootstrapURL, "?")[0], "/")
	privatePath := "/private/browser/profile/session.json"

	for name, target := range map[string]string{
		"guessed":   base + "/?token=guessed-bootstrap-token&path=" + url.QueryEscape(privatePath),
		"multiple":  base + "/?token=" + url.QueryEscape(secret) + "&token=guessed-bootstrap-token",
		"reordered": base + "/?token=guessed-bootstrap-token&token=" + url.QueryEscape(secret),
	} {
		t.Run(name, func(t *testing.T) {
			response := get(t, client, target)
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("bootstrap rejection status = %d; want 401", response.StatusCode)
			}
			assertResponseDoesNotLeak(t, response, secret, privatePath)
		})
	}

	// Rejected attempts must neither authorize a session nor consume the exact
	// one-time bootstrap credential.
	authorize(t, client, bootstrapURL)
	if got := get(t, client, bootstrapURL).StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("reused bootstrap token status = %d; want 401", got)
	}
}

func TestHostConfusionIsRejectedBeforeBootstrapOrSessionAuthorization(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	bootstrapURL := server.URL()
	bootstrap := mustParseURL(t, bootstrapURL)
	secret := bootstrap.Query().Get("token")
	listenerAddress := server.listener.Addr().String()

	for name, host := range map[string]string{
		"missing port":        "127.0.0.1",
		"localhost alias":     "localhost:" + bootstrap.Port(),
		"comma separated":     listenerAddress + ",evil.example",
		"user info confusion": "127.0.0.1@evil.example:" + bootstrap.Port(),
		"suffix":              listenerAddress + ".evil.example",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, bootstrapURL, nil)
			request.Host = host
			request.RemoteAddr = "127.0.0.1:54321"
			response := httptest.NewRecorder()
			server.handler().ServeHTTP(response, request)
			if response.Code != http.StatusMisdirectedRequest {
				t.Fatalf("Host %q status = %d; want 421", host, response.Code)
			}
			assertRecordedResponseDoesNotLeak(t, response, secret, "/private/browser/profile/session.json")
		})
	}

	// A hostile Host must be rejected before the bootstrap token can be spent.
	authorize(t, client, bootstrapURL)
}

func TestSessionCookiesAreScopedAndLogoutClearsThem(t *testing.T) {
	server, client := startTestServer(t, time.Now)
	bootstrapURL := server.URL()
	noRedirect := *client
	noRedirect.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := noRedirect.Get(bootstrapURL)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSeeOther {
		response.Body.Close()
		t.Fatalf("bootstrap status = %d; want 303", response.StatusCode)
	}
	assertSessionCookieAttributes(t, response.Cookies(), false)
	response.Body.Close()

	base := strings.TrimSuffix(strings.Split(bootstrapURL, "?")[0], "/")
	csrf := cookieFor(t, client, mustParseURL(t, base), csrfCookieName).Value
	logout := requestWith(t, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("{}"), map[string]string{
		"Origin": base, "Content-Type": "application/json", "X-CSRF-Token": csrf,
	})
	response, err = client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		response.Body.Close()
		t.Fatalf("logout status = %d; want 204", response.StatusCode)
	}
	assertSessionCookieAttributes(t, response.Cookies(), true)
	response.Body.Close()
	if got := get(t, client, base+"/api/v1/status").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d; want 401", got)
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
	assertSecurityHeaders(t, index.Header)

	asset := get(t, client, base+"/"+entrypoint.File)
	if asset.StatusCode != http.StatusOK || !strings.HasPrefix(asset.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("GET entrypoint status=%d content-type=%q", asset.StatusCode, asset.Header.Get("Content-Type"))
	}
	assertSecurityHeadersExceptCacheControl(t, asset.Header)
	if got := asset.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("entrypoint Cache-Control = %q; want immutable cache policy", got)
	}
	if got := asset.Header.Get("Pragma"); got != "" {
		t.Fatalf("entrypoint Pragma = %q; want absent so immutable assets remain cacheable", got)
	}
	if body := readResponse(t, asset); body == "" {
		t.Fatal("embedded entrypoint was empty")
	}

	fallback := get(t, client, base+"/articles")
	if fallback.StatusCode != http.StatusOK {
		t.Fatalf("GET SPA route status = %d; want 200", fallback.StatusCode)
	}
	assertSecurityHeaders(t, fallback.Header)
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

// A workspace that keeps polling must not die at sessionTTL: the bootstrap
// credential is one-time, so an expiring-under-use session leaves the open tab
// with no recovery path and every later request looking like a lost login.
func TestActiveSessionSlidesItsIdleTimeout(t *testing.T) {
	// Anchored to the real clock: the client cookie jar evaluates the renewed
	// cookie's Expires against wall time, so a fixed past epoch would drop it.
	now := time.Now()
	server := newTestServer(t, func() time.Time { return now })
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil }}
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	authorize(t, client, server.URL())

	// Four polls spread over twice the TTL keep the session alive throughout.
	for step := 0; step < 4; step++ {
		now = now.Add(defaultSessionTTL / 2)
		if got := get(t, client, base+"/api/v1/status").StatusCode; got != http.StatusOK {
			t.Fatalf("status after %s of continuous use = %d", time.Duration(step+1)*defaultSessionTTL/2, got)
		}
	}

	// Idling past the full TTL still expires it.
	now = now.Add(defaultSessionTTL + time.Second)
	if got := get(t, client, base+"/api/v1/status").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("idle session status = %d", got)
	}

	cancel()
	<-done
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

func assertSecurityHeadersExceptCacheControl(t *testing.T, header http.Header) {
	t.Helper()
	for key, want := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff",
		"X-Frame-Options": "DENY",
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

func assertResponseDoesNotLeak(t *testing.T, response *http.Response, values ...string) {
	t.Helper()
	body := readResponse(t, response)
	assertNoSensitiveValues(t, body+"\n"+fmt.Sprint(response.Header), values...)
}

func assertRecordedResponseDoesNotLeak(t *testing.T, response *httptest.ResponseRecorder, values ...string) {
	t.Helper()
	assertNoSensitiveValues(t, response.Body.String()+"\n"+fmt.Sprint(response.Header()), values...)
}

func assertNoSensitiveValues(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if value != "" && strings.Contains(output, value) {
			t.Fatalf("response leaked sensitive value %q: %q", value, output)
		}
	}
}

func assertSessionCookieAttributes(t *testing.T, cookies []*http.Cookie, clearing bool) {
	t.Helper()
	seen := map[string]*http.Cookie{}
	for _, cookie := range cookies {
		seen[cookie.Name] = cookie
	}
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		cookie := seen[name]
		if cookie == nil {
			t.Fatalf("%s cookie was not set", name)
		}
		if cookie.Path != "/" || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("%s cookie scope = %#v; want Path=/ and SameSite=Strict", name, cookie)
		}
		if cookie.HttpOnly != (name == sessionCookieName) {
			t.Fatalf("%s HttpOnly = %t", name, cookie.HttpOnly)
		}
		if clearing {
			if cookie.MaxAge >= 0 || cookie.Value != "" {
				t.Fatalf("cleared %s cookie = %#v; want expired empty cookie", name, cookie)
			}
			continue
		}
		if cookie.Value == "" || cookie.MaxAge != 0 || cookie.Expires.IsZero() {
			t.Fatalf("issued %s cookie = %#v; want non-empty session cookie with expiry", name, cookie)
		}
	}
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
func (testApplication) ResolveArticleAlbums(context.Context, string) (wechat.ArticleAlbums, error) {
	return wechat.ArticleAlbums{}, nil
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
func (testApplication) AccountNames(context.Context, []domain.AccountID) (map[domain.AccountID]string, error) {
	return map[domain.AccountID]string{}, nil
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
