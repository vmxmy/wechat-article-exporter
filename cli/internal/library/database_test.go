package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestOpenAppliesInitialSchemaAndPragmas(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	var version int
	if err := database.db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, CurrentSchemaVersion)
	}
	var foreignKeys int
	if err := database.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d", foreignKeys)
	}
	var journalMode string
	if err := database.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q", journalMode)
	}
}

func TestSQLiteWALSupportsSeparateProcessReaderDuringWriterAndSeesCommittedStateAfterward(t *testing.T) {
	if os.Getenv("WECHAT_ARTICLE_SQLITE_HELPER") != "" {
		runSQLiteProcessHelper(t)
		return
	}
	path := filepath.Join(t.TempDir(), "shared.sqlite")
	database := openPath(t, path, "profile-a")
	if err := database.UpsertAccount(context.Background(), AccountRecord{ID: "account-base", FakeID: "fake-base", Name: "Base"}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(filepath.Dir(path), "writer-ready")
	commit := filepath.Join(filepath.Dir(path), "writer-commit")
	writer := sqliteHelperCommand("writer", path, ready, commit)
	var writerOutput strings.Builder
	writer.Stdout, writer.Stderr = &writerOutput, &writerOutput
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Wait() }()
	waitForFile(t, ready, 5*time.Second)

	readerBefore := sqliteHelperCommand("reader-before", path, ready, commit)
	before, err := readerBefore.CombinedOutput()
	if err != nil {
		t.Fatalf("reader before commit: %v\n%s", err, before)
	}
	if firstOutputLine(string(before)) != "1" {
		t.Fatalf("reader before commit count = %q, want 1", before)
	}
	if err := os.WriteFile(commit, []byte("commit"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("writer: %v\n%s", err, writerOutput.String())
		}
	case <-time.After(5 * time.Second):
		_ = writer.Process.Kill()
		t.Fatal("writer did not commit within timeout")
	}

	readerAfter := sqliteHelperCommand("reader-after", path, ready, commit)
	after, err := readerAfter.CombinedOutput()
	if err != nil {
		t.Fatalf("reader after commit: %v\n%s", err, after)
	}
	if firstOutputLine(string(after)) != "2" {
		t.Fatalf("reader after commit count = %q, want 2", after)
	}
	verified, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	defer verified.Close()
	var integrity string
	if err := verified.db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
}

func firstOutputLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" && strings.TrimSpace(line) != "PASS" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func sqliteHelperCommand(mode, path, ready, commit string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestSQLiteWALSupportsSeparateProcessReaderDuringWriterAndSeesCommittedStateAfterward$")
	command.Env = append(os.Environ(),
		"WECHAT_ARTICLE_SQLITE_HELPER="+mode,
		"WECHAT_ARTICLE_SQLITE_PATH="+path,
		"WECHAT_ARTICLE_SQLITE_READY="+ready,
		"WECHAT_ARTICLE_SQLITE_COMMIT="+commit,
	)
	return command
}

