package library

import (
	"context"
	"reflect"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestNormalizeArticleURLRemovesTrackingFragmentsAndCanonicalizesHost(t *testing.T) {
	got, err := NormalizeArticleURL(" https://weixin.qq.com/s/fixture/?utm_source=test&scene=123#wechat_redirect ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://mp.weixin.qq.com/s/fixture" {
		t.Fatalf("canonical URL = %q", got)
	}
	variant, err := NormalizeArticleURL("https://mp.weixin.qq.com/s/fixture?scene=1&from=timeline")
	if err != nil || variant != got {
		t.Fatalf("tracking variant = %q, %v; want %q", variant, err, got)
	}
	for _, raw := range []string{"http://mp.weixin.qq.com/s/a", "https://example.com/s/a", "https://mp.weixin.qq.com/not-an-article"} {
		if _, err := NormalizeArticleURL(raw); err == nil {
			t.Fatalf("NormalizeArticleURL(%q) error = nil", raw)
		}
	}
}

func TestRepairSingleArticleMergesCollidingContentCommentsRepliesAndCheckpoint(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	provisional, err := database.SaveProvisionalArticle(ctx, SingleArticleInput{
		URL: "https://mp.weixin.qq.com/s/provisional?scene=123", Title: "Provisional",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "real-fakeid", Name: "Real"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(ctx, ArticleRecord{ID: "stable", AccountID: account.ID, Aid: "real-aid",
		Title: "Stable", CanonicalURL: "https://mp.weixin.qq.com/s/stable", ContentStatus: "available"}); err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at)
VALUES('stable-shared', 'stable', 'digest-shared', 'html', 10)`, nil},
		{`INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at)
VALUES('provisional-shared', ?, 'digest-shared', 'html', 20)`, []any{provisional.ID}},
		{`INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at)
VALUES('provisional-unique', ?, 'digest-unique', 'markdown', 30)`, []any{provisional.ID}},
		{`INSERT INTO metric_snapshots(id, article_id, read_count, captured_at)
VALUES('metric-stable', 'stable', 10, 10)`, nil},
		{`INSERT INTO metric_snapshots(id, article_id, read_count, captured_at)
VALUES('metric-provisional', ?, 20, 20)`, []any{provisional.ID}},
		{`INSERT INTO comments(id, article_id, upstream_id, content, fetched_at)
VALUES('comment-stable', 'stable', 'comment-shared', 'stable', 10)`, nil},
		{`INSERT INTO comments(id, article_id, upstream_id, content, fetched_at)
VALUES('comment-provisional', ?, 'comment-shared', 'provisional', 20)`, []any{provisional.ID}},
		{`INSERT INTO comments(id, article_id, upstream_id, parent_id, content, fetched_at)
VALUES('comment-child', ?, 'comment-child', 'comment-provisional', 'child', 30)`, []any{provisional.ID}},
		{`INSERT INTO replies(id, comment_id, upstream_id, content, fetched_at)
VALUES('reply-stable', 'comment-stable', 'reply-shared', 'stable', 10)`, nil},
		{`INSERT INTO replies(id, comment_id, upstream_id, content, fetched_at)
VALUES('reply-provisional-shared', 'comment-provisional', 'reply-shared', 'duplicate', 20)`, nil},
		{`INSERT INTO replies(id, comment_id, upstream_id, content, fetched_at)
VALUES('reply-provisional-unique', 'comment-provisional', 'reply-unique', 'unique', 30)`, nil},
		{`INSERT INTO comment_checkpoints(article_id, continuation, complete, updated_at)
VALUES('stable', '', 0, 10)`, nil},
		{`INSERT INTO comment_checkpoints(article_id, continuation, complete, updated_at)
VALUES(?, 'next-page', 1, 20)`, []any{provisional.ID}},
	}
	for _, statement := range statements {
		if _, err := database.db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.RepairSingleArticle(ctx, SingleArticleRepair{
		ArticleID: provisional.ID, RealFakeID: "real-fakeid", Aid: "real-aid",
	}); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{
		"content_versions": 2,
		"metric_snapshots": 2,
		"comments":         2,
	} {
		var count int
		if err := database.db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE article_id='stable'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("%s count = %d, want %d", table, count, want)
		}
	}
	var parentID string
	if err := database.db.QueryRow("SELECT parent_id FROM comments WHERE id='comment-child'").Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if parentID != "comment-stable" {
		t.Fatalf("child parent = %q", parentID)
	}
	rows, err := database.db.Query(`SELECT upstream_id FROM replies WHERE comment_id='comment-stable' ORDER BY upstream_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var replies []string
	for rows.Next() {
		var upstreamID string
		if err := rows.Scan(&upstreamID); err != nil {
			t.Fatal(err)
		}
		replies = append(replies, upstreamID)
	}
	if !reflect.DeepEqual(replies, []string{"reply-shared", "reply-unique"}) {
		t.Fatalf("replies = %#v", replies)
	}
	var continuation string
	var complete int
	if err := database.db.QueryRow(`SELECT continuation, complete FROM comment_checkpoints WHERE article_id='stable'`).Scan(
		&continuation, &complete); err != nil {
		t.Fatal(err)
	}
	if continuation != "next-page" || complete != 1 {
		t.Fatalf("checkpoint = %q, %d", continuation, complete)
	}
}

func TestProvisionalSingleArticleDeduplicatesAndRepairsRealFakeID(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	first, err := database.SaveProvisionalArticle(ctx, SingleArticleInput{
		URL: "https://weixin.qq.com/s/fixture?utm_source=test#wechat_redirect", Title: "Fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.SaveProvisionalArticle(ctx, SingleArticleInput{
		URL: "https://mp.weixin.qq.com/s/fixture", Title: "Fixture updated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || !second.Single {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	repaired, err := database.RepairSingleArticle(ctx, SingleArticleRepair{
		ArticleID: first.ID, RealFakeID: "real-fakeid", AccountName: "Real Account", Aid: "real-aid",
		Title: "Canonical title", Author: "Alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.GetAccountByFakeID(ctx, "real-fakeid")
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ID != first.ID || repaired.AccountID != account.ID || repaired.Aid != "real-aid" ||
		repaired.Title != "Canonical title" || !repaired.Single {
		t.Fatalf("repaired = %#v account=%#v", repaired, account)
	}
}

func TestGetArticleResolvesStableIDWithinProfile(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	article, err := database.SaveProvisionalArticle(context.Background(), SingleArticleInput{
		URL: "https://mp.weixin.qq.com/s/stable-id", Title: "Stable ID",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := database.GetArticle(context.Background(), article.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ID != article.ID || resolved.CanonicalURL != article.CanonicalURL {
		t.Fatalf("GetArticle() = %#v, want %#v", resolved, article)
	}
	if _, err := database.GetArticle(context.Background(), ""); err == nil {
		t.Fatal("GetArticle() accepted an empty stable ID")
	}
}

func TestRepairSingleArticleMergesIntoExistingStableIdentityWithoutDuplicate(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	provisional, err := database.SaveProvisionalArticle(ctx, SingleArticleInput{URL: "https://mp.weixin.qq.com/s/provisional", Title: "Provisional"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.SaveAccount(ctx, domain.Account{FakeID: "real-fakeid", Name: "Real"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(ctx, ArticleRecord{ID: "stable", AccountID: account.ID, Aid: "real-aid",
		Title: "Stable", CanonicalURL: "https://mp.weixin.qq.com/s/stable", ContentStatus: "available"}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`INSERT INTO metric_snapshots(id, article_id, read_count, captured_at)
VALUES('metric-provisional', ?, 42, 1)`, provisional.ID); err != nil {
		t.Fatal(err)
	}
	repaired, err := database.RepairSingleArticle(ctx, SingleArticleRepair{
		ArticleID: provisional.ID, RealFakeID: "real-fakeid", Aid: "real-aid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ID != "stable" || repaired.ReadCount != 42 || !repaired.HasContent || !repaired.Single {
		t.Fatalf("repaired = %#v", repaired)
	}
	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM articles WHERE profile_id='profile-a'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("article count = %d", count)
	}
}
