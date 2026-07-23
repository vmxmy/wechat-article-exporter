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

type ExportSnapshotRecord struct {
	Article   domain.Article
	Content   ContentVersion
	Comments  []CommentRecord
	Resources []ExportSnapshotResource
}

type ExportSnapshotResource struct {
	ArticleResourceRecord
	ObjectDigest string
	MediaType    string
	Status       string
}

// ReadExportSnapshot captures all mutable SQLite input for one queued export
// from a single read transaction. Object bytes remain immutable by digest and
// are verified by the caller after this metadata snapshot commits.
func (database *Database) ReadExportSnapshot(ctx context.Context, articleID domain.ArticleID) (ExportSnapshotRecord, error) {
	transaction, err := database.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ExportSnapshotRecord{}, fmt.Errorf("begin export snapshot: %w", err)
	}
	defer transaction.Rollback()
	var snapshot ExportSnapshotRecord
	var published, updated sql.NullInt64
	err = transaction.QueryRowContext(ctx, `SELECT a.id, COALESCE(a.account_id, ''), a.aid,
COALESCE(a.appmsg_id, 0), COALESCE(a.item_index, 0), a.title, a.author, a.digest,
a.canonical_url, a.cover_url, a.published_at, a.updated_at_upstream, a.message_type, a.state,
a.is_deleted, a.is_paid, a.is_original, a.is_single,
CASE WHEN a.content_status='available' THEN 1 ELSE 0 END,
CASE WHEN EXISTS (SELECT 1 FROM comments c JOIN articles comment_owner ON comment_owner.id=c.article_id
  WHERE c.article_id=a.id AND comment_owner.profile_id=?) THEN 1 ELSE 0 END,
a.wecoin_count, a.media_duration_seconds,
COALESCE(ms.read_count, 0), COALESCE(ms.old_like_count, 0), COALESCE(ms.share_count, 0),
COALESCE(ms.like_count, 0), COALESCE(ms.comment_count, 0)
FROM articles a LEFT JOIN metric_snapshots ms ON ms.id=(
  SELECT latest.id FROM metric_snapshots latest JOIN articles metric_owner ON metric_owner.id=latest.article_id
  WHERE latest.article_id=a.id AND metric_owner.profile_id=? ORDER BY latest.captured_at DESC, latest.id DESC LIMIT 1
) WHERE a.profile_id=? AND a.id=?`, database.profileID, database.profileID, database.profileID, articleID).Scan(
		&snapshot.Article.ID, &snapshot.Article.AccountID, &snapshot.Article.Aid, &snapshot.Article.AppMsgID,
		&snapshot.Article.ItemIndex, &snapshot.Article.Title, &snapshot.Article.Author, &snapshot.Article.Digest,
		&snapshot.Article.CanonicalURL, &snapshot.Article.CoverURL, &published, &updated,
		&snapshot.Article.MessageType, &snapshot.Article.State, &snapshot.Article.Deleted, &snapshot.Article.Paid,
		&snapshot.Article.Original, &snapshot.Article.Single, &snapshot.Article.HasContent,
		&snapshot.Article.HasComments, &snapshot.Article.WeCoinCount, &snapshot.Article.MediaDurationSeconds,
		&snapshot.Article.ReadCount, &snapshot.Article.OldLikeCount, &snapshot.Article.ShareCount,
		&snapshot.Article.LikeCount, &snapshot.Article.CommentCount)
	if err != nil {
		return ExportSnapshotRecord{}, err
	}
	snapshot.Article.PublishedAt = unixMillis(published)
	snapshot.Article.UpdatedAt = unixMillis(updated)

	albumRows, err := transaction.QueryContext(ctx, `SELECT al.id, COALESCE(al.account_id, ''), al.upstream_id,
al.title, al.description, al.article_count, al.is_paid
FROM article_albums aa JOIN albums al ON al.id=aa.album_id
JOIN articles owner ON owner.id=aa.article_id
WHERE owner.profile_id=? AND al.profile_id=? AND aa.article_id=? ORDER BY aa.ordinal, al.id`,
		database.profileID, database.profileID, articleID)
	if err != nil {
		return ExportSnapshotRecord{}, err
	}
	for albumRows.Next() {
		var album domain.Album
		if err := albumRows.Scan(&album.ID, &album.AccountID, &album.UpstreamID, &album.Name, &album.Description,
			&album.ArticleCount, &album.Paid); err != nil {
			albumRows.Close()
			return ExportSnapshotRecord{}, err
		}
		snapshot.Article.Albums = append(snapshot.Article.Albums, album)
	}
	if err := albumRows.Err(); err != nil {
		albumRows.Close()
		return ExportSnapshotRecord{}, err
	}
	if err := albumRows.Close(); err != nil {
		return ExportSnapshotRecord{}, err
	}

	var captured int64
	var current bool
	err = transaction.QueryRowContext(ctx, `SELECT cv.id, cv.article_id, cv.object_digest, cv.kind, cv.media_type,
cv.source_url, cv.classification, cv.comment_id, cv.captured_at, cv.is_current
FROM content_versions cv JOIN articles owner ON owner.id=cv.article_id
WHERE owner.profile_id=? AND cv.article_id=? AND cv.kind='html' AND cv.is_current=1
ORDER BY cv.captured_at DESC, cv.id DESC LIMIT 1`, database.profileID, articleID).Scan(
		&snapshot.Content.ID, &snapshot.Content.ArticleID, &snapshot.Content.ObjectDigest, &snapshot.Content.Kind,
		&snapshot.Content.MediaType, &snapshot.Content.SourceURL, &snapshot.Content.Classification,
		&snapshot.Content.CommentID, &captured, &current)
	if err != nil {
		return ExportSnapshotRecord{}, err
	}
	snapshot.Content.CapturedAt = time.UnixMilli(captured)
	snapshot.Content.Current = current

	commentRows, err := transaction.QueryContext(ctx, `SELECT c.id, c.article_id, c.upstream_id, c.author_name, c.content,
c.like_count, c.created_at_upstream, c.raw_object_digest, c.fetched_at,
COALESCE(rc.total_replies, 0), COALESCE(rc.max_reply_id, 0)
FROM comments c LEFT JOIN reply_checkpoints rc ON rc.article_id=c.article_id AND rc.content_id=c.upstream_id
JOIN articles owner ON owner.id=c.article_id
WHERE owner.profile_id=? AND c.article_id=? ORDER BY c.created_at_upstream, c.upstream_id`, database.profileID, articleID)
	if err != nil {
		return ExportSnapshotRecord{}, err
	}
	for commentRows.Next() {
		var comment CommentRecord
		var created sql.NullInt64
		var fetched int64
		if err := commentRows.Scan(&comment.ID, &comment.ArticleID, &comment.UpstreamID, &comment.AuthorName,
			&comment.Content, &comment.LikeCount, &created, &comment.RawObjectDigest, &fetched,
			&comment.ReplyTotal, &comment.ReplyMaxID); err != nil {
			commentRows.Close()
			return ExportSnapshotRecord{}, err
		}
		comment.CreatedAt = unixMillis(created)
		comment.FetchedAt = time.UnixMilli(fetched)
		comment.EmbeddedReplies, err = repliesForCommentID(ctx, transaction, comment.ID)
		if err != nil {
			commentRows.Close()
			return ExportSnapshotRecord{}, err
		}
		snapshot.Comments = append(snapshot.Comments, comment)
	}
	if err := commentRows.Err(); err != nil {
		commentRows.Close()
		return ExportSnapshotRecord{}, err
	}
	if err := commentRows.Close(); err != nil {
		return ExportSnapshotRecord{}, err
	}

	resourceRows, err := transaction.QueryContext(ctx, `SELECT ar.article_id, ar.resource_id, ar.role, ar.ordinal,
ar.original_url, COALESCE(r.object_digest, ''), r.media_type, r.status
FROM article_resources ar JOIN resources r ON r.id=ar.resource_id
JOIN articles owner ON owner.id=ar.article_id
WHERE owner.profile_id=? AND r.profile_id=? AND ar.article_id=? ORDER BY ar.role, ar.ordinal, ar.resource_id`,
		database.profileID, database.profileID, articleID)
	if err != nil {
		return ExportSnapshotRecord{}, err
	}
	for resourceRows.Next() {
		var resource ExportSnapshotResource
		if err := resourceRows.Scan(&resource.ArticleID, &resource.ResourceID, &resource.Role, &resource.Ordinal,
			&resource.OriginalURL, &resource.ObjectDigest, &resource.MediaType, &resource.Status); err != nil {
			resourceRows.Close()
			return ExportSnapshotRecord{}, err
		}
		snapshot.Resources = append(snapshot.Resources, resource)
	}
	if err := resourceRows.Err(); err != nil {
		resourceRows.Close()
		return ExportSnapshotRecord{}, err
	}
	if err := resourceRows.Close(); err != nil {
		return ExportSnapshotRecord{}, err
	}
	if err := transaction.Commit(); err != nil {
		return ExportSnapshotRecord{}, fmt.Errorf("commit export snapshot read: %w", err)
	}
	return snapshot, nil
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
