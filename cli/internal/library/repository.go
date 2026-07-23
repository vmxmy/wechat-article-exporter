package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
)

type AccountRecord struct {
	ID            domain.AccountID
	FakeID        string
	Name          string
	Alias         string
	Description   string
	AvatarURL     string
	ServiceType   int
	Completed     bool
	MessageCount  int
	ArticleCount  int
	UpstreamTotal int
	LastSyncAt    time.Time
}

type ArticleRecord struct {
	ID            domain.ArticleID
	AccountID     domain.AccountID
	Aid           string
	Title         string
	Author        string
	Digest        string
	CanonicalURL  string
	CoverURL      string
	PublishedAt   time.Time
	UpdatedAt     time.Time
	MessageType   int
	State         string
	Deleted       bool
	Paid          bool
	Single        bool
	ContentStatus string
}

type AlbumRecord struct {
	ID           domain.AlbumID
	AccountID    domain.AccountID
	UpstreamID   string
	Title        string
	Description  string
	ArticleCount int
	Paid         bool
}

type ExportRecord struct {
	ID                   domain.ExportID
	JobID                domain.JobID
	Format               string
	Manifest             any
	OutputRoot           string
	OutputAuthorization  *domain.ExportOutputAuthorization
	State                string
	CreatedAt            time.Time
	CompletedAt          time.Time
	Provenance           any
	ProvenancePath       string
	ProvenanceSHA256     string
	ProvenanceState      string
	ProvenanceError      string
	ProvenanceGeneration int64
}

type ExportFileRecord struct {
	ExportID     domain.ExportID
	ArticleID    domain.ArticleID
	RelativePath string
	SizeBytes    int64
	SHA256       string
	MediaType    string
	Status       string
}

func (database *Database) UpsertAccount(ctx context.Context, record AccountRecord) error {
	_, err := database.SaveAccount(ctx, domain.Account{ID: record.ID, FakeID: record.FakeID, Name: record.Name,
		Alias: record.Alias, Description: record.Description, AvatarURL: record.AvatarURL, ServiceType: record.ServiceType,
		ArticleCount: record.ArticleCount, LastSyncAt: record.LastSyncAt})
	return err
}

func (database *Database) UpsertArticle(ctx context.Context, record ArticleRecord) error {
	now := time.Now().UnixMilli()
	_, err := database.db.ExecContext(ctx, `INSERT INTO articles(
id, profile_id, account_id, aid, title, author, digest, canonical_url, cover_url, published_at, updated_at_upstream,
message_type, state, is_deleted, is_paid, is_single, content_status, created_at, updated_at)
VALUES(?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, canonical_url) DO UPDATE SET
account_id=excluded.account_id, aid=excluded.aid, title=excluded.title, author=excluded.author,
digest=excluded.digest, cover_url=excluded.cover_url, published_at=excluded.published_at, updated_at_upstream=excluded.updated_at_upstream,
message_type=excluded.message_type, state=excluded.state, is_deleted=excluded.is_deleted,
is_paid=excluded.is_paid, is_single=excluded.is_single, content_status=excluded.content_status, updated_at=excluded.updated_at`,
		record.ID, database.profileID, record.AccountID, record.Aid, record.Title, record.Author, record.Digest,
		record.CanonicalURL, record.CoverURL, nullableTime(record.PublishedAt), nullableTime(record.UpdatedAt), record.MessageType, record.State,
		record.Deleted, record.Paid, record.Single, record.ContentStatus, now, now)
	if err != nil {
		return fmt.Errorf("upsert article: %w", err)
	}
	return nil
}

