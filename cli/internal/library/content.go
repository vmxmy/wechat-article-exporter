package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

type ContentVersion struct {
	ID             string
	ArticleID      domain.ArticleID
	ObjectDigest   string
	Kind           string
	MediaType      string
	SourceURL      string
	Classification string
	CommentID      string
	CapturedAt     time.Time
	Current        bool
}

type DebugIncident struct {
	ID             string
	Operation      string
	Classification string
	RequestID      string
	ObjectDigest   string
	Summary        string
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

type ResourceRecord struct {
	ID           string
	SourceURL    string
	ObjectDigest string
	MediaType    string
	Status       string
}

type ArticleResourceRecord struct {
	ArticleID   domain.ArticleID
	ResourceID  string
	Role        string
	Ordinal     int
	OriginalURL string
}

func (database *Database) CurrentContent(ctx context.Context, articleID domain.ArticleID, kind string) (ContentVersion, error) {
	var record ContentVersion
	var captured int64
	var current bool
	err := database.db.QueryRowContext(ctx, `SELECT cv.id, cv.article_id, cv.object_digest, cv.kind, cv.media_type,
cv.source_url, cv.classification, cv.comment_id, cv.captured_at, cv.is_current
FROM content_versions cv JOIN articles a ON a.id=cv.article_id
WHERE a.profile_id=? AND cv.article_id=? AND cv.kind=? AND cv.is_current=1
ORDER BY cv.captured_at DESC, cv.id DESC LIMIT 1`, database.profileID, articleID, kind).Scan(
		&record.ID, &record.ArticleID, &record.ObjectDigest, &record.Kind, &record.MediaType,
		&record.SourceURL, &record.Classification, &record.CommentID, &captured, &current,
	)
	if err != nil {
		return ContentVersion{}, err
	}
	record.CapturedAt = time.UnixMilli(captured)
	record.Current = current
	return record, nil
}

func (database *Database) CommitContent(
	ctx context.Context,
	articleID domain.ArticleID,
	object objects.Object,
	kind, sourceURL, classification, commentID string,
	capturedAt time.Time,
) (ContentVersion, error) {
	if articleID == "" || object.Digest == "" {
		return ContentVersion{}, errors.New("article ID and object digest are required")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ContentVersion{}, errors.New("content kind is required")
	}
	if capturedAt.IsZero() {
		capturedAt = time.Now()
	}
	record := ContentVersion{
		ID: uuid.NewString(), ArticleID: articleID, ObjectDigest: object.Digest, Kind: kind,
		MediaType: object.MediaType, SourceURL: sourceURL, Classification: classification,
		CommentID: commentID, CapturedAt: capturedAt, Current: true,
	}
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		var exists int
		if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE id=? AND profile_id=?`,
			articleID, database.profileID).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return fmt.Errorf("article %s does not exist", articleID)
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO objects(digest, size_bytes, media_type, created_at)
VALUES(?, ?, ?, ?) ON CONFLICT(digest) DO UPDATE SET
size_bytes=excluded.size_bytes, media_type=CASE WHEN excluded.media_type<>'' THEN excluded.media_type ELSE objects.media_type END`,
			object.Digest, object.Size, object.MediaType, capturedAt.UnixMilli()); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE content_versions SET is_current=0
WHERE article_id=? AND kind=? AND is_current=1`, articleID, kind); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO content_versions(
id, article_id, object_digest, kind, media_type, source_url, classification, comment_id, captured_at, is_current)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`, record.ID, articleID, object.Digest, kind, object.MediaType,
			sourceURL, classification, commentID, capturedAt.UnixMilli()); err != nil {
			return err
		}
		_, err := transaction.ExecContext(ctx, `UPDATE articles SET content_status='available', state=?,
updated_at=? WHERE id=? AND profile_id=?`, classification, capturedAt.UnixMilli(), articleID, database.profileID)
		return err
	})
	if err != nil {
		return ContentVersion{}, fmt.Errorf("commit article content: %w", err)
	}
	return record, nil
}

