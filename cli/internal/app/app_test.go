package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type injectedClock struct{ value time.Time }

func (clock injectedClock) Now() time.Time { return clock.value }
func (injectedClock) After(time.Duration) <-chan time.Time {
	return make(chan time.Time)
}

func TestSecretBackendEnvironmentSupportsEphemeralSmokeRuntime(t *testing.T) {
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "memory")
	store, err := defaultSecretStoreFromEnvironment(paths, strings.NewReader(""), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if store.Backend() != "memory-ephemeral" {
		t.Fatalf("Backend() = %q", store.Backend())
	}

	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "plaintext")
	if _, err := defaultSecretStoreFromEnvironment(paths, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("unsupported secret backend was accepted")
	}
}

func TestHistoricalConstructorSurfacesInitializationFailureOnExecute(t *testing.T) {
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "unsupported")
	applicationAdapter := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	err := applicationAdapter.Execute(context.Background(), []string{"status"})
	if err == nil || !strings.Contains(err.Error(), "WECHAT_ARTICLE_SECRET_BACKEND") {
		t.Fatalf("initialization error = %v", err)
	}
}

func TestInitializationFailureStillAllowsHelpVersionAndVaultBootstrap(t *testing.T) {
	root := t.TempDir()
	passphrasePath := filepath.Join(root, "passphrase.txt")
	if err := os.WriteFile(passphrasePath, []byte("bootstrap-passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ARTICLE_PORTABLE_ROOT", root)
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "vault")
	t.Setenv("WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE", "")
	t.Setenv("WECHAT_ARTICLE_VAULT_PASSPHRASE", "")
	stdout := &bytes.Buffer{}
	applicationAdapter := New(strings.NewReader(""), stdout, &bytes.Buffer{})
	if err := applicationAdapter.Execute(context.Background(), []string{"help"}); err != nil {
		t.Fatalf("help after init failure: %v", err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"--version"}); err != nil {
		t.Fatalf("version after init failure: %v", err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"status", "--help"}); err != nil {
		t.Fatalf("subcommand help after init failure: %v", err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"vault", "init", "--passphrase-file", passphrasePath, "--json"}); err != nil {
		t.Fatalf("vault init after init failure: %v", err)
	}
}

func TestAppWithExplicitStartupArgsRejectsDifferentExecutionArguments(t *testing.T) {
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()}, Secrets: secrets.NewMemoryStore(),
		StartupArgs: []string{"status", "--json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer applicationAdapter.Close()
	if err := applicationAdapter.Execute(context.Background(), []string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"vault", "status", "--json"}); ExitCode(err) != 2 {
		t.Fatalf("mismatched execution error=%v exit=%d", err, ExitCode(err))
	}
}

func TestNewWithDependenciesUsesExplicitStartupArgsNotProcessArgs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "vault")
	_, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, StartupArgs: []string{"status"},
	})
	if err == nil || !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("explicit status startup error=%v", err)
	}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, StartupArgs: []string{"vault", "status"},
	})
	if err != nil {
		t.Fatalf("explicit vault startup: %v", err)
	}
	applicationAdapter.Close()
}

func TestNewDoesNotBindHostProcessArguments(t *testing.T) {
	original := os.Args
	os.Args = []string{"host-process", "vault", "status"}
	t.Cleanup(func() { os.Args = original })
	applicationAdapter := New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	defer applicationAdapter.Close()
	if len(applicationAdapter.startupArgs) != 0 {
		t.Fatalf("New startupArgs=%#v", applicationAdapter.startupArgs)
	}
}

