package library

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

func TestIntegrityMarksArticleIncompleteWhenSharedObjectIsMissing(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	insertArticle(t, database.db, "article-a", "profile-a", "account-a", "Article", "https://mp.weixin.qq.com/s/a", now, now)
	missing := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, 10, ?);
INSERT INTO resources(id, profile_id, source_url, object_digest, status, created_at, updated_at) VALUES('resource-a', 'profile-a', 'https://example.com/a.png', ?, 'available', ?, ?);
INSERT INTO article_resources(article_id, resource_id, role, ordinal) VALUES('article-a', 'resource-a', 'image', 0)`,
		missing, now, missing, now, now); err != nil {
		t.Fatal(err)
	}
	report, err := database.CheckIntegrity(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].Recommendation == "" {
		t.Fatalf("integrity report = %#v", report)
	}
	var status string
	if err := database.db.QueryRow("SELECT content_status FROM articles WHERE id='article-a'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "incomplete" {
		t.Fatalf("content status = %q", status)
	}
}
