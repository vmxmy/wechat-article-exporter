package library

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
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

func TestIntegrityChecksRetryableTerminalPinsAndReleasesCompletedPins(t *testing.T) {
	for _, state := range []domain.JobState{
		domain.JobFailed, domain.JobPartial, domain.JobCancelled, domain.JobCompleted,
	} {
		t.Run(string(state), func(t *testing.T) {
			database := openTestDatabase(t, "profile-a")
			store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
			if err != nil {
				t.Fatal(err)
			}
			missingCharacter := map[domain.JobState]string{
				domain.JobFailed: "d", domain.JobPartial: "e", domain.JobCancelled: "f", domain.JobCompleted: "a",
			}[state]
			missing := strings.Repeat(missingCharacter, 64)
			if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, media_type, created_at)
VALUES(?, 10, 'application/json', ?)`, missing, time.Now().UnixMilli()); err != nil {
				t.Fatal(err)
			}
			key, err := json.Marshal(map[string]any{"pinnedDigests": []string{missing}})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UnixMilli()
			if _, err := database.db.Exec(`INSERT INTO jobs(id, profile_id, kind, state, created_at, updated_at, completed_at)
VALUES('job-a', 'profile-a', 'export', ?, ?, ?, ?)`, state, now, now, now); err != nil {
				t.Fatal(err)
			}
			if _, err := database.db.Exec(`INSERT INTO job_items(id, job_id, item_key, state, created_at, updated_at, completed_at)
VALUES('item-a', 'job-a', ?, ?, ?, ?, ?)`, string(key), state, now, now, now); err != nil {
				t.Fatal(err)
			}
			var references int
			if err := database.db.QueryRow(`SELECT COUNT(*) FROM job_items ji
JOIN jobs j ON j.id=ji.job_id
JOIN json_each(CASE WHEN json_valid(ji.item_key) THEN ji.item_key ELSE '{}' END, '$.pinnedDigests') pin
WHERE j.profile_id='profile-a' AND j.state<>'completed' AND ji.state<>'completed' AND pin.type='text'`).Scan(&references); err != nil {
				t.Fatal(err)
			}
			if state != domain.JobCompleted && references != 1 {
				var jobState, itemState, storedKey string
				_ = database.db.QueryRow(`SELECT j.state, ji.state, ji.item_key FROM jobs j JOIN job_items ji ON ji.job_id=j.id
WHERE j.id='job-a'`).Scan(&jobState, &itemState, &storedKey)
				t.Fatalf("pin references=%d job=%q item=%q key=%s", references, jobState, itemState, storedKey)
			}
			report, err := database.CheckIntegrityWithOptions(context.Background(), store, IntegrityOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if state == domain.JobCompleted {
				if len(report.Issues) != 0 {
					t.Fatalf("completed pin remained reachable: %#v", report)
				}
				return
			}
			if len(report.Issues) != 1 || report.Issues[0].ObjectDigest != missing {
				t.Fatalf("retryable terminal pin report=%#v", report)
			}
		})
	}
}

func TestIntegrityChecksCurrentArticleContentObjects(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	insertArticle(t, database.db, "article-a", "profile-a", "account-a", "Article", "https://mp.weixin.qq.com/s/a", now, now)
	missing := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, 10, ?);
INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at, is_current)
VALUES('content-a', 'article-a', ?, 'html', ?, 1)`, missing, now, missing, now); err != nil {
		t.Fatal(err)
	}
	report, err := database.CheckIntegrity(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].ArticleID != "article-a" || report.Issues[0].ResourceID != "" ||
		!strings.Contains(report.Issues[0].Recommendation, "HTML") {
		t.Fatalf("integrity report = %#v", report)
	}
}

func TestIntegrityUsesCompleteBackupAndGarbageCollectionReachabilitySet(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	insertArticle(t, database.db, "article-a", "profile-a", "account-a", "Article", "https://mp.weixin.qq.com/s/a", now, now)
	digests := map[string]string{
		"content": strings.Repeat("1", 64),
		"comment": strings.Repeat("2", 64),
		"reply":   strings.Repeat("3", 64),
		"debug":   strings.Repeat("4", 64),
	}
	for _, digest := range digests {
		if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, 10, ?)`, digest, now); err != nil {
			t.Fatal(err)
		}
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at, is_current)
VALUES('content-old', 'article-a', ?, 'html', ?, 0)`, []any{digests["content"], now}},
		{`INSERT INTO comments(id, article_id, upstream_id, raw_object_digest, fetched_at)
VALUES('comment-a', 'article-a', 'upstream-comment', ?, ?)`, []any{digests["comment"], now}},
		{`INSERT INTO replies(id, comment_id, upstream_id, raw_object_digest, fetched_at)
VALUES('reply-a', 'comment-a', 'upstream-reply', ?, ?)`, []any{digests["reply"], now}},
		{`INSERT INTO debug_incidents(id, profile_id, operation, object_digest, summary, created_at)
VALUES('debug-a', 'profile-a', 'download', ?, 'sanitized', ?)`, []any{digests["debug"], now}},
	}
	for _, statement := range statements {
		if _, err := database.db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	report, err := database.CheckIntegrityWithOptions(context.Background(), store, IntegrityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != len(digests) {
		t.Fatalf("integrity issue count=%d, want %d: %#v", len(report.Issues), len(digests), report)
	}
	seen := make(map[string]IntegrityIssue, len(report.Issues))
	for _, issue := range report.Issues {
		seen[issue.ObjectDigest] = issue
	}
	for kind, digest := range digests {
		issue, ok := seen[digest]
		if !ok {
			t.Fatalf("integrity omitted retained %s object %s: %#v", kind, digest, report)
		}
		if issue.Recommendation == "" {
			t.Fatalf("integrity %s issue has no recommendation: %#v", kind, issue)
		}
	}
	if seen[digests["content"]].ArticleID != "article-a" || seen[digests["comment"]].ArticleID != "article-a" ||
		seen[digests["reply"]].ArticleID != "article-a" {
		t.Fatalf("article context was not retained: %#v", report)
	}
}

func TestDiagnosticIntegrityCheckIsReadOnlyAndUsesPresenceValidation(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	insertArticle(t, database.db, "article-a", "profile-a", "account-a", "Article", "https://mp.weixin.qq.com/s/a", now, now)
	missing := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, 10, ?);
INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at, is_current)
VALUES('content-a', 'article-a', ?, 'html', ?, 1)`, missing, now, missing, now); err != nil {
		t.Fatal(err)
	}
	report, err := database.CheckIntegrityWithOptions(context.Background(), store, IntegrityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].Kind != "missing-object" {
		t.Fatalf("diagnostic integrity report=%#v", report)
	}
	var status string
	if err := database.db.QueryRow("SELECT content_status FROM articles WHERE id='article-a'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "available" {
		t.Fatalf("diagnostic integrity mutated content status to %q", status)
	}
}

func TestIntegrityChecksObjectsPinnedByActiveJobItems(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	missing := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	jobStore := NewJobStore(database)
	key, err := json.Marshal(map[string]any{
		"articleId": "article-a", "pinnedDigests": []string{missing},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobStore.CreateWithItems(context.Background(), jobs.Spec{Kind: "export", Profile: "profile-a"},
		[]string{string(key)}); err != nil {
		t.Fatal(err)
	}
	report, err := database.CheckIntegrityWithOptions(context.Background(), store, IntegrityOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Issues) != 1 || report.Issues[0].ObjectDigest != missing ||
		!strings.Contains(report.Issues[0].Recommendation, "job snapshot") {
		t.Fatalf("integrity report=%#v", report)
	}
}
