package library

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const earliestSupportedSchemaVersion = 1

func TestMinimumSchemaVersionMatchesCompatibilityPolicy(t *testing.T) {
	if MinimumSchemaVersion != earliestSupportedSchemaVersion {
		t.Fatalf("MinimumSchemaVersion = %d, documented compatibility floor = %d", MinimumSchemaVersion, earliestSupportedSchemaVersion)
	}
}

func TestMigrationUpgradeFromEverySupportedBaseline(t *testing.T) {
	for version := earliestSupportedSchemaVersion; version <= CurrentSchemaVersion; version++ {
		t.Run(fmt.Sprintf("schema-v%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fmt.Sprintf("baseline-v%d.sqlite3", version))
			createMigrationBaseline(t, path, version)
			seedMigrationSentinel(t, path, version)

			database, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a", ProfileName: "Profile A"})
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()

			assertCurrentMigrationState(t, database.db)
			assertMigrationSentinel(t, database.db, version)
			if version < CurrentSchemaVersion {
				backupPath := path + fmt.Sprintf(".pre-migration-v%d.sqlite3", version)
				assertPreMigrationBackup(t, backupPath, version)
			}
		})
	}
}

func TestMigrationReplyCheckpointsIsOrderedAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reply-checkpoints.sqlite3")
	createMigrationBaseline(t, path, 2)

	database, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ensureReplyCheckpointTable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	database, err = Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertCurrentMigrationState(t, database.db)
	assertNamedSchemaObject(t, database.db, "table", "reply_checkpoints")
	assertNamedSchemaObject(t, database.db, "index", "reply_checkpoints_pending_idx")
}

