package application

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }
func (clock fixedClock) After(time.Duration) <-chan time.Time {
	return make(chan time.Time)
}

type fakeLibrary struct {
	accounts domain.Page[domain.Account]
	articles domain.Page[domain.Article]
	albums   domain.Page[domain.Album]
	storage  domain.StorageStatus
	saved    domain.Account
	updated  domain.Account
	byID     domain.Account
	byFakeID domain.Account
	manifest domain.AccountManifest
	imported domain.AccountImportReport
	deleted  domain.AccountDeleteReport
}

func (library fakeLibrary) QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return library.accounts, nil
}
func (library fakeLibrary) QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error) {
	return library.articles, nil
}
func (library fakeLibrary) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return library.albums, nil
}
func (library fakeLibrary) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return library.storage, nil
}
func (library fakeLibrary) SaveAccount(context.Context, domain.Account) (domain.Account, error) {
	return library.saved, nil
}
func (library fakeLibrary) UpdateAccount(context.Context, domain.Account) (domain.Account, error) {
	return library.updated, nil
}
func (library fakeLibrary) GetAccount(context.Context, domain.AccountID) (domain.Account, error) {
	return library.byID, nil
}
func (library fakeLibrary) GetAccountByFakeID(context.Context, string) (domain.Account, error) {
	return library.byFakeID, nil
}
func (library fakeLibrary) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return library.manifest, nil
}
func (library fakeLibrary) ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error) {
	return library.imported, nil
}
func (library fakeLibrary) DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error) {
	return library.deleted, nil
}

type fakeJobs struct {
	now     time.Time
	created []jobs.Spec
}

type fakeDiscovery struct{}

func (fakeDiscovery) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (fakeDiscovery) SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{Items: []domain.Account{{FakeID: "fixture-a", Name: "Fixture"}}, Total: 1}, nil
}
func (fakeDiscovery) ResolveAccountName(context.Context, string) (string, error) {
	return "Fixture", nil
}
func (fakeDiscovery) ResolveAccountFromArticle(context.Context, string) (domain.Account, error) {
	return domain.Account{FakeID: "fixture-a", Name: "Fixture"}, nil
}
func (fakeDiscovery) ResolveArticleAlbums(context.Context, string) (wechat.ArticleAlbums, error) {
	return wechat.ArticleAlbums{FakeID: "fixture-a", Albums: []wechat.AlbumRef{{AlbumID: "album-a"}}}, nil
}
func (fakeDiscovery) AccountDetails(context.Context, string) (wechat.AccountDetails, error) {
	return wechat.AccountDetails{Account: domain.Account{FakeID: "fixture-a", Name: "Fixture"}}, nil
}
func (fakeDiscovery) AuthorInfo(context.Context, string) (wechat.AuthorInfo, error) {
	return wechat.AuthorInfo{Name: "Fixture"}, nil
}
func (fakeDiscovery) ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	return wechat.ArticlePage{Items: []domain.Article{{Aid: "aid-a", Title: "Article"}}, Total: 1}, nil
}

func (manager *fakeJobs) Create(_ context.Context, spec jobs.Spec) (domain.Job, error) {
	manager.created = append(manager.created, spec)
	return domain.Job{ID: "job-1", Kind: spec.Kind, Profile: spec.Profile, State: domain.JobQueued, CreatedAt: manager.now}, nil
}
func (*fakeJobs) Get(context.Context, domain.JobID) (domain.Job, error) { return domain.Job{}, nil }
func (*fakeJobs) Query(context.Context, domain.JobQuery) (domain.Page[domain.Job], error) {
	return domain.Page[domain.Job]{Items: []domain.Job{}}, nil
}
func (*fakeJobs) Cancel(context.Context, domain.JobID) (domain.Job, error) { return domain.Job{}, nil }

func TestApplicationRuntimeStatusUsesInjectedDependencies(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	service := New(Options{
		Version: "test-version",
		Runtime: runtimeenv.Dependencies{
			Clock:    fixedClock{value: now},
			Paths:    domain.RuntimePaths{Config: "/config", Data: "/data", Cache: "/cache", State: "/state"},
			Profile:  "research",
			Portable: true,
		},
		Library: fakeLibrary{storage: domain.StorageStatus{DatabaseAvailable: true, ObjectStoreReady: true, Articles: 2}},
	})

	status, err := service.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != "test-version" || status.Profile != "research" || !status.Portable || !status.OfflineReady {
		t.Fatalf("RuntimeStatus() = %#v", status)
	}
	if status.CheckedAt != now || status.Paths.Data != "/data" || status.Storage.Articles != 2 {
		t.Fatalf("RuntimeStatus() did not use injected values: %#v", status)
	}
}