// ImportLegacyArticleRecord is the transactional persistence primitive used by
// the explicit browser-data migration. The caller has already selected a
// conflict policy; this method writes all identity fields preserved by the
// legacy archive, including appmsg/item indexes omitted by generic upserts.
func (database *Database) ImportLegacyArticleRecord(ctx context.Context, record ArticleRecord, appMsgID int64, itemIndex int) error {
	now := time.Now().UnixMilli()
	_, err := database.db.ExecContext(ctx, `INSERT INTO articles(
id, profile_id, account_id, aid, appmsg_id, item_index, title, author, digest, canonical_url, cover_url,
published_at, updated_at_upstream, message_type, state, is_deleted, is_paid, is_single, content_status, created_at, updated_at)
VALUES(?, ?, NULLIF(?, ''), ?, NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, canonical_url) DO UPDATE SET
account_id=excluded.account_id, aid=excluded.aid, appmsg_id=excluded.appmsg_id, item_index=excluded.item_index,
title=excluded.title, author=excluded.author, digest=excluded.digest, cover_url=excluded.cover_url,
published_at=excluded.published_at, updated_at_upstream=excluded.updated_at_upstream,
message_type=excluded.message_type, state=excluded.state, is_deleted=excluded.is_deleted,
is_paid=excluded.is_paid, is_single=excluded.is_single,
content_status=CASE WHEN articles.content_status='available' THEN articles.content_status ELSE excluded.content_status END,
updated_at=excluded.updated_at`,
		record.ID, database.profileID, record.AccountID, record.Aid, appMsgID, itemIndex, record.Title, record.Author,
		record.Digest, record.CanonicalURL, record.CoverURL, nullableTime(record.PublishedAt), nullableTime(record.UpdatedAt),
		record.MessageType, record.State, record.Deleted, record.Paid, record.Single, record.ContentStatus, now, now)
	if err != nil {
		return fmt.Errorf("import legacy article record: %w", err)
	}
	return nil
}

func (database *Database) UpsertAlbum(ctx context.Context, record AlbumRecord) error {
	now := time.Now().UnixMilli()
	_, err := database.db.ExecContext(ctx, `INSERT INTO albums(
id, profile_id, account_id, upstream_id, title, description, article_count, is_paid, created_at, updated_at)
VALUES(?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, upstream_id) DO UPDATE SET
account_id=excluded.account_id, title=excluded.title, description=excluded.description,
article_count=excluded.article_count, is_paid=excluded.is_paid, updated_at=excluded.updated_at`,
		record.ID, database.profileID, record.AccountID, record.UpstreamID, record.Title, record.Description,
		record.ArticleCount, record.Paid, now, now)
	if err != nil {
		return fmt.Errorf("upsert album: %w", err)
	}
	return nil
}

func (database *Database) LinkArticleAlbum(ctx context.Context, articleID domain.ArticleID, albumID domain.AlbumID, ordinal int) error {
	_, err := database.db.ExecContext(ctx, `INSERT INTO article_albums(article_id, album_id, ordinal) VALUES(?, ?, ?)
ON CONFLICT(article_id, album_id) DO UPDATE SET ordinal=excluded.ordinal`, articleID, albumID, ordinal)
	return err
}

func (database *Database) UpsertExport(ctx context.Context, record ExportRecord) error {
	manifest, err := json.Marshal(record.Manifest)
	if err != nil {
		return fmt.Errorf("encode export manifest: %w", err)
	}
	created := record.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	authorization, err := json.Marshal(record.OutputAuthorization)
	if err != nil {
		return fmt.Errorf("encode export output authorization: %w", err)
	}
	_, err = database.db.ExecContext(ctx, `INSERT INTO exports(
id, profile_id, job_id, format, manifest_json, output_root, output_authorization_json, state, created_at, provenance_state)
VALUES(?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, 'pending')
ON CONFLICT(id) DO UPDATE SET job_id=excluded.job_id, format=excluded.format,
manifest_json=excluded.manifest_json, output_root=excluded.output_root,
output_authorization_json=excluded.output_authorization_json, state=excluded.state`,
		record.ID, database.profileID, record.JobID, record.Format, string(manifest), record.OutputRoot,
		string(authorization), record.State, created.UnixMilli())
	return err
}

