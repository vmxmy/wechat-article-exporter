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

func TestSaveAlbumPageLinksExistingCanonicalArticle(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	canonicalURL := "https://mp.weixin.qq.com/s/single"
	provisional, err := database.SaveProvisionalArticle(ctx, SingleArticleInput{URL: canonicalURL, Title: "Single"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.SaveAlbumPage(ctx, AlbumPageCommit{
		Album: domain.Album{ID: "album-a", AccountID: account.ID, UpstreamID: "upstream-a", Name: "Album"},
		Articles: []AlbumArticleCommit{{Article: domain.Article{
			ID: "article-discovered", AccountID: account.ID, Aid: "aid-a", Title: "Discovered", CanonicalURL: canonicalURL,
		}}},
	})
	if err != nil {
		t.Fatalf("SaveAlbumPage() error = %v", err)
	}
	var articleID domain.ArticleID
	if err := database.db.QueryRow(`SELECT article_id FROM article_albums WHERE album_id='album-a'`).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	if articleID != provisional.ID {
		t.Fatalf("album article ID = %q, want existing canonical article %q", articleID, provisional.ID)
	}
}

func TestQueryArticleIDsPreservesAlbumOrdinalOrder(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for _, album := range []AlbumPageCommit{
		{Album: domain.Album{ID: "album-a", AccountID: account.ID, UpstreamID: "upstream-a", Name: "A"}, Articles: []AlbumArticleCommit{
			{Ordinal: 2, Article: domain.Article{ID: "article-b", AccountID: account.ID, Aid: "aid-b", Title: "B", CanonicalURL: "https://mp.weixin.qq.com/s/b"}},
			{Ordinal: 1, Article: domain.Article{ID: "article-a", AccountID: account.ID, Aid: "aid-a", Title: "A", CanonicalURL: "https://mp.weixin.qq.com/s/a"}},
		}},
		{Album: domain.Album{ID: "album-b", AccountID: account.ID, UpstreamID: "upstream-b", Name: "B"}, Articles: []AlbumArticleCommit{
			{Ordinal: 1, Article: domain.Article{ID: "article-c", AccountID: account.ID, Aid: "aid-c", Title: "C", CanonicalURL: "https://mp.weixin.qq.com/s/c"}},
			{Ordinal: 2, Article: domain.Article{ID: "article-a", AccountID: account.ID, Aid: "aid-a", Title: "A", CanonicalURL: "https://mp.weixin.qq.com/s/a"}},
		}},
	} {
		if _, err := database.SaveAlbumPage(ctx, album); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		album domain.AlbumID
		want  []domain.ArticleID
	}{
		{album: "album-a", want: []domain.ArticleID{"article-a", "article-b"}},
		{album: "album-b", want: []domain.ArticleID{"article-c", "article-a"}},
	} {
		ids, err := database.QueryArticleIDs(ctx, domain.ArticleQuery{AlbumID: test.album})
		if err != nil || !reflect.DeepEqual(ids, test.want) {
			t.Fatalf("QueryArticleIDs(%q) = %#v, %v; want %#v", test.album, ids, err, test.want)
		}
	}
}
