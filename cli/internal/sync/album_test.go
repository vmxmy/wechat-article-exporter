package sync

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestAlbumRunnerCommitsPagesAndResumesFromCheckpoint(t *testing.T) {
	source := &fakeAlbumSource{pages: map[string]wechat.AlbumPage{
		"10002": {
			Album: domain.Album{ID: "album-a", UpstreamID: "album-a", Name: "Album"},
			Items: []wechat.AlbumArticle{
				{Key: "10002:1", Article: domain.Article{ID: "duplicate", Aid: "duplicate", Title: "Duplicate", CanonicalURL: "https://mp.weixin.qq.com/s/duplicate"}},
				{Key: "10003:1", Article: domain.Article{ID: "article-c", Aid: "aid-c", Title: "C", CanonicalURL: "https://mp.weixin.qq.com/s/c"}},
			}, Completed: true,
		},
	}}
	store := &fakeAlbumStore{}
	runner, err := NewAlbumRunner(source, store, AlbumRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	initial := domain.AlbumCheckpoint{BeginMessageID: "10002", BeginItemIndex: "1", SeenKeys: []string{"10002:1"},
		PagesCommitted: 1, ItemsCommitted: 2}
	encoded, _ := json.Marshal(initial)
	var saved []domain.AlbumCheckpoint
	result, err := runner.Run(context.Background(), AlbumSyncRequest{FakeID: "fake-a", AlbumID: "album-a"}, encoded,
		func(value any) error {
			saved = append(saved, value.(domain.AlbumCheckpoint))
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(source.begins, []string{"10002"}) || !result.Completed || result.PagesCommitted != 2 ||
		result.ItemsCommitted != 3 || len(store.commits) != 1 || len(saved) != 1 ||
		!reflect.DeepEqual(result.Checkpoint.SeenKeys, []string{"10002:1", "10003:1"}) {
		t.Fatalf("source=%#v result=%#v commits=%#v saved=%#v", source.begins, result, store.commits, saved)
	}
	if got := store.commits[0].Articles[1].Ordinal; got != 2 {
		t.Fatalf("resumed fresh article ordinal = %d, want 2", got)
	}
}

func TestAlbumRunnerPersistsStableGlobalOrderAcrossPages(t *testing.T) {
	database, err := library.Open(context.Background(), library.OpenOptions{
		Path: t.TempDir() + "/library.sqlite3", ProfileID: "profile-a", ProfileName: "Fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	account, err := database.SaveAccount(context.Background(), domain.Account{FakeID: "fake-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	album := domain.Album{ID: "album-a", AccountID: account.ID, UpstreamID: "album-a", Name: "Album", ArticleCount: 3}
	article := func(id string) domain.Article {
		return domain.Article{ID: domain.ArticleID("article-" + id), AccountID: account.ID, Aid: "aid-" + id,
			Title: id, CanonicalURL: "https://mp.weixin.qq.com/s/" + id}
	}
	source := &fakeAlbumSource{pages: map[string]wechat.AlbumPage{
		"": {
			Album: album,
			Items: []wechat.AlbumArticle{
				{Key: "10001:1", Article: article("A")},
				{Key: "10002:1", Article: article("B")},
			},
			Next: domain.AlbumCheckpoint{BeginMessageID: "10002", BeginItemIndex: "1"},
		},
		"10002": {
			Album: album,
			Items: []wechat.AlbumArticle{
				{Key: "10002:1", Article: article("B")},
				{Key: "10003:1", Article: article("C")},
			},
			Completed: true,
		},
	}}
	runner, err := NewAlbumRunner(source, database, AlbumRunnerOptions{
		Now: func() time.Time { return time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), AlbumSyncRequest{FakeID: "fake-a", AlbumID: "album-a"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || result.PagesCommitted != 2 || result.ItemsCommitted != 3 {
		t.Fatalf("result = %#v", result)
	}
	page, err := database.QueryArticles(context.Background(), domain.ArticleQuery{
		AlbumID: "album-a", Sorts: []domain.ArticleSort{{Field: "title", Direction: domain.SortAscending}},
	})
	if err != nil || page.Total != 3 {
		t.Fatalf("page=%#v error=%v", page, err)
	}
}

func TestAlbumRunnerClassifiesAuthenticationWithoutAdvancingCheckpoint(t *testing.T) {
	source := &fakeAlbumSource{err: wechat.ErrDiscoveryAuthentication}
	store := &fakeAlbumStore{}
	runner, err := NewAlbumRunner(source, store, AlbumRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(context.Background(), AlbumSyncRequest{FakeID: "fake-a", AlbumID: "album-a"}, nil, nil)
	var classified *jobs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != jobs.FailureAuthentication ||
		result.PagesCommitted != 0 || len(store.commits) != 0 {
		t.Fatalf("result=%#v error=%T %v", result, err, err)
	}
}

func TestAlbumRunnerRejectsMalformedCheckpointBeforeNetwork(t *testing.T) {
	source := &fakeAlbumSource{}
	store := &fakeAlbumStore{}
	runner, err := NewAlbumRunner(source, store, AlbumRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), AlbumSyncRequest{FakeID: "fake-a", AlbumID: "album-a"},
		json.RawMessage(`{bad`), nil)
	if err == nil || len(source.begins) != 0 || len(store.commits) != 0 {
		t.Fatalf("error=%v begins=%v commits=%v", err, source.begins, store.commits)
	}
}

type fakeAlbumSource struct {
	pages  map[string]wechat.AlbumPage
	err    error
	begins []string
}

func (source *fakeAlbumSource) ListAlbumArticles(_ context.Context, request wechat.AlbumListRequest) (wechat.AlbumPage, error) {
	source.begins = append(source.begins, request.BeginMessageID)
	if source.err != nil {
		return wechat.AlbumPage{}, source.err
	}
	return source.pages[request.BeginMessageID], nil
}

type fakeAlbumStore struct {
	commits []library.AlbumPageCommit
}

func (store *fakeAlbumStore) SaveAlbumPage(_ context.Context, commit library.AlbumPageCommit) (library.AlbumCommitResult, error) {
	store.commits = append(store.commits, commit)
	seen := make(map[string]struct{}, len(commit.Checkpoint.SeenKeys))
	for _, key := range commit.Checkpoint.SeenKeys {
		seen[key] = struct{}{}
	}
	stored := 0
	for _, article := range commit.Articles {
		if _, duplicate := seen[article.Key]; duplicate {
			continue
		}
		seen[article.Key] = struct{}{}
		stored++
	}
	checkpoint := commit.Checkpoint
	checkpoint.PagesCommitted++
	checkpoint.ItemsCommitted += stored
	checkpoint.SeenKeys = sortedTestKeys(seen)
	return library.AlbumCommitResult{Stored: stored, Checkpoint: checkpoint, Completed: commit.Completed}, nil
}

func sortedTestKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	if len(result) == 2 && result[0] > result[1] {
		result[0], result[1] = result[1], result[0]
	}
	return result
}