func TestMigration004BackfillsOnlyUnambiguousLegacyExportFileArticleIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-export-files.sqlite3")
	createMigrationBaseline(t, path, 3)
	database := openMigrationFixture(t, path)
	now := time.Unix(1_700_000_000, 0).UnixMilli()
	if _, err := database.Exec(`INSERT INTO profiles(id, name, created_at, updated_at) VALUES('profile-a', 'Profile A', ?, ?);
INSERT INTO profiles(id, name, created_at, updated_at) VALUES('profile-b', 'Profile B', ?, ?);
INSERT INTO articles(id, profile_id, title, canonical_url, created_at, updated_at) VALUES
  ('article-a', 'profile-a', 'Article A', 'https://mp.weixin.qq.com/s/a', ?, ?),
  ('article-b', 'profile-a', 'Article B', 'https://mp.weixin.qq.com/s/b', ?, ?),
  ('article-foreign', 'profile-b', 'Foreign Article', 'https://mp.weixin.qq.com/s/foreign', ?, ?);
INSERT INTO exports(id, profile_id, format, manifest_json, state, created_at) VALUES
  ('export-provenance', 'profile-a', 'text', '{"selection":{"articleIds":["article-a","article-b"]},"outputs":[{"path":"b.txt","articleId":"article-b"},{"path":"a.txt","articleId":"article-a"}]}', 'completed', ?),
  ('export-duplicate-path', 'profile-a', 'text', '{"outputs":[{"path":"same.txt","articleId":"article-a"},{"path":"same.txt","articleId":"article-b"}]}', 'completed', ?),
  ('export-foreign-path', 'profile-a', 'text', '{"outputs":[{"path":"foreign.txt","articleId":"article-foreign"}]}', 'completed', ?),
  ('export-output-multi', 'profile-a', 'html', '{"outputs":[{"path":"batch.zip","articleIds":["article-a","article-b"]}]}', 'completed', ?),
  ('export-selection', 'profile-a', 'text', '{"articleIds":["article-a","article-b"]}', 'completed', ?),
  ('export-single', 'profile-a', 'html', '{"selection":{"articleIds":["article-a"]}}', 'completed', ?),
  ('export-nonobject-output', 'profile-a', 'text', '{"outputs":[null,42,"bad",{"path":"safe.txt","articleId":"article-a"}]}', 'completed', ?),
  ('export-malformed', 'profile-a', 'text', '{malformed', 'completed', ?);
INSERT INTO export_files(id, export_id, relative_path) VALUES
  ('file-provenance-a', 'export-provenance', 'a.txt'),
  ('file-provenance-b', 'export-provenance', 'b.txt'),
  ('file-duplicate-path', 'export-duplicate-path', 'same.txt'),
  ('file-foreign-path', 'export-foreign-path', 'foreign.txt'),
  ('file-output-multi', 'export-output-multi', 'batch.zip'),
  ('file-selection-a', 'export-selection', '01.txt'),
  ('file-selection-b', 'export-selection', '02.txt'),
  ('file-single-a', 'export-single', 'index.html'),
  ('file-single-b', 'export-single', 'assets/image.png'),
  ('file-nonobject-output', 'export-nonobject-output', 'safe.txt'),
  ('file-malformed', 'export-malformed', 'unknown.txt')`,
		now, now, now, now, now, now, now, now, now, now, now, now, now, now, now, now, now, now); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()

	assertExportFileArticleID(t, upgraded.db, "file-provenance-a", "article-a")
	assertExportFileArticleID(t, upgraded.db, "file-provenance-b", "article-b")
	assertExportFileArticleID(t, upgraded.db, "file-duplicate-path", "")
	assertExportFileArticleID(t, upgraded.db, "file-foreign-path", "")
	assertExportFileArticleID(t, upgraded.db, "file-output-multi", "")
	assertExportFileArticleID(t, upgraded.db, "file-selection-a", "")
	assertExportFileArticleID(t, upgraded.db, "file-selection-b", "")
	assertExportFileArticleID(t, upgraded.db, "file-single-a", "article-a")
	assertExportFileArticleID(t, upgraded.db, "file-single-b", "article-a")
	assertExportFileArticleID(t, upgraded.db, "file-nonobject-output", "article-a")
	assertExportFileArticleID(t, upgraded.db, "file-malformed", "")

	rows, err := upgraded.db.Query(`SELECT DISTINCT article_id FROM export_files
WHERE export_id='export-provenance' ORDER BY article_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var affected []string
	for rows.Next() {
		var articleID string
		if err := rows.Scan(&articleID); err != nil {
			t.Fatal(err)
		}
		affected = append(affected, articleID)
	}
	if strings.Join(affected, ",") != "article-a,article-b" {
		t.Fatalf("legacy multi-file affected article IDs = %v", affected)
	}
}

func TestMigration005DeterministicallyClearsDuplicateProfileJobAssociations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate-export-jobs.sqlite3")
	createMigrationBaseline(t, path, 4)
	database := openMigrationFixture(t, path)
	if _, err := database.Exec(`INSERT INTO profiles(id, name, created_at, updated_at) VALUES('profile-a', 'Profile A', 1, 1);
INSERT INTO jobs(id, profile_id, kind, state, created_at, updated_at) VALUES('job-a', 'profile-a', 'export', 'completed', 1, 1);
INSERT INTO exports(id, profile_id, job_id, format, state, created_at) VALUES
  ('export-z', 'profile-a', 'job-a', 'text', 'completed', 10),
  ('export-a', 'profile-a', 'job-a', 'text', 'completed', 10),
  ('export-oldest', 'profile-a', 'job-a', 'text', 'completed', 5)`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()

	rows, err := upgraded.db.Query(`SELECT id, COALESCE(job_id, '') FROM exports ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	associations := map[string]string{}
	for rows.Next() {
		var id, jobID string
		if err := rows.Scan(&id, &jobID); err != nil {
			t.Fatal(err)
		}
		associations[id] = jobID
	}
	if associations["export-oldest"] != "job-a" || associations["export-a"] != "" || associations["export-z"] != "" {
		t.Fatalf("deduplicated job associations = %#v", associations)
	}
	assertNamedSchemaObject(t, upgraded.db, "index", "exports_profile_job_idx")
}

