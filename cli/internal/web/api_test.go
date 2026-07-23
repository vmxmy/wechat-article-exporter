package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestReadAPIProvidesVersionedBoundedWorkspaceData(t *testing.T) {
	app := &apiApplication{
		runtime:  domain.RuntimeStatus{Version: "fixture", Profile: "fixture-profile", Paths: domain.RuntimePaths{Data: "/private/profile"}, Storage: domain.StorageStatus{Articles: 2}},
		session:  wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-1", AccountName: "Fixture"},
		accounts: domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", Name: "Fixture"}}, Total: 1},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", Title: "Fixture", CanonicalURL: "https://example.test/article"}}, Total: 1},
		albums:   domain.Page[domain.Album]{Items: []domain.Album{{ID: "album-1", Name: "Album"}}, Total: 1},
		jobs:     domain.Page[domain.Job]{Items: []domain.Job{{ID: "11111111-1111-1111-1111-111111111111", Kind: "sync", State: domain.JobRunning}}, Total: 1},
		saved:    []domain.SavedArticleQuery{{Name: "recent"}},
		job:      domain.Job{ID: "11111111-1111-1111-1111-111111111111", Kind: "sync", State: domain.JobRunning},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	for _, target := range []string{
		"/api/v1/runtime", "/api/v1/session", "/api/v1/accounts?keyword=fixture&limit=100", "/api/v1/articles?accountId=account-1&deleted=false&messageType=1&messageType=2&sort=published:desc",
		"/api/v1/albums?accountId=account-1", "/api/v1/saved-queries?limit=100", "/api/v1/jobs?state=running", "/api/v1/jobs/11111111-1111-1111-1111-111111111111", "/api/v1/storage", "/api/v1/events/snapshot", "/api/v1/snapshot",
	} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		var envelope apiEnvelope
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatalf("decode %s: %v", target, err)
		}
		response.Body.Close()
		if envelope.APIVersion != apiVersion || envelope.Data == nil {
			t.Fatalf("GET %s envelope = %#v", target, envelope)
		}
	}
	if app.accountQuery.Limit != 100 || app.articleQuery.Limit != application.WorkspaceDefaultPageLimit || app.articleQuery.Deleted == nil || *app.articleQuery.Deleted || len(app.articleQuery.MessageTypes) != 2 || len(app.articleQuery.Sorts) != 1 {
		t.Fatalf("queries not parsed/bounded: account=%#v article=%#v", app.accountQuery, app.articleQuery)
	}
	response := get(t, client, base+"/api/v1/runtime")
	body := readResponse(t, response)
	if strings.Contains(body, "/private/profile") {
		t.Fatalf("runtime response leaked absolute path: %s", body)
	}
}

func TestReadAPIRejectsUnauthorizedUnsupportedAndUnboundedQueries(t *testing.T) {
	server, client := startAPIApplicationServer(t, &apiApplication{})
	base := strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
	if response := get(t, client, base+"/api/v1/accounts"); response.StatusCode != http.StatusUnauthorized {
		response.Body.Close()
		t.Fatalf("unauthenticated status = %d", response.StatusCode)
	}
	authorize(t, client, server.URL())

	for _, target := range []string{
		"/api/v1/articles?limit=101", "/api/v1/articles?wat=1", "/api/v1/articles?deleted=maybe", "/api/v1/articles?state=one&state=two", "/api/v1/articles?sort=published:asc&direction=desc", "/api/v1/articles?readMin=9&readMax=1", "/api/v1/articles?publishedFrom=bad", "/api/v1/articles?sort=unsafe:asc", "/api/v1/jobs?state=wat", "/api/v1/saved-queries?offset=-1", "/api/v1/accounts?sort=name",
	} {
		response := get(t, client, base+target)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, readResponse(t, response))
		}
		assertAPIError(t, response, "invalid_argument")
	}
	request := requestWith(t, http.MethodPost, base+"/api/v1/accounts", nil, nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("method status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "method_not_allowed")

	response = get(t, client, base+"/api/v1/jobs/not-a-uuid")
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid job path status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	assertAPIError(t, response, "invalid_argument")
}

func TestReadAPIErrorModelDoesNotLeakApplicationFailures(t *testing.T) {
	server, client := startAPIApplicationServer(t, &apiApplication{accountsErr: errors.New("sqlite at /private/token=secret")})
	base := authorizeAPI(t, client, server.URL())
	response := get(t, client, base+"/api/v1/accounts")
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.StatusCode)
	}
	body := readResponse(t, response)
	if strings.Contains(body, "/private") || strings.Contains(body, "secret") {
		t.Fatalf("unsafe error leaked: %s", body)
	}
	var envelope apiErrorEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil || envelope.APIVersion != apiVersion || envelope.Error.Code != "internal" || envelope.Error.Message != "workspace operation failed" {
		t.Fatalf("error envelope = %#v err=%v", envelope, err)
	}
}