func (database *Database) QueryExports(ctx context.Context, offset, limit int) (domain.Page[domain.ExportID], error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM exports WHERE profile_id=?", database.profileID).Scan(&total); err != nil {
		return domain.Page[domain.ExportID]{}, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT id FROM exports WHERE profile_id=? ORDER BY created_at DESC, id LIMIT ? OFFSET ?`,
		database.profileID, limit, offset)
	if err != nil {
		return domain.Page[domain.ExportID]{}, err
	}
	defer rows.Close()
	items := make([]domain.ExportID, 0)
	for rows.Next() {
		var id domain.ExportID
		if err := rows.Scan(&id); err != nil {
			return domain.Page[domain.ExportID]{}, err
		}
		items = append(items, id)
	}
	return domain.Page[domain.ExportID]{Items: items, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

func (database *Database) QueryExportRecords(ctx context.Context, offset, limit int) (domain.Page[ExportRecord], error) {
	limit, offset = normalizePage(limit, offset)
	var total int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM exports WHERE profile_id=?", database.profileID).Scan(&total); err != nil {
		return domain.Page[ExportRecord]{}, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT id, COALESCE(job_id, ''), format, manifest_json,
output_root, output_authorization_json, state, created_at, completed_at, provenance_json,
provenance_path, provenance_sha256, provenance_state, provenance_error, provenance_generation
FROM exports WHERE profile_id=? ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, database.profileID, limit, offset)
	if err != nil {
		return domain.Page[ExportRecord]{}, err
	}
	defer rows.Close()
	items := make([]ExportRecord, 0)
	for rows.Next() {
		record, scanErr := scanExportRecord(rows)
		if scanErr != nil {
			return domain.Page[ExportRecord]{}, scanErr
		}
		items = append(items, record)
	}
	return domain.Page[ExportRecord]{Items: items, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

func (database *Database) GetExport(ctx context.Context, id domain.ExportID) (ExportRecord, error) {
	row := database.db.QueryRowContext(ctx, `SELECT id, COALESCE(job_id, ''), format, manifest_json,
output_root, output_authorization_json, state, created_at, completed_at, provenance_json,
provenance_path, provenance_sha256, provenance_state, provenance_error, provenance_generation
FROM exports WHERE profile_id=? AND id=?`, database.profileID, id)
	return scanExportRecord(row)
}

type exportRecordScanner interface {
	Scan(...any) error
}

func scanExportRecord(scanner exportRecordScanner) (ExportRecord, error) {
	var record ExportRecord
	var manifest, authorization, provenance string
	var created int64
	var completed sql.NullInt64
	err := scanner.Scan(
		&record.ID, &record.JobID, &record.Format, &manifest, &record.OutputRoot, &authorization, &record.State,
		&created, &completed, &provenance, &record.ProvenancePath, &record.ProvenanceSHA256,
		&record.ProvenanceState, &record.ProvenanceError, &record.ProvenanceGeneration,
	)
	if err != nil {
		return ExportRecord{}, err
	}
	if err := json.Unmarshal([]byte(manifest), &record.Manifest); err != nil {
		return ExportRecord{}, fmt.Errorf("decode export manifest: %w", err)
	}
	if authorization != "" && authorization != "null" && authorization != "{}" {
		var decoded domain.ExportOutputAuthorization
		if err := json.Unmarshal([]byte(authorization), &decoded); err != nil {
			return ExportRecord{}, fmt.Errorf("decode export output authorization: %w", err)
		}
		record.OutputAuthorization = &decoded
	}
	if provenance != "" && provenance != "{}" {
		if err := json.Unmarshal([]byte(provenance), &record.Provenance); err != nil {
			return ExportRecord{}, fmt.Errorf("decode export provenance: %w", err)
		}
	}
	record.CreatedAt = time.UnixMilli(created)
	if completed.Valid {
		record.CompletedAt = time.UnixMilli(completed.Int64)
	}
	return record, nil
}

func (database *Database) ListExportFiles(ctx context.Context, id domain.ExportID) ([]ExportFileRecord, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT ef.export_id, COALESCE(ef.article_id, ''), ef.relative_path, ef.size_bytes, ef.sha256, ef.media_type, ef.status
FROM export_files ef JOIN exports e ON e.id=ef.export_id
WHERE e.profile_id=? AND ef.export_id=? ORDER BY ef.relative_path`, database.profileID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]ExportFileRecord, 0)
	for rows.Next() {
		var record ExportFileRecord
		if err := rows.Scan(&record.ExportID, &record.ArticleID, &record.RelativePath, &record.SizeBytes, &record.SHA256,
			&record.MediaType, &record.Status); err != nil {
			return nil, err
		}
		files = append(files, record)
	}
	return files, rows.Err()
}

