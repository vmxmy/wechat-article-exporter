package library

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	_ "modernc.org/sqlite"
)

const (
	CurrentSchemaVersion = 10
	MinimumSchemaVersion = 1
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Database struct {
	db           *sql.DB
	profileID    domain.ProfileID
	path         string
	objectsReady func() bool
}

func (database *Database) SetObjectStoreReadyProbe(probe func() bool) {
	database.objectsReady = probe
}

type OpenOptions struct {
	Path        string
	ProfileID   domain.ProfileID
	ProfileName string
	BusyTimeout time.Duration
}

func Open(ctx context.Context, options OpenOptions) (*Database, error) {
	if options.Path == "" {
		return nil, errors.New("database path is required")
	}
	if options.ProfileID == "" {
		options.ProfileID = "default"
	}
	if options.ProfileName == "" {
		options.ProfileName = string(options.ProfileID)
	}
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = 5 * time.Second
	}
	if err := ensureParent(options.Path); err != nil {
		return nil, err
	}
	dsn := options.Path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(" +
		strconv.FormatInt(options.BusyTimeout.Milliseconds(), 10) + ")"
	databaseSQL, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	databaseSQL.SetMaxOpenConns(4)
	databaseSQL.SetMaxIdleConns(4)
	databaseSQL.SetConnMaxLifetime(0)
	database := &Database{db: databaseSQL, profileID: options.ProfileID, path: options.Path}
	if err := database.migrate(ctx); err != nil {
		databaseSQL.Close()
		return nil, err
	}
	now := time.Now().UnixMilli()
	if _, err := databaseSQL.ExecContext(ctx, `
INSERT INTO profiles(id, name, created_at, updated_at) VALUES(?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`,
		options.ProfileID, options.ProfileName, now, now); err != nil {
		databaseSQL.Close()
		return nil, fmt.Errorf("ensure profile row: %w", err)
	}
	return database, nil
}

func ensureParent(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	return nil
}

func (database *Database) Close() error { return database.db.Close() }
func (database *Database) Path() string { return database.path }

func (database *Database) Backup(ctx context.Context, destination string) error {
	if destination == "" {
		return errors.New("backup destination is required")
	}
	if err := ensureParent(destination); err != nil {
		return err
	}
	if err := database.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(FULL)").Scan(new(int), new(int), new(int)); err != nil {
		return fmt.Errorf("checkpoint database before backup: %w", err)
	}
	temporary := destination + ".tmp"
	_ = os.Remove(temporary)
	_, err := database.db.ExecContext(ctx, "VACUUM INTO ?", temporary)
	if err != nil {
		return fmt.Errorf("create database backup: %w", err)
	}
	if file, openErr := os.OpenFile(temporary, os.O_RDWR, 0); openErr == nil {
		if syncErr := file.Sync(); syncErr != nil {
			file.Close()
			_ = os.Remove(temporary)
			return fmt.Errorf("sync database backup: %w", syncErr)
		}
		_ = file.Close()
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit database backup: %w", err)
	}
	return nil
}

func (database *Database) WithTx(ctx context.Context, operation func(*sql.Tx) error) error {
	transaction, err := database.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := operation(transaction); err != nil {
		_ = transaction.Rollback()
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (database *Database) migrate(ctx context.Context) error {
	if _, err := database.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
version INTEGER PRIMARY KEY,
name TEXT NOT NULL,
applied_at INTEGER NOT NULL
)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var maximum sql.NullInt64
	if err := database.db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&maximum); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if maximum.Valid && maximum.Int64 > CurrentSchemaVersion {
		return fmt.Errorf("database schema %d is newer than supported schema %d; upgrade the CLI", maximum.Int64, CurrentSchemaVersion)
	}
	if maximum.Valid && maximum.Int64 > 0 && maximum.Int64 < CurrentSchemaVersion {
		backup := database.path + ".pre-migration-v" + strconv.FormatInt(maximum.Int64, 10) + ".sqlite3"
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			if err := database.Backup(ctx, backup); err != nil {
				return fmt.Errorf("backup database before migration: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("inspect migration backup: %w", err)
		}
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "_", 2)[0])
		if parseErr != nil {
			return fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		if maximum.Valid && int64(version) <= maximum.Int64 {
			continue
		}
		contents, readErr := migrationFiles.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), readErr)
		}
		statements, splitErr := splitSQLStatements(string(contents))
		if splitErr != nil {
			return fmt.Errorf("parse migration %s: %w", entry.Name(), splitErr)
		}
		if err := database.WithTx(ctx, func(transaction *sql.Tx) error {
			for _, statement := range statements {
				if _, executeErr := transaction.ExecContext(ctx, statement); executeErr != nil {
					return fmt.Errorf("apply migration %s: %w", entry.Name(), executeErr)
				}
			}
			_, executeErr := transaction.ExecContext(ctx,
				"INSERT INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)", version, entry.Name(), time.Now().UnixMilli())
			return executeErr
		}); err != nil {
			return err
		}
	}
	return nil
}