func TestVaultCommandsInitializeVerifyAndNeverEchoPassphrase(t *testing.T) {
	root := t.TempDir()
	passphrase := "correct horse battery staple"
	passphrasePath := filepath.Join(root, "vault-passphrase.txt")
	if err := os.WriteFile(passphrasePath, []byte(passphrase+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), stdout, stderr, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root},
		Secrets:     secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })

	if err := applicationAdapter.Execute(context.Background(), []string{
		"vault", "init", "--passphrase-file", passphrasePath, "--json",
	}); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(output, passphrase) {
			t.Fatalf("vault init leaked passphrase: %s", output)
		}
	}
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.VaultFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("vault permissions = %o", info.Mode().Perm())
	}

	stdout.Reset()
	stderr.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{
		"vault", "verify", "--passphrase-file", passphrasePath, "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"verified": true`) || strings.Contains(stdout.String(), passphrase) {
		t.Fatalf("vault verify output = %s", stdout.String())
	}

	if err := os.WriteFile(passphrasePath, []byte("wrong-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{
		"vault", "verify", "--passphrase-file", passphrasePath, "--json",
	}); err == nil || !strings.Contains(err.Error(), "invalid passphrase") {
		t.Fatalf("vault verify wrong passphrase error = %v", err)
	}
}

func TestVaultStatusReportsConfiguredBackendAndInvalidEnvelope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "vault")
	stdout := &bytes.Buffer{}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), stdout, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root},
		Secrets:     secrets.NewMemoryStore(),
		StartupArgs: []string{"vault", "status", "--json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.VaultFile(), []byte(`{"version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"vault", "status", "--json"}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	if !strings.Contains(output, `"backend": "vault"`) || !strings.Contains(output, `"active": true`) ||
		!strings.Contains(output, `"initialized": false`) || !strings.Contains(output, `"invalid": true`) {
		t.Fatalf("vault status=%s", output)
	}
}

func TestVaultBackendUnlocksAcrossApplicationRestart(t *testing.T) {
	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("restart-safe-passphrase")
	store := secrets.NewVaultStore(paths.VaultFile(), secrets.VaultParameters{Memory: 8 * 1024, Iterations: 1, Parallelism: 1})
	if err := store.Initialize(passphrase); err != nil {
		t.Fatal(err)
	}
	store.Lock()
	passphrasePath := filepath.Join(root, "passphrase.txt")
	if err := os.WriteFile(passphrasePath, append(append([]byte(nil), passphrase...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "vault")
	t.Setenv("WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE", passphrasePath)

	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	if applicationAdapter.secret.Backend() != "encrypted-vault" {
		t.Fatalf("secret backend = %q", applicationAdapter.secret.Backend())
	}
}

func TestVaultBackendFailsClosedWithoutInitializationOrSafeInput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WECHAT_ARTICLE_SECRET_BACKEND", "vault")
	t.Setenv("WECHAT_ARTICLE_VAULT_PASSPHRASE", "")
	t.Setenv("WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE", "")
	_, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root},
	})
	if err == nil || (!strings.Contains(err.Error(), "requires") && !strings.Contains(err.Error(), "not initialized")) {
		t.Fatalf("non-interactive vault startup error = %v", err)
	}
}

func TestHelpDocumentsStableCommandsAndStructuredInput(t *testing.T) {
	application, stdout, _ := newTestApp(t)
	if err := application.Execute(context.Background(), []string{"help"}); err != nil {
		t.Fatalf("Execute(help) error = %v", err)
	}
	output := stdout.String()
	for _, expected := range []string{
		"login", "logout", "profile", "article", "account", "album", "sync", "download", "metadata", "comments",
		"credential", "proxy", "job", "export", "db", "diagnostics", "vault", "completion", "--json",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("help output missing %q:\n%s", expected, output)
		}
	}
	for _, command := range application.rootCommand().Commands() {
		if command.Name() == "legacy" {
			t.Fatalf("retired legacy command is still registered")
		}
	}
}

