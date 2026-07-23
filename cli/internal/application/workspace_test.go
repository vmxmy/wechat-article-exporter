package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type workspaceLibrary struct {
	accounts      domain.Page[domain.Account]
	articles      domain.Page[domain.Article]
	albums        domain.Page[domain.Album]
	storage       domain.StorageStatus
	saved         []domain.SavedArticleQuery
	accountQuery  domain.AccountQuery
	articleQuery  domain.ArticleQuery
	albumQuery    domain.AlbumQuery
	accountsError error
}

func (library *workspaceLibrary) QueryAccounts(_ context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	library.accountQuery = query
	return library.accounts, library.accountsError
}

func (library *workspaceLibrary) QueryArticles(_ context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	library.articleQuery = query
	return library.articles, nil
}

func (library *workspaceLibrary) QueryAlbums(_ context.Context, query domain.AlbumQuery) (domain.Page[domain.Album], error) {
	library.albumQuery = query
	return library.albums, nil
}

func (library *workspaceLibrary) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return library.storage, nil
}

func (library *workspaceLibrary) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return library.saved, nil
}

func (library *workspaceLibrary) SaveArticleQuery(_ context.Context, name string, query domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	return domain.SavedArticleQuery{Name: name, Query: query}, nil
}

func (*workspaceLibrary) DeleteSavedArticleQuery(context.Context, string) (bool, error) {
	return false, nil
}

type workspaceJobManager struct {
	page  domain.Page[domain.Job]
	query domain.JobQuery
}

