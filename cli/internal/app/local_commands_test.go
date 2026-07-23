package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/download"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/migration"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	runtimeenv "github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	syncrunner "github.com/wechat-article/wechat-article-exporter/cli/internal/sync"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestLocalCommandGroupsArePresentInHelp(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	if err := applicationAdapter.Execute(context.Background(), []string{"help"}); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{
		"profile", "session", "account", "article", "album", "sync", "download", "metadata", "comments",
		"credential", "proxy", "job", "export", "db", "migration", "diagnostics", "mcp", "completion",
	} {
		if !strings.Contains(stdout.String(), group) {
			t.Fatalf("root help missing command group %q:\n%s", group, stdout.String())
		}
	}
}

func TestNoSubcommandRoutesInteractiveStartupToFullWorkspaceWithSavedDisplayPreferences(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	configuration := profiles.DefaultConfig(string(applicationAdapter.active.Profile.ID))
	configuration.Preferences.Sync.PageSize = 23
	configuration.Preferences.Display = profiles.DisplayPreferences{NoColor: true, ASCII: true, Plain: true, Language: "zh-CN"}
	if err := profiles.NewConfigStore(applicationAdapter.active.Profile.Paths.Config).Write(configuration); err != nil {
		t.Fatal(err)
	}
	called := false
	applicationAdapter.workspaceRunner = func(_ context.Context, options tui.WorkspaceOptions) error {
		called = true
		if options.Application != applicationAdapter.core || options.Extensions == nil ||
			options.Input != applicationAdapter.stdin || options.Output != applicationAdapter.stdout ||
			!options.NoColor || !options.ASCII || !options.Plain || options.Language != "zh-CN" || options.PageSize != 23 {
			t.Fatalf("workspace options = %#v", options)
		}
		return nil
	}
	if err := applicationAdapter.runDashboard(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("full Bubble Tea workspace runner was not called")
	}
}

