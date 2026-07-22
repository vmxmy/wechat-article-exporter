package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestQueryArticlesAppliesAllCompoundFiltersLocally(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fake-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	original := true
	content := true
	comments := true
	deleted := false
	paid := false
	readMin := 100
	readMax := 130
	oldLikeMin, oldLikeMax := 4, 6
	shareMin, shareMax := 6, 8
	likeMin, likeMax := 5, 7
	commentMin, commentMax := 7, 9
	weCoinMin, weCoinMax := 7, 9
	mediaMin, mediaMax := 80, 100
	if _, err := database.db.Exec(`INSERT INTO articles(
id, profile_id, account_id, aid, title, author, digest, canonical_url, published_at, message_type,
is_paid, is_original, wecoin_count, media_duration_seconds, content_status, created_at, updated_at)
VALUES
('match', 'profile-a', ?, 'aid-match', 'Agent notes', 'Alice', 'fixture digest',
 'https://mp.weixin.qq.com/s/match', ?, 5, 0, 1, 8, 90, 'available', ?, ?),
('other', 'profile-a', ?, 'aid-other', 'Other', 'Bob', 'other',
 'https://mp.weixin.qq.com/s/other', ?, 0, 1, 0, 0, 0, 'missing', ?, ?)`,
		account.ID, now.UnixMilli(), now.UnixMilli(), now.UnixMilli(), account.ID,
		now.Add(-48*time.Hour).UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO metric_snapshots(
id, article_id, read_count, old_like_count, like_count, share_count, comment_count, captured_at)
VALUES('metric-old', 'match', 10, 1, 1, 1, 1, ?), ('metric-latest', 'match', 120, 5, 6, 7, 8, ?)`,
		now.Add(-time.Hour).UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO comments(id, article_id, upstream_id, fetched_at)
VALUES('comment-a', 'match', 'upstream-a', ?)`, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO albums(
id, profile_id, account_id, upstream_id, title, created_at, updated_at)
VALUES('album-a', 'profile-a', ?, 'upstream-album', 'Agent Album', ?, ?);
INSERT INTO article_albums(article_id, album_id, ordinal) VALUES('match', 'album-a', 0)`,
		account.ID, now.UnixMilli(), now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	page, err := database.QueryArticles(ctx, domain.ArticleQuery{
		AccountID: account.ID, AlbumID: "album-a", Keyword: "agent", Author: "ali",
		PublishedFrom: now.Add(-time.Hour), PublishedTo: now.Add(time.Hour),
		Deleted: &deleted, Original: &original, Paid: &paid, HasContent: &content, HasComments: &comments,
		MessageTypes: []int{5}, ReadMin: &readMin, ReadMax: &readMax,
		OldLikeMin: &oldLikeMin, OldLikeMax: &oldLikeMax,
		ShareMin: &shareMin, ShareMax: &shareMax,
		LikeMin: &likeMin, LikeMax: &likeMax,
		CommentMin: &commentMin, CommentMax: &commentMax,
		WeCoinMin: &weCoinMin, WeCoinMax: &weCoinMax,
		MediaSecondsMin: &mediaMin, MediaSecondsMax: &mediaMax,
		Sorts: []domain.ArticleSort{
			{Field: "read", Direction: domain.SortDescending}, {Field: "title", Direction: domain.SortAscending},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "match" || page.Items[0].ReadCount != 120 ||
		!page.Items[0].Original || !page.Items[0].HasContent || !page.Items[0].HasComments || page.Items[0].WeCoinCount != 8 ||
		page.Items[0].MediaDurationSeconds != 90 || len(page.Items[0].Albums) != 1 || page.Items[0].Albums[0].ID != "album-a" {
		t.Fatalf("page = %#v", page)
	}
}

func TestQueryArticlesStableSortAndPaginationUseIDTieBreak(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC).UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Fixture", now)
	for _, id := range []string{"article-c", "article-a", "article-b"} {
		insertArticle(t, database.db, id, "profile-a", "account-a", "Same", "https://mp.weixin.qq.com/s/"+id, now, now)
	}
	first, err := database.QueryArticles(ctx, domain.ArticleQuery{
		Sorts: []domain.ArticleSort{{Field: "title", Direction: domain.SortAscending}}, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.QueryArticles(ctx, domain.ArticleQuery{
		Sorts: []domain.ArticleSort{{Field: "title", Direction: domain.SortAscending}}, Offset: 2, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || len(first.Items) != 2 || first.Items[0].ID != "article-a" ||
		first.Items[1].ID != "article-b" || len(second.Items) != 1 || second.Items[0].ID != "article-c" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestQueryArticlesRejectsUntrustedSortSQL(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	_, err := database.QueryArticles(context.Background(), domain.ArticleQuery{
		Sorts: []domain.ArticleSort{{Field: "title; DROP TABLE articles", Direction: domain.SortAscending}},
	})
	if !errors.Is(err, ErrInvalidArticleSort) {
		t.Fatalf("error = %v", err)
	}
}