func TestMigration006MakesLegacyWritingProvenanceImmediatelyReclaimable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "writing-provenance.sqlite3")
	createMigrationBaseline(t, path, 5)
	database := openMigrationFixture(t, path)
	if _, err := database.Exec(`INSERT INTO profiles(id, name, created_at, updated_at) VALUES('profile-a', 'Profile A', 1, 1);
INSERT INTO exports(id, profile_id, format, state, created_at, provenance_state)
VALUES('export-writing', 'profile-a', 'text', 'completed', 1, 'writing')`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var claimedAt sql.NullInt64
	if err := upgraded.db.QueryRow(`SELECT provenance_claimed_at FROM exports WHERE id='export-writing'`).Scan(&claimedAt); err != nil {
		t.Fatal(err)
	}
	if !claimedAt.Valid || claimedAt.Int64 != 0 {
		t.Fatalf("legacy writing provenance_claimed_at = %#v, want 0", claimedAt)
	}
	if _, claimed, err := upgraded.ClaimExportProvenance(context.Background(), "export-writing", 1, time.Now()); err != nil || !claimed {
		t.Fatalf("ClaimExportProvenance() claimed=%v error=%v", claimed, err)
	}
}

func TestMigrationFailureRollsBackVersionAndSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.sqlite")
	database, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT, applied_at INTEGER);
CREATE TABLE articles(id TEXT PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a"}); err == nil {
		t.Fatal("Open(conflicting migration) error = nil")
	}
	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var count int
	if err := check.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("migration version count = %d", count)
	}
	var accounts int
	if err := check.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='accounts'").Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Fatalf("partially applied accounts table count = %d", accounts)
	}
}

func createMigrationBaseline(t *testing.T, path string, targetVersion int) {
	t.Helper()
	if targetVersion < earliestSupportedSchemaVersion || targetVersion > CurrentSchemaVersion {
		t.Fatalf("unsupported test baseline %d", targetVersion)
	}
	database, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE schema_migrations (
version INTEGER PRIMARY KEY,
name TEXT NOT NULL,
applied_at INTEGER NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrationSources(t) {
		if migration.version > targetVersion {
			break
		}
		statements, err := splitSQLStatements(migration.source)
		if err != nil {
			t.Fatalf("parse %s: %v", migration.name, err)
		}
		transaction, err := database.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range statements {
			if _, err := transaction.Exec(statement); err != nil {
				_ = transaction.Rollback()
				t.Fatalf("apply %s: %v", migration.name, err)
			}
		}
		if _, err := transaction.Exec(
			"INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)",
			migration.version,
			migration.name,
			time.Unix(int64(migration.version), 0).UnixMilli(),
		); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
		if err := transaction.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func openMigrationFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	return database
}

func assertExportFileArticleID(t *testing.T, database *sql.DB, fileID, expected string) {
	t.Helper()
	var articleID sql.NullString
	if err := database.QueryRow(`SELECT article_id FROM export_files WHERE id=?`, fileID).Scan(&articleID); err != nil {
		t.Fatal(err)
	}
	if expected == "" && !articleID.Valid {
		return
	}
	if !articleID.Valid || articleID.String != expected {
		t.Fatalf("export file %s article_id = %#v, want %q", fileID, articleID, expected)
	}
}

type migrationSource struct {
	version int
	name    string
	source  string
}

func migrationSources(t *testing.T) []migrationSource {
	t.Helper()
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	sources := make([]migrationSource, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
			t.Fatalf("parse migration filename %s: %v", entry.Name(), err)
		}
		contents, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, migrationSource{version: version, name: entry.Name(), source: string(contents)})
	}
	sort.Slice(sources, func(left, right int) bool { return sources[left].version < sources[right].version })
	if len(sources) == 0 || sources[0].version != earliestSupportedSchemaVersion || sources[len(sources)-1].version != CurrentSchemaVersion {
		t.Fatalf("migration versions do not cover supported window: %#v", sources)
	}
	for index, source := range sources {
		want := earliestSupportedSchemaVersion + index
		if source.version != want {
			t.Fatalf("migration sequence has a gap: got v%d at index %d, want v%d", source.version, index, want)
		}
	}
	return sources
}

