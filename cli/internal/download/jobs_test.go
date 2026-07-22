package download

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestPersistentArticleJobPersistsIntentBeforeNetworkAndCommits(t *testing.T) {
	database, objectStore := openPersistentDownloadDB(t)
	seedPersistentArticle(t, database, "article-a")
	store := library.NewJobStore(database)
	client := &countingClient{body: persistentValidArticle("Title")}
	service := JobService{
		Store: store,
		Engine: jobs.EngineOptions{Owner: "worker-a", MaxAttempts: 1,
			Scheduler: jobs.NewScheduler(jobs.Limits{Global: 1})},
		Articles: ArticleDownloader{Network: client, Processor: processor.New(), Objects: objectStore, Store: database},
	}
	job, err := service.Start(context.Background(), JobRequest{Kind: JobArticle, IdempotencyKey: "article-a",
		Articles: []ArticleRequest{{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Fatalf("network ran before durable job intent: %d calls", client.calls)
	}
	items, err := store.ListItems(context.Background(), job.ID)
	if err != nil || len(items) != 1 || items[0].State != domain.JobQueued {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	final, err := service.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted || client.calls != 1 {
		t.Fatalf("final=%#v calls=%d err=%v", final, client.calls, err)
	}
	content, err := database.CurrentContent(context.Background(), "article-a", "html")
	if err != nil || content.ObjectDigest == "" || content.Classification != "valid" {
		t.Fatalf("content=%#v err=%v", content, err)
	}
	if _, err := service.Run(context.Background(), job.ID); err == nil {
		t.Fatal("terminal job unexpectedly reran")
	}
	if client.calls != 1 {
		t.Fatalf("completed item fetched twice: %d", client.calls)
	}
}

func TestPersistentArticleJobClassifiesDeletedRiskAndProxyFailures(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		clientErr error
		class     jobs.FailureClass
		attempts  int
	}{
		{name: "deleted", body: `<html><body>该内容已被作者删除</body></html>`, class: jobs.FailureDeleted, attempts: 1},
		{name: "risk", body: `<html><body>当前环境异常，请完成验证后继续访问</body></html>`, class: jobs.FailureThrottling, attempts: 2},
		{name: "parse", body: `<html><body><div id="js_article"></div><script>window.cgiDataNew={invalid:</script></body></html>`,
			class: jobs.FailureParsing, attempts: 1},
		{name: "proxy", clientErr: errors.New("proxy timeout"), class: jobs.FailureNetwork, attempts: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, objectStore := openPersistentDownloadDB(t)
			seedPersistentArticle(t, database, "article-a")
			store := library.NewJobStore(database)
			client := &countingClient{body: test.body, err: test.clientErr}
			service := JobService{Store: store, Engine: jobs.EngineOptions{Owner: "worker-a", MaxAttempts: 2,
				Scheduler: jobs.NewScheduler(jobs.Limits{Global: 1}),
				Backoff:   jobs.Backoff{Base: time.Millisecond, Max: time.Millisecond}},
				Articles: ArticleDownloader{Network: client, Processor: processor.New(), Objects: objectStore, Store: database}}
			job, err := service.Start(context.Background(), JobRequest{Kind: JobArticle,
				Articles: []ArticleRequest{{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"}}})
			if err != nil {
				t.Fatal(err)
			}
			final, err := service.Run(context.Background(), job.ID)
			if err != nil || final.State != domain.JobFailed {
				t.Fatalf("final=%#v err=%v", final, err)
			}
			items, err := store.ListItems(context.Background(), job.ID)
			if err != nil || len(items) != 1 || items[0].ErrorClass != test.class || client.calls != test.attempts {
				t.Fatalf("items=%#v calls=%d err=%v", items, client.calls, err)
			}
		})
	}
}

func TestPersistentMetadataJobBlocksAuthenticationBeforeNetwork(t *testing.T) {
	database, _ := openPersistentDownloadDB(t)
	seedPersistentArticle(t, database, "article-a")
	store := library.NewJobStore(database)
	source := &fakeCredentialArticleSource{}
	service := JobService{Store: store, Engine: jobs.EngineOptions{Owner: "worker-a", MaxAttempts: 1},
		Metadata: MetadataDownloader{Credentials: &fixedCredentialLoader{err: credentials.ErrCredentialMissing},
			Source: source, Store: database}}
	job, err := service.Start(context.Background(), JobRequest{Kind: JobMetadata,
		Metadata: []MetadataRequest{{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/a"}}})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobBlockedAuth || source.calls != 0 {
		t.Fatalf("final=%#v calls=%d err=%v", final, source.calls, err)
	}
}

func TestPersistentCommentsJobStoresPartialResultInCheckpoint(t *testing.T) {
	database, _ := openPersistentDownloadDB(t)
	seedPersistentArticle(t, database, "article-a")
	store := library.NewJobStore(database)
	commentStore := newCommentMemoryStore()
	source := &fakeCommentSource{commentPages: []wechat.CommentPage{{Comments: []wechat.Comment{
		{ID: "comment-1", Content: "one", ReplyTotal: 1},
		{ID: "comment-2", Content: "two", ReplyTotal: 1},
	}}}, replyResults: map[string][]replySourceResult{
		"comment-1": {{page: wechat.ReplyPage{ContentID: "comment-1", MaxReplyID: 1,
			Replies: []wechat.Reply{{ID: "reply-1", Content: "done"}}}}},
		"comment-2": {{err: errors.New("temporary upstream failure")}},
	}}
	service := JobService{Store: store, Engine: jobs.EngineOptions{Owner: "worker-a", MaxAttempts: 1},
		Comments: CommentsDownloader{Credentials: &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a"},
			record: downloadCredential()}, Source: source, Store: commentStore, MaxRetries: 1}}
	job, err := service.Start(context.Background(), JobRequest{Kind: JobComments, Comments: []CommentsRequest{{
		ArticleID: "article-a", AccountID: "account-a", BusinessID: "biz-a", AppMessageID: 1,
		ItemIndex: 1, CommentID: "comment-stream",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobFailed {
		t.Fatalf("final=%#v err=%v", final, err)
	}
	if len(commentStore.comments) != 2 || commentStore.threads["comment-1"].Complete != true ||
		commentStore.threads["comment-2"].Complete {
		t.Fatalf("comments=%#v threads=%#v", commentStore.comments, commentStore.threads)
	}
	items, err := store.ListItems(context.Background(), job.ID)
	if err != nil || len(items) != 1 || !containsJSONField(items[0].Checkpoint, `"partial":true`) {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestPersistentPaidJobUsesSensitiveClassAndCommits(t *testing.T) {
	database, objectStore := openPersistentDownloadDB(t)
	seedPersistentArticle(t, database, "article-a")
	store := library.NewJobStore(database)
	source := &fakeCredentialArticleSource{response: wechat.ContentResponse{Body: []byte(persistentValidArticle("Paid")),
		MediaType: "text/html", Route: "trusted", RequestID: "paid-request"}}
	service := JobService{Store: store, Engine: jobs.EngineOptions{Owner: "worker-a", MaxAttempts: 1},
		Paid: PaidArticleDownloader{Fetcher: PaidContentDownloader{
			Credentials: &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a"}, record: downloadCredential()},
			Source:      source,
		}, Processor: processor.New(), Objects: objectStore, Store: database}}
	job, err := service.Start(context.Background(), JobRequest{Kind: JobPaid,
		Paid: []PaidContentJobRequest{{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/paid"}}})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted || source.request.Class != network.PaidContent {
		t.Fatalf("final=%#v request=%#v err=%v", final, source.request, err)
	}
	if _, err := database.CurrentContent(context.Background(), "article-a", "html"); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentJobRecoversStaleRunningItem(t *testing.T) {
	database, objectStore := openPersistentDownloadDB(t)
	seedPersistentArticle(t, database, "article-a")
	store := library.NewJobStore(database)
	service := JobService{Store: store, Engine: jobs.EngineOptions{Owner: "worker-b", MaxAttempts: 1},
		Articles: ArticleDownloader{Network: &countingClient{body: persistentValidArticle("Recovered")},
			Processor: processor.New(), Objects: objectStore, Store: database}}
	job, err := service.Start(context.Background(), JobRequest{Kind: JobArticle,
		Articles: []ArticleRequest{{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(context.Background(), job.ID, "dead-worker", 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	items, _ := store.ListItems(context.Background(), job.ID)
	if _, err := store.ClaimItem(context.Background(), job.ID, items[0].ID, "dead-worker"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if recovered, err := service.Recover(context.Background()); err != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	final, err := service.Run(context.Background(), job.ID)
	if err != nil || final.State != domain.JobCompleted {
		t.Fatalf("final=%#v err=%v", final, err)
	}
}

type persistentObjectStore struct{ store *objects.FileStore }

func (store persistentObjectStore) Put(ctx context.Context, source io.Reader, mediaType string) (objects.Object, error) {
	return store.store.Put(ctx, source, mediaType)
}
func (store persistentObjectStore) Validate(ctx context.Context, digest string) error {
	return store.store.Validate(ctx, digest)
}

func openPersistentDownloadDB(t *testing.T) (*library.Database, persistentObjectStore) {
	t.Helper()
	database, err := library.Open(context.Background(), library.OpenOptions{Path: filepath.Join(t.TempDir(), "library.sqlite"), ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	objectStore, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	return database, persistentObjectStore{store: objectStore}
}

func seedPersistentArticle(t *testing.T, database *library.Database, articleID domain.ArticleID) {
	t.Helper()
	if err := database.UpsertAccount(context.Background(), library.AccountRecord{ID: "account-a", FakeID: "fake-a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(context.Background(), library.ArticleRecord{ID: articleID, AccountID: "account-a",
		Aid: string(articleID), Title: string(articleID), CanonicalURL: "https://mp.weixin.qq.com/s/" + string(articleID),
		ContentStatus: "missing"}); err != nil {
		t.Fatal(err)
	}
}

func persistentValidArticle(title string) string {
	return `<html><body><div id="js_article"><div id="js_content">hello</div></div>` +
		`<script>window.cgiDataNew={title:'` + title + `',user_name:'gh_fixture',content_noencode:'hello',comment_id:'comment-a'}</script></body></html>`
}

func containsJSONField(value []byte, field string) bool {
	return len(value) > 0 && string(value) != "null" && containsString(string(value), field)
}

func containsString(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
