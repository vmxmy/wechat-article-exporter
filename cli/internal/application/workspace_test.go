package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type workspaceLibrary struct {
	accounts        domain.Page[domain.Account]
	articles        domain.Page[domain.Article]
	albums          domain.Page[domain.Album]
	storage         domain.StorageStatus
	saved           []domain.SavedArticleQuery
	accountQuery    domain.AccountQuery
	articleQuery    domain.ArticleQuery
	albumQuery      domain.AlbumQuery
	accountsError   error
	availability    library.ArticleResourceAvailability
	availabilityErr error
	metrics         library.ArticleMetrics
	metricsErr      error
	resourceDetails domain.Page[library.ArticleResourceDetail]
	resourceErr     error
	comments        domain.Page[library.CommentRecord]
	replies         domain.Page[library.ReplyRecord]
	pendingReplies  []library.ReplyThread
	commentsID      domain.ArticleID
	commentsOffset  int
	commentsLimit   int
	repliesID       domain.ArticleID
	repliesComment  string
	repliesOffset   int
	repliesLimit    int
}

func (library *workspaceLibrary) GetArticle(_ context.Context, id domain.ArticleID) (domain.Article, error) {
	for _, article := range library.articles.Items {
		if article.ID == id {
			return article, nil
		}
	}
	return domain.Article{}, errors.New("article missing")
}

func (repository *workspaceLibrary) ArticleResourceAvailability(_ context.Context, id domain.ArticleID) (library.ArticleResourceAvailability, error) {
	availability := repository.availability
	availability.ArticleID = id
	return availability, repository.availabilityErr
}

func (repository *workspaceLibrary) LatestArticleMetrics(context.Context, domain.ArticleID) (library.ArticleMetrics, error) {
	return repository.metrics, repository.metricsErr
}

func (repository *workspaceLibrary) ListArticleResourceDetails(_ context.Context, _ domain.ArticleID, offset, limit int) (domain.Page[library.ArticleResourceDetail], error) {
	page := repository.resourceDetails
	page.Offset, page.Limit = offset, limit
	return page, repository.resourceErr
}

func (repository *workspaceLibrary) ListCommentsForArticle(_ context.Context, id domain.ArticleID, offset, limit int) (domain.Page[library.CommentRecord], error) {
	repository.commentsID, repository.commentsOffset, repository.commentsLimit = id, offset, limit
	page := repository.comments
	page.Offset, page.Limit = offset, limit
	return page, nil
}

func (repository *workspaceLibrary) ListRepliesForComment(_ context.Context, id domain.ArticleID, commentID string, offset, limit int) (domain.Page[library.ReplyRecord], error) {
	repository.repliesID, repository.repliesComment, repository.repliesOffset, repository.repliesLimit = id, commentID, offset, limit
	page := repository.replies
	page.Offset, page.Limit = offset, limit
	return page, nil
}

func (repository *workspaceLibrary) PendingReplyThreads(context.Context, domain.ArticleID) ([]library.ReplyThread, error) {
	return append([]library.ReplyThread(nil), repository.pendingReplies...), nil
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
	job   domain.Job
	items []jobs.Item
	logs  []library.JobLog
	lease library.JobLease
}

