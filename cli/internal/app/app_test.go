package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/legacyremote"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtimeutil"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type injectedClock struct{ value time.Time }

func (clock injectedClock) Now() time.Time { return clock.value }
func (injectedClock) After(time.Duration) <-chan time.Time {
	return make(chan time.Time)
}

func TestHelpDocumentsStableCommandsAndStructuredInput(t *testing.T) {
	application, stdout, _ := newTestApp(t)
	if err := application.Execute(context.Background(), []string{"help"}); err != nil {
		t.Fatalf("Execute(help) error = %v", err)
	}
	output := stdout.String()
	for _, expected := range []string{
		"login", "logout", "profile", "article", "account", "album", "sync", "download", "metadata", "comments",
		"credential", "proxy", "job", "export", "db", "diagnostics", "completion", "legacy", "--json",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("help output missing %q:\n%s", expected, output)
		}
	}
}

func TestProxyCommandsCoverLifecycleAndRedactAuthorization(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	manager := &fakeProxyManager{}
	applicationAdapter.proxy = manager
	if err := applicationAdapter.Execute(context.Background(), []string{
		"proxy", "add", "public", "--endpoint", "https://proxy.example/wrap?authorization=url-secret",
		"--authorization", "proxy-secret", "--classes", "public_resource,public_content", "--priority", "7", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if manager.added.Name != "public" || manager.added.Priority != 7 || manager.added.Authorization != "proxy-secret" {
		t.Fatalf("proxy add request = %#v", manager.added)
	}
	for _, forbidden := range []string{"proxy-secret", "url-secret"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("proxy add output leaked %q: %s", forbidden, stdout.String())
		}
	}

	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"proxy", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"count": 1`) || strings.Contains(stdout.String(), "secret-ref") {
		t.Fatalf("proxy list output = %s", stdout.String())
	}

	for _, operation := range []string{"disable", "enable", "test", "remove"} {
		stdout.Reset()
		if err := applicationAdapter.Execute(context.Background(), []string{"proxy", operation, "public", "--json"}); err != nil {
			t.Fatalf("proxy %s: %v", operation, err)
		}
	}
	if strings.Join(manager.operations, ",") != "disable:public,enable:public,test:public,remove:public" {
		t.Fatalf("proxy operations = %v", manager.operations)
	}
}

func TestCredentialTrustedProxyRequiresExactExplicitConfirmation(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	manager := &fakeProxyManager{}
	applicationAdapter.proxy = manager
	args := []string{
		"proxy", "add", "trusted", "--endpoint", "https://trusted.example/wrap",
		"--trust", "credential-trusted", "--classes", "comments,engagement_metrics,paid_content", "--json",
	}
	err := applicationAdapter.Execute(context.Background(), args)
	required := network.CredentialTrustConfirmation("trusted")
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), required) ||
		!strings.Contains(err.Error(), "cookie, key, pass_ticket, appmsg_token") ||
		!strings.Contains(err.Error(), "comments, engagement_metrics, paid_content") ||
		!strings.Contains(err.Error(), "https://trusted.example/wrap") {
		t.Fatalf("credential trust confirmation error = %v", err)
	}
	if manager.addCalls != 0 {
		t.Fatalf("unconfirmed trusted proxy was added")
	}

	args = append(args[:len(args)-1], "--confirm", required, "--json")
	if err := applicationAdapter.Execute(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if manager.addCalls != 1 || manager.added.Trust != network.TrustCredential {
		t.Fatalf("confirmed add = %#v calls=%d", manager.added, manager.addCalls)
	}
}

func TestLocalLoginRequiresQROutputWhenNonInteractive(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	err := applicationAdapter.Execute(context.Background(), []string{"login", "--json"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "--qr-output") {
		t.Fatalf("error = %v, exit = %d", err, ExitCode(err))
	}
}

func TestLocalLogoutUsesSharedApplication(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	fake := &sessionApplication{fixedApplication: fixedApplication{}, status: wechat.Session{State: wechat.SessionAuthenticated}}
	applicationAdapter.core = fake
	if err := applicationAdapter.Execute(context.Background(), []string{"logout", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !fake.logoutCalled || !strings.Contains(stdout.String(), `"localSessionRemoved": true`) {
		t.Fatalf("logoutCalled=%v output=%s", fake.logoutCalled, stdout.String())
	}
}

func TestLegacyCommandPreservesRemoteStatusBehavior(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	server := "https://example.com"
	if err := applicationAdapter.store.Write(config.File{
		Server: server,
		Tokens: &config.Tokens{AccessToken: "legacy-access", RefreshToken: "legacy-refresh"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"legacy", "status", "--json"}); err != nil {
		t.Fatalf("Execute(legacy status) error = %v", err)
	}
	if strings.Contains(stdout.String(), "legacy-access") || strings.Contains(stdout.String(), "legacy-refresh") {
		t.Fatalf("legacy status leaked token: %s", stdout.String())
	}
	var envelope struct {
		Data struct {
			Server        string `json:"server"`
			Authenticated bool   `json:"authenticated"`
			Legacy        bool   `json:"legacy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Server != server || !envelope.Data.Authenticated || !envelope.Data.Legacy {
		t.Fatalf("legacy status = %#v", envelope.Data)
	}
}