func (database *Database) UpdateExportStateByJob(
	ctx context.Context,
	jobID domain.JobID,
	expectedGeneration int64,
	state string,
	completedAt time.Time,
) error {
	if expectedGeneration <= 0 {
		return errors.New("positive export generation is required")
	}
	var completed any
	switch domain.JobState(state) {
	case domain.JobCompleted, domain.JobPartial, domain.JobFailed, domain.JobCancelled:
		if completedAt.IsZero() {
			completedAt = time.Now()
		}
		completed = completedAt.UnixMilli()
	}
	result, err := database.db.ExecContext(ctx, `UPDATE exports SET state=?, completed_at=?
WHERE profile_id=? AND job_id=? AND provenance_generation=? AND provenance_state<>'writing'`,
		state, completed, database.profileID, jobID, expectedGeneration)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.ErrStateChanged
	}
	return nil
}

func (database *Database) UpsertExportFile(ctx context.Context, record ExportFileRecord) error {
	if record.ExportID == "" || record.RelativePath == "" {
		return errors.New("export ID and relative path are required")
	}
	if record.Status == "" {
		record.Status = "written"
	}
	var exportCount, articleCount int
	if err := database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM exports WHERE id=? AND profile_id=?`,
		record.ExportID, database.profileID).Scan(&exportCount); err != nil {
		return err
	}
	if exportCount != 1 {
		return fmt.Errorf("export %s does not belong to profile %s", record.ExportID, database.profileID)
	}
	if record.ArticleID != "" {
		if err := database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE id=? AND profile_id=?`,
			record.ArticleID, database.profileID).Scan(&articleCount); err != nil {
			return err
		}
		if articleCount != 1 {
			return fmt.Errorf("article %s does not belong to profile %s", record.ArticleID, database.profileID)
		}
	}
	_, err := database.db.ExecContext(ctx, `INSERT INTO export_files(
id, export_id, article_id, relative_path, size_bytes, sha256, media_type, status) VALUES(?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)
ON CONFLICT(export_id, relative_path) DO UPDATE SET size_bytes=excluded.size_bytes,
sha256=excluded.sha256, media_type=excluded.media_type, article_id=excluded.article_id, status=excluded.status`,
		uuid.NewString(), record.ExportID, record.ArticleID, record.RelativePath, record.SizeBytes, record.SHA256, record.MediaType, record.Status)
	return err
}

func (database *Database) ClaimExportProvenance(
	ctx context.Context,
	id domain.ExportID,
	expectedGeneration int64,
	staleBefore time.Time,
) (int64, bool, error) {
	if expectedGeneration <= 0 {
		return 0, false, errors.New("positive export generation is required")
	}
	if staleBefore.IsZero() {
		staleBefore = time.Now().Add(-5 * time.Minute)
	}
	now := time.Now()
	var generation int64
	err := database.db.QueryRowContext(ctx, `UPDATE exports SET provenance_state='writing', provenance_error='',
provenance_generation=provenance_generation + CASE WHEN provenance_state IN ('writing', 'failed') THEN 1 ELSE 0 END,
provenance_claimed_at=? WHERE profile_id=? AND id=? AND (
  provenance_state IN ('pending', 'failed') OR provenance_state='writing' AND provenance_claimed_at<?
) AND provenance_generation=? RETURNING provenance_generation`, now.UnixMilli(), database.profileID, id,
		staleBefore.UnixMilli(), expectedGeneration).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return generation, true, nil
}

func (database *Database) CompleteExportProvenance(
	ctx context.Context,
	id domain.ExportID,
	generation int64,
	provenance any,
	path, sha256 string,
) error {
	encoded, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("encode export provenance: %w", err)
	}
	result, err := database.db.ExecContext(ctx, `UPDATE exports SET provenance_json=?, provenance_path=?,
provenance_sha256=?, provenance_state='ready', provenance_error='', provenance_claimed_at=NULL
WHERE profile_id=? AND id=? AND provenance_generation=? AND provenance_state='writing'`,
		string(encoded), path, sha256, database.profileID, id, generation)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.ErrStateChanged
	}
	return nil
}