func splitSQLStatements(source string) ([]string, error) {
	statements := make([]string, 0)
	var builder strings.Builder
	var quote rune
	lineComment := false
	blockComment := false
	runes := []rune(source)
	for index := 0; index < len(runes); index++ {
		current := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}
		if lineComment {
			if current == '\n' {
				lineComment = false
				builder.WriteRune(current)
			}
			continue
		}
		if blockComment {
			if current == '*' && next == '/' {
				blockComment = false
				index++
			}
			continue
		}
		if quote != 0 {
			builder.WriteRune(current)
			if current == quote {
				if next == quote {
					builder.WriteRune(next)
					index++
				} else {
					quote = 0
				}
			}
			continue
		}
		if current == '-' && next == '-' {
			lineComment = true
			index++
			continue
		}
		if current == '/' && next == '*' {
			blockComment = true
			index++
			continue
		}
		if current == '\'' || current == '"' || current == '`' {
			quote = current
			builder.WriteRune(current)
			continue
		}
		if current == ';' {
			statement := strings.TrimSpace(builder.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			builder.Reset()
			continue
		}
		builder.WriteRune(current)
	}
	if quote != 0 || blockComment {
		return nil, errors.New("unterminated SQL quote or comment")
	}
	if statement := strings.TrimSpace(builder.String()); statement != "" {
		statements = append(statements, statement)
	}
	return statements, nil
}