func TestProfileCommandsCreateUseListAndConfirmDelete(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	applicationAdapter.profiles = profiles.NewRegistry(paths, secrets.NewMemoryStore())
	if err := applicationAdapter.Execute(context.Background(), []string{"profile", "create", "left", "--json"}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"profile", "create", "right", "--json"}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"profile", "use", "right", "--json"}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"profile", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"count": 2`) || !strings.Contains(stdout.String(), `"active": true`) {
		t.Fatalf("profile list output = %s", stdout.String())
	}
	stdout.Reset()
	err = applicationAdapter.Execute(context.Background(), []string{"profile", "delete", "left", "--json"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "--confirm delete-profile:left") {
		t.Fatalf("unconfirmed delete error = %v", err)
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"profile", "delete", "left", "--confirm", "delete-profile:left", "--json"}); err != nil {
		t.Fatal(err)
	}
}

func TestProfileUseRebuildsProfileIsolatedRuntime(t *testing.T) {
	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	secretStore := secrets.NewMemoryStore()
	registry := profiles.NewRegistry(paths, secretStore)
	if _, err := registry.Create("left"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Create("right"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Use("left"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC)
	stdout := &bytes.Buffer{}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), stdout, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root},
		Clock:       injectedClock{value: now},
		Secrets:     secretStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })

	if applicationAdapter.active.Profile.ID != "left" || applicationAdapter.active.Library.Path() != paths.ForProfile("left").Database {
		t.Fatalf("initial runtime = %#v", applicationAdapter.active)
	}
	if err := applicationAdapter.active.Library.UpsertAccount(context.Background(), library.AccountRecord{
		ID: "left-account", FakeID: "left-fakeid", Name: "Left Account",
	}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"profile", "use", "right", "--json"}); err != nil {
		t.Fatal(err)
	}
	if applicationAdapter.active.Profile.ID != "right" || applicationAdapter.active.Library.Path() != paths.ForProfile("right").Database {
		t.Fatalf("activated runtime = %#v", applicationAdapter.active)
	}
	status, err := applicationAdapter.core.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Profile != "right" || status.Paths.Data != paths.ForProfile("right").Data || status.CheckedAt != now {
		t.Fatalf("right status = %#v", status)
	}
	accounts, err := applicationAdapter.core.QueryAccounts(context.Background(), domain.AccountQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if accounts.Total != 0 {
		t.Fatalf("right profile leaked left accounts: %#v", accounts)
	}
	if applicationAdapter.active.Jobs == nil || applicationAdapter.active.Objects == nil || applicationAdapter.active.WeChat == nil || applicationAdapter.active.Network == nil {
		t.Fatalf("runtime modules not wired: %#v", applicationAdapter.active)
	}
}

func TestNewWithDependenciesInjectsClockHTTPSecretsAndApplicationFactory(t *testing.T) {
	now := time.Date(2026, 7, 22, 16, 30, 0, 0, time.UTC)
	secretStore := secrets.NewMemoryStore()
	var captured *ProfileRuntime
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()},
		Clock:       injectedClock{value: now},
		HTTP:        roundTripDoer(func(request *http.Request) (*http.Response, error) { return nil, context.Canceled }),
		Secrets:     secretStore,
		ApplicationFactory: func(runtime *ProfileRuntime) application.Application {
			captured = runtime
			return application.New(application.Options{
				Version: Version,
				Runtime: runtimeenv.Dependencies{Clock: injectedClock{value: now}, Profile: runtime.Profile.ID, Secrets: secretStore},
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	if captured == nil || captured.Profile.ID == "" || captured.Library == nil || captured.Objects == nil || captured.Jobs == nil || captured.WeChat == nil || captured.Network == nil {
		t.Fatalf("factory runtime was not fully wired")
	}
	status, err := applicationAdapter.core.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.CheckedAt != now || status.SecretBackend != secretStore.Backend() {
		t.Fatalf("injected status = %#v", status)
	}
}

type roundTripDoer func(*http.Request) (*http.Response, error)

func (doer roundTripDoer) Do(request *http.Request) (*http.Response, error) { return doer(request) }

type fixedBrowserDiscovery struct {
	browser runtimeenv.Browser
	err     error
}

func (discovery fixedBrowserDiscovery) FindChromium(context.Context) (runtimeenv.Browser, error) {
	return discovery.browser, discovery.err
}

func TestInjectedBrowserDiscoveryRejectsEmptyExecutable(t *testing.T) {
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()},
		Browser:     fixedBrowserDiscovery{},
		Secrets:     secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	err = applicationAdapter.openBrowser(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "empty executable") {
		t.Fatalf("openBrowser error = %v", err)
	}
}

func TestDryRunIsRedactedAndDoesNotNeedNetwork(t *testing.T) {
	application, stdout, _ := newTestApp(t)
	err := application.Execute(context.Background(), []string{
		"legacy", "api", "call", "download_article",
		"--input", `{"url":"https://mp.weixin.qq.com/s/example","auth_key":"secret"}`,
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("Execute(dry-run) error = %v", err)
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("dry-run leaked secret: %s", stdout.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("dry-run output is not JSON: %v", err)
	}
	data, ok := output["data"].(map[string]any)
	if !ok || data["dryRun"] != true {
		t.Fatalf("dryRun envelope = %#v", output)
	}
}

func TestOutputAppliesCentralRedactionBeforeJSONSerialization(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	applicationAdapter.jsonOut = true
	value := struct {
		URL     string          `json:"url"`
		Token   string          `json:"token"`
		Raw     json.RawMessage `json:"raw"`
		Visible string          `json:"visible"`
	}{
		URL:     "https://mp.weixin.qq.com/s/example?pass_ticket=query-secret&keep=yes",
		Token:   "field-secret",
		Raw:     json.RawMessage(`{"authorization":"raw-secret","trace":"trace-visible"}`),
		Visible: "visible",
	}
	if err := applicationAdapter.output(&value); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"query-secret", "field-secret", "raw-secret"} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("JSON output leaked %q: %s", forbidden, stdout.String())
		}
	}
	for _, retained := range []string{"keep=yes", "trace-visible", "visible"} {
		if !strings.Contains(stdout.String(), retained) {
			t.Fatalf("JSON output removed %q: %s", retained, stdout.String())
		}
	}
}

func TestAmbiguousInputAndCredentialedServerAreUsageErrors(t *testing.T) {
	application, _, _ := newTestApp(t)
	err := application.Execute(context.Background(), []string{"legacy", "api", "call", "download_article", "--input", "{}", "--stdin"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "exactly one JSON input source") {
		t.Fatalf("ambiguous input error = %v, code = %d", err, ExitCode(err))
	}

	application, _, _ = newTestApp(t)
	err = application.Execute(context.Background(), []string{"legacy", "status", "--server", "https://user:password@example.com"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "must not contain credentials") || strings.Contains(err.Error(), "password@example") {
		t.Fatalf("credentialed server error = %v, code = %d", err, ExitCode(err))
	}
}

func TestCobraInvocationErrorsUseExitCodeTwo(t *testing.T) {
	application, _, _ := newTestApp(t)
	err := application.Execute(context.Background(), []string{"--unknown"})
	if ExitCode(err) != 2 {
		t.Fatalf("unknown flag exit code = %d, error = %v", ExitCode(err), err)
	}
	application, _, _ = newTestApp(t)
	err = application.Execute(context.Background(), []string{"status", "extra"})
	if ExitCode(err) != 2 {
		t.Fatalf("extra argument exit code = %d, error = %v", ExitCode(err), err)
	}
}

func TestCobraStatusMatchesApplicationRuntimeStatus(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	want := domain.RuntimeStatus{
		Version: "contract-test", Profile: "profile-contract",
		Paths:         domain.RuntimePaths{Config: "/contract/config", Data: "/contract/data"},
		SecretBackend: "fake", Storage: domain.StorageStatus{DatabaseAvailable: true, Articles: 7},
	}
	applicationAdapter.core = fixedApplication{status: want}
	if err := applicationAdapter.Execute(context.Background(), []string{"status", "--json"}); err != nil {
		t.Fatalf("Execute(status) error = %v", err)
	}
	var envelope struct {
		Data struct {
			Runtime domain.RuntimeStatus `json:"runtime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Runtime != want {
		t.Fatalf("Cobra status = %#v, application status = %#v", envelope.Data.Runtime, want)
	}
}