func TestMigrationCommandsImportAndVerifyWebArchive(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	archive := buildAppLegacyArchive(t)

	if err := applicationAdapter.Execute(context.Background(), []string{"migration", "inspect", archive, "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"schemaVersion": 1`) || !strings.Contains(stdout.String(), `"plannedRecords": 2`) {
		t.Fatalf("migration inspect output = %s", stdout.String())
	}

	stdout.Reset()
	confirmation := "import-legacy:" + archive
	if err := applicationAdapter.Execute(context.Background(), []string{
		"migration", "import", archive, "--confirm", confirmation, "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"success": true`) || !strings.Contains(stdout.String(), `"recordsInserted": 2`) {
		t.Fatalf("migration import output = %s", stdout.String())
	}

	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"migration", "verify", archive, "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"success": true`) || !strings.Contains(stdout.String(), `"missingRecords": []`) {
		t.Fatalf("migration verify output = %s", stdout.String())
	}
}

func buildAppLegacyArchive(t *testing.T) string {
	t.Helper()
	accounts := []byte(`[{"key":"account-a","value":{"fakeid":"account-a","nickname":"Legacy"}}]`)
	articles := []byte(`[{"key":"article-a","value":{"fakeid":"account-a","aid":"aid-a","title":"Legacy article","link":"https://mp.weixin.qq.com/s/legacy-a","create_time":1700000000}}]`)
	files := map[string][]byte{"records/accounts.json": accounts, "records/articles.json": articles}
	manifestFiles := make([]migration.ManifestFile, 0, len(files))
	for path, body := range files {
		digest := sha256.Sum256(body)
		dataset := migration.DatasetAccounts
		if strings.Contains(path, "articles") {
			dataset = migration.DatasetArticles
		}
		manifestFiles = append(manifestFiles, migration.ManifestFile{Path: path, Kind: migration.FileRecords,
			Dataset: dataset, Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])})
	}
	sort.Slice(manifestFiles, func(i, j int) bool { return manifestFiles[i].Path < manifestFiles[j].Path })
	manifestBody, err := json.Marshal(migration.Manifest{Format: migration.ArchiveFormat,
		SchemaVersion: migration.CurrentSchemaVersion, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		Source: migration.SourceInfo{DexieDatabase: "exporter.wxdown.online", DexieSchemaVersion: 3}, Files: manifestFiles})
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "legacy.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(file)
	entries := append([]migration.ManifestFile{{Path: migration.ManifestPath}}, manifestFiles...)
	for _, entry := range entries {
		part, createErr := zipWriter.Create(entry.Path)
		if createErr != nil {
			t.Fatal(createErr)
		}
		body := files[entry.Path]
		if entry.Path == migration.ManifestPath {
			body = manifestBody
		}
		if _, writeErr := part.Write(body); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func TestLocalJSONSuccessEnvelopeIsOnePureDocument(t *testing.T) {
	applicationAdapter, stdout, stderr := newTestApp(t)
	applicationAdapter.core = fixedApplication{status: domain.RuntimeStatus{Version: "test", Profile: "local"}}
	if err := applicationAdapter.Execute(context.Background(), []string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
		Success       bool   `json:"success"`
		Data          struct {
			Runtime domain.RuntimeStatus `json:"runtime"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains trailing data: error=%v value=%#v", err, trailing)
	}
	if envelope.SchemaVersion != JSONSchemaVersion || !envelope.Success || envelope.Data.Runtime.Profile != "local" {
		t.Fatalf("success envelope = %#v", envelope)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("status emitted unexpected stderr: %q", stderr.String())
	}
}

func TestLocalStatusJSONExposesAllEffectiveRetainedPreferences(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	store := profiles.NewConfigStore(applicationAdapter.active.Profile.Paths.Config)
	configuration := profiles.DefaultConfig(string(applicationAdapter.active.Profile.ID))
	configuration.Preferences.Sync = profiles.SyncPreferences{
		Range: "point", DatePoint: time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC),
		PageDelay: 2 * time.Second, Jitter: 300 * time.Millisecond, PageSize: 17,
		Incremental: false, UnsafePacingSaved: true,
	}
	configuration.Preferences.Download = profiles.DownloadPreferences{
		Concurrency: 8, ForceContent: true, MetadataOverridesContent: true,
	}
	configuration.Preferences.Export = profiles.ExportPreferences{
		Root: "/tmp/effective-exports", NamingTemplate: "{account}-{title}", MaximumNameBytes: 144,
		CollisionPolicy: "suffix", ExcelIncludeContent: false, JSONIncludeContent: false,
		JSONIncludeComments: false, HTMLIncludeComments: false,
	}
	configuration.Preferences.Display = profiles.DisplayPreferences{
		NoColor: true, ASCII: true, Plain: true, HideDeleted: false,
	}
	configuration.Preferences.Proxy = profiles.ProxyPreferences{DirectFirst: false, FallbackEnabled: true}
	configuration.MCP = profiles.MCPPolicy{ReadOnly: true, Allow: []string{"query_articles"}}
	if err := store.Write(configuration); err != nil {
		t.Fatal(err)
	}
	written, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}

	if err := applicationAdapter.Execute(context.Background(), []string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Configuration profiles.EffectiveConfig `json:"configuration"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("status output is not JSON: %v\n%s", err, stdout.String())
	}
	effective := envelope.Data.Configuration
	if effective.Path != store.Path() || effective.SchemaVersion != profiles.CurrentConfigVersion ||
		effective.ProfileID != string(applicationAdapter.active.Profile.ID) {
		t.Fatalf("effective configuration identity = %#v", effective)
	}
	if effective.Preferences.Sync.Range != written.Preferences.Sync.Range ||
		!effective.Preferences.Sync.DatePoint.Equal(written.Preferences.Sync.DatePoint) ||
		effective.Preferences.Sync.PageDelay != written.Preferences.Sync.PageDelay ||
		effective.Preferences.Sync.Jitter != written.Preferences.Sync.Jitter ||
		effective.Preferences.Sync.PageSize != written.Preferences.Sync.PageSize ||
		effective.Preferences.Sync.Incremental != written.Preferences.Sync.Incremental ||
		effective.Preferences.Sync.UnsafePacingSaved != written.Preferences.Sync.UnsafePacingSaved ||
		!reflect.DeepEqual(effective.Preferences.Download, written.Preferences.Download) ||
		!reflect.DeepEqual(effective.Preferences.Export, written.Preferences.Export) ||
		!reflect.DeepEqual(effective.Preferences.Display, written.Preferences.Display) ||
		!reflect.DeepEqual(effective.Preferences.Proxy, written.Preferences.Proxy) ||
		effective.Preferences.DownloadConcurrency != written.Preferences.DownloadConcurrency ||
		effective.Preferences.ExportRoot != written.Preferences.ExportRoot ||
		effective.Preferences.NoColor != written.Preferences.NoColor ||
		effective.MCP.ReadOnly != written.MCP.ReadOnly || !reflect.DeepEqual(effective.MCP.Allow, written.MCP.Allow) ||
		len(effective.MCP.Deny) != len(written.MCP.Deny) {
		t.Fatalf("effective preferences = %#v, want %#v", effective.Preferences, written.Preferences)
	}
}

func TestLocalErrorExitCodesAndVersionedJSON(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	usageErr := applicationAdapter.Execute(context.Background(), []string{"download", "article", "--json"})
	if ExitCode(usageErr) != 2 {
		t.Fatalf("usage exit code = %d, error = %v", ExitCode(usageErr), usageErr)
	}
	var usageJSON bytes.Buffer
	if err := WriteErrorJSON(&usageJSON, usageErr); err != nil {
		t.Fatal(err)
	}
	assertErrorEnvelope(t, usageJSON.Bytes(), "usage", 2)

	applicationAdapter.core = &commandJobApplication{states: []domain.JobState{domain.JobQueued}}
	runtimeErr := applicationAdapter.Execute(context.Background(), []string{
		"metadata", "download", "--article", "article-a", "--json",
	})
	if runtimeErr != nil {
		t.Fatalf("metadata command should create a local job: %v", runtimeErr)
	}
	applicationAdapter.core = unavailableDownloadApplication{fixedApplication: fixedApplication{}}
	runtimeErr = applicationAdapter.Execute(context.Background(), []string{
		"download", "article", "--article", "article-a", "--json",
	})
	if ExitCode(runtimeErr) != 1 {
		t.Fatalf("runtime exit code = %d, error = %v", ExitCode(runtimeErr), runtimeErr)
	}
	var runtimeJSON bytes.Buffer
	if err := WriteErrorJSON(&runtimeJSON, runtimeErr); err != nil {
		t.Fatal(err)
	}
	assertErrorEnvelope(t, runtimeJSON.Bytes(), "runtime", 1)
}

func TestExitCodeHandlesTypedNilResultError(t *testing.T) {
	var result *ResultError
	var err error = result
	if code := ExitCode(err); code != 1 {
		t.Fatalf("typed-nil result error exit code=%d", code)
	}
	var output bytes.Buffer
	if writeErr := WriteErrorJSON(&output, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	assertErrorEnvelope(t, output.Bytes(), "runtime", 1)
}

type unavailableDownloadApplication struct{ fixedApplication }

func (unavailableDownloadApplication) StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error) {
	return domain.Job{}, errors.New("download runtime unavailable")
}

func TestAsyncJobStartWaitAndFollowKeepProgressOnStderr(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		extraArgs  []string
		states     []domain.JobState
		wantFollow bool
	}{
		{name: "start", states: []domain.JobState{domain.JobQueued}},
		{name: "wait", extraArgs: []string{"--wait", "--poll-interval", "100ms"}, states: []domain.JobState{domain.JobCompleted}},
		{name: "follow", extraArgs: []string{"--follow", "--poll-interval", "100ms"}, states: []domain.JobState{domain.JobQueued, domain.JobCompleted}, wantFollow: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			applicationAdapter, stdout, stderr := newTestApp(t)
			jobs := &commandJobApplication{states: scenario.states}
			applicationAdapter.core = jobs
			args := []string{"download", "article", "--article", "article-a", "--json"}
			args = append(args, scenario.extraArgs...)
			if err := applicationAdapter.Execute(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				SchemaVersion string     `json:"schemaVersion"`
				Success       bool       `json:"success"`
				Data          domain.Job `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("job stdout is not pure JSON: %v\n%s", err, stdout.String())
			}
			if envelope.SchemaVersion != JSONSchemaVersion || !envelope.Success || envelope.Data.ID != "job-local" {
				t.Fatalf("job envelope = %#v", envelope)
			}
			if strings.Contains(stdout.String(), "queued (") || strings.Contains(stdout.String(), "job job-local:") {
				t.Fatalf("progress leaked to stdout: %s", stdout.String())
			}
			if scenario.name != "wait" && strings.TrimSpace(stderr.String()) == "" {
				t.Fatalf("expected job guidance/progress on stderr")
			}
			if scenario.wantFollow && (!strings.Contains(stderr.String(), "queued") || !strings.Contains(stderr.String(), "completed")) {
				t.Fatalf("follow stderr = %q", stderr.String())
			}
		})
	}
}

func TestNoWaitDownloadDetachedWorkerCompletesAfterParentReturns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `<html><body><div id="js_article"><div id="js_content">hello</div></div>`+
			`<script>window.cgiDataNew={title:'Detached',user_name:'gh_fixture',content_noencode:'hello',comment_id:'comment-a'}</script></body></html>`)
	}))
	t.Cleanup(server.Close)

	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	database, err := library.Open(context.Background(), library.OpenOptions{
		Path: paths.ForProfile("default").Database, ProfileID: "default", ProfileName: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertAccount(context.Background(), library.AccountRecord{ID: "account-a", FakeID: "fake-a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(context.Background(), library.ArticleRecord{ID: "article-a", AccountID: "account-a",
		Aid: "article-a", Title: "Article", CanonicalURL: server.URL + "/article", ContentStatus: "missing"}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	launcher := &inProcessWorkerLauncher{run: func(ctx context.Context, environment []string, args []string) error {
		worker, workerErr := NewWithDependencies(ctx, strings.NewReader(""), io.Discard, io.Discard, Dependencies{
			PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
		})
		if workerErr != nil {
			return workerErr
		}
		worker.active.Downloads, workerErr = newLocalDownloadRuntime(worker.active, worker.secret, http.DefaultClient, downloadRuntimeOptions{
			DestinationPolicy: network.DestinationPolicy{AllowedHosts: map[string]struct{}{"127.0.0.1": {}},
				AllowLoopback: true, Resolver: loopbackResolver{}},
		})
		if workerErr != nil {
			return workerErr
		}
		defer worker.Close()
		return worker.Execute(ctx, args)
	}}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(), Worker: launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	applicationAdapter.active.Downloads, err = newLocalDownloadRuntime(applicationAdapter.active, applicationAdapter.secret, http.DefaultClient,
		downloadRuntimeOptions{DestinationPolicy: network.DestinationPolicy{AllowedHosts: map[string]struct{}{"127.0.0.1": {}},
			AllowLoopback: true, Resolver: loopbackResolver{}}})
	if err != nil {
		t.Fatal(err)
	}
	applicationAdapter.core = application.New(application.Options{Version: Version,
		Runtime: runtimeenv.Dependencies{Profile: "default"}, Library: applicationAdapter.active.Library,
		Jobs: applicationAdapter.active.Jobs, Downloads: applicationAdapter.active.Downloads,
		Starter: persistentJobStarter{executable: "in-process", paths: paths, profile: "default", launcher: launcher},
	})

	if err := applicationAdapter.Execute(context.Background(), []string{
		"download", "article", "--article", "article-a", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-launcher.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("detached worker did not finish")
	}
	jobPage, err := applicationAdapter.core.QueryJobs(context.Background(), domain.JobQuery{Kind: "article_download", Limit: 10})
	if err != nil || len(jobPage.Items) != 1 || jobPage.Items[0].State != domain.JobCompleted {
		items, _ := applicationAdapter.active.Jobs.ListItems(context.Background(), jobPage.Items[0].ID)
		t.Fatalf("jobs=%#v items=%#v err=%v", jobPage, items, err)
	}
}

func TestControlledOriginDependenciesRequireExactPairedPolicy(t *testing.T) {
	origin, err := url.Parse("http://127.0.0.1:43125")
	if err != nil {
		t.Fatal(err)
	}
	valid := network.DestinationPolicy{AllowedHosts: map[string]struct{}{"127.0.0.1": {}},
		AllowedAuthorities: map[string]struct{}{"127.0.0.1:43125": {}}, AllowLoopback: true}
	if err := validateControlledOriginDependencies(origin, valid); err != nil {
		t.Fatalf("valid policy: %v", err)
	}
	if err := validateControlledOriginDependencies(nil, valid); err == nil {
		t.Fatal("policy without controlled origin was accepted")
	}
	if err := validateControlledOriginDependencies(nil, network.DestinationPolicy{AllowedHosts: map[string]struct{}{}}); err == nil {
		t.Fatal("empty non-nil policy override without controlled origin was accepted")
	}
	if err := validateControlledOriginDependencies(origin, network.DestinationPolicy{}); err == nil {
		t.Fatal("controlled origin without exact policy was accepted")
	}
	wrongAuthority := valid
	wrongAuthority.AllowedAuthorities = map[string]struct{}{"127.0.0.1:43126": {}}
	if err := validateControlledOriginDependencies(origin, wrongAuthority); err == nil {
		t.Fatal("controlled origin with mismatched authority was accepted")
	}
}

func TestSingleArticleDownloadRepairsProvisionalIdentityAndKeepsContentReadable(t *testing.T) {
	html := `<html><body><div id="js_article"><div id="js_content">identity body</div></div>` +
		`<script>window.cgiDataNew={title:'Identity repaired',author:'Author',user_name:'gh_real',bizuin:'biz-real',` +
		`nick_name:'Real account',appmsgid:'aid-real',link:'https://mp.weixin.qq.com/s/identity-real',` +
		`content_noencode:'<p>identity body</p>',comment_id:'comment-real'}</script></body></html>`
	root := t.TempDir()
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	runtime := &localDownloadRuntime{profile: applicationAdapter.active.Profile.ID, library: applicationAdapter.active.Library,
		objects: applicationAdapter.active.Objects, service: download.JobService{
			Store: applicationAdapter.active.Jobs, Engine: jobs.EngineOptions{Owner: "identity-repair-test"},
			Articles: download.ArticleDownloader{Processor: processor.New(), Objects: applicationAdapter.active.Objects,
				Store: applicationAdapter.active.Library, Network: network.StaticClient{RouteName: "fixture", Call: func(context.Context, network.Request) (network.Result, error) {
					return network.Result{Route: "fixture", RequestID: "identity-repair", Response: &http.Response{
						StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(html)),
					}}, nil
				}}},
		}}
	job, err := runtime.Start(context.Background(), domain.DownloadRequest{URLs: []string{
		"https://mp.weixin.qq.com/s/identity-provisional?utm_source=ignored",
	}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := applicationAdapter.active.Jobs.ListItems(context.Background(), job.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	var item struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(items[0].Key), &item); err != nil {
		t.Fatal(err)
	}
	var envelope download.ArticleRequest
	if err := json.Unmarshal(item.Payload, &envelope); err != nil {
		t.Fatal(err)
	}
	provisionalID := envelope.ArticleID
	final, err := runtime.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	repaired, err := applicationAdapter.active.Library.GetArticle(context.Background(), provisionalID)
	if err != nil {
		t.Fatal(err)
	}
	account, err := applicationAdapter.active.Library.GetAccount(context.Background(), repaired.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ID != provisionalID || repaired.Aid != "aid-real" || account.FakeID != "biz-real" ||
		account.Name != "Real account" || repaired.Title != "Identity repaired" || !repaired.HasContent {
		t.Fatalf("repaired=%#v account=%#v provisional=%s", repaired, account, provisionalID)
	}
	content, err := applicationAdapter.active.Library.CurrentContent(context.Background(), repaired.ID, "html")
	if err != nil || content.ObjectDigest == "" {
		t.Fatalf("content=%#v err=%v", content, err)
	}
	preview, err := newWorkspaceExtensions(applicationAdapter).PreviewArticle(context.Background(), repaired.ID)
	if err != nil || !strings.Contains(preview.Text, "identity body") || strings.Contains(preview.Text, "<script") {
		t.Fatalf("preview=%#v err=%v", preview, err)
	}
}

func TestResumeJobRelaunchesPersistentWorker(t *testing.T) {
	root := t.TempDir()
	launcher := &recordingWorkerLauncher{}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
		Executable: "wechat-article-test", Worker: launcher,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	job, err := applicationAdapter.active.Jobs.CreateWithItems(context.Background(), jobs.Spec{
		Kind: "article_download", Profile: "default",
	}, []string{"item-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Jobs.Pause(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"job", "resume", string(job.ID), "--json"}); err != nil {
		t.Fatal(err)
	}
	if launcher.calls != 1 || launcher.executable != "wechat-article-test" ||
		!reflect.DeepEqual(launcher.args, []string{"job", "worker", string(job.ID)}) {
		t.Fatalf("launcher calls=%d executable=%q args=%#v", launcher.calls, launcher.executable, launcher.args)
	}
}

func TestResourceDownloadDiscoversStoredArticleResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write([]byte("resource-bytes"))
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	if err := applicationAdapter.active.Library.UpsertAccount(context.Background(), library.AccountRecord{
		ID: "account-a", FakeID: "fake-a", Name: "Account",
	}); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.active.Library.UpsertArticle(context.Background(), library.ArticleRecord{
		ID: "article-a", AccountID: "account-a", Aid: "article-a", Title: "Resources",
		CanonicalURL: "https://mp.weixin.qq.com/s/resource-a", ContentStatus: "missing",
	}); err != nil {
		t.Fatal(err)
	}
	html := `<html><body><div id="js_article"><div id="js_content"><img src="` + server.URL + `/asset.png"></div></div>` +
		`<script>window.cgiDataNew={title:'Resources',user_name:'gh_fixture',content_noencode:'<p><img src="` + server.URL +
		`/asset.png">resources</p>'}</script></body></html>`
	object, err := applicationAdapter.active.Objects.Put(context.Background(), strings.NewReader(html), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitContent(context.Background(), "article-a", object, "html",
		"https://mp.weixin.qq.com/s/resource-a", "valid", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	runtime, err := newLocalDownloadRuntime(applicationAdapter.active, applicationAdapter.secret, http.DefaultClient, downloadRuntimeOptions{
		DestinationPolicy: network.DestinationPolicy{AllowedHosts: map[string]struct{}{"127.0.0.1": {}},
			AllowLoopback: true, Resolver: loopbackResolver{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := runtime.Start(context.Background(), domain.DownloadRequest{
		Kind: "resources", ArticleIDs: []domain.ArticleID{"article-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := runtime.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	record, err := applicationAdapter.active.Library.ResourceByURL(context.Background(), server.URL+"/asset.png")
	if err != nil || record.Status != "available" || record.ObjectDigest == "" {
		t.Fatalf("resource=%#v err=%v", record, err)
	}
}

func TestLocalExportRuntimeExecutesAllNonPDFFormats(t *testing.T) {
	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	if err := applicationAdapter.active.Library.UpsertAccount(context.Background(), library.AccountRecord{
		ID: "account-a", FakeID: "fake-a", Name: "Account",
	}); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.active.Library.UpsertArticle(context.Background(), library.ArticleRecord{
		ID: "article-a", AccountID: "account-a", Aid: "article-a", Title: "Export article",
		CanonicalURL: "https://mp.weixin.qq.com/s/export-a", ContentStatus: "missing",
	}); err != nil {
		t.Fatal(err)
	}
	html := `<html><body><div id="js_article"><div id="js_content">hello export</div></div>` +
		`<script>window.cgiDataNew={title:'Export article',nick_name:'Account',user_name:'gh_fixture',content_noencode:'<p>hello export</p>',comment_id:'comment-a'}</script></body></html>`
	object, err := applicationAdapter.active.Objects.Put(context.Background(), strings.NewReader(html), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitContent(context.Background(), "article-a", object, "html",
		"https://mp.weixin.qq.com/s/export-a", "valid", "comment-a", time.Now()); err != nil {
		t.Fatal(err)
	}
	runtime := applicationAdapter.active.Exports
	for _, format := range []string{"html", "markdown", "text", "json", "xlsx", "docx"} {
		t.Run(format, func(t *testing.T) {
			outputRoot := filepath.Join(paths.ForProfile("default").Data, "exports", format)
			job, err := runtime.Start(context.Background(), domain.ExportRequest{
				Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
				Format:    format, OutputRoot: outputRoot,
				Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail",
					FormatOptions: map[string]any{"content": true, "metadata": true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			final, err := runtime.Run(context.Background(), job.ID)
			if err != nil || final.State != domain.JobCompleted {
				items, _ := applicationAdapter.active.Jobs.ListItems(context.Background(), job.ID)
				t.Fatalf("final=%#v items=%#v err=%v", final, items, err)
			}
			entries, err := os.ReadDir(outputRoot)
			if err != nil || len(entries) == 0 {
				t.Fatalf("output entries=%#v err=%v", entries, err)
			}
		})
	}
}

func TestLocalExportRuntimeExecutesRelativeOutputRoot(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	workingDirectory := t.TempDir()
	root := filepath.Join(workingDirectory, "relative-export")
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: "relative-export",
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) == 0 {
		t.Fatalf("output entries=%#v err=%v", entries, err)
	}
}

func TestNormalizeExportOutputRootExpandsHomeShortcut(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]string{
		"~":              home,
		"~/Downloads":    filepath.Join(home, "Downloads"),
		"~/downloads/":   filepath.Join(home, "downloads"),
		"./downloads":    "./downloads",
		"/tmp/exports":   "/tmp/exports",
		"  ~/Downloads ": filepath.Join(home, "Downloads"),
	} {
		got, err := normalizeExportOutputRoot(raw)
		if err != nil || got != want {
			t.Fatalf("normalizeExportOutputRoot(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}

func TestQueuedExportUsesImmutableMetadataCommentsAndResourceSnapshot(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	ctx := context.Background()
	if _, err := applicationAdapter.active.Library.CommitCommentPage(ctx, "article-a", library.CommentPageCommit{
		Comments: []library.CommentRecord{{UpstreamID: "comment-a", AuthorName: "reader", Content: "queued comment"}},
		Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	resourceA, err := applicationAdapter.active.Objects.Put(ctx, strings.NewReader("asset-a"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	const resourceURL = "https://mmbiz.qpic.cn/snapshot.txt"
	if _, err := applicationAdapter.active.Library.CommitResource(ctx, "article-a", resourceURL, "attachment", 0, resourceA); err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(t.TempDir(), "snapshot")
	job, err := applicationAdapter.active.Exports.Start(ctx, domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "text", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail",
			FormatOptions: map[string]any{"metadata": true, "comments": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := applicationAdapter.active.Jobs.ListItems(ctx, job.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	var envelope exportJobItem
	if err := json.Unmarshal([]byte(items[0].Key), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != 4 || envelope.SnapshotDigest == "" ||
		!slices.Contains(envelope.PinnedDigests, resourceA.Digest) || !slices.Contains(envelope.PinnedDigests, envelope.SnapshotDigest) {
		t.Fatalf("queued envelope=%#v", envelope)
	}
	if err := applicationAdapter.active.Library.UpsertArticle(ctx, library.ArticleRecord{
		ID: "article-a", AccountID: "account-a", Aid: "article-a", Title: "Changed title",
		CanonicalURL: "https://mp.weixin.qq.com/s/export-a", ContentStatus: "available",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitCommentPage(ctx, "article-a", library.CommentPageCommit{
		Comments: []library.CommentRecord{{UpstreamID: "comment-a", AuthorName: "reader", Content: "changed comment"}},
		Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	resourceB, err := applicationAdapter.active.Objects.Put(ctx, strings.NewReader("asset-b"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitResource(ctx, "article-a", resourceURL, "attachment", 0, resourceB); err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(ctx, job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	data, err := os.ReadFile(filepath.Join(outputRoot, "Export article.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "queued comment") || strings.Contains(text, "changed comment") ||
		!strings.Contains(text, "Title: Export article") || strings.Contains(text, "Title: Changed title") {
		t.Fatalf("exported queued snapshot:\n%s", text)
	}
	record, err := applicationAdapter.active.Library.GetExportByJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record.Provenance)
	if err != nil {
		t.Fatal(err)
	}
	var provenance exporter.ProvenanceManifest
	if err := json.Unmarshal(encoded, &provenance); err != nil {
		t.Fatal(err)
	}
	if len(provenance.Sources) != 1 || provenance.Sources[0].SnapshotSHA256 != envelope.SnapshotDigest {
		t.Fatalf("provenance=%#v", provenance)
	}
}

func TestLocalExportRuntimeExecutesPDFAndPersistsManifestState(t *testing.T) {
	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	runner := &appPDFRunner{pdf: []byte("%PDF-1.7\nlocal runtime\n%%EOF\n")}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
		Browser: fixedBrowserDiscovery{browser: runtimeenv.Browser{Path: "/fixture/chromium"}}, PDFRunner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(paths.ForProfile("default").Data, "exports", "pdf")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "pdf", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail",
			FormatOptions: map[string]any{"content": true, "metadata": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted || runner.calls != 1 {
		t.Fatalf("final=%#v runner.calls=%d err=%v", final, runner.calls, err)
	}
	exportsPage, err := applicationAdapter.active.Library.QueryExports(context.Background(), 0, 10)
	if err != nil || len(exportsPage.Items) != 1 {
		t.Fatalf("exports=%#v err=%v", exportsPage, err)
	}
	record, err := applicationAdapter.active.Library.GetExport(context.Background(), exportsPage.Items[0])
	if err != nil || record.State != string(domain.JobCompleted) || record.CompletedAt.IsZero() {
		t.Fatalf("export record=%#v err=%v", record, err)
	}
	files, err := applicationAdapter.active.Library.ListExportFiles(context.Background(), record.ID)
	if err != nil || len(files) != 1 || files[0].MediaType != "application/pdf" || files[0].ArticleID != "article-a" ||
		record.ProvenancePath != exportProvenancePath(record.ID) || record.ProvenanceState != "ready" {
		t.Fatalf("export files=%#v err=%v", files, err)
	}
	verification, err := exporter.VerifyProvenanceManifest(context.Background(), outputRoot, exportProvenancePath(record.ID))
	if err != nil || !verification.Valid || verification.VerifiedOutputs != 1 {
		t.Fatalf("export verification=%#v err=%v", verification, err)
	}
}

func TestExportVerifyCommandReportsChangedOutput(t *testing.T) {
	root := t.TempDir()
	stdout := &bytes.Buffer{}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), stdout, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(root, "verified-export")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	record, err := applicationAdapter.active.Library.GetExport(context.Background(), firstExportID(t, applicationAdapter.active.Library))
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{
		"export", "verify", "--root", outputRoot, "--manifest", exportProvenancePath(record.ID), "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"valid": true`) || !strings.Contains(stdout.String(), `"verifiedOutputs": 1`) {
		t.Fatalf("valid verification output = %s", stdout.String())
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "Export article.md"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	err = applicationAdapter.Execute(context.Background(), []string{
		"export", "verify", "--root", outputRoot, "--manifest", exportProvenancePath(record.ID), "--json",
	})
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("changed verification error=%v exit=%d", err, ExitCode(err))
	}
	if stdout.Len() != 0 {
		t.Fatalf("application layer wrote a partial JSON document: %s", stdout.String())
	}
	var encoded bytes.Buffer
	if writeErr := WriteErrorJSON(&encoded, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	if !strings.Contains(encoded.String(), `"success":false`) || !strings.Contains(encoded.String(), `"valid":false`) ||
		!strings.Contains(encoded.String(), "checksum_mismatch") || !strings.Contains(encoded.String(), "article-a") {
		t.Fatalf("changed verification error envelope = %s", encoded.String())
	}
}

func TestExportCommandExposesStrictHTMLAndBatchArchive(t *testing.T) {
	root := t.TempDir()
	stdout := &bytes.Buffer{}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), stdout, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(root, "html-batch")
	if err := applicationAdapter.Execute(context.Background(), []string{
		"export", "start", "--format", "html", "--article", "article-a", "--output", outputRoot,
		"--html-resource-policy", "strict", "--html-batch-archive", "articles.zip", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data domain.Job `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(context.Background(), envelope.Data.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	archive := filepath.Join(outputRoot, "articles.zip")
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) == 0 || reader.File[0].Name == "" {
		t.Fatalf("HTML batch entries=%#v", reader.File)
	}
	record, err := applicationAdapter.active.Library.GetExport(context.Background(), firstExportID(t, applicationAdapter.active.Library))
	if err != nil {
		t.Fatal(err)
	}
	verification, err := exporter.VerifyProvenanceManifest(context.Background(), outputRoot, exportProvenancePath(record.ID))
	if err != nil || !verification.Valid || verification.VerifiedOutputs != 1 {
		t.Fatalf("verification=%#v err=%v", verification, err)
	}

	stdout.Reset()
	err = applicationAdapter.Execute(context.Background(), []string{
		"export", "start", "--format", "markdown", "--article", "article-a", "--output", filepath.Join(root, "invalid"),
		"--html-batch-archive", "articles.zip", "--json",
	})
	if err == nil || !strings.Contains(err.Error(), "requires --format html") {
		t.Fatalf("non-HTML batch validation error=%v", err)
	}
}

func TestPersistentExportProducesRedactedLifecycleLogs(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: filepath.Join(t.TempDir(), "export"),
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	logs, err := applicationAdapter.active.Jobs.ListLogs(context.Background(), job.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(logs)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	for _, required := range []string{"job worker started", "job item attempt started", "job item attempt completed", "job finalized", "duration"} {
		if !strings.Contains(output, required) {
			t.Fatalf("job logs missing %q: %s", required, output)
		}
	}
}

func TestExportRetryRecoversCommittedOutputAfterDatabaseRecordFailure(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(t.TempDir(), "record-recovery")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := applicationAdapter.active.Exports.(*localExportRuntime)
	recordCalls := 0
	runtime.recordFile = func(ctx context.Context, record library.ExportFileRecord) error {
		recordCalls++
		if recordCalls == 1 {
			return errors.New("injected export_files failure")
		}
		return runtime.library.UpsertExportFile(ctx, record)
	}
	first, err := runtime.Run(context.Background(), job.ID)
	if err != nil || first.State != domain.JobFailed {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	outputPath := filepath.Join(outputRoot, "Export article.md")
	before, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	infoBefore, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Jobs.RetryExport(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Run(context.Background(), job.ID)
	if err != nil || second.State != domain.JobCompleted {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	after, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	infoAfter, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Fatalf("retry rewrote committed output: before=%d/%s after=%d/%s", len(before), infoBefore.ModTime(), len(after), infoAfter.ModTime())
	}
	exportID := firstExportID(t, runtime.library)
	files, err := runtime.library.ListExportFiles(context.Background(), exportID)
	if err != nil || len(files) != 1 || files[0].RelativePath != "Export article.md" {
		t.Fatalf("files=%#v err=%v", files, err)
	}
}

func TestExportRetryRecoversCommittedOutputAfterCheckpointFailureAcrossRuntimeRestart(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(t.TempDir(), "checkpoint-recovery")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := applicationAdapter.active.Exports.(*localExportRuntime)
	runtime.publish = func(checkpoint jobs.CheckpointFunc, intent exportOutputIntent) error {
		if err := checkpoint(intent); err != nil {
			return err
		}
		return errors.New("injected failure after durable output checkpoint")
	}
	first, err := runtime.Run(context.Background(), job.ID)
	if err != nil || first.State != domain.JobFailed {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	outputPath := filepath.Join(outputRoot, "Export article.md")
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination published before recovery: %v", err)
	}
	staging, err := filepath.Glob(filepath.Join(outputRoot, ".wechat-export-*.tmp"))
	if err != nil || len(staging) != 1 {
		t.Fatalf("staging=%#v err=%v", staging, err)
	}
	if _, err := applicationAdapter.active.Jobs.RetryExport(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	restarted := newLocalExportRuntime(applicationAdapter.active, nil, nil, runtime.scheduler)
	second, err := restarted.Run(context.Background(), job.ID)
	if err != nil || second.State != domain.JobCompleted {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	after, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	infoAfter, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte("hello export")) || infoAfter.Size() != int64(len(after)) {
		t.Fatalf("recovered destination=%q info=%#v", after, infoAfter)
	}
	staging, err = filepath.Glob(filepath.Join(outputRoot, ".wechat-export-*.tmp"))
	if err != nil || len(staging) != 0 {
		t.Fatalf("staging after recovery=%#v err=%v", staging, err)
	}
	files, err := restarted.library.ListExportFiles(context.Background(), firstExportID(t, restarted.library))
	if err != nil || len(files) != 1 || files[0].MediaType != "text/markdown" {
		t.Fatalf("files=%#v err=%v", files, err)
	}
}

func TestHTMLExportRetryReplaysCheckpointedDiagnosticsExactlyOnce(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(t.TempDir(), "html-checkpoint-recovery")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "html", OutputRoot: outputRoot,
		Options: domain.ExportOptions{
			NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail",
			FormatOptions: map[string]any{"htmlResourcePolicy": "best_effort"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := applicationAdapter.active.Exports.(*localExportRuntime)
	runtime.publish = func(checkpoint jobs.CheckpointFunc, intent exportOutputIntent) error {
		intent.Diagnostics = append(intent.Diagnostics, exportOutputDiagnostic{
			Level: "warning", Message: "checkpointed diagnostic", Kind: "test_warning", ArticleID: "article-a",
		})
		if err := checkpoint(intent); err != nil {
			return err
		}
		return errors.New("injected HTML checkpoint failure")
	}
	first, err := runtime.Run(context.Background(), job.ID)
	if err != nil || first.State != domain.JobFailed {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	if _, err := applicationAdapter.active.Jobs.RetryExport(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	runtime.publish = checkpointExportOutputIntent
	second, err := runtime.Run(context.Background(), job.ID)
	if err != nil || second.State != domain.JobCompleted {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	logs, err := runtime.store.ListAllLogs(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, log := range logs {
		if log.Message == "checkpointed diagnostic" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("checkpointed diagnostic count=%d logs=%#v", count, logs)
	}
}

func TestExportRecoveryPreservesCollisionPoliciesWithoutDuplicateOutputs(t *testing.T) {
	for _, collisionPolicy := range []string{"fail", "suffix", "replace"} {
		t.Run(collisionPolicy, func(t *testing.T) {
			applicationAdapter, _, _ := newTestApp(t)
			prepareExportArticle(t, applicationAdapter)
			outputRoot := filepath.Join(t.TempDir(), "collision-recovery")
			if collisionPolicy == "suffix" || collisionPolicy == "replace" {
				if err := os.MkdirAll(outputRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(outputRoot, "Export article.md"), []byte("existing"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
				Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
				Format:    "markdown", OutputRoot: outputRoot,
				Options: domain.ExportOptions{
					NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: collisionPolicy,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime := applicationAdapter.active.Exports.(*localExportRuntime)
			runtime.publish = func(checkpoint jobs.CheckpointFunc, intent exportOutputIntent) error {
				if err := checkpoint(intent); err != nil {
					return err
				}
				return errors.New("injected checkpoint handoff failure")
			}
			first, err := runtime.Run(context.Background(), job.ID)
			if err != nil || first.State != domain.JobFailed {
				t.Fatalf("first=%#v err=%v", first, err)
			}
			if _, err := applicationAdapter.active.Jobs.RetryExport(context.Background(), job.ID); err != nil {
				t.Fatal(err)
			}
			runtime.publish = checkpointExportOutputIntent
			second, err := runtime.Run(context.Background(), job.ID)
			if err != nil || second.State != domain.JobCompleted {
				t.Fatalf("second=%#v err=%v", second, err)
			}
			entries, err := os.ReadDir(outputRoot)
			if err != nil {
				t.Fatal(err)
			}
			var markdown []string
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".md") {
					markdown = append(markdown, entry.Name())
				}
			}
			sort.Strings(markdown)
			switch collisionPolicy {
			case "fail", "replace":
				if len(markdown) != 1 || markdown[0] != "Export article.md" {
					t.Fatalf("markdown=%#v", markdown)
				}
			case "suffix":
				if len(markdown) != 2 || !slices.Contains(markdown, "Export article.md") ||
					(!strings.Contains(markdown[0], "--") && !strings.Contains(markdown[1], "--")) {
					t.Fatalf("markdown=%#v", markdown)
				}
			}
			staging, err := filepath.Glob(filepath.Join(outputRoot, ".wechat-export-*.tmp"))
			if err != nil || len(staging) != 0 {
				t.Fatalf("staging=%#v err=%v", staging, err)
			}
		})
	}
}

func TestHTMLMultiFileExportRecoversAllStagedOutputsAcrossRestart(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	assetObject, err := applicationAdapter.active.Objects.Put(context.Background(), strings.NewReader("fixture-image"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitResource(context.Background(), "article-a",
		"https://mmbiz.qpic.cn/mmbiz_png/fixture/0", "image", 0, assetObject); err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(t.TempDir(), "html-multi-recovery")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "html", OutputRoot: outputRoot,
		Options: domain.ExportOptions{
			NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail",
			FormatOptions: map[string]any{"htmlResourcePolicy": "best_effort"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := applicationAdapter.active.Exports.(*localExportRuntime)
	runtime.publish = func(checkpoint jobs.CheckpointFunc, intent exportOutputIntent) error {
		if err := checkpoint(intent); err != nil {
			return err
		}
		return errors.New("injected multi-file checkpoint handoff failure")
	}
	first, err := runtime.Run(context.Background(), job.ID)
	if err != nil || first.State != domain.JobFailed {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	if _, err := applicationAdapter.active.Jobs.RetryExport(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	restarted := newLocalExportRuntime(applicationAdapter.active, nil, nil, runtime.scheduler)
	second, err := restarted.Run(context.Background(), job.ID)
	if err != nil || second.State != domain.JobCompleted {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	files, err := restarted.library.ListExportFiles(context.Background(), firstExportID(t, restarted.library))
	if err != nil || len(files) < 2 {
		t.Fatalf("files=%#v err=%v", files, err)
	}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(outputRoot, filepath.FromSlash(file.RelativePath))); err != nil {
			t.Fatalf("missing recovered output %s: %v", file.RelativePath, err)
		}
	}
}

func firstExportID(t *testing.T, database *library.Database) domain.ExportID {
	t.Helper()
	page, err := database.QueryExports(context.Background(), 0, 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("exports=%#v err=%v", page, err)
	}
	return page.Items[0]
}

func TestLocalExportSuffixCollisionPreservesExistingOutput(t *testing.T) {
	root := t.TempDir()
	paths, err := profiles.ResolvePaths(profiles.PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(paths.ForProfile("default").Data, "exports", "suffix")
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(outputRoot, "Export article.md")
	if err := os.WriteFile(original, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "suffix",
			FormatOptions: map[string]any{"content": true, "metadata": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	contents, err := os.ReadFile(original)
	if err != nil || string(contents) != "existing" {
		t.Fatalf("original=%q err=%v", contents, err)
	}
	entries, err := os.ReadDir(outputRoot)
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries=%#v err=%v", entries, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !strings.Contains(strings.Join(names, "\n"), "--") || !strings.Contains(strings.Join(names, "\n"), "-manifest.json") {
		t.Fatalf("suffix entries=%#v", names)
	}
}

func TestLocalExportWorkerRejectsReplacedAuthorizedRoot(t *testing.T) {
	root := t.TempDir()
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	prepareExportArticle(t, applicationAdapter)
	authorizedRoot := filepath.Join(root, "authorized")
	if err := os.MkdirAll(authorizedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	identityFile, err := os.Open(authorizedRoot)
	if err != nil {
		t.Fatal(err)
	}
	device, inode, err := exportRootIdentityFromFile(identityFile)
	closeErr := identityFile.Close()
	err = errors.Join(err, closeErr)
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(authorizedRoot, "queued")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
		OutputAuthorization: &domain.ExportOutputAuthorization{
			Root: authorizedRoot, RelativePath: "queued", Device: device, Inode: inode,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Rename(authorizedRoot, authorizedRoot+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, authorizedRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	final, runErr := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if runErr != nil || final.State != domain.JobFailed {
		t.Fatalf("final=%#v err=%v", final, runErr)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside entries=%#v err=%v", entries, err)
	}
}

func TestExportProvenanceFinalizerRecordsFailureAfterAuthorizedRootReplacement(t *testing.T) {
	root := t.TempDir()
	applicationAdapter, err := NewWithDependencies(context.Background(), strings.NewReader(""), io.Discard, io.Discard, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root}, Secrets: secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = applicationAdapter.Close() })
	prepareExportArticle(t, applicationAdapter)
	authorizedRoot := filepath.Join(root, "authorized")
	if err := os.MkdirAll(authorizedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	identityFile, err := os.Open(authorizedRoot)
	if err != nil {
		t.Fatal(err)
	}
	device, inode, err := exportRootIdentityFromFile(identityFile)
	closeErr := identityFile.Close()
	err = errors.Join(err, closeErr)
	if err != nil {
		t.Fatal(err)
	}
	outputRoot := filepath.Join(authorizedRoot, "queued")
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options:             domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
		OutputAuthorization: &domain.ExportOutputAuthorization{Root: authorizedRoot, RelativePath: "queued", Device: device, Inode: inode},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := applicationAdapter.active.Exports.(*localExportRuntime)
	engine, err := jobs.NewEngine(runtime.store, jobs.EngineOptions{Owner: "finalizer-test", Scheduler: runtime.scheduler,
		Metadata: func(jobs.Item) jobs.WorkMetadata { return jobs.WorkMetadata{Operation: "export", Host: "local"} }})
	if err != nil {
		t.Fatal(err)
	}
	final, err := engine.Run(context.Background(), job.ID, runtime.execute)
	if err != nil || final.State != domain.JobCompleted {
		items, _ := runtime.store.ListItems(context.Background(), job.ID)
		t.Fatalf("export execution=%#v items=%#v err=%v", final, items, err)
	}
	recordBeforeFinalize, err := runtime.library.GetExportByJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.library.UpdateExportStateByJob(context.Background(), job.ID, recordBeforeFinalize.ProvenanceGeneration,
		string(final.State), runtime.now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(authorizedRoot, authorizedRoot+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authorizedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runtime.finalizeProvenance(context.Background(), final); err == nil || !strings.Contains(err.Error(), "replaced") {
		t.Fatalf("finalize error=%v", err)
	}
	record, err := runtime.library.GetExportByJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ProvenanceState != "failed" || !strings.Contains(record.ProvenanceError, "replaced") || record.ProvenanceGeneration != 1 {
		t.Fatalf("provenance record=%#v", record)
	}
}

func TestLegacyExportProvenanceBecomesTerminalUnavailable(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	runtime := applicationAdapter.active.Exports.(*localExportRuntime)
	ctx := context.Background()
	selection, err := exporter.BuildSelectionManifest(ctx, runtime.library, domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown",
	}, runtime.now())
	if err != nil {
		t.Fatal(err)
	}
	exportID := domain.ExportID("legacy-export")
	legacyItem, err := json.Marshal(exportJobItem{
		Version: 3, ExportID: exportID, ArticleID: "article-a", Format: "markdown",
		Output: filepath.Join(t.TempDir(), "legacy.md"), Selection: selection,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := runtime.store.CreateWithItems(ctx, jobs.Spec{Kind: "export", Profile: runtime.profile}, []string{string(legacyItem)})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.library.UpsertExport(ctx, library.ExportRecord{
		ID: exportID, JobID: job.ID, Format: "markdown", Manifest: selection,
		State: string(domain.JobCompleted), CreatedAt: runtime.now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.store.Transition(ctx, job.ID, domain.JobRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.store.Transition(ctx, job.ID, domain.JobCompleted); err != nil {
		t.Fatal(err)
	}
	final, err := runtime.store.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.finalizeProvenance(ctx, final); !errors.Is(err, errLegacyProvenanceUnavailable) {
		t.Fatalf("finalize legacy provenance error=%v", err)
	}
	record, err := runtime.library.GetExport(ctx, exportID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ProvenanceState != "unavailable" || !strings.Contains(record.ProvenanceError, "producer version") {
		t.Fatalf("legacy provenance record=%#v", record)
	}
	pending, err := runtime.library.PendingTerminalExports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range pending {
		if candidate.ID == exportID {
			t.Fatalf("terminal unavailable export remained recoverable: %#v", pending)
		}
	}
}

func TestExportRetryAdvancesProvenanceGenerationAndUsesNewManifestPath(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	prepareExportArticle(t, applicationAdapter)
	outputRoot := filepath.Join(t.TempDir(), "generation")
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	collisionPath := filepath.Join(outputRoot, "Export article.md")
	if err := os.WriteFile(collisionPath, []byte("collision"), 0o600); err != nil {
		t.Fatal(err)
	}
	job, err := applicationAdapter.active.Exports.Start(context.Background(), domain.ExportRequest{
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: []domain.ArticleID{"article-a"}},
		Format:    "markdown", OutputRoot: outputRoot,
		Options: domain.ExportOptions{NamingTemplate: "{title}", MaximumNameBytes: 180, CollisionPolicy: "fail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || first.State != domain.JobFailed {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	firstRecord, err := applicationAdapter.active.Library.GetExportByJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(collisionPath); err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Jobs.RetryExport(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	second, err := applicationAdapter.active.Exports.Run(context.Background(), job.ID)
	if err != nil || second.State != domain.JobCompleted {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	secondRecord, err := applicationAdapter.active.Library.GetExportByJob(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstRecord.ProvenanceGeneration != 1 || secondRecord.ProvenanceGeneration != 2 ||
		secondRecord.ProvenancePath != exportProvenanceGenerationPath(secondRecord.ID, 2) {
		t.Fatalf("first=%#v second=%#v", firstRecord, secondRecord)
	}
}

func prepareExportArticle(t *testing.T, applicationAdapter *App) {
	t.Helper()
	if err := applicationAdapter.active.Library.UpsertAccount(context.Background(), library.AccountRecord{
		ID: "account-a", FakeID: "fake-a", Name: "Account",
	}); err != nil {
		t.Fatal(err)
	}
	if err := applicationAdapter.active.Library.UpsertArticle(context.Background(), library.ArticleRecord{
		ID: "article-a", AccountID: "account-a", Aid: "article-a", Title: "Export article",
		CanonicalURL: "https://mp.weixin.qq.com/s/export-a", ContentStatus: "missing",
	}); err != nil {
		t.Fatal(err)
	}
	html := `<html><body><div id="js_article"><div id="js_content">hello export</div></div>` +
		`<script>window.cgiDataNew={title:'Export article',nick_name:'Account',user_name:'gh_fixture',content_noencode:'<p>hello export</p>',comment_id:'comment-a'}</script></body></html>`
	object, err := applicationAdapter.active.Objects.Put(context.Background(), strings.NewReader(html), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applicationAdapter.active.Library.CommitContent(context.Background(), "article-a", object, "html",
		"https://mp.weixin.qq.com/s/export-a", "valid", "comment-a", time.Now()); err != nil {
		t.Fatal(err)
	}
}

type appPDFRunner struct {
	calls int
	pdf   []byte
}

func (runner *appPDFRunner) Run(_ context.Context, _ string, args []string, _, _ io.Writer) error {
	runner.calls++
	for _, argument := range args {
		if strings.HasPrefix(argument, "--print-to-pdf=") {
			return os.WriteFile(strings.TrimPrefix(argument, "--print-to-pdf="), runner.pdf, 0o600)
		}
	}
	return errors.New("PDF output argument is missing")
}

var _ exporter.ProcessRunner = (*appPDFRunner)(nil)

func TestLocalSyncRuntimeExecutesPersistentAccountJob(t *testing.T) {
	database := openAppTestDatabase(t)
	if err := database.UpsertAccount(context.Background(), library.AccountRecord{ID: "account-a", FakeID: "fake-a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	store := library.NewJobStore(database)
	runner, err := syncrunner.NewRunner(syncPageSource{}, database, syncrunner.Options{
		Now:    func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep:  func(context.Context, time.Duration) error { return nil },
		Jitter: func(time.Duration) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &localSyncRuntime{profile: "profile-a", store: store, runner: runner}
	job, err := runtime.Start(context.Background(), domain.SynchronizeAccountRequest{AccountID: "account-a", Range: domain.SyncRangeAll})
	if err != nil {
		t.Fatal(err)
	}
	final, err := runtime.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	page, err := database.QueryArticles(context.Background(), domain.ArticleQuery{AccountID: "account-a", Limit: 10})
	if err != nil || page.Total != 1 || page.Items[0].Title != "Synced" {
		t.Fatalf("articles=%#v err=%v", page, err)
	}
}

func TestConfiguredProxyFirstPreservesDirectSafetyIdentityAndOrder(t *testing.T) {
	direct := network.Candidate{Client: network.StaticClient{RouteName: "direct"}, Direct: true, Enabled: true}
	proxy := network.Candidate{Client: network.StaticClient{RouteName: "proxy"}, Priority: 10, Enabled: true}
	routes := configureDownloadRoutes([]network.Candidate{direct, proxy}, downloadRuntimeOptions{
		ProxyConfigured: true, Proxy: profiles.ProxyPreferences{DirectFirst: false, FallbackEnabled: true},
	})
	if len(routes) != 2 || routes[0].Client.Name() != "proxy" || routes[1].Client.Name() != "direct" || !routes[1].Direct {
		t.Fatalf("proxy-first routes = %#v", routes)
	}
	var calls []string
	routes[0].Client = network.StaticClient{RouteName: "proxy", Call: func(context.Context, network.Request) (network.Result, error) {
		calls = append(calls, "proxy")
		return network.Result{}, errors.New("retry proxy")
	}}
	routes[1].Client = network.StaticClient{RouteName: "direct", Call: func(context.Context, network.Request) (network.Result, error) {
		calls = append(calls, "direct")
		return network.Result{Route: "direct"}, nil
	}}
	router := &downloadRouter{routes: routes, retryable: func(error) bool { return true }}
	result, err := router.Do(context.Background(), network.Request{
		Class: network.PublicContent,
	})
	if err != nil || result.Route != "direct" || !reflect.DeepEqual(calls, []string{"proxy", "direct"}) {
		t.Fatalf("router result=%#v calls=%#v error=%v", result, calls, err)
	}
}

func TestDownloadRouterDoesNotFallbackAfterCancellation(t *testing.T) {
	var calls []string
	router := downloadRouter{routes: []network.Candidate{
		{Client: network.StaticClient{RouteName: "primary", Call: func(context.Context, network.Request) (network.Result, error) {
			calls = append(calls, "primary")
			return network.Result{}, context.Canceled
		}}, Direct: true, Enabled: true},
		{Client: network.StaticClient{RouteName: "fallback", Call: func(context.Context, network.Request) (network.Result, error) {
			calls = append(calls, "fallback")
			return network.Result{Route: "fallback"}, nil
		}}, Enabled: true},
	}, retryable: func(error) bool { return true }}
	_, err := router.Do(context.Background(), network.Request{Class: network.PublicContent})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(calls, []string{"primary"}) {
		t.Fatalf("cancel result err=%v calls=%v", err, calls)
	}
}

func TestDownloadRouterCachesSuccessfulRecoveryProbe(t *testing.T) {
	var probes, calls int
	router := &downloadRouter{routes: []network.Candidate{{
		Client: network.StaticClient{RouteName: "recovering", Call: func(context.Context, network.Request) (network.Result, error) {
			calls++
			return network.Result{Route: "recovering"}, nil
		}}, Trust: network.TrustPublicOnly, Enabled: true, ProbeRequired: true,
		Probe:   func(context.Context) error { probes++; return nil },
		Classes: network.ClassesMap([]network.RequestClass{network.PublicContent}),
	}}}
	for range 2 {
		if _, err := router.Do(context.Background(), network.Request{Class: network.PublicContent}); err != nil {
			t.Fatal(err)
		}
	}
	if probes != 1 || calls != 2 || router.routes[0].ProbeRequired {
		t.Fatalf("probe cache probes=%d calls=%d candidate=%#v", probes, calls, router.routes[0])
	}
}

func TestConfiguredFallbackDisabledUsesOnlyTheSelectedPrimaryRoute(t *testing.T) {
	direct := network.Candidate{Client: network.StaticClient{RouteName: "direct"}, Direct: true, Enabled: true}
	proxy := network.Candidate{Client: network.StaticClient{RouteName: "proxy"}, Priority: 10, Enabled: true}
	tests := []struct {
		name        string
		directFirst bool
		want        string
	}{
		{name: "direct-only", directFirst: true, want: "direct"},
		{name: "proxy-only", directFirst: false, want: "proxy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			routes := configureDownloadRoutes([]network.Candidate{direct, proxy}, downloadRuntimeOptions{
				ProxyConfigured: true, Proxy: profiles.ProxyPreferences{DirectFirst: test.directFirst, FallbackEnabled: false},
			})
			if len(routes) != 1 || routes[0].Client.Name() != test.want {
				t.Fatalf("routes = %#v, want %s only", routes, test.want)
			}
			if test.want == "direct" && !routes[0].Direct {
				t.Fatal("direct-only policy erased Candidate.Direct")
			}
		})
	}
}

func TestUnconfiguredProxyOptionsKeepDefaultDirectFirstFallback(t *testing.T) {
	direct := network.Candidate{Client: network.StaticClient{RouteName: "direct"}, Direct: true, Enabled: true}
	proxy := network.Candidate{Client: network.StaticClient{RouteName: "proxy"}, Priority: 10, Enabled: true}
	routes := configureDownloadRoutes([]network.Candidate{proxy, direct}, downloadRuntimeOptions{})
	if len(routes) != 2 || routes[0].Client.Name() != "direct" || !routes[0].Direct || routes[1].Client.Name() != "proxy" {
		t.Fatalf("default routes = %#v", routes)
	}
}

func TestDownloadSchedulerUsesDurableOperationKeys(t *testing.T) {
	limits := downloadSchedulerLimits(7)
	want := map[string]int{
		string(download.JobArticle): 7, string(download.JobResource): 7, string(download.JobMetadata): 1,
		string(download.JobComments): 1, string(download.JobPaid): 1,
	}
	if !reflect.DeepEqual(limits.PerOperation, want) || limits.Global != 7 || limits.PerHost != 7 || limits.Sensitive != 1 {
		t.Fatalf("download scheduler limits = %#v", limits)
	}
}

func TestProfileSchedulerLimitsRespectDownloadConcurrencyAndRetainOtherCaps(t *testing.T) {
	limits := profileSchedulerLimits(9)
	if limits.Global != 9 || limits.PerHost != 9 || limits.PerOperation[string(download.JobArticle)] != 9 ||
		limits.PerOperation[string(download.JobResource)] != 9 || limits.PerOperation[string(download.JobMetadata)] != 1 ||
		limits.PerOperation["account_sync"] != 1 || limits.PerOperation["album_sync"] != 1 || limits.PerOperation["export"] != 2 {
		t.Fatalf("profile scheduler limits = %#v", limits)
	}
}

func TestLocalSyncRuntimeCreatesOneDurableMultiAccountJobWithIndependentItems(t *testing.T) {
	database := openAppTestDatabase(t)
	for _, account := range []library.AccountRecord{
		{ID: "account-a", FakeID: "fake-a", Name: "Account A"},
		{ID: "account-b", FakeID: "fake-b", Name: "Account B"},
	} {
		if err := database.UpsertAccount(context.Background(), account); err != nil {
			t.Fatal(err)
		}
	}
	store := library.NewJobStore(database)
	runner, err := syncrunner.NewRunner(multiAccountSyncPageSource{}, database, syncrunner.Options{
		Now:    func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep:  func(context.Context, time.Duration) error { return nil },
		Jitter: func(time.Duration) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &localSyncRuntime{profile: "profile-a", store: store, runner: runner}
	job, err := runtime.Start(context.Background(), domain.SynchronizeAccountRequest{
		AccountIDs: []domain.AccountID{"account-a", "account-b", "account-a"}, Range: domain.SyncRangeAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("job items = %#v", items)
	}
	for _, item := range items {
		var envelope syncJobItem
		if err := json.Unmarshal([]byte(item.Key), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Request.AccountID == "" || len(envelope.Request.AccountIDs) != 0 {
			t.Fatalf("item request = %#v", envelope.Request)
		}
	}
	final, err := runtime.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	items, err = store.ListItems(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.State != domain.JobCompleted || len(item.Checkpoint) == 0 || string(item.Checkpoint) == "{}" {
			t.Fatalf("completed job item = %#v", item)
		}
	}
	for _, accountID := range []domain.AccountID{"account-a", "account-b"} {
		page, queryErr := database.QueryArticles(context.Background(), domain.ArticleQuery{AccountID: accountID, Limit: 10})
		if queryErr != nil || page.Total != 1 {
			t.Fatalf("articles for %s = %#v err=%v", accountID, page, queryErr)
		}
	}
}

func TestLocalAlbumRuntimeTraversesPagesThenQueuesOneBatchDownloadJob(t *testing.T) {
	database := openAppTestDatabase(t)
	account, err := database.SaveAccount(context.Background(), domain.Account{FakeID: "fake-a", Name: "Account"})
	if err != nil {
		t.Fatal(err)
	}
	albumID := domain.AlbumID("album-a")
	album := domain.Album{ID: albumID, AccountID: account.ID, UpstreamID: "upstream-album", Name: "Album", ArticleCount: 3}
	article := func(suffix string) domain.Article {
		return domain.Article{ID: domain.ArticleID("article-" + suffix), AccountID: account.ID, Aid: "aid-" + suffix,
			Title: strings.ToUpper(suffix), CanonicalURL: "https://mp.weixin.qq.com/s/" + suffix}
	}
	source := &appAlbumPageSource{pages: map[string]wechat.AlbumPage{
		"": {Album: album, Items: []wechat.AlbumArticle{{Key: "100:1", Article: article("a")}, {Key: "200:1", Article: article("b")}},
			Next: domain.AlbumCheckpoint{BeginMessageID: "200", BeginItemIndex: "1"}},
		"200": {Album: album, Items: []wechat.AlbumArticle{{Key: "200:1", Article: article("b")}, {Key: "300:1", Article: article("c")}}, Completed: true},
	}}
	albumRunner, err := syncrunner.NewAlbumRunner(source, database, syncrunner.AlbumRunnerOptions{Sleep: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	downloads := &recordingDownloadJobs{job: domain.Job{ID: "download-batch", Kind: "article_download", State: domain.JobQueued}}
	starter := &recordingJobStarter{}
	runtime := &localSyncRuntime{profile: "profile-a", store: library.NewJobStore(database), library: database,
		downloads: downloads, starter: starter, album: albumRunner}
	job, err := runtime.StartAlbum(context.Background(), syncrunner.AlbumSyncRequest{
		FakeID: "fake-a", AlbumID: "upstream-album", Order: wechat.AlbumForward, PageSize: 2,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	final, err := runtime.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	if !reflect.DeepEqual(source.begins, []string{"", "200"}) || len(downloads.requests) != 1 ||
		!reflect.DeepEqual(downloads.requests[0].ArticleIDs, []domain.ArticleID{"article-a", "article-b", "article-c"}) ||
		!reflect.DeepEqual(starter.jobs, []domain.Job{downloads.job}) {
		t.Fatalf("begins=%#v downloads=%#v started=%#v", source.begins, downloads.requests, starter.jobs)
	}
	items, err := runtime.store.ListItems(context.Background(), job.ID)
	if err != nil || len(items) != 1 || !strings.Contains(string(items[0].Checkpoint), "download-batch") {
		t.Fatalf("album items=%#v err=%v", items, err)
	}
	page, err := database.QueryArticles(context.Background(), domain.ArticleQuery{AlbumID: albumID, Limit: 10})
	if err != nil || page.Total != 3 {
		t.Fatalf("album articles=%#v err=%v", page, err)
	}
}

func TestLocalSyncRuntimeFindsAlbumBeyondFirstFiveHundred(t *testing.T) {
	database := openAppTestDatabase(t)
	account, err := database.SaveAccount(context.Background(), domain.Account{FakeID: "fake-many", Name: "Many albums"})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 501; index++ {
		_, err := database.SaveAlbumPage(context.Background(), library.AlbumPageCommit{Album: domain.Album{
			ID: domain.AlbumID(fmt.Sprintf("album-%03d", index)), AccountID: account.ID,
			UpstreamID: fmt.Sprintf("upstream-%03d", index), Name: fmt.Sprintf("Album %03d", index),
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	runtime := &localSyncRuntime{profile: "profile-a", store: library.NewJobStore(database), library: database,
		album: &syncrunner.AlbumRunner{}}
	job, err := runtime.StartAlbumByID(context.Background(), account.ID, "album-500")
	if err != nil {
		t.Fatal(err)
	}
	if job.ID == "" {
		t.Fatal("album job ID is empty")
	}
}

type appAlbumPageSource struct {
	pages  map[string]wechat.AlbumPage
	begins []string
}

func (source *appAlbumPageSource) ListAlbumArticles(_ context.Context, request wechat.AlbumListRequest) (wechat.AlbumPage, error) {
	source.begins = append(source.begins, request.BeginMessageID)
	return source.pages[request.BeginMessageID], nil
}

type recordingDownloadJobs struct {
	requests []domain.DownloadRequest
	job      domain.Job
}

func (runtime *recordingDownloadJobs) Start(_ context.Context, request domain.DownloadRequest) (domain.Job, error) {
	runtime.requests = append(runtime.requests, request)
	return runtime.job, nil
}
func (*recordingDownloadJobs) Run(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (*recordingDownloadJobs) Recover(context.Context) (int64, error) { return 0, nil }

type recordingJobStarter struct{ jobs []domain.Job }

func (starter *recordingJobStarter) Start(_ context.Context, job domain.Job) error {
	starter.jobs = append(starter.jobs, job)
	return nil
}

type multiAccountSyncPageSource struct{}

func (multiAccountSyncPageSource) ListArticles(_ context.Context, request wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	suffix := strings.TrimPrefix(request.FakeID, "fake-")
	return wechat.ArticlePage{Items: []domain.Article{{ID: domain.ArticleID("article-" + suffix), Aid: "sync-" + suffix,
		Title: "Synced " + strings.ToUpper(suffix), CanonicalURL: "https://mp.weixin.qq.com/s/sync-" + suffix}},
		Total: 1, Next: 1, Completed: true}, nil
}

type syncPageSource struct{}

func (syncPageSource) ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	return wechat.ArticlePage{Items: []domain.Article{{ID: "article-sync", Aid: "sync-a", Title: "Synced",
		CanonicalURL: "https://mp.weixin.qq.com/s/sync-a"}}, Total: 1, Next: 1, Completed: true}, nil
}

func openAppTestDatabase(t *testing.T) *library.Database {
	t.Helper()
	database, err := library.Open(context.Background(), library.OpenOptions{
		Path: filepath.Join(t.TempDir(), "library.sqlite"), ProfileID: "profile-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

type inProcessWorkerLauncher struct {
	run  func(context.Context, []string, []string) error
	done chan error
}

type recordingWorkerLauncher struct {
	calls      int
	executable string
	args       []string
}

func (launcher *recordingWorkerLauncher) Start(_ context.Context, executable string, args, _ []string) error {
	launcher.calls++
	launcher.executable = executable
	launcher.args = append([]string(nil), args...)
	return nil
}

func (launcher *inProcessWorkerLauncher) Start(ctx context.Context, _ string, args, environment []string) error {
	if launcher.done == nil {
		launcher.done = make(chan error, 1)
	}
	go func() { launcher.done <- launcher.run(ctx, environment, args) }()
	return nil
}

type loopbackResolver struct{}

func (loopbackResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
}

func TestSignalInterruptionProducesDistinctStructuredResult(t *testing.T) {
	applicationAdapter, stdout, stderr := newTestApp(t)
	signals := make(chan os.Signal, 1)
	applicationAdapter.core = &commandJobApplication{states: []domain.JobState{domain.JobQueued}, signals: signals}
	signals <- os.Interrupt
	err := applicationAdapter.Execute(context.Background(), []string{
		"download", "article", "--article", "article-a", "--follow", "--poll-interval", "100ms", "--json",
	})
	if ExitCode(err) != 1 || !IsInterrupted(err) {
		t.Fatalf("interruption = %v, exit=%d, interrupted=%v", err, ExitCode(err), IsInterrupted(err))
	}
	if stdout.Len() != 0 {
		t.Fatalf("interrupted command wrote partial success JSON: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "queued") {
		t.Fatalf("follow progress missing from stderr: %q", stderr.String())
	}
	var encoded bytes.Buffer
	if err := WriteErrorJSON(&encoded, err); err != nil {
		t.Fatal(err)
	}
	assertErrorEnvelope(t, encoded.Bytes(), "interrupted", 1)
}

func TestDestructiveCommandsRequireExactConfirmation(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	core := &commandJobApplication{}
	applicationAdapter.core = core
	for _, confirmation := range []string{"", "delete-accounts:wrong"} {
		err := applicationAdapter.Execute(context.Background(), []string{
			"account", "delete", "account-a", "--confirm", confirmation, "--json",
		})
		if ExitCode(err) != 2 || core.deleteCalls != 0 {
			t.Fatalf("confirmation %q: error=%v exit=%d deleteCalls=%d", confirmation, err, ExitCode(err), core.deleteCalls)
		}
	}
	if err := applicationAdapter.Execute(context.Background(), []string{
		"account", "delete", "account-a", "--confirm", "delete-accounts:account-a", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if core.deleteCalls != 1 || !strings.Contains(stdout.String(), `"accountsDeleted": 1`) {
		t.Fatalf("confirmed delete calls=%d output=%s", core.deleteCalls, stdout.String())
	}
}

func TestLocalFirstSessionCommandsAndRetirementMessaging(t *testing.T) {
	applicationAdapter, stdout, stderr := newTestApp(t)
	core := &localSessionCommandApplication{commandJobApplication: commandJobApplication{}}
	applicationAdapter.core = core

	if err := applicationAdapter.Execute(context.Background(), []string{"status", "--json"}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"phase": "retired"`, `"webRetained": false`, `"remoteMCPRetained": false`, `"remoteOAuth": false`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("local retirement status missing %q: %s", expected, stdout.String())
		}
	}

	stdout.Reset()
	qrPath := filepath.Join(t.TempDir(), "login.png")
	if err := applicationAdapter.Execute(context.Background(), []string{
		"login", "--qr-output", qrPath, "--poll-interval", "500ms", "--refreshes", "0", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if core.beginCalls != 1 || core.completeCalls != 1 || !strings.Contains(stderr.String(), "QR written") {
		t.Fatalf("login calls begin=%d complete=%d stderr=%q", core.beginCalls, core.completeCalls, stderr.String())
	}
	if _, err := os.Stat(qrPath); err != nil {
		t.Fatalf("login QR not written: %v", err)
	}

	stdout.Reset()
	if err := applicationAdapter.Execute(context.Background(), []string{"logout", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !core.logoutCalled || !strings.Contains(stdout.String(), `"localSessionRemoved": true`) {
		t.Fatalf("local logout called=%v output=%s", core.logoutCalled, stdout.String())
	}

	stdout.Reset()
	err := applicationAdapter.Execute(context.Background(), []string{"legacy"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("retired legacy command error=%v exit=%d", err, ExitCode(err))
	}
}

func TestCompletionWritesShellScriptWithoutJSONEnvelope(t *testing.T) {
	applicationAdapter, stdout, _ := newTestApp(t)
	if err := applicationAdapter.Execute(context.Background(), []string{"completion", "bash"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "__start_wechat-article") || strings.Contains(stdout.String(), `"schemaVersion"`) {
		t.Fatalf("completion output is not a pure bash script:\n%s", stdout.String())
	}

	stdout.Reset()
	err := applicationAdapter.Execute(context.Background(), []string{"completion", "invalid"})
	if ExitCode(err) != 2 {
		t.Fatalf("invalid completion shell exit=%d error=%v", ExitCode(err), err)
	}
}

func TestLocalMCPServeUsesStdioAndProfilePolicy(t *testing.T) {
	applicationAdapter, stdout, stderr := newTestApp(t)
	applicationAdapter.stdin = strings.NewReader(fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":%q}}\n", mcp.ProtocolVersion))
	if err := applicationAdapter.Execute(context.Background(), []string{"mcp", "serve", "--transport", "stdio"}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		JSONRPC string `json:"jsonrpc"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Experimental map[string]any `json:"experimental"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &response); err != nil {
		t.Fatalf("MCP stdout is not one JSON-RPC response: %v\n%s", err, stdout.String())
	}
	if response.JSONRPC != "2.0" || response.Result.ProtocolVersion != mcp.ProtocolVersion || response.Result.Capabilities.Experimental["localOnly"] != true ||
		response.Result.Capabilities.Experimental["remoteOAuth"] != false {
		t.Fatalf("MCP initialize response = %#v", response)
	}
	if strings.Contains(stdout.String(), "mcp:") || strings.Contains(stderr.String(), "oauth") {
		t.Fatalf("protocol/log isolation violated: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if err := applicationAdapter.Execute(context.Background(), []string{"mcp", "serve", "--transport", "http"}); ExitCode(err) != 2 {
		t.Fatalf("unsupported transport error=%v exit=%d", err, ExitCode(err))
	}
}

func assertErrorEnvelope(t *testing.T, data []byte, kind string, exitCode int) {
	t.Helper()
	var envelope struct {
		SchemaVersion string `json:"schemaVersion"`
		Success       bool   `json:"success"`
		Error         struct {
			Kind     string `json:"kind"`
			ExitCode int    `json:"exitCode"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("error JSON invalid: %v\n%s", err, data)
	}
	if envelope.SchemaVersion != JSONSchemaVersion || envelope.Success || envelope.Error.Kind != kind || envelope.Error.ExitCode != exitCode {
		t.Fatalf("error envelope = %#v", envelope)
	}
}

type commandJobApplication struct {
	fixedApplication
	mu          sync.Mutex
	states      []domain.JobState
	getCalls    int
	deleteCalls int
	signals     <-chan os.Signal
}

func (application *commandJobApplication) StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error) {
	return domain.Job{ID: "job-local", Kind: "article_download", State: domain.JobQueued, Profile: "local"}, nil
}

func (application *commandJobApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	application.mu.Lock()
	defer application.mu.Unlock()
	state := domain.JobCompleted
	if len(application.states) > 0 {
		index := application.getCalls
		if index >= len(application.states) {
			index = len(application.states) - 1
		}
		state = application.states[index]
	}
	application.getCalls++
	return domain.Job{ID: "job-local", Kind: "article_download", State: state, Profile: "local"}, nil
}

func (application *commandJobApplication) DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error) {
	application.deleteCalls++
	return domain.AccountDeleteReport{AccountsDeleted: 1, ArticlesDeleted: 2, ObjectsGarbageEligible: 3}, nil
}

func (application *commandJobApplication) ProcessSignals() <-chan os.Signal {
	return application.signals
}

type localSessionCommandApplication struct {
	commandJobApplication
	beginCalls    int
	completeCalls int
	logoutCalled  bool
}

func (application *localSessionCommandApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return domain.RuntimeStatus{Version: "test", Profile: "local"}, nil
}

func (application *localSessionCommandApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-a", AccountName: "Local account"}, nil
}

func (application *localSessionCommandApplication) BeginLogin(context.Context, string) (wechat.LoginFlow, error) {
	application.beginCalls++
	return wechat.LoginFlow{SessionID: "login-local", QRBytes: testQRPNG(), ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (application *localSessionCommandApplication) PollLogin(context.Context) (wechat.PollResult, error) {
	return wechat.PollResult{State: wechat.QRConfirmed, AccountCount: 1}, nil
}

func (application *localSessionCommandApplication) CompleteLogin(context.Context) (wechat.Session, error) {
	application.completeCalls++
	return wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-a", AccountName: "Local account"}, nil
}

func (application *localSessionCommandApplication) Logout(context.Context) error {
	application.logoutCalled = true
	return nil
}

func testQRPNG() []byte {
	var buffer bytes.Buffer
	value := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if (x/8+y/8)%2 == 0 {
				value.Set(x, y, color.Black)
			} else {
				value.Set(x, y, color.White)
			}
		}
	}
	_ = png.Encode(&buffer, value)
	return buffer.Bytes()
}