func (database *Database) QueryAccounts(ctx context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	limit, offset := normalizePage(query.Limit, query.Offset)
	keyword := "%" + strings.TrimSpace(query.Keyword) + "%"
	where := "profile_id = ?"
	arguments := []any{database.profileID}
	if len(query.IDs) > 0 {
		placeholders := make([]string, 0, len(query.IDs))
		seen := make(map[domain.AccountID]struct{}, len(query.IDs))
		for _, id := range query.IDs {
			if strings.TrimSpace(string(id)) == "" {
				return domain.Page[domain.Account]{}, errors.New("account identifier is required")
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			placeholders = append(placeholders, "?")
			arguments = append(arguments, id)
		}
		if len(placeholders) == 0 {
			return domain.Page[domain.Account]{}, errors.New("account identifier is required")
		}
		where += " AND id IN (" + strings.Join(placeholders, ",") + ")"
	}
	if query.Keyword != "" {
		where += " AND (nickname LIKE ? OR alias LIKE ? OR fakeid LIKE ? OR signature LIKE ?)"
		arguments = append(arguments, keyword, keyword, keyword, keyword)
	}
	var total int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE "+where, arguments...).Scan(&total); err != nil {
		return domain.Page[domain.Account]{}, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT id, fakeid, nickname, alias, signature, avatar_url, service_type,
last_sync_at, article_count, message_count, upstream_total, sync_cursor, completed
FROM accounts WHERE `+where+` ORDER BY nickname COLLATE NOCASE, fakeid LIMIT ? OFFSET ?`, append(arguments, limit, offset)...)
	if err != nil {
		return domain.Page[domain.Account]{}, err
	}
	defer rows.Close()
	items := make([]domain.Account, 0)
	for rows.Next() {
		var item domain.Account
		var lastSync sql.NullInt64
		var cursor string
		if err := rows.Scan(&item.ID, &item.FakeID, &item.Name, &item.Alias, &item.Description, &item.AvatarURL,
			&item.ServiceType, &lastSync, &item.ArticleCount, &item.MessageCount, &item.UpstreamTotal, &cursor,
			&item.SyncCompleted); err != nil {
			return domain.Page[domain.Account]{}, err
		}
		item.LastSyncAt = unixMillis(lastSync)
		item.SyncCursor, _ = strconv.Atoi(cursor)
		items = append(items, item)
	}
	return domain.Page[domain.Account]{Items: items, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

func (database *Database) QueryArticles(ctx context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	return database.queryArticlesAdvanced(ctx, query)
}

func (database *Database) QueryAlbums(ctx context.Context, query domain.AlbumQuery) (domain.Page[domain.Album], error) {
	limit, offset := normalizePage(query.Limit, query.Offset)
	where := []string{"profile_id = ?"}
	arguments := []any{database.profileID}
	if query.AccountID != "" {
		where = append(where, "account_id = ?")
		arguments = append(arguments, query.AccountID)
	}
	if query.Keyword != "" {
		where = append(where, "title LIKE ?")
		arguments = append(arguments, "%"+strings.TrimSpace(query.Keyword)+"%")
	}
	predicate := strings.Join(where, " AND ")
	var total int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM albums WHERE "+predicate, arguments...).Scan(&total); err != nil {
		return domain.Page[domain.Album]{}, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT id, COALESCE(account_id, ''), upstream_id, title,
description, article_count, is_paid
FROM albums WHERE `+predicate+` ORDER BY title COLLATE NOCASE, id LIMIT ? OFFSET ?`, append(arguments, limit, offset)...)
	if err != nil {
		return domain.Page[domain.Album]{}, err
	}
	defer rows.Close()
	items := make([]domain.Album, 0)
	for rows.Next() {
		var item domain.Album
		if err := rows.Scan(&item.ID, &item.AccountID, &item.UpstreamID, &item.Name, &item.Description,
			&item.ArticleCount, &item.Paid); err != nil {
			return domain.Page[domain.Album]{}, err
		}
		items = append(items, item)
	}
	return domain.Page[domain.Album]{Items: items, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

func (database *Database) GetAlbumForAccount(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Album, error) {
	var item domain.Album
	err := database.db.QueryRowContext(ctx, `SELECT id, COALESCE(account_id, ''), upstream_id, title,
description, article_count, is_paid
FROM albums WHERE profile_id=? AND account_id=? AND id=?`, database.profileID, accountID, albumID).Scan(
		&item.ID, &item.AccountID, &item.UpstreamID, &item.Name, &item.Description, &item.ArticleCount, &item.Paid)
	return item, err
}

func (database *Database) GetAlbum(ctx context.Context, albumID domain.AlbumID) (domain.Album, error) {
	var item domain.Album
	err := database.db.QueryRowContext(ctx, `SELECT id, COALESCE(account_id, ''), upstream_id, title,
description, article_count, is_paid FROM albums WHERE profile_id=? AND id=?`, database.profileID, albumID).Scan(
		&item.ID, &item.AccountID, &item.UpstreamID, &item.Name, &item.Description, &item.ArticleCount, &item.Paid)
	return item, err
}

func (database *Database) StorageStatus(ctx context.Context) (domain.StorageStatus, error) {
	status := domain.StorageStatus{DatabaseAvailable: true}
	if database.objectsReady != nil {
		status.ObjectStoreReady = database.objectsReady()
	}
	queries := []struct {
		query string
		value *int64
	}{
		{"SELECT COUNT(*) FROM accounts WHERE profile_id = ?", &status.Accounts},
		{"SELECT COUNT(*) FROM articles WHERE profile_id = ?", &status.Articles},
		{"SELECT COUNT(*) FROM albums WHERE profile_id = ?", &status.Albums},
		{"SELECT COUNT(*) FROM jobs WHERE profile_id = ?", &status.Jobs},
	}
	for _, item := range queries {
		if err := database.db.QueryRowContext(ctx, item.query, database.profileID).Scan(item.value); err != nil {
			return domain.StorageStatus{}, err
		}
	}
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM objects").Scan(&status.Objects, &status.ObjectBytes); err != nil {
		return domain.StorageStatus{}, err
	}
	return status, nil
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func unixMillis(value sql.NullInt64) time.Time {
	if !value.Valid || value.Int64 <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value.Int64)
}