func TestProcessContractReusesSavedTokenAndCallsDomainAlias(t *testing.T) {
	server := newMCPTestServer(t)
	application, stdout, _ := newTestApp(t)
	configPath := filepath.Join(t.TempDir(), "cli.json")
	application.store = config.NewStore(configPath)
	application.legacy = legacyremote.New(application.store, Version, http.DefaultClient)
	if err := application.store.Write(config.File{
		Server: server.URL,
		Tokens: &config.Tokens{AccessToken: "process-test-token", TokenType: "bearer", RefreshToken: "refresh-secret"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := application.Execute(context.Background(), []string{"legacy", "article", "download", "https://mp.weixin.qq.com/s/example", "--format", "text"}); err != nil {
		t.Fatalf("Execute(article download) error = %v", err)
	}
	if strings.Contains(stdout.String(), "process-test-token") || strings.Contains(stdout.String(), "refresh-secret") {
		t.Fatalf("CLI output leaked tokens: %s", stdout.String())
	}
	var envelope struct {
		Data mcp.CallToolResult `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("call output is not an MCP result: %v", err)
	}
	output := envelope.Data
	if len(output.Content) != 1 {
		t.Fatalf("content = %#v", output.Content)
	}
	text, ok := output.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "text:https://mp.weixin.qq.com/s/example" {
		t.Fatalf("content[0] = %#v", output.Content[0])
	}
	runtimeutil.AssertPrivatePermissions(t, configPath, 0o600)
}

func newTestApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	application, err := NewWithDependencies(context.Background(), strings.NewReader(""), stdout, stderr, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()},
		Secrets:     secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Close() })
	application.store = config.NewStore(filepath.Join(t.TempDir(), "cli.json"))
	application.legacy = legacyremote.New(application.store, Version, http.DefaultClient)
	return application, stdout, stderr
}

func newMCPTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "process-test", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "download_article", Description: "download article"}, func(_ context.Context, _ *mcp.CallToolRequest, input struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Format + ":" + input.URL}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mcp" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer process-test-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(testServer.Close)
	return testServer
}

type fakeProxyManager struct {
	added      network.AddProxyRequest
	addCalls   int
	operations []string
}

func (manager *fakeProxyManager) Add(_ context.Context, request network.AddProxyRequest) (network.RouteConfig, error) {
	manager.addCalls++
	manager.added = request
	return network.RouteConfig{
		ID: "route-a", Name: request.Name, Kind: network.RouteURLWrapper,
		Endpoint: request.Endpoint, AuthorizationRef: "secret-ref", AuthorizationConfigured: request.Authorization != "",
		Trust: request.Trust, Classes: request.Classes, Priority: request.Priority, Enabled: true,
	}, nil
}

func (manager *fakeProxyManager) List(context.Context) ([]network.RouteConfig, error) {
	return []network.RouteConfig{{
		ID: "route-a", Name: "public", Kind: network.RouteURLWrapper,
		Endpoint: "https://proxy.example/wrap?authorization=url-secret", AuthorizationRef: "secret-ref",
		AuthorizationConfigured: true, Trust: network.TrustPublicOnly,
		Classes: []network.RequestClass{network.PublicContent}, Priority: 7, Enabled: true,
	}}, nil
}

func (manager *fakeProxyManager) Remove(_ context.Context, id string) (network.RouteConfig, error) {
	manager.operations = append(manager.operations, "remove:"+id)
	return network.RouteConfig{ID: "route-a", Name: id}, nil
}

func (manager *fakeProxyManager) Enable(_ context.Context, id string) (network.RouteConfig, error) {
	manager.operations = append(manager.operations, "enable:"+id)
	return network.RouteConfig{ID: "route-a", Name: id, Enabled: true}, nil
}

func (manager *fakeProxyManager) Disable(_ context.Context, id string) (network.RouteConfig, error) {
	manager.operations = append(manager.operations, "disable:"+id)
	return network.RouteConfig{ID: "route-a", Name: id, Enabled: false}, nil
}

func (manager *fakeProxyManager) Test(_ context.Context, id string) (network.ProbeResult, error) {
	manager.operations = append(manager.operations, "test:"+id)
	return network.ProbeResult{Route: network.RouteConfig{ID: "route-a", Name: id}, ResponseValid: true}, nil
}

type fixedApplication struct{ status domain.RuntimeStatus }

type sessionApplication struct {
	fixedApplication
	status       wechat.Session
	logoutCalled bool
}

func (application *sessionApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return application.status, nil
}

func (application *sessionApplication) Logout(context.Context) error {
	application.logoutCalled = true
	return nil
}

func (application fixedApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return application.status, nil
}
func (fixedApplication) BeginLogin(context.Context, string) (wechat.LoginFlow, error) {
	return wechat.LoginFlow{}, nil
}
func (fixedApplication) PollLogin(context.Context) (wechat.PollResult, error) {
	return wechat.PollResult{}, nil
}
func (fixedApplication) CompleteLogin(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (fixedApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (fixedApplication) ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error) {
	return []wechat.SwitchableAccount{}, nil
}
func (fixedApplication) SwitchAccount(context.Context, string) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (fixedApplication) Logout(context.Context) error { return nil }
func (fixedApplication) SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (fixedApplication) ResolveAccountName(context.Context, string) (string, error) { return "", nil }
func (fixedApplication) ResolveAccountFromArticle(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (fixedApplication) AccountDetails(context.Context, string) (wechat.AccountDetails, error) {
	return wechat.AccountDetails{}, nil
}
func (fixedApplication) AuthorInfo(context.Context, string) (wechat.AuthorInfo, error) {
	return wechat.AuthorInfo{}, nil
}
func (fixedApplication) ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	return wechat.ArticlePage{}, nil
}
func (fixedApplication) SaveAccount(context.Context, domain.Account) (domain.Account, error) {
	return domain.Account{}, nil
}
func (fixedApplication) UpdateAccount(context.Context, domain.Account) (domain.Account, error) {
	return domain.Account{}, nil
}
func (fixedApplication) GetAccount(context.Context, domain.AccountID) (domain.Account, error) {
	return domain.Account{}, nil
}
func (fixedApplication) GetAccountByFakeID(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (fixedApplication) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return domain.AccountManifest{}, nil
}
func (fixedApplication) ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error) {
	return domain.AccountImportReport{}, nil
}
func (fixedApplication) DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error) {
	return domain.AccountDeleteReport{}, nil
}
func (fixedApplication) QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (fixedApplication) QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error) {
	return domain.Page[domain.Article]{}, nil
}
func (fixedApplication) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return domain.Page[domain.Album]{}, nil
}
func (fixedApplication) SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (fixedApplication) StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (fixedApplication) StartExport(context.Context, domain.ExportRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (fixedApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (fixedApplication) QueryJobs(context.Context, domain.JobQuery) (domain.Page[domain.Job], error) {
	return domain.Page[domain.Job]{}, nil
}
func (fixedApplication) CancelJob(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (application fixedApplication) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return application.status.Storage, nil
}
func (fixedApplication) DiscoverBrowser(context.Context) (runtimeenv.Browser, error) {
	return runtimeenv.Browser{}, application.ErrUnavailable
}
func (fixedApplication) ProcessSignals() <-chan os.Signal { return nil }