func runSQLiteProcessHelper(t *testing.T) {
	mode := os.Getenv("WECHAT_ARTICLE_SQLITE_HELPER")
	path := os.Getenv("WECHAT_ARTICLE_SQLITE_PATH")
	if mode == "reader-before" || mode == "reader-after" {
		reader, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(1000)&mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		var count int
		if err := reader.QueryRow("SELECT COUNT(*) FROM accounts WHERE profile_id=?", "profile-a").Scan(&count); err != nil {
			t.Fatal(err)
		}
		fmt.Println(count)
		return
	}
	database, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a", BusyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	switch mode {
	case "writer":
		transaction, err := database.db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UnixMilli()
		if _, err := transaction.Exec(`INSERT INTO accounts(id, profile_id, fakeid, nickname, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)`, "account-writer", "profile-a", "fake-writer", "Writer", now, now); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("WECHAT_ARTICLE_SQLITE_READY"), []byte("ready"), 0o600); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("WECHAT_ARTICLE_SQLITE_COMMIT")); err == nil {
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				_ = transaction.Rollback()
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				_ = transaction.Rollback()
				t.Fatal("timed out waiting for commit marker")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := transaction.Commit(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported SQLite helper mode %q", mode)
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDatabaseTypedQueriesAreStableAndProfileIsolated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	ctx := context.Background()
	databaseA, err := Open(ctx, OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = databaseA.Close() })
	databaseB, err := Open(ctx, OpenOptions{Path: path, ProfileID: "profile-b"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = databaseB.Close() })
	now := time.Now().UnixMilli()
	insertAccount(t, databaseA.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	insertAccount(t, databaseA.db, "account-z", "profile-a", "fake-z", "Zulu", now)
	insertAccount(t, databaseB.db, "account-b", "profile-b", "fake-b", "Beta", now)
	insertArticle(t, databaseA.db, "article-old", "profile-a", "account-a", "Old", "https://mp.weixin.qq.com/s/old", now-1000, now)
	insertArticle(t, databaseA.db, "article-new", "profile-a", "account-a", "New", "https://mp.weixin.qq.com/s/new", now, now)
	insertArticle(t, databaseB.db, "article-b", "profile-b", "account-b", "Hidden", "https://mp.weixin.qq.com/s/hidden", now, now)

	accounts, err := databaseA.QueryAccounts(ctx, domain.AccountQuery{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if accounts.Total != 2 || len(accounts.Items) != 1 || accounts.Items[0].Name != "Alpha" {
		t.Fatalf("QueryAccounts() = %#v", accounts)
	}
	articles, err := databaseA.QueryArticles(ctx, domain.ArticleQuery{AccountID: "account-a", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if articles.Total != 2 || len(articles.Items) != 2 || articles.Items[0].Title != "New" || articles.Items[1].Title != "Old" {
		t.Fatalf("QueryArticles() = %#v", articles)
	}
	status, err := databaseA.StorageStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Accounts != 2 || status.Articles != 2 || !status.DatabaseAvailable {
		t.Fatalf("StorageStatus() = %#v", status)
	}
}

func TestWithTxRollsBackAllRelatedChanges(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	now := time.Now().UnixMilli()
	err := database.WithTx(context.Background(), func(transaction *sql.Tx) error {
		if _, err := transaction.Exec(`INSERT INTO accounts(id, profile_id, fakeid, nickname, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)`, "account-a", "profile-a", "fake-a", "Alpha", now, now); err != nil {
			return err
		}
		_, err := transaction.Exec(`INSERT INTO articles(id, profile_id, account_id, canonical_url, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)`, "article-a", "profile-a", "missing-account", "https://mp.weixin.qq.com/s/a", now, now)
		return err
	})
	if err == nil {
		t.Fatal("WithTx() error = nil, want foreign-key failure")
	}
	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id = 'account-a'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back account count = %d", count)
	}
}

func TestRepositoryUpsertsAccountsArticlesAndAlbums(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	if err := database.UpsertAccount(ctx, AccountRecord{ID: "account-a", FakeID: "fake-a", Name: "Alpha", ArticleCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(ctx, ArticleRecord{
		ID: "article-a", AccountID: "account-a", Aid: "aid-a", Title: "First",
		CanonicalURL: "https://mp.weixin.qq.com/s/a", ContentStatus: "available",
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertAlbum(ctx, AlbumRecord{ID: "album-a", AccountID: "account-a", UpstreamID: "upstream-a", Title: "Album", ArticleCount: 1}); err != nil {
		t.Fatal(err)
	}
	if err := database.LinkArticleAlbum(ctx, "article-a", "album-a", 1); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertArticle(ctx, ArticleRecord{
		ID: "ignored-new-id", AccountID: "account-a", Aid: "aid-a", Title: "Updated",
		CanonicalURL: "https://mp.weixin.qq.com/s/a", ContentStatus: "available",
	}); err != nil {
		t.Fatal(err)
	}
	articles, err := database.QueryArticles(ctx, domain.ArticleQuery{AlbumID: "album-a"})
	if err != nil {
		t.Fatal(err)
	}
	if articles.Total != 1 || articles.Items[0].Title != "Updated" || articles.Items[0].ID != "article-a" {
		t.Fatalf("articles = %#v", articles)
	}
	if err := database.UpsertExport(ctx, ExportRecord{ID: "export-a", Format: "markdown", Manifest: map[string]any{"count": 1}, State: "completed"}); err != nil {
		t.Fatal(err)
	}
	exports, err := database.QueryExports(ctx, 0, 10)
	if err != nil || exports.Total != 1 || exports.Items[0] != "export-a" {
		t.Fatalf("exports = %#v, %v", exports, err)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.sqlite")
	database := openPath(t, path, "profile-a")
	if _, err := database.db.Exec("INSERT INTO schema_migrations(version, name, applied_at) VALUES(99, 'future', 0)"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"}); err == nil {
		t.Fatal("Open(newer schema) error = nil")
	}
}

func TestDatabaseBackupProducesReadableSnapshot(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	now := time.Now().UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	destination := filepath.Join(t.TempDir(), "backup.sqlite")
	if err := database.Backup(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	backup, err := sql.Open("sqlite", destination+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var count int
	if err := backup.QueryRow("SELECT COUNT(*) FROM accounts").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backup account count = %d", count)
	}
}

func openTestDatabase(t *testing.T, profile domain.ProfileID) *Database {
	t.Helper()
	return openPath(t, filepath.Join(t.TempDir(), "library.sqlite"), profile)
}

func openPath(t *testing.T, path string, profile domain.ProfileID) *Database {
	t.Helper()
	database, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: profile})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertAccount(t *testing.T, database *sql.DB, id, profileID, fakeID, name string, now int64) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO accounts(id, profile_id, fakeid, nickname, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)`, id, profileID, fakeID, name, now, now); err != nil {
		t.Fatal(err)
	}
}

func insertArticle(t *testing.T, database *sql.DB, id, profileID, accountID, title, url string, publishedAt, now int64) {
	t.Helper()
	if _, err := database.Exec(`INSERT INTO articles(id, profile_id, account_id, title, canonical_url, published_at, content_status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, 'available', ?, ?)`, id, profileID, accountID, title, url, publishedAt, now, now); err != nil {
		t.Fatal(err)
	}
}