func TestSavedArticleQueryCommandRequiresVersionedValidatedDocument(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	directory := t.TempDir()
	valid := filepath.Join(directory, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"schemaVersion":1,"query":{"keyword":"release","limit":50,"sorts":[{"field":"published","direction":"desc"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"article", "query", "save", "release", "--file", valid, "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"name": "release"`) {
		t.Fatalf("saved query output=%s", stdout.String())
	}

	for name, contents := range map[string]string{
		"raw":     `{"keyword":"release"}`,
		"range":   `{"schemaVersion":1,"query":{"readMin":10,"readMax":1}}`,
		"version": `{"schemaVersion":2,"query":{}}`,
	} {
		path := filepath.Join(directory, name+".json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := applicationAdapter.Execute(context.Background(), []string{"article", "query", "save", name, "--file", path, "--json"}); err == nil {
			t.Fatalf("invalid %s query was accepted", name)
		}
	}
}

func TestLocalMCPContentReaderKeepsJSONContractAndRenderedDigests(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	ctx := context.Background()
	if err := applicationAdapter.active.Library.UpsertArticle(ctx, library.ArticleRecord{
		ID: "article-mcp", Title: "MCP article", CanonicalURL: "https://mp.weixin.qq.com/s/mcp", ContentStatus: "available",
	}); err != nil {
		t.Fatal(err)
	}
	bodyBytes, err := os.ReadFile(filepath.Join("..", "processor", "testdata", "valid_cgidata.html"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	object, err := applicationAdapter.active.Objects.Put(ctx, strings.NewReader(body), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitContent(ctx, "article-mcp", objects.Object{
		Digest: object.Digest, Size: object.Size, MediaType: "text/html",
	}, "html", "https://mp.weixin.qq.com/s/mcp", "available", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	reader := localMCPContentReader{runtime: applicationAdapter.active}
	jsonValue, err := reader.ReadContent(ctx, "article-mcp", "json")
	if err != nil || jsonValue == nil {
		t.Fatalf("json content=%#v err=%v", jsonValue, err)
	}
	for _, kind := range []string{"text", "markdown"} {
		value, err := reader.ReadContent(ctx, "article-mcp", kind)
		if err != nil {
			t.Fatal(err)
		}
		result := value.(map[string]any)
		if result["sha256"] == object.Digest {
			t.Fatalf("%s digest reused source HTML digest", kind)
		}
		if kind == "text" && result["mediaType"] != "text/plain" {
			t.Fatalf("text media type=%v", result["mediaType"])
		}
	}
}

func TestLocalMCPContentReaderHashesTheSameHandleItReads(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	ctx := context.Background()
	if err := applicationAdapter.active.Library.UpsertArticle(ctx, library.ArticleRecord{
		ID: "article-mcp-integrity", Title: "MCP integrity", CanonicalURL: "https://mp.weixin.qq.com/s/mcp-integrity", ContentStatus: "available",
	}); err != nil {
		t.Fatal(err)
	}
	body := `<div id="js_article"></div><script>window.cgiData={"title":"MCP integrity","content":"<p>safe</p>"};</script>`
	object, err := applicationAdapter.active.Objects.Put(ctx, strings.NewReader(body), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitContent(ctx, "article-mcp-integrity", objects.Object{
		Digest: object.Digest, Size: object.Size, MediaType: "text/html",
	}, "html", "https://mp.weixin.qq.com/s/mcp-integrity", "available", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	opened := make(chan struct{})
	resume := make(chan struct{})
	reader := localMCPContentReader{runtime: applicationAdapter.active, afterOpen: func() {
		close(opened)
		<-resume
	}}
	result := make(chan error, 1)
	go func() {
		_, readErr := reader.ReadContent(ctx, "article-mcp-integrity", "html")
		result <- readErr
	}()
	<-opened
	path := filepath.Join(applicationAdapter.active.Objects.Root(), "sha256", object.Digest[:2], object.Digest[2:4], object.Digest)
	replacement := path + ".replacement"
	if err := os.WriteFile(replacement, []byte(strings.Repeat("x", len(body))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := <-result; err != nil {
		t.Fatalf("same-handle read rejected content changed only after open: %v", err)
	}

	if _, err := (localMCPContentReader{runtime: applicationAdapter.active}).ReadContent(ctx, "article-mcp-integrity", "html"); !errors.Is(err, objects.ErrIntegrity) {
		t.Fatalf("corrupt current handle error=%v", err)
	}
}

func TestDiagnosticsBundleWritesOnePrivateRedactedArchive(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	job, err := applicationAdapter.active.Jobs.Create(context.Background(), jobs.Spec{
		Kind: "diagnostic-fixture", Profile: applicationAdapter.active.Profile.ID,
		Payload: map[string]any{"summary": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.active.Jobs.AppendLog(context.Background(), job.ID, "", "error",
		"request failed Cookie: sid=bundle-cookie-secret; request-id=diagnostic-visible",
		map[string]any{"access_token": "bundle-token-secret", "body": "innocent-key-article-body", "routeId": "direct", "requestId": "diagnostic-visible"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "diagnostics.zip")
	if err := applicationAdapter.Execute(context.Background(), []string{"diagnostics", "bundle", "--output", path, "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"article bodies"`) || !strings.Contains(stdout.String(), `"sha256"`) {
		t.Fatalf("diagnostics output = %s", stdout.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("diagnostics permissions = %o", info.Mode().Perm())
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "diagnostics.json" {
		t.Fatalf("diagnostics entries = %#v", reader.File)
	}
	entry, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(entry)
	closeErr := entry.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	for _, forbidden := range []string{"articleBodies", `"secrets"`, "pass_ticket", "bundle-cookie-secret", "bundle-token-secret", "innocent-key-article-body"} {
		if strings.Contains(string(contents), forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, contents)
		}
	}
	for _, required := range []string{"schemaVersion", "integrity", "configuration", "system", "diagnostic-fixture", "diagnostic-visible", `"routeId": "direct"`} {
		if !strings.Contains(string(contents), required) {
			t.Fatalf("diagnostics missing %q: %s", required, contents)
		}
	}

	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"diagnostics", "bundle", "--output", path, "--json"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("overwrite error = %v", err)
	}
}

func TestDiagnosticLogsRespectPerEntryAndTotalBudgets(t *testing.T) {
	large := strings.Repeat("x", diagnosticLogEntryBudget*2)
	logs := make([]library.JobLog, 200)
	for index := range logs {
		logs[index] = library.JobLog{Message: large, Fields: map[string]any{"value": large}}
	}
	remaining := diagnosticLogTotalBudget
	bounded := boundedDiagnosticLogs(logs, &remaining)
	encoded, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > diagnosticLogTotalBudget+len(bounded)+2 {
		t.Fatalf("bounded logs bytes=%d", len(encoded))
	}
	if len(bounded) == 0 || !strings.Contains(bounded[0].Message, "[truncated]") || len(bounded[0].Fields) != 0 {
		t.Fatalf("bounded log=%#v", bounded)
	}
}

func TestDiagnosticsBundleKeepsSubsystemFailuresAsBoundedReceipts(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	applicationAdapter.core = diagnosticFailureApplication{fixedApplication: fixedApplication{}, failure: strings.Repeat("diagnostic failure ", 10_000)}
	path := filepath.Join(t.TempDir(), "diagnostics.zip")
	if _, err := applicationAdapter.createDiagnosticBundle(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entry, err := reader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	contents, readErr := io.ReadAll(entry)
	closeErr := entry.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal(errors.Join(readErr, closeErr))
	}
	if !strings.Contains(string(contents), `"runtimeError"`) || !strings.Contains(string(contents), `"sessionError"`) ||
		!strings.Contains(string(contents), `"jobsError"`) || !strings.Contains(string(contents), `"browserError"`) ||
		!strings.Contains(string(contents), "[truncated]") {
		t.Fatalf("diagnostic failure receipts=%s", contents)
	}
	if len(contents) > 512<<10 {
		t.Fatalf("diagnostic failure receipts are unbounded: %d bytes", len(contents))
	}
}

func TestWriteDiagnosticBundleDoesNotReplaceConcurrentDestination(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "diagnostics.zip")
	if err := os.WriteFile(path, []byte("created-by-another-process"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := writeDiagnosticBundle(path, map[string]any{"system": map[string]any{"goos": "fixture"}})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("writeDiagnosticBundle overwrite error = %v", err)
	}
	contents, readErr := os.ReadFile(path)
	if readErr != nil || string(contents) != "created-by-another-process" {
		t.Fatalf("concurrent destination = %q, %v", contents, readErr)
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

func TestRetiredLegacyCommandIsAUsageError(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	err := applicationAdapter.Execute(context.Background(), []string{"legacy", "status", "--json"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("legacy command error=%v exit=%d", err, ExitCode(err))
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

func TestRestoreCoordinatorBlocksRunningJobsBeforeClosingRuntime(t *testing.T) {
	root := t.TempDir()
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	job, err := applicationAdapter.active.Jobs.Create(context.Background(), jobs.Spec{Kind: "export", Profile: applicationAdapter.active.Profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Jobs.StartJob(context.Background(), job.ID, "worker", time.Minute); err != nil {
		t.Fatal(err)
	}
	_, err = applicationAdapter.restoreActiveProfile(context.Background(), filepath.Join(root, "missing.zip"), library.RestoreRefuseConflicts)
	if !errors.Is(err, profiles.ErrProfileBusy) {
		t.Fatalf("restore blocker error = %v", err)
	}
	if _, err := applicationAdapter.active.Jobs.Get(context.Background(), job.ID); err != nil {
		t.Fatalf("active runtime was closed before restore blocker check: %v", err)
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

func TestWorkspaceExportUsesFormatSpecificCommentPreference(t *testing.T) {
	root := t.TempDir()
	captured := &capturingExportApplication{fixedApplication: fixedApplication{}}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
		ApplicationFactory: func(*ProfileRuntime) application.Application { return captured },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	store := profiles.NewConfigStore(applicationAdapter.active.Profile.Paths.Config)
	if _, err := store.Update(func(configuration *profiles.ProfileConfig) error {
		configuration.Preferences.Export.HTMLIncludeComments = false
		configuration.Preferences.Export.JSONIncludeComments = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	extensions := newWorkspaceExtensions(applicationAdapter).(*workspaceExtensions)
	for _, test := range []struct {
		format string
		want   bool
	}{{format: "html", want: false}, {format: "json", want: true}, {format: "markdown", want: false}} {
		t.Run(test.format, func(t *testing.T) {
			_, err := extensions.startExport(context.Background(), tui.OperationRequest{
				Area: tui.AreaArticles, IDs: []string{"article-a"},
				Parameters: map[string]string{"format": test.format, "outputRoot": filepath.Join(root, "exports", test.format)},
			})
			if err != nil {
				t.Fatal(err)
			}
			actual, ok := captured.request.Options.FormatOptions["comments"].(bool)
			if !ok || actual != test.want {
				t.Fatalf("%s comments option = %#v, want %t", test.format, captured.request.Options.FormatOptions["comments"], test.want)
			}
		})
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
	return application, stdout, stderr
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

type diagnosticFailureApplication struct {
	fixedApplication
	failure string
}

func (application diagnosticFailureApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return domain.RuntimeStatus{}, errors.New(application.failure)
}

func (application diagnosticFailureApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{}, errors.New(application.failure)
}

func (application diagnosticFailureApplication) QueryJobs(context.Context, domain.JobQuery) (domain.Page[domain.Job], error) {
	return domain.Page[domain.Job]{}, errors.New(application.failure)
}

func (application diagnosticFailureApplication) DiscoverBrowser(context.Context) (runtimeenv.Browser, error) {
	return runtimeenv.Browser{}, errors.New(application.failure)
}

type sessionApplication struct {
	fixedApplication
	status       wechat.Session
	logoutCalled bool
}

type capturingExportApplication struct {
	fixedApplication
	request domain.ExportRequest
}

func (application *capturingExportApplication) StartExport(_ context.Context, request domain.ExportRequest) (domain.Job, error) {
	application.request = request
	return domain.Job{ID: "job-export", State: domain.JobQueued}, nil
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
func (fixedApplication) SaveArticleQuery(context.Context, string, domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	return domain.SavedArticleQuery{}, nil
}
func (fixedApplication) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return nil, nil
}
func (fixedApplication) DeleteSavedArticleQuery(context.Context, string) (bool, error) {
	return false, nil
}
func (fixedApplication) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return domain.Page[domain.Album]{}, nil
}
func (fixedApplication) SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error) {
	return domain.Job{}, nil
}
func (fixedApplication) SynchronizeAlbum(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error) {
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