func seedMigrationSentinel(t *testing.T, path string, version int) {
	t.Helper()
	database, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Unix(1_700_000_000, 0).UnixMilli()
	if _, err := database.Exec(`INSERT INTO profiles(id, name, created_at, updated_at) VALUES('profile-a', 'Baseline', ?, ?);
INSERT INTO accounts(id, profile_id, fakeid, nickname, created_at, updated_at)
VALUES('account-a', 'profile-a', 'baseline-fakeid', 'Baseline account', ?, ?);
INSERT INTO articles(id, profile_id, account_id, aid, title, canonical_url, content_status, created_at, updated_at)
VALUES('article-a', 'profile-a', 'account-a', 'baseline-aid', 'Baseline article',
  'https://mp.weixin.qq.com/s/baseline', 'available', ?, ?);
INSERT INTO comments(id, article_id, upstream_id, content, fetched_at)
VALUES('comment-a', 'article-a', 'comment-upstream-a', 'Baseline comment', ?);
INSERT INTO replies(id, comment_id, upstream_id, content, fetched_at)
VALUES('reply-a', 'comment-a', 'reply-upstream-a', 'Baseline reply', ?)`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("seed schema v%d baseline: %v", version, err)
	}
}

func assertCurrentMigrationState(t *testing.T, database *sql.DB) {
	t.Helper()
	var version, count int
	if err := database.QueryRow("SELECT MAX(version), COUNT(*) FROM schema_migrations").Scan(&version, &count); err != nil {
		t.Fatal(err)
	}
	if version != CurrentSchemaVersion || count != CurrentSchemaVersion-earliestSupportedSchemaVersion+1 {
		t.Fatalf("migration state max=%d count=%d", version, count)
	}
	for _, table := range []string{
		"profiles", "accounts", "articles", "albums", "content_versions", "metric_snapshots", "comments", "replies",
		"comment_checkpoints", "reply_checkpoints", "resources", "network_routes", "jobs", "exports", "debug_incidents",
		"scheduler_permits",
	} {
		assertNamedSchemaObject(t, database, "table", table)
	}
	for _, column := range []string{"is_original", "wecoin_count", "media_duration_seconds"} {
		assertTableColumn(t, database, "articles", column)
	}
	for _, column := range []string{"output_authorization_json", "provenance_json", "provenance_path", "provenance_sha256", "provenance_state", "provenance_error", "provenance_generation", "provenance_claimed_at"} {
		assertTableColumn(t, database, "exports", column)
	}
	for _, column := range []string{"article_id", "status"} {
		assertTableColumn(t, database, "export_files", column)
	}
	assertNamedSchemaObject(t, database, "index", "reply_checkpoints_pending_idx")
	assertNamedSchemaObject(t, database, "index", "exports_profile_job_idx")
	assertNamedSchemaObject(t, database, "index", "scheduler_permits_profile_expiry_idx")
}

func assertMigrationSentinel(t *testing.T, database *sql.DB, sourceVersion int) {
	t.Helper()
	var title, comment, reply string
	if err := database.QueryRow(`SELECT a.title, c.content, r.content
FROM articles a JOIN comments c ON c.article_id=a.id JOIN replies r ON r.comment_id=c.id
WHERE a.id='article-a'`).Scan(&title, &comment, &reply); err != nil {
		t.Fatalf("read upgraded schema v%d sentinel: %v", sourceVersion, err)
	}
	if title != "Baseline article" || comment != "Baseline comment" || reply != "Baseline reply" {
		t.Fatalf("schema v%d sentinel changed: %q %q %q", sourceVersion, title, comment, reply)
	}
}

func assertPreMigrationBackup(t *testing.T, path string, sourceVersion int) {
	t.Helper()
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("pre-migration backup %s: info=%v err=%v", path, info, err)
	}
	database, err := sql.Open("sqlite", path+"?_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var version int
	if err := database.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != sourceVersion {
		t.Fatalf("backup schema version = %d, want %d", version, sourceVersion)
	}
	assertMigrationSentinel(t, database, sourceVersion)
}

func assertNamedSchemaObject(t *testing.T, database *sql.DB, objectType, name string) {
	t.Helper()
	var actual string
	if err := database.QueryRow("SELECT name FROM sqlite_master WHERE type=? AND name=?", objectType, name).Scan(&actual); err != nil {
		t.Fatalf("required %s %s: %v", objectType, name, err)
	}
}

func assertTableColumn(t *testing.T, database *sql.DB, table, column string) {
	t.Helper()
	rows, err := database.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return
		}
	}
	t.Fatalf("required column %s.%s is missing", table, column)
}