func (*workspaceJobManager) Create(context.Context, jobs.Spec) (domain.Job, error) {
	return domain.Job{}, nil
}
func (*workspaceJobManager) Get(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (manager *workspaceJobManager) Query(_ context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	manager.query = query
	return manager.page, nil
}
func (*workspaceJobManager) Cancel(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}

func TestWorkspaceReadFacadeUsesApplicationAndReturnsSafeDTOs(t *testing.T) {
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	library := &workspaceLibrary{
		accounts: domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", Name: "Fixture"}}, Total: 1, Offset: 3, Limit: 20},
		articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", Title: "Fixture article"}}, Total: 1, Offset: 0, Limit: 20},
		albums:   domain.Page[domain.Album]{Items: []domain.Album{{ID: "album-1", Name: "Fixture album"}}, Total: 1, Offset: 0, Limit: 20},
		storage:  domain.StorageStatus{DatabaseAvailable: true, ObjectStoreReady: true, Articles: 1},
		saved:    []domain.SavedArticleQuery{{Name: "recent"}},
	}
	manager := &workspaceJobManager{page: domain.Page[domain.Job]{Items: []domain.Job{{ID: "job-1", Kind: "export"}}, Total: 1, Limit: 20}}
	service := New(Options{
		Version: "fixture-version",
		Runtime: runtimeenv.Dependencies{Clock: fixedClock{value: now}, Profile: "fixture-profile",
			Paths: domain.RuntimePaths{Config: "/secret-config", Data: "/secret-data"}},
		Library: library,
		Jobs:    manager,
	})
	workspace := NewWorkspace(service)

	runtime, err := workspace.Runtime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Profile != "fixture-profile" || runtime.Version != "fixture-version" || runtime.CheckedAt != now || !runtime.OfflineReady {
		t.Fatalf("Runtime() = %#v", runtime)
	}
	if reflect.ValueOf(runtime).FieldByName("Paths").IsValid() {
		t.Fatalf("WorkspaceRuntime exposed runtime paths: %#v", runtime)
	}

	session, err := workspace.Session(context.Background())
	if err != nil || session.State != wechat.SessionMissing || session.AccountID != "" {
		t.Fatalf("Session() = %#v, %v", session, err)
	}

	accounts, err := workspace.Accounts(context.Background(), WorkspaceAccountQuery{Keyword: " fixture ", Page: WorkspacePageRequest{Offset: 3, Limit: 20}})
	if err != nil || accounts.Total != 1 || accounts.Items[0].ID != "account-1" {
		t.Fatalf("Accounts() = %#v, %v", accounts, err)
	}
	if library.accountQuery != (domain.AccountQuery{Keyword: "fixture", Offset: 3, Limit: 20}) {
		t.Fatalf("account query = %#v", library.accountQuery)
	}

	_, err = workspace.Articles(context.Background(), WorkspaceArticleQuery{AccountID: "account-1", Keyword: " article ",
		Sorts: []domain.ArticleSort{{Field: "published", Direction: domain.SortDescending}}, Page: WorkspacePageRequest{Limit: 20}})
	if err != nil || library.articleQuery.AccountID != "account-1" || library.articleQuery.Keyword != "article" || library.articleQuery.Limit != 20 {
		t.Fatalf("Articles() error=%v query=%#v", err, library.articleQuery)
	}

	_, err = workspace.Albums(context.Background(), WorkspaceAlbumQuery{AccountID: "account-1", Page: WorkspacePageRequest{Limit: 20}})
	if err != nil || library.albumQuery.AccountID != "account-1" || library.albumQuery.Limit != 20 {
		t.Fatalf("Albums() error=%v query=%#v", err, library.albumQuery)
	}

	saved, err := workspace.SavedArticleQueries(context.Background(), WorkspacePageRequest{Limit: 20})
	if err != nil || len(saved.Items) != 1 || saved.Items[0].Name != "recent" || saved.Total != 1 {
		t.Fatalf("SavedArticleQueries() = %#v, %v", saved, err)
	}

	jobsPage, err := workspace.Jobs(context.Background(), WorkspaceJobQuery{Kind: " export ", States: []domain.JobState{domain.JobRunning}, Page: WorkspacePageRequest{Limit: 20}})
	if err != nil || jobsPage.Total != 1 || manager.query.Kind != "export" || !reflect.DeepEqual(manager.query.States, []domain.JobState{domain.JobRunning}) {
		t.Fatalf("Jobs() = %#v, query=%#v, err=%v", jobsPage, manager.query, err)
	}
}

func TestWorkspacePaginationRejectsUnboundedOrInvalidRequests(t *testing.T) {
	workspace := NewWorkspace(New(Options{}))

	page, err := workspace.Accounts(context.Background(), WorkspaceAccountQuery{})
	if err != nil || page.Limit != WorkspaceDefaultPageLimit || page.Offset != 0 || len(page.Items) != 0 {
		t.Fatalf("default page = %#v, %v", page, err)
	}

	for _, request := range []WorkspacePageRequest{{Offset: -1}, {Limit: -1}, {Limit: WorkspaceMaximumPageLimit + 1}} {
		_, err := workspace.Jobs(context.Background(), WorkspaceJobQuery{Page: request})
		var workspaceErr *WorkspaceError
		if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInvalidArgument {
			t.Fatalf("Jobs(%#v) error = %v", request, err)
		}
	}
}

func TestWorkspaceSavedArticleQueriesAreBounded(t *testing.T) {
	workspace := NewWorkspace(New(Options{Library: &workspaceLibrary{saved: []domain.SavedArticleQuery{
		{Name: "a"}, {Name: "b"}, {Name: "c"},
	}}}))

	page, err := workspace.SavedArticleQueries(context.Background(), WorkspacePageRequest{Offset: 1, Limit: 1})
	if err != nil || page.Total != 3 || page.Offset != 1 || page.Limit != 1 || len(page.Items) != 1 || page.Items[0].Name != "b" {
		t.Fatalf("SavedArticleQueries() = %#v, %v", page, err)
	}
}

func TestWorkspaceErrorModelRedactsApplicationFailures(t *testing.T) {
	library := &workspaceLibrary{accountsError: errors.New("sqlite failure at /private/profile/database.sqlite")}
	workspace := NewWorkspace(New(Options{Library: library}))
	_, err := workspace.Accounts(context.Background(), WorkspaceAccountQuery{})
	var workspaceErr *WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInternal || workspaceErr.Message != "workspace operation failed" {
		t.Fatalf("unexpected error %#v", err)
	}
	if workspaceErr.Message == library.accountsError.Error() {
		t.Fatalf("unsafe underlying error leaked: %#v", workspaceErr)
	}

	_, err = NewWorkspace(New(Options{})).SavedArticleQueries(context.Background(), WorkspacePageRequest{})
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorUnavailable {
		t.Fatalf("unavailable error = %#v", err)
	}
}
