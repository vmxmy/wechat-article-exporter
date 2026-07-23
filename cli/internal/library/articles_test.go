package library

import (
	"context"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestSaveArticlePageCommitsNormalizedPageAtomically(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	fetchedAt := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	err = database.SaveArticlePage(ctx, ArticlePageCommit{
		AccountFakeID: "fixture-a", UpstreamTotal: 3, NextOffset: 1, FetchedAt: fetchedAt,
		Articles: []domain.Article{{
			ID: "article-a", Aid: "aid-a", AppMsgID: 1001, ItemIndex: 1, Title: "Article",
			CanonicalURL: "https://mp.weixin.qq.com/s/a", PublishedAt: fetchedAt.Add(-time.Hour),
			UpdatedAt: fetchedAt, MessageType: 5, Paid: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.QueryArticles(ctx, domain.ArticleQuery{AccountID: account.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Aid != "aid-a" || page.Items[0].AppMsgID != 1001 ||
		page.Items[0].MessageType != 5 || !page.Items[0].Paid || page.Items[0].UpdatedAt.IsZero() {
		t.Fatalf("page = %#v", page)
	}
	var cursor string
	var total int
	if err := database.db.QueryRow("SELECT sync_cursor, upstream_total FROM accounts WHERE id=?", account.ID).Scan(&cursor, &total); err != nil {
		t.Fatal(err)
	}
	if cursor != "1" || total != 3 {
		t.Fatalf("cursor=%q total=%d", cursor, total)
	}
}

func TestSaveArticlePageRejectsMalformedItemWithoutPartialWrite(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	if _, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"}); err != nil {
		t.Fatal(err)
	}
	err := database.SaveArticlePage(ctx, ArticlePageCommit{AccountFakeID: "fixture-a", Articles: []domain.Article{
		{ID: "article-valid", Aid: "aid-valid", Title: "Valid", CanonicalURL: "https://mp.weixin.qq.com/s/valid"},
		{ID: "article-invalid", Aid: "", Title: "Invalid", CanonicalURL: "https://mp.weixin.qq.com/s/invalid"},
	}})
	if err == nil {
		t.Fatal("SaveArticlePage() error = nil")
	}
	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM articles WHERE profile_id='profile-a'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("partial article writes = %d", count)
	}
}

func TestSaveArticlePageLinksAlbumsToExistingCanonicalArticle(t *testing.T) {
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
	if err := database.SaveArticlePage(ctx, ArticlePageCommit{AccountFakeID: account.FakeID, Articles: []domain.Article{{
		ID: "article-discovered", Aid: "aid-a", Title: "Discovered", CanonicalURL: canonicalURL,
		Albums: []domain.Album{{ID: "album-a", UpstreamID: "upstream-a", Name: "Album"}},
	}}}); err != nil {
		t.Fatalf("SaveArticlePage() error = %v", err)
	}
	var articleID domain.ArticleID
	if err := database.db.QueryRow(`SELECT article_id FROM article_albums WHERE album_id='album-a'`).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	if articleID != provisional.ID {
		t.Fatalf("album article ID = %q, want existing canonical article %q", articleID, provisional.ID)
	}
}

func TestSaveArticlePageTracksPaginationCompletionAndAllowsTotalCorrection(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []ArticlePageCommit{
		{
			AccountFakeID: "fixture-a", UpstreamTotal: 3, NextOffset: 1, MessageCount: 1,
			Articles: []domain.Article{{
				ID: "article-a", Aid: "aid-a", Title: "Article A", CanonicalURL: "https://mp.weixin.qq.com/s/a",
			}},
		},
		{
			AccountFakeID: "fixture-a", UpstreamTotal: 3, NextOffset: 2, MessageCount: 2, Completed: true,
			Articles: []domain.Article{{
				ID: "article-b", Aid: "aid-b", Title: "Article B", CanonicalURL: "https://mp.weixin.qq.com/s/b",
			}},
		},
	} {
		if err := database.SaveArticlePage(ctx, page); err != nil {
			t.Fatal(err)
		}
	}
	var cursor string
	var total, messages, articles, completed int
	if err := database.db.QueryRow(`SELECT sync_cursor, upstream_total, message_count, article_count, completed
FROM accounts WHERE id=?`, account.ID).Scan(&cursor, &total, &messages, &articles, &completed); err != nil {
		t.Fatal(err)
	}
	if cursor != "2" || total != 3 || messages != 2 || articles != 2 || completed != 1 {
		t.Fatalf("cursor=%q total=%d messages=%d articles=%d completed=%d", cursor, total, messages, articles, completed)
	}

	if err := database.SaveArticlePage(ctx, ArticlePageCommit{
		AccountFakeID: "fixture-a", UpstreamTotal: 1, NextOffset: 0, MessageCount: 0, Completed: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow(`SELECT sync_cursor, upstream_total, message_count, completed
FROM accounts WHERE id=?`, account.ID).Scan(&cursor, &total, &messages, &completed); err != nil {
		t.Fatal(err)
	}
	if cursor != "0" || total != 1 || messages != 0 || completed != 1 {
		t.Fatalf("corrected cursor=%q total=%d messages=%d completed=%d", cursor, total, messages, completed)
	}
}

func TestSaveArticlePageDeduplicatesRepeatedPagesAndPersistsFinalSyncTime(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "fixture-a", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	firstSync := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	finalSync := firstSync.Add(5 * time.Minute)
	article := domain.Article{ID: "article-a", Aid: "aid-a", Title: "Article",
		CanonicalURL: "https://mp.weixin.qq.com/s/a", PublishedAt: firstSync.Add(-time.Hour)}
	if err := database.SaveArticlePage(ctx, ArticlePageCommit{
		AccountFakeID: "fixture-a", Articles: []domain.Article{article}, UpstreamTotal: 1,
		NextOffset: 1, MessageCount: 1, FetchedAt: firstSync,
	}); err != nil {
		t.Fatal(err)
	}
	article.ID = "duplicate-upstream-id"
	article.Title = "Updated title"
	if err := database.SaveArticlePage(ctx, ArticlePageCommit{
		AccountFakeID: "fixture-a", Articles: []domain.Article{article}, UpstreamTotal: 1,
		NextOffset: 1, MessageCount: 1, Completed: true, FetchedAt: finalSync,
	}); err != nil {
		t.Fatal(err)
	}
	page, err := database.QueryArticles(ctx, domain.ArticleQuery{AccountID: account.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	state, err := database.GetAccountSyncState(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "article-a" || page.Items[0].Title != "Updated title" {
		t.Fatalf("deduplicated page = %#v", page)
	}
	if !state.Completed || state.Account.ArticleCount != 1 || !state.Account.LastSyncAt.Equal(finalSync) {
		t.Fatalf("final sync state = %#v", state)
	}
}
