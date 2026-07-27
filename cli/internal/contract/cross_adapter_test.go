// Package contract exercises public local-adapter seams against one
// deterministic application facade.
package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/app"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/web"
)

func TestCrossAdapterQueriesShareAccountArticleAndJobOutcomes(t *testing.T) {
	want := newFixture()
	wantAccounts, wantArticles, wantJobs := want.library.accounts, want.library.articles, want.jobs.page

	t.Run("Cobra", func(t *testing.T) {
		fixture := newFixture()
		if got := decodePage[domain.Account](t, runCobra(t, fixture.service, "account", "list", "--limit", "50", "--json")); !reflect.DeepEqual(got, wantAccounts) {
			t.Fatalf("accounts = %#v, want %#v", got, wantAccounts)
		}
		if got := decodePage[domain.Article](t, runCobra(t, fixture.service, "article", "list", "--limit", "50", "--sort", "", "--json")); !samePage(got, wantArticles) {
			t.Fatalf("articles = %#v, want %#v", got, wantArticles)
		}
		if got := decodePage[domain.Job](t, runCobra(t, fixture.service, "job", "list", "--limit", "50", "--json")); !reflect.DeepEqual(got, wantJobs) {
			t.Fatalf("jobs = %#v, want %#v", got, wantJobs)
		}
		assertDefaultQueries(t, fixture)
	})

	t.Run("TUI", func(t *testing.T) {
		fixture := newFixture()
		model := tui.NewWorkspace(tui.WorkspaceOptions{Context: context.Background(), Application: fixture.service, PageSize: 50, Plain: true})
		updated, command := model.Update(model.Init()())
		_ = command             // A successful initial load schedules the normal refresh tick.
		_ = updated.(tui.Model) // The model accepted the shared load message.
		assertDefaultQueries(t, fixture)
	})

	t.Run("MCP", func(t *testing.T) {
		fixture := newFixture()
		adapter := mcp.New(fixture.service)
		result, err := adapter.Call(context.Background(), "accounts.query", json.RawMessage(`{"limit":50}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeMCPPage[domain.Account](t, result); !reflect.DeepEqual(got, wantAccounts) {
			t.Fatalf("accounts.query = %#v, want %#v", got, wantAccounts)
		}
		result, err = adapter.Call(context.Background(), "articles.query", json.RawMessage(`{"limit":50}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeMCPPage[domain.Article](t, result); !samePage(got, wantArticles) {
			t.Fatalf("articles.query = %#v, want %#v", got, wantArticles)
		}
		result, err = adapter.Call(context.Background(), "jobs.query", json.RawMessage(`{"limit":50}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeMCPPage[domain.Job](t, result); !reflect.DeepEqual(got, wantJobs) {
			t.Fatalf("jobs.query = %#v, want %#v", got, wantJobs)
		}
		assertDefaultQueries(t, fixture)
	})

	t.Run("browser API", func(t *testing.T) {
		fixture := newFixture()
		server, client, base := startBrowser(t, fixture.service, nil)
		defer stopBrowser(t, server)
		if got := getPage[domain.Account](t, client, base+"/api/v1/accounts?limit=50"); !reflect.DeepEqual(got, wantAccounts) {
			t.Fatalf("GET accounts = %#v, want %#v", got, wantAccounts)
		}
		articlePage := getPage[application.WorkspaceArticle](t, client, base+"/api/v1/articles?limit=50")
		if articlePage.Total != wantArticles.Total || articlePage.Offset != wantArticles.Offset || articlePage.Limit != wantArticles.Limit || len(articlePage.Items) != len(wantArticles.Items) || articlePage.Items[0].ID != wantArticles.Items[0].ID || articlePage.Items[0].Title != wantArticles.Items[0].Title {
			t.Fatalf("GET articles = %#v, want page for %#v", articlePage, wantArticles)
		}
		if got := getPage[domain.Job](t, client, base+"/api/v1/jobs?limit=50"); !reflect.DeepEqual(got, wantJobs) {
			t.Fatalf("GET jobs = %#v, want %#v", got, wantJobs)
		}
		assertDefaultQueries(t, fixture)
	})
}

func TestCrossAdapterExportQueuesJobsWithoutBypassingSafeDestinationBoundaries(t *testing.T) {
	t.Run("Cobra queues the shared export job", func(t *testing.T) {
		fixture := newFixture()
		var envelope struct {
			Data domain.Job `json:"data"`
		}
		if err := json.Unmarshal(runCobra(t, fixture.service, "export", "start", "--article", "article-1", "--output", t.TempDir(), "--json"), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.Kind != "export" || len(fixture.jobs.created) != 1 || fixture.jobs.created[0].Kind != "export" {
			t.Fatalf("Cobra outcome=%#v specs=%#v", envelope.Data, fixture.jobs.created)
		}
	})

	t.Run("MCP rejects escaped output before the shared facade and queues an allowed output", func(t *testing.T) {
		fixture := newFixture()
		allowed := t.TempDir()
		adapter := mcp.New(fixture.service, mcp.Options{AllowedRoots: []string{allowed}})
		outside := filepath.Join(t.TempDir(), "outside")
		_, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(`{"format":"markdown","outputRoot":`+quoteJSON(outside)+`,"selection":{"kind":"explicit_ids","articleIds":["article-1"]}}`))
		if err == nil || len(fixture.jobs.created) != 0 {
			t.Fatalf("escaped MCP export error=%v specs=%#v", err, fixture.jobs.created)
		}
		result, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(`{"format":"markdown","outputRoot":`+quoteJSON(filepath.Join(allowed, "batch"))+`,"selection":{"kind":"explicit_ids","articleIds":["article-1"]}}`))
		if err != nil {
			t.Fatal(err)
		}
		var job struct {
			JobID   domain.JobID     `json:"jobId"`
			State   domain.JobState  `json:"state"`
			Kind    string           `json:"kind"`
			Profile domain.ProfileID `json:"profile"`
		}
		decodeMCP(t, result, &job)
		if job.Kind != "export" || len(fixture.jobs.created) != 1 {
			t.Fatalf("MCP outcome=%#v specs=%#v", job, fixture.jobs.created)
		}
	})

	t.Run("browser accepts only opaque directory capabilities", func(t *testing.T) {
		fixture := newFixture()
		exports := &exportFixture{}
		server, client, base := startBrowser(t, fixture.service, exports)
		defer stopBrowser(t, server)
		csrf := csrfToken(t, client, base)

		response := postJSON(t, client, base+"/api/v1/exports/start", csrf, `{"directoryToken":"/tmp/escape","format":"markdown","confirm":"start-export:/tmp/escape"}`)
		if response.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("escaped browser status=%d body=%s", response.StatusCode, body)
		}
		response.Body.Close()
		if exports.startCalls != 0 {
			t.Fatal("browser forwarded a filesystem-looking directory token")
		}

		response = postJSON(t, client, base+"/api/v1/exports/start", csrf, `{"directoryToken":"dir_fixture","subdirectory":"batch","selection":{"kind":"explicit_ids","articleIds":["article-1"]},"format":"markdown","confirm":"start-export:dir_fixture"}`)
		if response.StatusCode != http.StatusAccepted {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("safe browser status=%d body=%s", response.StatusCode, body)
		}
		response.Body.Close()
		if exports.startCalls != 1 || exports.start.DirectoryToken != "dir_fixture" || exports.start.Subdirectory != "batch" {
			t.Fatalf("browser request=%#v calls=%d", exports.start, exports.startCalls)
		}
	})
}

type fixtureLibrary struct {
	accounts domain.Page[domain.Account]
	articles domain.Page[domain.Article]
	albums   domain.Page[domain.Album]
	storage  domain.StorageStatus

	accountQuery domain.AccountQuery
	articleQuery domain.ArticleQuery
}

func (library *fixtureLibrary) QueryAccounts(_ context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	library.accountQuery = query
	return library.accounts, nil
}
func (library *fixtureLibrary) AccountNames(_ context.Context, ids []domain.AccountID) (map[domain.AccountID]string, error) {
	names := make(map[domain.AccountID]string)
	for _, id := range ids {
		for _, account := range library.accounts.Items {
			if account.ID == id {
				names[id] = account.Name
			}
		}
	}
	return names, nil
}
func (library *fixtureLibrary) QueryArticles(_ context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	library.articleQuery = query
	return library.articles, nil
}
func (library *fixtureLibrary) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return library.albums, nil
}
func (library *fixtureLibrary) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return library.storage, nil
}

type fixtureJobs struct {
	page    domain.Page[domain.Job]
	created []jobs.Spec
	query   domain.JobQuery
}

func (manager *fixtureJobs) Create(_ context.Context, spec jobs.Spec) (domain.Job, error) {
	manager.created = append(manager.created, spec)
	return domain.Job{ID: "11111111-1111-1111-1111-111111111111", Kind: spec.Kind, Profile: spec.Profile, State: domain.JobQueued, CreatedAt: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)}, nil
}
func (*fixtureJobs) Get(context.Context, domain.JobID) (domain.Job, error) { return domain.Job{}, nil }
func (manager *fixtureJobs) Query(_ context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	manager.query = query
	return manager.page, nil
}
func (*fixtureJobs) Cancel(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}

type contractFixture struct {
	service *application.Service
	library *fixtureLibrary
	jobs    *fixtureJobs
}

func newFixture() contractFixture {
	library := &fixtureLibrary{
		accounts: domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", Name: "Account fixture"}}, Total: 1, Limit: 50},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", AccountID: "account-1", Title: "Article fixture"}}, Total: 1, Limit: 50},
		albums:   domain.Page[domain.Album]{Items: []domain.Album{}, Limit: 50},
		storage:  domain.StorageStatus{DatabaseAvailable: true, ObjectStoreReady: true},
	}
	manager := &fixtureJobs{page: domain.Page[domain.Job]{Items: []domain.Job{{ID: "22222222-2222-2222-2222-222222222222", Kind: "export", State: domain.JobQueued}}, Total: 1, Limit: 50}}
	return contractFixture{service: application.New(application.Options{Runtime: runtimeenv.Dependencies{Profile: "contract"}, Library: library, Jobs: manager}), library: library, jobs: manager}
}

func assertDefaultQueries(t *testing.T, fixture contractFixture) {
	t.Helper()
	if !reflect.DeepEqual(fixture.library.accountQuery, domain.AccountQuery{Limit: 50}) || !reflect.DeepEqual(fixture.library.articleQuery, domain.ArticleQuery{Limit: 50}) ||
		!reflect.DeepEqual(fixture.jobs.query, domain.JobQuery{Limit: 50}) {
		t.Fatalf("adapter queries account=%#v article=%#v job=%#v", fixture.library.accountQuery, fixture.library.articleQuery, fixture.jobs.query)
	}
}

func runCobra(t *testing.T, core application.Application, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	instance, err := app.NewWithDependencies(context.Background(), strings.NewReader(""), &stdout, &stderr, app.Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()}, Secrets: secrets.NewMemoryStore(),
		ApplicationFactory: func(*app.ProfileRuntime) application.Application { return core },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.Execute(context.Background(), args); err != nil {
		t.Fatalf("Cobra %v: %v stderr=%s", args, err, stderr.String())
	}
	return stdout.Bytes()
}

func decodePage[T any](t *testing.T, output []byte) domain.Page[T] {
	t.Helper()
	var envelope struct {
		Data domain.Page[T] `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func decodeMCPPage[T any](t *testing.T, value any) domain.Page[T] {
	t.Helper()
	var page domain.Page[T]
	decodeMCP(t, value, &page)
	return page
}

func decodeMCP(t *testing.T, value, destination any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		t.Fatal(err)
	}
}

func samePage[T any](got, want domain.Page[T]) bool {
	return got.Total == want.Total && got.Offset == want.Offset && got.Limit == want.Limit && reflect.DeepEqual(normalizeJSON(got.Items), normalizeJSON(want.Items))
}

func normalizeJSON(value any) any {
	encoded, _ := json.Marshal(value)
	var result any
	_ = json.Unmarshal(encoded, &result)
	return result
}

type exportFixture struct {
	start      application.WorkspaceStartExportRequest
	startCalls int
}

func (*exportFixture) DefaultExportDirectory(context.Context) (application.WorkspaceExportDirectory, error) {
	return application.WorkspaceExportDirectory{}, nil
}
func (*exportFixture) CreateExportDirectory(context.Context, application.WorkspaceCreateExportDirectoryRequest) (application.WorkspaceExportDirectory, error) {
	return application.WorkspaceExportDirectory{}, nil
}
func (service *exportFixture) StartExport(_ context.Context, request application.WorkspaceStartExportRequest) (application.WorkspaceExportJob, error) {
	service.start, service.startCalls = request, service.startCalls+1
	return application.WorkspaceExportJob{ID: "33333333-3333-3333-3333-333333333333", State: domain.JobQueued}, nil
}
func (*exportFixture) ExportRecords(context.Context, application.WorkspacePageRequest) (application.WorkspacePage[application.WorkspaceExportRecord], error) {
	return application.WorkspacePage[application.WorkspaceExportRecord]{}, nil
}
func (*exportFixture) ExportManifest(context.Context, string) (application.WorkspaceExportManifest, error) {
	return application.WorkspaceExportManifest{}, nil
}
func (*exportFixture) VerifyExport(context.Context, string) (application.WorkspaceExportVerification, error) {
	return application.WorkspaceExportVerification{}, nil
}
func (*exportFixture) DownloadArtifact(context.Context, application.WorkspaceDownloadArtifactRequest) (application.WorkspaceDownloadArtifact, error) {
	return application.WorkspaceDownloadArtifact{}, nil
}
func (*exportFixture) OpenExportOutput(context.Context, string) error { return nil }

func startBrowser(t *testing.T, core application.Application, exports application.WorkspaceExportService) (*web.Server, *http.Client, string) {
	t.Helper()
	server, err := web.New(web.Options{Application: core, Exports: exports})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(context.Background()) }()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.Get(server.URL())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status=%d", response.StatusCode)
	}
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	return server, client, base
}

func stopBrowser(t *testing.T, server *web.Server) {
	t.Helper()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func getPage[T any](t *testing.T, client *http.Client, target string) domain.Page[T] {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, body)
	}
	var payload struct {
		Items  []T `json:"items"`
		Total  int `json:"total"`
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return domain.Page[T]{Items: payload.Items, Total: payload.Total, Offset: payload.Offset, Limit: payload.Limit}
}

func csrfToken(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	response, err := client.Get(base + "/api/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload struct {
		Data struct {
			CSRF string `json:"csrfToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.CSRF == "" {
		t.Fatal("browser status omitted CSRF token")
	}
	return payload.Data.CSRF
}

func postJSON(t *testing.T, client *http.Client, target, csrf, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", strings.TrimSuffix(strings.Split(target, "/api/")[0], "/"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
