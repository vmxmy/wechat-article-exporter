package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

type MetricSnapshot struct {
	ID           string
	ArticleID    domain.ArticleID
	ReadCount    int
	OldLikeCount int
	LikeCount    int
	ShareCount   int
	CommentCount int
	CredentialID string
	CapturedAt   time.Time
}

func (database *Database) CommitMetricSnapshot(ctx context.Context, snapshot MetricSnapshot) (MetricSnapshot, error) {
	if snapshot.ArticleID == "" || snapshot.CredentialID == "" {
		return MetricSnapshot{}, errors.New("article ID and credential ID are required")
	}
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}
	result, err := database.db.ExecContext(ctx, `INSERT INTO metric_snapshots(
id, article_id, read_count, old_like_count, like_count, share_count, comment_count, credential_ref, captured_at)
SELECT ?, a.id, ?, ?, ?, ?, ?, ?, ? FROM articles a WHERE a.id=? AND a.profile_id=?`,
		snapshot.ID, snapshot.ReadCount, snapshot.OldLikeCount, snapshot.LikeCount, snapshot.ShareCount,
		snapshot.CommentCount, snapshot.CredentialID, snapshot.CapturedAt.UnixMilli(), snapshot.ArticleID, database.profileID)
	if err != nil {
		return MetricSnapshot{}, fmt.Errorf("commit metric snapshot: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return MetricSnapshot{}, sql.ErrNoRows
	}
	return snapshot, nil
}

// ImportLegacyMetricSnapshot persists browser-exported engagement values
// without requiring a live credential reference. Imported values keep an
// explicit provenance marker and never grant access to credentialed requests.
func (database *Database) ImportLegacyMetricSnapshot(ctx context.Context, snapshot MetricSnapshot) (MetricSnapshot, error) {
	if snapshot.ArticleID == "" {
		return MetricSnapshot{}, errors.New("article ID is required")
	}
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now()
	}
	snapshot.CredentialID = ""
	result, err := database.db.ExecContext(ctx, `INSERT INTO metric_snapshots(
id, article_id, read_count, old_like_count, like_count, share_count, comment_count, credential_ref, captured_at)
SELECT ?, a.id, ?, ?, ?, ?, ?, '', ? FROM articles a WHERE a.id=? AND a.profile_id=?`,
		snapshot.ID, snapshot.ReadCount, snapshot.OldLikeCount, snapshot.LikeCount, snapshot.ShareCount,
		snapshot.CommentCount, snapshot.CapturedAt.UnixMilli(), snapshot.ArticleID, database.profileID)
	if err != nil {
		return MetricSnapshot{}, fmt.Errorf("import legacy metric snapshot: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return MetricSnapshot{}, sql.ErrNoRows
	}
	return snapshot, nil
}

func (database *Database) LatestMetricSnapshot(ctx context.Context, articleID domain.ArticleID) (MetricSnapshot, error) {
	var snapshot MetricSnapshot
	var capturedAt int64
	err := database.db.QueryRowContext(ctx, `SELECT ms.id, ms.article_id, ms.read_count, ms.old_like_count,
ms.like_count, ms.share_count, ms.comment_count, ms.credential_ref, ms.captured_at
FROM metric_snapshots ms JOIN articles a ON a.id=ms.article_id
WHERE a.profile_id=? AND ms.article_id=? ORDER BY ms.captured_at DESC, ms.id DESC LIMIT 1`,
		database.profileID, articleID).Scan(&snapshot.ID, &snapshot.ArticleID, &snapshot.ReadCount,
		&snapshot.OldLikeCount, &snapshot.LikeCount, &snapshot.ShareCount, &snapshot.CommentCount,
		&snapshot.CredentialID, &capturedAt)
	if err != nil {
		return MetricSnapshot{}, err
	}
	snapshot.CapturedAt = time.UnixMilli(capturedAt).UTC()
	return snapshot, nil
}