func (database *Database) FailExportProvenance(ctx context.Context, id domain.ExportID, generation int64, message string) error {
	result, err := database.db.ExecContext(ctx, `UPDATE exports SET provenance_state='failed', provenance_error=?,
provenance_claimed_at=NULL WHERE profile_id=? AND id=? AND provenance_generation=? AND provenance_state='writing'`,
		redactString(message), database.profileID, id, generation)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.ErrStateChanged
	}
	return nil
}

func (database *Database) MarkExportProvenanceUnavailable(
	ctx context.Context,
	id domain.ExportID,
	generation int64,
	message string,
) error {
	result, err := database.db.ExecContext(ctx, `UPDATE exports SET provenance_state='unavailable', provenance_error=?,
provenance_claimed_at=NULL WHERE profile_id=? AND id=? AND provenance_generation=? AND provenance_state='writing'`,
		redactString(message), database.profileID, id, generation)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.ErrStateChanged
	}
	return nil
}

func (database *Database) GetExportByJob(ctx context.Context, jobID domain.JobID) (ExportRecord, error) {
	var id domain.ExportID
	if err := database.db.QueryRowContext(ctx, `SELECT id FROM exports WHERE profile_id=? AND job_id=?`,
		database.profileID, jobID).Scan(&id); err != nil {
		return ExportRecord{}, err
	}
	return database.GetExport(ctx, id)
}

func (database *Database) PendingTerminalExports(ctx context.Context) ([]ExportRecord, error) {
	return database.PendingTerminalExportsPage(ctx, "", 100)
}

func (database *Database) PendingTerminalExportsPage(ctx context.Context, afterID domain.ExportID, limit int) ([]ExportRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := database.db.QueryContext(ctx, `SELECT id, COALESCE(job_id, ''), format, manifest_json,
output_root, output_authorization_json, state, created_at, completed_at, provenance_json,
provenance_path, provenance_sha256, provenance_state, provenance_error, provenance_generation
FROM exports WHERE profile_id=? AND id>?
AND state IN (?, ?, ?, ?) AND (
  provenance_state IN ('pending', 'failed') OR provenance_state='writing' AND provenance_claimed_at<?
) ORDER BY id LIMIT ?`, database.profileID, afterID,
		domain.JobCompleted, domain.JobPartial, domain.JobFailed, domain.JobCancelled,
		time.Now().Add(-5*time.Minute).UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]ExportRecord, 0, limit)
	for rows.Next() {
		var record ExportRecord
		var manifest, authorization, provenance string
		var created int64
		var completed sql.NullInt64
		if err := rows.Scan(&record.ID, &record.JobID, &record.Format, &manifest, &record.OutputRoot, &authorization, &record.State,
			&created, &completed, &provenance, &record.ProvenancePath, &record.ProvenanceSHA256,
			&record.ProvenanceState, &record.ProvenanceError, &record.ProvenanceGeneration); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(manifest), &record.Manifest); err != nil {
			return nil, fmt.Errorf("decode export manifest %s: %w", record.ID, err)
		}
		if authorization != "" && authorization != "null" && authorization != "{}" {
			var decoded domain.ExportOutputAuthorization
			if err := json.Unmarshal([]byte(authorization), &decoded); err != nil {
				return nil, fmt.Errorf("decode export output authorization %s: %w", record.ID, err)
			}
			record.OutputAuthorization = &decoded
		}
		if provenance != "" && provenance != "{}" {
			if err := json.Unmarshal([]byte(provenance), &record.Provenance); err != nil {
				return nil, fmt.Errorf("decode export provenance %s: %w", record.ID, err)
			}
		}
		record.CreatedAt = time.UnixMilli(created)
		if completed.Valid {
			record.CompletedAt = time.UnixMilli(completed.Int64)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UnixMilli()
}

func countRows(ctx context.Context, transaction *sql.Tx, table string) (int, error) {
	var count int
	err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	return count, err
}