func TestReadAPIAdaptsExistingBrowserClientDTO(t *testing.T) {
	app := &apiApplication{
		runtime:  domain.RuntimeStatus{Version: "fixture", Profile: "fixture-profile", Storage: domain.StorageStatus{Articles: 2}},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", Title: "Fixture"}}, Total: 1},
		jobs:     domain.Page[domain.Job]{Items: []domain.Job{{ID: "job-1", Kind: "sync"}}, Total: 1},
	}
	server, client := startAPIApplicationServer(t, app)
	base := authorizeAPI(t, client, server.URL())

	response := get(t, client, base+"/api/v1/articles?page=2&page_size=25&search=fixture&sort=publishedAt&direction=desc")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("article status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var page struct {
		APIVersion string           `json:"apiVersion"`
		Data       []domain.Article `json:"data"`
		Pagination struct {
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
			Total    int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if page.APIVersion != apiVersion || len(page.Data) != 1 || page.Pagination.Page != 2 || page.Pagination.PageSize != 25 || page.Pagination.Total != 1 {
		t.Fatalf("article DTO = %#v", page)
	}
	if app.articleQuery.Keyword != "fixture" || app.articleQuery.Offset != 25 || app.articleQuery.Limit != 25 || len(app.articleQuery.Sorts) != 1 || app.articleQuery.Sorts[0] != (domain.ArticleSort{Field: "published", Direction: domain.SortDescending}) {
		t.Fatalf("browser article query = %#v", app.articleQuery)
	}

	response = get(t, client, base+"/api/v1/snapshot")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", response.StatusCode, readResponse(t, response))
	}
	var snapshot struct {
		APIVersion string `json:"apiVersion"`
		Runtime    struct {
			Profile string `json:"profile"`
		} `json:"runtime"`
		Storage domain.StorageStatus `json:"storage"`
		Jobs    struct {
			Items []domain.Job `json:"items"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if snapshot.APIVersion != apiVersion || snapshot.Runtime.Profile != "fixture-profile" || snapshot.Storage.Articles != 2 || len(snapshot.Jobs.Items) != 1 {
		t.Fatalf("snapshot DTO = %#v", snapshot)
	}
}

func startAPIApplicationServer(t *testing.T, app application.Application) (*Server, *http.Client) {
	t.Helper()
	server, err := New(Options{Application: app, Now: time.Now})
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

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return nil }}
}

func authorizeAPI(t *testing.T, client *http.Client, bootstrapURL string) string {
	t.Helper()
	authorize(t, client, bootstrapURL)
	return strings.TrimSuffix(strings.Split(bootstrapURL, "?")[0], "/")
}

func assertAPIError(t *testing.T, response *http.Response, code string) {
	t.Helper()
	var envelope apiErrorEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if envelope.APIVersion != apiVersion || envelope.Error.Code != code || envelope.Error.Message == "" {
		t.Fatalf("error envelope = %#v", envelope)
	}
}

type apiApplication struct {
	testApplication
	runtime      domain.RuntimeStatus
	session      wechat.Session
	accounts     domain.Page[domain.Account]
	articles     domain.Page[domain.Article]
	albums       domain.Page[domain.Album]
	jobs         domain.Page[domain.Job]
	saved        []domain.SavedArticleQuery
	job          domain.Job
	accountsErr  error
	accountQuery domain.AccountQuery
	articleQuery domain.ArticleQuery
	jobQuery     domain.JobQuery
}

func (app *apiApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return app.runtime, nil
}
func (app *apiApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return app.session, nil
}
func (app *apiApplication) QueryAccounts(_ context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	app.accountQuery = query
	page := app.accounts
	page.Offset, page.Limit = query.Offset, query.Limit
	return page, app.accountsErr
}
func (app *apiApplication) QueryArticles(_ context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	app.articleQuery = query
	page := app.articles
	page.Offset, page.Limit = query.Offset, query.Limit
	return page, nil
}
func (app *apiApplication) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return app.albums, nil
}
func (app *apiApplication) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return app.saved, nil
}
func (app *apiApplication) QueryJobs(_ context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	app.jobQuery = query
	return app.jobs, nil
}
func (app *apiApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	return app.job, nil
}

var _ application.Application = (*apiApplication)(nil)