func TestApplicationQueriesAndJobsUseSharedSeams(t *testing.T) {
	accountPage := domain.Page[domain.Account]{Items: []domain.Account{{ID: "account-1", Name: "示例公众号"}}, Total: 1, Limit: 20}
	manager := &fakeJobs{}
	service := New(Options{
		Runtime: runtimeenv.Dependencies{Profile: "profile-a"},
		Library: fakeLibrary{accounts: accountPage},
		Jobs:    manager,
	})

	got, err := service.QueryAccounts(context.Background(), domain.AccountQuery{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, accountPage) {
		t.Fatalf("QueryAccounts() = %#v, want %#v", got, accountPage)
	}
	job, err := service.StartDownload(context.Background(), domain.DownloadRequest{URLs: []string{"https://mp.weixin.qq.com/s/example"}})
	if err != nil {
		t.Fatal(err)
	}
	if job.Kind != "article_download" || job.Profile != "profile-a" || len(manager.created) != 1 {
		t.Fatalf("StartDownload() = %#v, specs = %#v", job, manager.created)
	}
}

func TestApplicationDiscoveryMethodsUseSharedGateway(t *testing.T) {
	service := New(Options{WeChat: fakeDiscovery{}})
	accounts, err := service.SearchAccounts(context.Background(), domain.AccountQuery{Keyword: "fixture"})
	if err != nil || accounts.Total != 1 {
		t.Fatalf("SearchAccounts() = %#v, %v", accounts, err)
	}
	name, err := service.ResolveAccountName(context.Background(), "https://mp.weixin.qq.com/s/fixture")
	if err != nil || name != "Fixture" {
		t.Fatalf("ResolveAccountName() = %q, %v", name, err)
	}
	articles, err := service.ListArticles(context.Background(), wechat.ArticleListRequest{FakeID: "fixture-a"})
	if err != nil || articles.Total != 1 || articles.Items[0].Aid != "aid-a" {
		t.Fatalf("ListArticles() = %#v, %v", articles, err)
	}
	// The gateway is bound by runtime type assertion, so a gateway that stops
	// satisfying the interface degrades to "capability unavailable" instead of
	// failing to build. Every method needs its own reach-through assertion.
	albums, err := service.ResolveArticleAlbums(context.Background(), "https://mp.weixin.qq.com/s/fixture")
	if err != nil || len(albums.Albums) != 1 || albums.Albums[0].AlbumID != "album-a" {
		t.Fatalf("ResolveArticleAlbums() = %#v, %v", albums, err)
	}
}

func TestApplicationAccountOperationsUseSharedLibrary(t *testing.T) {
	account := domain.Account{ID: "account-a", FakeID: "fixture-a", Name: "Fixture"}
	service := New(Options{Library: fakeLibrary{
		saved: account, updated: account, byID: account, byFakeID: account,
		manifest: domain.AccountManifest{SchemaVersion: 1, Accounts: []domain.Account{account}},
		imported: domain.AccountImportReport{Added: 1},
		deleted:  domain.AccountDeleteReport{AccountsDeleted: 1},
	}})
	ctx := context.Background()
	if got, err := service.SaveAccount(ctx, account); err != nil || got.ID != account.ID {
		t.Fatalf("SaveAccount() = %#v, %v", got, err)
	}
	if got, err := service.UpdateAccount(ctx, account); err != nil || got.ID != account.ID {
		t.Fatalf("UpdateAccount() = %#v, %v", got, err)
	}
	if got, err := service.GetAccount(ctx, account.ID); err != nil || got.FakeID != account.FakeID {
		t.Fatalf("GetAccount() = %#v, %v", got, err)
	}
	if got, err := service.GetAccountByFakeID(ctx, account.FakeID); err != nil || got.ID != account.ID {
		t.Fatalf("GetAccountByFakeID() = %#v, %v", got, err)
	}
	if got, err := service.ExportAccounts(ctx, domain.AccountQuery{}); err != nil || len(got.Accounts) != 1 {
		t.Fatalf("ExportAccounts() = %#v, %v", got, err)
	}
	if got, err := service.ImportAccounts(ctx, domain.AccountManifest{SchemaVersion: 1}); err != nil || got.Added != 1 {
		t.Fatalf("ImportAccounts() = %#v, %v", got, err)
	}
	if got, err := service.DeleteAccounts(ctx, []domain.AccountID{account.ID}); err != nil || got.AccountsDeleted != 1 {
		t.Fatalf("DeleteAccounts() = %#v, %v", got, err)
	}
}