func (*workspaceJobManager) Create(context.Context, jobs.Spec) (domain.Job, error) {
	return domain.Job{}, nil
}
func (manager *workspaceJobManager) Get(context.Context, domain.JobID) (domain.Job, error) {
	return manager.job, nil
}
func (manager *workspaceJobManager) Query(_ context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	manager.query = query
	return manager.page, nil
}
func (*workspaceJobManager) Cancel(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (manager *workspaceJobManager) ListItems(context.Context, domain.JobID) ([]jobs.Item, error) {
	return append([]jobs.Item(nil), manager.items...), nil
}
func (manager *workspaceJobManager) ListLogsBounded(context.Context, domain.JobID, library.JobLogBudget) ([]library.JobLog, error) {
	return append([]library.JobLog(nil), manager.logs...), nil
}
func (manager *workspaceJobManager) Lease(context.Context, domain.JobID) (library.JobLease, error) {
	return manager.lease, nil
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

func TestWorkspaceArticleCommentsAreBoundedSafeAndMarkPendingReplies(t *testing.T) {
	library := &workspaceLibrary{
		comments:       domain.Page[library.CommentRecord]{Items: []library.CommentRecord{{ID: "database-id", UpstreamID: "comment-1", AuthorName: "Reader", Content: "Stored", LikeCount: 2, ReplyTotal: 1, RawObjectDigest: "not-for-browser"}}, Total: 3},
		replies:        domain.Page[library.ReplyRecord]{Items: []library.ReplyRecord{{ID: "database-reply", UpstreamID: "reply-1", AuthorName: "Author", Content: "Stored reply", LikeCount: 1, RawObjectDigest: "not-for-browser"}}, Total: 1},
		pendingReplies: []library.ReplyThread{{ContentID: "comment-1", LastError: "private upstream error"}},
	}
	workspace := NewWorkspace(New(Options{Runtime: runtimeenv.Dependencies{Profile: "fixture"}, Library: library, Jobs: &workspaceJobManager{}}))
	comments, err := workspace.ArticleComments(context.Background(), "article-1", WorkspacePageRequest{Offset: 1, Limit: 2})
	if err != nil || comments.ArticleID != "article-1" || comments.PendingReplies != 1 || comments.Comments.Total != 3 || len(comments.Comments.Items) != 1 || comments.Comments.Items[0] != (WorkspaceArticleComment{ID: "comment-1", AuthorName: "Reader", Content: "Stored", LikeCount: 2, ReplyCount: 1, ReplyStatus: "pending"}) {
		t.Fatalf("comments=%#v err=%v", comments, err)
	}
	if library.commentsID != "article-1" || library.commentsOffset != 1 || library.commentsLimit != 2 {
		t.Fatalf("comments query = article=%q offset=%d limit=%d", library.commentsID, library.commentsOffset, library.commentsLimit)
	}
	replies, err := workspace.ArticleCommentReplies(context.Background(), "article-1", "comment-1", WorkspacePageRequest{Limit: 1})
	if err != nil || replies.Total != 1 || len(replies.Items) != 1 || replies.Items[0] != (WorkspaceArticleReply{ID: "reply-1", AuthorName: "Author", Content: "Stored reply", LikeCount: 1}) {
		t.Fatalf("replies=%#v err=%v", replies, err)
	}
	if library.repliesID != "article-1" || library.repliesComment != "comment-1" || library.repliesLimit != 1 {
		t.Fatalf("replies query = article=%q comment=%q limit=%d", library.repliesID, library.repliesComment, library.repliesLimit)
	}
	for _, input := range []struct{ articleID, commentID string }{{"article one", "comment-1"}, {"article-1", "comment one"}} {
		if _, err := workspace.ArticleCommentReplies(context.Background(), domain.ArticleID(input.articleID), input.commentID, WorkspacePageRequest{}); err == nil {
			t.Fatalf("expected invalid identifiers for %#v", input)
		}
	}
}

func TestWorkspaceArticleResourcesReturnsSafeCompletenessAggregate(t *testing.T) {
	service := New(Options{Library: &workspaceLibrary{availability: library.ArticleResourceAvailability{Total: 3, Available: 2}}})
	workspace := NewWorkspace(service)

	resources, err := workspace.ArticleResources(context.Background(), " article-1 ")
	if err != nil || resources != (WorkspaceArticleResources{ArticleID: "article-1", Total: 3, Available: 2, Missing: 1}) {
		t.Fatalf("ArticleResources() = %#v, %v", resources, err)
	}

	completeService := New(Options{Library: &workspaceLibrary{availability: library.ArticleResourceAvailability{Total: 2, Available: 2}}})
	complete, err := NewWorkspace(completeService).ArticleResources(context.Background(), "article-1")
	if err != nil || !complete.Complete {
		t.Fatalf("complete ArticleResources() = %#v, %v", complete, err)
	}

	emptyService := New(Options{Library: &workspaceLibrary{availability: library.ArticleResourceAvailability{}}})
	empty, err := NewWorkspace(emptyService).ArticleResources(context.Background(), "article-1")
	if err != nil || empty.Complete || empty.Missing != 0 {
		t.Fatalf("empty ArticleResources() = %#v, %v", empty, err)
	}

	_, err = workspace.ArticleResources(context.Background(), " ")
	var workspaceErr *WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInvalidArgument {
		t.Fatalf("empty ArticleResources error = %v", err)
	}
}

func TestWorkspaceAccountSwitchingProjectsOnlySafeIdentityAndSessionFields(t *testing.T) {
	service := New(Options{Session: &workspaceSessionGateway{
		switchable: []wechat.SwitchableAccount{{ID: " account-1 ", Name: " Fixture ", AvatarURL: "https://safe.example/avatar", Alias: " fixture "}, {Name: "discarded"}},
		switched:   wechat.Session{State: wechat.SessionAuthenticated, AccountID: "account-1", AccountName: "Fixture", Token: "session-secret"},
	}})
	workspace := NewWorkspace(service)

	accounts, err := workspace.SwitchableAccounts(context.Background())
	if err != nil || !accounts.Available || len(accounts.Accounts) != 1 || accounts.Accounts[0] != (WorkspaceSwitchableAccount{ID: "account-1", Name: "Fixture", Alias: "fixture"}) {
		t.Fatalf("SwitchableAccounts() = %#v, %v", accounts, err)
	}
	encoded, err := json.Marshal(accounts)
	if err != nil || strings.Contains(string(encoded), "session-secret") || strings.Contains(string(encoded), "safe.example") {
		t.Fatalf("switchable account JSON = %s, %v", encoded, err)
	}

	session, err := workspace.SwitchAccount(context.Background(), "account-1")
	if err != nil || session.AccountID != "account-1" || session.AccountName != "Fixture" {
		t.Fatalf("SwitchAccount() = %#v, %v", session, err)
	}
	encoded, err = json.Marshal(session)
	if err != nil || strings.Contains(string(encoded), "session-secret") {
		t.Fatalf("switched session JSON = %s, %v", encoded, err)
	}
	if _, err = workspace.SwitchAccount(context.Background(), "../session-secret"); err == nil {
		t.Fatal("SwitchAccount accepted an unsafe identifier")
	}

	unavailable := NewWorkspace(New(Options{Session: &workspaceSessionGateway{switchableErr: wechat.ErrAccountSwitching}}))
	accounts, err = unavailable.SwitchableAccounts(context.Background())
	if err != nil || accounts.Available || len(accounts.Accounts) != 0 {
		t.Fatalf("unavailable SwitchableAccounts() = %#v, %v", accounts, err)
	}
}

func TestWorkspaceArticleDetailReturnsBoundedSafeMetricsAndResources(t *testing.T) {
	capturedAt := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	service := New(Options{Library: &workspaceLibrary{
		metrics:         library.ArticleMetrics{ReadCount: 12, OldLikeCount: 3, LikeCount: 4, ShareCount: 5, CommentCount: 6, CapturedAt: capturedAt},
		resourceDetails: domain.Page[library.ArticleResourceDetail]{Items: []library.ArticleResourceDetail{{Role: "image", Ordinal: 2, Available: true}, {Role: "audio", Ordinal: 0}}, Total: 3},
	}})
	detail, err := NewWorkspace(service).ArticleDetail(context.Background(), " article-1 ", WorkspacePageRequest{Offset: 1, Limit: 2})
	if err != nil || detail.ArticleID != "article-1" || !detail.Metrics.Available || detail.Metrics.ReadCount != 12 || !detail.Metrics.CapturedAt.Equal(capturedAt) || detail.Resources.Total != 3 || detail.Resources.Offset != 1 || detail.Resources.Limit != 2 || len(detail.Resources.Items) != 2 || detail.Resources.Items[0] != (WorkspaceArticleResourceDetail{Role: "image", Ordinal: 2, Available: true}) {
		t.Fatalf("ArticleDetail() = %#v, %v", detail, err)
	}

	withoutMetrics := NewWorkspace(New(Options{Library: &workspaceLibrary{resourceDetails: domain.Page[library.ArticleResourceDetail]{}, metricsErr: sql.ErrNoRows}}))
	detail, err = withoutMetrics.ArticleDetail(context.Background(), "article-1", WorkspacePageRequest{})
	if err != nil || detail.Metrics.Available || detail.Resources.Limit != WorkspaceDefaultPageLimit {
		t.Fatalf("detail without metrics = %#v, %v", detail, err)
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

func TestWorkspaceJobDetailsAreBoundedAndDoNotExposeSensitiveInternals(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	items := make([]jobs.Item, WorkspaceJobDetailMaximumItems+2)
	for index := range items {
		items[index] = jobs.Item{ID: "item-" + string(rune('a'+index%26)), Key: "/private/secret/item", State: domain.JobRunning, AttemptCount: 2, Checkpoint: []byte(`{"token":"secret"}`), ErrorClass: jobs.FailureNetwork, ErrorMessage: "cookie=secret", CreatedAt: now, UpdatedAt: now}
	}
	manager := &workspaceJobManager{
		job: domain.Job{ID: "job-1", Kind: "download", State: domain.JobRunning}, items: items,
		logs:  []library.JobLog{{ID: 1, ItemID: "item-a", Level: "error", Message: "failed at /private/profile with token=secret", Fields: map[string]any{"path": "/private/profile", "token": "secret"}, CreatedAt: now}},
		lease: library.JobLease{Owner: "executor-secret", Active: true, ExpiresAt: now.Add(time.Minute)},
	}
	workspace := NewWorkspace(New(Options{Jobs: manager, Runtime: runtimeenv.Dependencies{Clock: fixedClock{value: now}}}))

	detail, err := workspace.JobDetails(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Job.ID != "job-1" || len(detail.Job.PermittedActions) != 1 || detail.Job.PermittedActions[0] != WorkspaceJobActionCancel || len(detail.Items) != WorkspaceJobDetailMaximumItems || detail.ItemsTotal != WorkspaceJobDetailMaximumItems+2 || !detail.ItemsLimited || len(detail.Logs) != 1 || !detail.Lease.Active || detail.RefreshedAt != now {
		t.Fatalf("JobDetails() = %#v", detail)
	}
	if detail.Items[0].ID == "" || detail.Items[0].ErrorClass != string(jobs.FailureNetwork) {
		t.Fatalf("item detail = %#v", detail.Items[0])
	}
	encoded := string(mustJSON(t, detail))
	for _, forbidden := range []string{"/private", "secret", "executor-secret", "checkpoint", "item_key", "fields"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("job details leaked %q: %s", forbidden, encoded)
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
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

func TestWorkspaceArticleControlsUseSharedApplicationJobs(t *testing.T) {
	library := &workspaceLibrary{articles: domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-1", Title: "Fixture", HasContent: true}}}}
	application := &workspaceControlApplication{Service: New(Options{Library: library}), job: domain.Job{ID: "job-1", Kind: "article_download", State: domain.JobQueued}}
	workspace := NewWorkspace(application)

	preview, err := workspace.ArticlePreview(context.Background(), "article-1")
	if err != nil || preview.ArticleID != "article-1" || !preview.Available {
		t.Fatalf("ArticlePreview() = %#v, %v", preview, err)
	}
	job, err := workspace.StartDownload(context.Background(), domain.DownloadRequest{Kind: "metadata", ArticleIDs: []domain.ArticleID{"article-1"}})
	if err != nil || job.ID != "job-1" || application.download.Kind != "metadata" {
		t.Fatalf("StartDownload() = %#v, request=%#v, err=%v", job, application.download, err)
	}
	job, err = workspace.SynchronizeAlbum(context.Background(), WorkspaceAlbumTraversalRequest{AccountID: "account-1", AlbumID: "album-1", Order: wechat.AlbumReverse, Download: true})
	if err != nil || job.ID != "job-1" || !application.albumDownload || application.albumOrder != wechat.AlbumReverse {
		t.Fatalf("SynchronizeAlbum() = %#v, download=%t order=%q, err=%v", job, application.albumDownload, application.albumOrder, err)
	}
	_, err = workspace.StartDownload(context.Background(), domain.DownloadRequest{Kind: "metadata"})
	var workspaceErr *WorkspaceError
	if !errors.As(err, &workspaceErr) || workspaceErr.Code != WorkspaceErrorInvalidArgument {
		t.Fatalf("empty download error = %v", err)
	}
}

type workspaceControlApplication struct {
	*Service
	job           domain.Job
	download      domain.DownloadRequest
	albumDownload bool
	albumOrder    wechat.AlbumOrder
}

type workspaceSessionGateway struct {
	switchable    []wechat.SwitchableAccount
	switchableErr error
	switched      wechat.Session
}

func (*workspaceSessionGateway) BeginLogin(context.Context, string) (wechat.LoginFlow, error) {
	return wechat.LoginFlow{}, nil
}
func (*workspaceSessionGateway) PollLogin(context.Context) (wechat.PollResult, error) {
	return wechat.PollResult{}, nil
}
func (*workspaceSessionGateway) CompleteLogin(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (*workspaceSessionGateway) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (gateway *workspaceSessionGateway) ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error) {
	return append([]wechat.SwitchableAccount(nil), gateway.switchable...), gateway.switchableErr
}
func (gateway *workspaceSessionGateway) SwitchAccount(context.Context, string) (wechat.Session, error) {
	return gateway.switched, nil
}
func (*workspaceSessionGateway) Logout(context.Context) error { return nil }

func (application *workspaceControlApplication) GetArticle(ctx context.Context, id domain.ArticleID) (domain.Article, error) {
	return application.Service.library.(*workspaceLibrary).GetArticle(ctx, id)
}

func (application *workspaceControlApplication) StartDownload(_ context.Context, request domain.DownloadRequest) (domain.Job, error) {
	application.download = request
	return application.job, nil
}

func (application *workspaceControlApplication) SynchronizeAlbumWithOrder(_ context.Context, _ domain.AccountID, _ domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	application.albumOrder = order
	return application.job, nil
}

func (application *workspaceControlApplication) SynchronizeAlbumWithOrderAndDownload(_ context.Context, _ domain.AccountID, _ domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	application.albumDownload = true
	application.albumOrder = order
	return application.job, nil
}
