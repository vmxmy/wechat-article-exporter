package library

import (
	"context"
	"reflect"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestSaveAlbumPageCommitsMetadataOrderDeduplicationAndCheckpoint(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fake-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.SaveAlbumPage(ctx, AlbumPageCommit{
		Album: domain.Album{AccountID: account.ID, UpstreamID: "album-a", Name: "Album", ArticleCount: 2},
		Articles: []AlbumArticleCommit{
			{Key: "10001:1", Ordinal: 2, Article: domain.Article{ID: "article-b", AccountID: account.ID,
				Aid: "aid-b", Title: "B", CanonicalURL: "https://mp.weixin.qq.com/s/b"}},
			{Key: "10002:1", Ordinal: 1, Article: domain.Article{ID: "article-a", AccountID: account.ID,
				Aid: "aid-a", Title: "A", CanonicalURL: "https://mp.weixin.qq.com/s/a"}},
			{Key: "10002:1", Ordinal: 1, Article: domain.Article{ID: "article-a", AccountID: account.ID,
				Aid: "aid-a", Title: "A", CanonicalURL: "https://mp.weixin.qq.com/s/a"}},
		},
		Checkpoint: domain.AlbumCheckpoint{BeginMessageID: "10002", BeginItemIndex: "1"},
		Completed:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 2 || !reflect.DeepEqual(result.DuplicateKeys, []string{"10002:1"}) ||
		result.Checkpoint.PagesCommitted != 1 || result.Checkpoint.ItemsCommitted != 2 ||
		!reflect.DeepEqual(result.Checkpoint.SeenKeys, []string{"10001:1", "10002:1"}) {
		t.Fatalf("result = %#v", result)
	}
	page, err := database.QueryArticles(ctx, domain.ArticleQuery{AlbumID: domain.AlbumID("album:" + localStableDigest("album-a")),
		Sorts: []domain.ArticleSort{{Field: "title", Direction: domain.SortAscending}}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items[0].Albums) != 1 || page.Items[0].Albums[0].Name != "Album" {
		t.Fatalf("page = %#v", page)
	}
	rows, err := database.db.Query(`SELECT a.title FROM article_albums aa JOIN articles a ON a.id=aa.article_id
ORDER BY aa.ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ordered []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		ordered = append(ordered, title)
	}
	if !reflect.DeepEqual(ordered, []string{"A", "B"}) {
		t.Fatalf("ordered = %#v", ordered)
	}
}
