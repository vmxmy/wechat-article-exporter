package library

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

func TestCommitContentMakesNewVersionCurrentAndMarksArticleAvailable(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	first := objects.Object{Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 10, MediaType: "text/html"}
	second := objects.Object{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Size: 20, MediaType: "text/html"}
	if _, err := database.CommitContent(context.Background(), "article-a", first, "html", "https://mp.weixin.qq.com/s/a", "valid", "comment-a", time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	current, err := database.CommitContent(context.Background(), "article-a", second, "html", "https://mp.weixin.qq.com/s/a", "valid", "comment-b", time.Unix(20, 0))
	if err != nil {
		t.Fatal(err)
	}
	if current.ObjectDigest != second.Digest || current.CommentID != "comment-b" || !current.Current {
		t.Fatalf("current content = %#v", current)
	}
	read, err := database.CurrentContent(context.Background(), "article-a", "html")
	if err != nil || read.ObjectDigest != second.Digest {
		t.Fatalf("CurrentContent() = %#v, %v", read, err)
	}
	var currentCount, objectCount int
	var contentStatus string
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM content_versions WHERE article_id='article-a' AND is_current=1`).Scan(&currentCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM objects`).Scan(&objectCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow(`SELECT content_status FROM articles WHERE id='article-a'`).Scan(&contentStatus); err != nil {
		t.Fatal(err)
	}
	if currentCount != 1 || objectCount != 2 || contentStatus != "available" {
		t.Fatalf("current=%d objects=%d status=%q", currentCount, objectCount, contentStatus)
	}
}

func TestCommitResourceDeduplicatesBySourceAndTracksArticleMapping(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	object := objects.Object{Digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Size: 3, MediaType: "image/png"}
	first, err := database.CommitResource(context.Background(), "article-a", "https://mmbiz.qpic.cn/a.png", "image", 0, object)
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.CommitResource(context.Background(), "article-a", "https://mmbiz.qpic.cn/a.png", "image", 0, object)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("resource IDs differ: %q != %q", first.ID, second.ID)
	}
	var resources, mappings int
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM resources`).Scan(&resources); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM article_resources`).Scan(&mappings); err != nil {
		t.Fatal(err)
	}
	if resources != 1 || mappings != 1 {
		t.Fatalf("resources=%d mappings=%d", resources, mappings)
	}
}

func TestRecordDebugIncidentAndMissingResource(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	incident, err := database.RecordDebugIncident(context.Background(), DebugIncident{
		Operation: "article_download", Classification: "risk_control", RequestID: "request-a", Summary: "sanitized",
	})
	if err != nil || incident.ID == "" {
		t.Fatalf("incident=%#v err=%v", incident, err)
	}
	if err := database.MarkResourceMissing(context.Background(), "article-a", "https://mmbiz.qpic.cn/missing.png", "image", 1); err != nil {
		t.Fatal(err)
	}
	resource, err := database.ResourceByURL(context.Background(), "https://mmbiz.qpic.cn/missing.png")
	if err != nil || resource.Status != "missing" || resource.ObjectDigest != "" {
		t.Fatalf("resource=%#v err=%v", resource, err)
	}
	if _, err := database.CurrentContent(context.Background(), "article-a", "html"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CurrentContent error = %v", err)
	}
}

func TestArticleResourceAvailabilityCountsPersistedMappings(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	available := objects.Object{Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Size: 3, MediaType: "image/png"}
	if _, err := database.CommitResource(context.Background(), "article-a", "https://mmbiz.qpic.cn/available.png", "image", 0, available); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkResourceMissing(context.Background(), "article-a", "https://mmbiz.qpic.cn/missing.png", "image", 1); err != nil {
		t.Fatal(err)
	}

	availability, err := database.ArticleResourceAvailability(context.Background(), "article-a")
	if err != nil || availability.ArticleID != "article-a" || availability.Total != 2 || availability.Available != 1 {
		t.Fatalf("ArticleResourceAvailability() = %#v, %v", availability, err)
	}
	if _, err := database.ArticleResourceAvailability(context.Background(), "unknown"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown article error = %v", err)
	}
}

func openContentDatabase(t *testing.T) *Database {
	t.Helper()
	database, err := Open(context.Background(), OpenOptions{
		Path: filepath.Join(t.TempDir(), "content.sqlite3"), ProfileID: "profile-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func seedContentArticle(t *testing.T, database *Database) {
	t.Helper()
	if err := database.UpsertAccount(context.Background(), AccountRecord{ID: "account-a", FakeID: "fake-a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(context.Background(), ArticleRecord{
		ID: "article-a", AccountID: domain.AccountID("account-a"), Aid: "aid-a", Title: "Article",
		CanonicalURL: "https://mp.weixin.qq.com/s/a", ContentStatus: "missing",
	}); err != nil {
		t.Fatal(err)
	}
}