func (database *Database) MarkArticleState(ctx context.Context, articleID domain.ArticleID, state string, deleted bool) error {
	result, err := database.db.ExecContext(ctx, `UPDATE articles SET state=?, is_deleted=?, content_status='missing', updated_at=?
WHERE id=? AND profile_id=?`, state, deleted, time.Now().UnixMilli(), articleID, database.profileID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (database *Database) RecordDebugIncident(ctx context.Context, incident DebugIncident) (DebugIncident, error) {
	if incident.ID == "" {
		incident.ID = uuid.NewString()
	}
	if incident.CreatedAt.IsZero() {
		incident.CreatedAt = time.Now()
	}
	_, err := database.db.ExecContext(ctx, `INSERT INTO debug_incidents(
id, profile_id, operation, classification, request_id, object_digest, summary, created_at, expires_at)
VALUES(?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)`, incident.ID, database.profileID, incident.Operation,
		incident.Classification, incident.RequestID, incident.ObjectDigest, incident.Summary,
		incident.CreatedAt.UnixMilli(), nullableTime(incident.ExpiresAt))
	if err != nil {
		return DebugIncident{}, fmt.Errorf("record debug incident: %w", err)
	}
	return incident, nil
}

func (database *Database) ResourceByURL(ctx context.Context, sourceURL string) (ResourceRecord, error) {
	var record ResourceRecord
	err := database.db.QueryRowContext(ctx, `SELECT id, source_url, COALESCE(object_digest, ''), media_type, status
FROM resources WHERE profile_id=? AND source_url=?`, database.profileID, sourceURL).Scan(
		&record.ID, &record.SourceURL, &record.ObjectDigest, &record.MediaType, &record.Status,
	)
	return record, err
}

// ListArticleResources returns every persisted mapping for one local article.
// Migration verification uses this read-only view to compare the imported
// library with the source archive without exposing SQLite internals.
func (database *Database) ListArticleResources(ctx context.Context, articleID domain.ArticleID) ([]ArticleResourceRecord, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT ar.article_id, ar.resource_id, ar.role, ar.ordinal, ar.original_url
FROM article_resources ar JOIN articles a ON a.id=ar.article_id
WHERE a.profile_id=? AND ar.article_id=? ORDER BY ar.role, ar.ordinal, ar.resource_id`, database.profileID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ArticleResourceRecord, 0)
	for rows.Next() {
		var item ArticleResourceRecord
		if err := rows.Scan(&item.ArticleID, &item.ResourceID, &item.Role, &item.Ordinal, &item.OriginalURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) CommitResource(
	ctx context.Context,
	articleID domain.ArticleID,
	sourceURL, role string,
	ordinal int,
	object objects.Object,
) (ResourceRecord, error) {
	if articleID == "" || strings.TrimSpace(sourceURL) == "" || object.Digest == "" {
		return ResourceRecord{}, errors.New("article ID, source URL, and object digest are required")
	}
	now := time.Now()
	record := ResourceRecord{ID: uuid.NewString(), SourceURL: sourceURL, ObjectDigest: object.Digest,
		MediaType: object.MediaType, Status: "available"}
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO objects(digest, size_bytes, media_type, created_at)
VALUES(?, ?, ?, ?) ON CONFLICT(digest) DO UPDATE SET
size_bytes=excluded.size_bytes, media_type=CASE WHEN excluded.media_type<>'' THEN excluded.media_type ELSE objects.media_type END`,
			object.Digest, object.Size, object.MediaType, now.UnixMilli()); err != nil {
			return err
		}
		_, err := transaction.ExecContext(ctx, `INSERT INTO resources(
id, profile_id, source_url, object_digest, media_type, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, 'available', ?, ?)
ON CONFLICT(profile_id, source_url) DO UPDATE SET object_digest=excluded.object_digest,
media_type=excluded.media_type, status='available', updated_at=excluded.updated_at`,
			record.ID, database.profileID, sourceURL, object.Digest, object.MediaType, now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return err
		}
		if err := transaction.QueryRowContext(ctx, `SELECT id FROM resources WHERE profile_id=? AND source_url=?`,
			database.profileID, sourceURL).Scan(&record.ID); err != nil {
			return err
		}
		_, err = transaction.ExecContext(ctx, `INSERT INTO article_resources(article_id, resource_id, role, ordinal, original_url)
VALUES(?, ?, ?, ?, ?) ON CONFLICT(article_id, resource_id, role, ordinal) DO UPDATE SET original_url=excluded.original_url`,
			articleID, record.ID, role, ordinal, sourceURL)
		return err
	})
	if err != nil {
		return ResourceRecord{}, fmt.Errorf("commit article resource: %w", err)
	}
	return record, nil
}

func (database *Database) MarkResourceMissing(ctx context.Context, articleID domain.ArticleID, sourceURL, role string, ordinal int) error {
	now := time.Now().UnixMilli()
	resourceID := uuid.NewString()
	return database.WithTx(ctx, func(transaction *sql.Tx) error {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO resources(id, profile_id, source_url, status, created_at, updated_at)
VALUES(?, ?, ?, 'missing', ?, ?) ON CONFLICT(profile_id, source_url) DO UPDATE SET status='missing', updated_at=excluded.updated_at`,
			resourceID, database.profileID, sourceURL, now, now); err != nil {
			return err
		}
		if err := transaction.QueryRowContext(ctx, `SELECT id FROM resources WHERE profile_id=? AND source_url=?`,
			database.profileID, sourceURL).Scan(&resourceID); err != nil {
			return err
		}
		_, err := transaction.ExecContext(ctx, `INSERT INTO article_resources(article_id, resource_id, role, ordinal, original_url)
VALUES(?, ?, ?, ?, ?) ON CONFLICT(article_id, resource_id, role, ordinal) DO UPDATE SET original_url=excluded.original_url`,
			articleID, resourceID, role, ordinal, sourceURL)
		return err
	})
}
