package library

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

func TestBackupCreateVerifyAndOmitSecretsByDefault(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.Put(context.Background(), strings.NewReader("object body"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, database.db, "account-a", "profile-a", "fake-a", "Alpha", now)
	insertArticle(t, database.db, "article-a", "profile-a", "account-a", "Article", "https://mp.weixin.qq.com/s/a", now, now)
	if _, err := database.db.Exec(`INSERT INTO objects(digest, size_bytes, media_type, created_at) VALUES(?, ?, 'text/plain', ?);
INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at) VALUES('content-a', 'article-a', ?, 'html', ?)`,
		object.Digest, object.Size, now, object.Digest, now); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion":2,"profileId":"profile-a","preferences":{},"mcp":{},"extensions":{"secretRef":"opaque-keychain-id"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "backup.wab")
	manifest, err := database.CreateBackup(context.Background(), BackupOptions{
		Destination: destination, ObjectStore: store, ConfigPath: configPath, Now: time.UnixMilli(now),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Objects) != 1 || manifest.Counts["articles"] != 1 || len(manifest.Omitted) == 0 {
		t.Fatalf("manifest = %#v", manifest)
	}
	verification, err := VerifyBackup(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Valid || verification.ArchiveSHA == "" {
		t.Fatalf("verification = %#v", verification)
	}
	reader, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if strings.Contains(strings.ToLower(file.Name), "secret") || strings.Contains(strings.ToLower(file.Name), "vault") {
			t.Fatalf("secret-bearing archive path = %q", file.Name)
		}
		if file.Name == backupConfigPath {
			data, err := readZipFile(context.Background(), file, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "opaque-keychain-id") || strings.Contains(string(data), "extensions") {
				t.Fatalf("backup configuration retained uncontracted extension data: %s", data)
			}
		}
	}
}

func TestVerifyBackupReportsAllMissingOrInvalidEntries(t *testing.T) {
	digest := sha256.Sum256([]byte("expected"))
	digestHex := hex.EncodeToString(digest[:])
	manifest := BackupManifest{
		FormatVersion: BackupFormatVersion,
		SchemaVersion: CurrentSchemaVersion,
		CreatedAt:     time.Now(),
		Files: map[string]BackupFile{
			backupDatabasePath: {Size: 10, SHA256: strings.Repeat("0", 64)},
		},
		Objects: []BackupObject{{Digest: digestHex, Size: 8, Path: backupObjectPath(digestHex)}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "invalid.wab")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	if err := writeZipBytes(writer, backupManifestPath, manifestBytes); err != nil {
		t.Fatal(err)
	}
	if err := writeZipBytes(writer, backupDatabasePath, []byte("not sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyBackup(context.Background(), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if verification.Valid || len(verification.Failures) < 2 {
		t.Fatalf("verification = %#v", verification)
	}
}

func TestRestoreValidatesBeforeMutationAndRollsBackBeforeCommit(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.sqlite")
	source, err := Open(context.Background(), OpenOptions{Path: sourcePath, ProfileID: "source-profile", ProfileName: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	sourceStore, err := objects.NewFileStore(filepath.Join(t.TempDir(), "source-objects"))
	if err != nil {
		t.Fatal(err)
	}
	object, err := sourceStore.Put(context.Background(), strings.NewReader("restore object"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, source.db, "source-account", "source-profile", "source-fake", "Source", now)
	insertArticle(t, source.db, "source-article", "source-profile", "source-account", "Source article", "https://mp.weixin.qq.com/s/source", now, now)
	if _, err := source.db.Exec(`INSERT INTO objects(digest, size_bytes, created_at) VALUES(?, ?, ?);
INSERT INTO content_versions(id, article_id, object_digest, kind, captured_at) VALUES('source-content', 'source-article', ?, 'html', ?)`,
		object.Digest, object.Size, now, object.Digest, now); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "restore.wab")
	if _, err := source.CreateBackup(context.Background(), BackupOptions{Destination: archivePath, ObjectStore: sourceStore}); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	livePath := filepath.Join(t.TempDir(), "live.sqlite")
	live, err := Open(context.Background(), OpenOptions{Path: livePath, ProfileID: "live-profile", ProfileName: "Live"})
	if err != nil {
		t.Fatal(err)
	}
	insertAccount(t, live.db, "live-account", "live-profile", "live-fake", "Live", now)
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	liveStore, err := objects.NewFileStore(filepath.Join(t.TempDir(), "live-objects"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = RestoreBackup(context.Background(), RestoreOptions{
		ArchivePath: archivePath, DatabasePath: livePath, ObjectStore: liveStore,
		BeforeCommit: func() error { return errors.New("injected stop") },
	})
	if err == nil {
		t.Fatal("RestoreBackup() error = nil")
	}
	assertAccountExists(t, livePath, "live-account")
	assertAccountMissing(t, livePath, "source-account")

	tampered := rewriteArchive(t, archivePath, func(name string, contents []byte) ([]byte, bool) {
		if name == backupObjectPath(object.Digest) {
			return []byte("tampered"), true
		}
		return contents, true
	})
	if _, err := RestoreBackup(context.Background(), RestoreOptions{ArchivePath: tampered, DatabasePath: livePath, ObjectStore: liveStore}); err == nil {
		t.Fatal("RestoreBackup(tampered) error = nil")
	}
	assertAccountExists(t, livePath, "live-account")
	assertAccountMissing(t, livePath, "source-account")
}

func TestRestorePreservesRollbackBackupWhenCommitAndRecoveryRenamesFail(t *testing.T) {
	ctx := context.Background()
	workingRoot := t.TempDir()
	sourcePath := filepath.Join(workingRoot, "source.sqlite")
	source, err := Open(ctx, OpenOptions{Path: sourcePath, ProfileID: "source-profile", ProfileName: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	sourceStore, err := objects.NewFileStore(filepath.Join(workingRoot, "source-objects"))
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(workingRoot, "restore.wab")
	if _, err := source.CreateBackup(ctx, BackupOptions{Destination: archivePath, ObjectStore: sourceStore}); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	livePath := filepath.Join(workingRoot, "live.sqlite")
	live, err := Open(ctx, OpenOptions{Path: livePath, ProfileID: "live-profile", ProfileName: "Live"})
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	liveStore, err := objects.NewFileStore(filepath.Join(workingRoot, "live-objects"))
	if err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected rename failure")
	operations := defaultRestoreFileOperations()
	operations.rename = func(source, destination string) error {
		if destination == livePath && (strings.Contains(source, ".restore-staging-") || strings.Contains(source, ".restore-rollback-")) {
			return injected
		}
		return os.Rename(source, destination)
	}
	_, err = RestoreBackup(ctx, RestoreOptions{
		ArchivePath: archivePath, DatabasePath: livePath, ObjectStore: liveStore, fileOperations: operations,
	})
	if err == nil || !strings.Contains(err.Error(), "restore rollback failed; preserve") {
		t.Fatalf("RestoreBackup() error = %v, want preserved rollback failure", err)
	}
	rollbackRoots, globErr := filepath.Glob(filepath.Join(workingRoot, ".restore-rollback-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(rollbackRoots) != 1 {
		t.Fatalf("rollback roots = %v, want one preserved root", rollbackRoots)
	}
	rollbackDatabase := filepath.Join(rollbackRoots[0], "database")
	assertProfile(t, rollbackDatabase, "live-profile", "Live")
	if _, statErr := os.Stat(livePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("live database stat error = %v, want missing path while rollback backup is preserved", statErr)
	}
}

func TestRestoreConflictPolicyRefusesOrRenamesProfiles(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.sqlite")
	source, err := Open(context.Background(), OpenOptions{Path: sourcePath, ProfileID: "shared-id", ProfileName: "Archive Name"})
	if err != nil {
		t.Fatal(err)
	}
	sourceStore, _ := objects.NewFileStore(filepath.Join(t.TempDir(), "source-objects"))
	archivePath := filepath.Join(t.TempDir(), "profiles.wab")
	if _, err := source.CreateBackup(context.Background(), BackupOptions{Destination: archivePath, ObjectStore: sourceStore}); err != nil {
		t.Fatal(err)
	}
	_ = source.Close()

	livePath := filepath.Join(t.TempDir(), "live.sqlite")
	live, err := Open(context.Background(), OpenOptions{Path: livePath, ProfileID: "shared-id", ProfileName: "Live Name"})
	if err != nil {
		t.Fatal(err)
	}
	_ = live.Close()
	liveStore, _ := objects.NewFileStore(filepath.Join(t.TempDir(), "live-objects"))
	if _, err := RestoreBackup(context.Background(), RestoreOptions{
		ArchivePath: archivePath, DatabasePath: livePath, ObjectStore: liveStore, ConflictPolicy: RestoreRefuseConflicts,
	}); err == nil {
		t.Fatal("RestoreBackup(conflict refuse) error = nil")
	}
	assertProfile(t, livePath, "shared-id", "Live Name")

	report, err := RestoreBackup(context.Background(), RestoreOptions{
		ArchivePath: archivePath, DatabasePath: livePath, ObjectStore: liveStore, ConflictPolicy: RestoreRenameConflicts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Profiles) != 1 || report.Profiles[0].Resolution != "renamed" || report.Profiles[0].TargetID == "shared-id" {
		t.Fatalf("report = %#v", report)
	}
	assertProfile(t, livePath, string(report.Profiles[0].TargetID), report.Profiles[0].TargetName)
}

func TestRestoreIntoActiveProfileRemapsAllOwnedRecordsAndConfiguration(t *testing.T) {
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "source.sqlite")
	source, err := Open(ctx, OpenOptions{Path: sourcePath, ProfileID: "source-profile", ProfileName: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	sourceStore, err := objects.NewFileStore(filepath.Join(t.TempDir(), "source-objects"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	insertAccount(t, source.db, "source-account", "source-profile", "source-fake", "Source", now)
	if _, err := source.db.Exec(`INSERT INTO saved_article_queries(profile_id, name, query_json, created_at, updated_at)
VALUES('source-profile', 'recent', '{}', ?, ?);
INSERT INTO scheduler_permits(id, profile_id, owner, operation, host, sensitive, acquired_at, renewed_at, expires_at)
VALUES('permit-a', 'source-profile', 'worker-a', 'export', 'local', 0, ?, ?, ?)`,
		now, now, now, now, now+60_000); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "source-config.json")
	if err := os.WriteFile(configPath, []byte(`{"schemaVersion":2,"profileId":"source-profile","preferences":{},"mcp":{},"extensions":{"large":9007199254740993}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "active-profile.wab")
	if _, err := source.CreateBackup(ctx, BackupOptions{Destination: archivePath, ObjectStore: sourceStore, ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}

	livePath := filepath.Join(t.TempDir(), "live.sqlite")
	live, err := Open(ctx, OpenOptions{Path: livePath, ProfileID: "active-profile", ProfileName: "Active"})
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	liveStore, err := objects.NewFileStore(filepath.Join(t.TempDir(), "live-objects"))
	if err != nil {
		t.Fatal(err)
	}
	liveConfig := filepath.Join(t.TempDir(), "live-config.json")
	restoreTime := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	report, err := RestoreBackup(ctx, RestoreOptions{ArchivePath: archivePath, DatabasePath: livePath, ObjectStore: liveStore,
		ConfigPath: liveConfig, TargetProfile: "active-profile", TargetName: "Active", Now: restoreTime})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Profiles) != 1 || report.Profiles[0].TargetID != "active-profile" || report.Profiles[0].Resolution != "remapped" {
		t.Fatalf("profile resolutions = %#v", report.Profiles)
	}
	rawDatabase, err := sql.Open("sqlite", "file:"+filepath.ToSlash(livePath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var profileUpdatedAt int64
	if err := rawDatabase.QueryRow("SELECT updated_at FROM profiles WHERE id=?", "active-profile").Scan(&profileUpdatedAt); err != nil {
		rawDatabase.Close()
		t.Fatal(err)
	}
	if err := rawDatabase.Close(); err != nil {
		t.Fatal(err)
	}
	if profileUpdatedAt != restoreTime.UnixMilli() {
		t.Fatalf("restored profile updated_at=%d, want %d", profileUpdatedAt, restoreTime.UnixMilli())
	}
	database, err := Open(ctx, OpenOptions{Path: livePath, ProfileID: "active-profile", ProfileName: "Active"})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	page, err := database.QueryAccounts(ctx, domain.AccountQuery{Limit: 10})
	if err != nil || page.Total != 1 || page.Items[0].ID != "source-account" {
		t.Fatalf("remapped accounts=%#v err=%v", page, err)
	}
	for table, expected := range map[string]int{"saved_article_queries": 1, "scheduler_permits": 1} {
		var count int
		if err := database.db.QueryRow("SELECT COUNT(*) FROM "+quoteSQLiteIdentifier(table)+" WHERE profile_id=?", "active-profile").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != expected {
			t.Fatalf("remapped %s count=%d, want %d", table, count, expected)
		}
	}
	configData, err := os.ReadFile(liveConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), `"profileId": "active-profile"`) || strings.Contains(string(configData), `"source-profile"`) {
		t.Fatalf("remapped config = %s", configData)
	}
}

func TestRewriteRestoredConfigRejectsNullRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteRestoredConfigProfile(path, "active-profile"); err == nil || !strings.Contains(err.Error(), "expected a JSON object") {
		t.Fatalf("rewrite null config error=%v", err)
	}
}

func TestRewriteRestoredConfigPreservesUnknownLargeIntegers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"profileId":"source","extensions":{"large":9007199254740993}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteRestoredConfigProfile(path, "active"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `9007199254740993`) || !strings.Contains(string(data), `"profileId": "active"`) {
		t.Fatalf("rewritten config=%s", data)
	}
}

func rewriteArchive(t *testing.T, source string, transform func(string, []byte) ([]byte, bool)) string {
	t.Helper()
	reader, err := zip.OpenReader(source)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	destination := filepath.Join(t.TempDir(), "rewritten.wab")
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range reader.File {
		contents, err := readZipFile(context.Background(), entry, 64<<20)
		if err != nil {
			t.Fatal(err)
		}
		contents, keep := transform(entry.Name, contents)
		if !keep {
			continue
		}
		if err := writeZipBytes(writer, entry.Name, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return destination
}

func assertAccountExists(t *testing.T, path, id string) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM accounts WHERE id=?", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("account %q count = %d", id, count)
	}
}

func assertAccountMissing(t *testing.T, path, id string) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM accounts WHERE id=?", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("account %q count = %d", id, count)
	}
}

func assertProfile(t *testing.T, path, id, name string) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var actual string
	if err := database.QueryRow("SELECT name FROM profiles WHERE id=?", id).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != name {
		t.Fatalf("profile %q name = %q, want %q", id, actual, name)
	}
}
