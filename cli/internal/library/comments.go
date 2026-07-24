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
)

type CommentRecord struct {
	ID              string
	ArticleID       domain.ArticleID
	UpstreamID      string
	AuthorName      string
	Content         string
	LikeCount       int
	CreatedAt       time.Time
	RawObjectDigest string
	FetchedAt       time.Time
	ReplyTotal      int
	ReplyMaxID      int64
	EmbeddedReplies []ReplyRecord
}

type ReplyRecord struct {
	ID              string
	CommentID       string
	UpstreamID      string
	AuthorName      string
	Content         string
	LikeCount       int
	CreatedAt       time.Time
	RawObjectDigest string
	FetchedAt       time.Time
}

type CommentPageCommit struct {
	Comments  []CommentRecord
	Buffer    string
	Complete  bool
	FetchedAt time.Time
}

type CommentPageResult struct {
	Received   int
	Stored     int
	Duplicates int
}

type CommentCheckpoint struct {
	ArticleID domain.ArticleID
	Buffer    string
	Complete  bool
	UpdatedAt time.Time
}

func (database *Database) CommitCommentPage(ctx context.Context, articleID domain.ArticleID, page CommentPageCommit) (CommentPageResult, error) {
	if articleID == "" {
		return CommentPageResult{}, errors.New("article ID is required")
	}
	fetchedAt := page.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}
	if err := database.ensureReplyCheckpointTable(ctx); err != nil {
		return CommentPageResult{}, err
	}
	result := CommentPageResult{Received: len(page.Comments)}
	seen := make(map[string]struct{}, len(page.Comments))
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		if err := requireArticle(ctx, transaction, database.profileID, articleID); err != nil {
			return err
		}
		for _, comment := range page.Comments {
			upstreamID := strings.TrimSpace(comment.UpstreamID)
			if upstreamID == "" {
				return errors.New("comment upstream ID is required")
			}
			if _, duplicate := seen[upstreamID]; duplicate {
				result.Duplicates++
				continue
			}
			seen[upstreamID] = struct{}{}
			stored, commentID, err := upsertComment(ctx, transaction, articleID, comment, fetchedAt)
			if err != nil {
				return err
			}
			if stored {
				result.Stored++
			} else {
				result.Duplicates++
			}
			for _, reply := range comment.EmbeddedReplies {
				if _, err := upsertReply(ctx, transaction, commentID, reply, fetchedAt); err != nil {
					return err
				}
			}
			if err := upsertReplyCheckpoint(ctx, transaction, articleID, upstreamID, comment.ReplyMaxID,
				comment.ReplyTotal, len(comment.EmbeddedReplies) >= comment.ReplyTotal && comment.ReplyTotal > 0,
				"", fetchedAt); err != nil {
				return err
			}
		}
		buffer := page.Buffer
		if page.Complete {
			buffer = ""
		}
		_, err := transaction.ExecContext(ctx, `INSERT INTO comment_checkpoints(article_id, continuation, complete, updated_at)
VALUES(?, ?, ?, ?) ON CONFLICT(article_id) DO UPDATE SET
continuation=excluded.continuation, complete=excluded.complete, updated_at=excluded.updated_at`,
			articleID, buffer, page.Complete, fetchedAt.UnixMilli())
		return err
	})
	if err != nil {
		return CommentPageResult{}, fmt.Errorf("commit comment page: %w", err)
	}
	return result, nil
}

func (database *Database) CommentCheckpointForArticle(ctx context.Context, articleID domain.ArticleID) (CommentCheckpoint, error) {
	var checkpoint CommentCheckpoint
	var updatedAt int64
	err := database.db.QueryRowContext(ctx, `SELECT cc.article_id, cc.continuation, cc.complete, cc.updated_at
FROM comment_checkpoints cc JOIN articles a ON a.id=cc.article_id
WHERE a.profile_id=? AND cc.article_id=?`, database.profileID, articleID).Scan(
		&checkpoint.ArticleID, &checkpoint.Buffer, &checkpoint.Complete, &updatedAt)
	if err != nil {
		return CommentCheckpoint{}, err
	}
	checkpoint.UpdatedAt = time.UnixMilli(updatedAt)
	return checkpoint, nil
}

func (database *Database) CommentsForArticle(ctx context.Context, articleID domain.ArticleID) ([]CommentRecord, error) {
	if err := database.ensureReplyCheckpointTable(ctx); err != nil {
		return nil, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT c.id, c.article_id, c.upstream_id, c.author_name, c.content,
c.like_count, c.created_at_upstream, c.raw_object_digest, c.fetched_at,
COALESCE(rc.total_replies, 0), COALESCE(rc.max_reply_id, 0)
FROM comments c JOIN articles a ON a.id=c.article_id
LEFT JOIN reply_checkpoints rc ON rc.article_id=c.article_id AND rc.content_id=c.upstream_id
WHERE a.profile_id=? AND c.article_id=? ORDER BY c.created_at_upstream, c.upstream_id`, database.profileID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := make([]CommentRecord, 0)
	for rows.Next() {
		var comment CommentRecord
		var created sql.NullInt64
		var fetchedAt int64
		if err := rows.Scan(&comment.ID, &comment.ArticleID, &comment.UpstreamID, &comment.AuthorName, &comment.Content,
			&comment.LikeCount, &created, &comment.RawObjectDigest, &fetchedAt, &comment.ReplyTotal, &comment.ReplyMaxID); err != nil {
			return nil, err
		}
		comment.CreatedAt = unixMillis(created)
		comment.FetchedAt = time.UnixMilli(fetchedAt)
		comment.EmbeddedReplies, err = repliesForCommentID(ctx, database.db, comment.ID)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

// ListCommentsForArticle returns one deterministic, bounded local comment
// page. It intentionally does not hydrate replies: callers that need replies
// must use the separately bounded reply reader.
func (database *Database) ListCommentsForArticle(ctx context.Context, articleID domain.ArticleID, offset, limit int) (domain.Page[CommentRecord], error) {
	if err := database.ensureReplyCheckpointTable(ctx); err != nil {
		return domain.Page[CommentRecord]{}, err
	}
	var total int
	if err := database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments c JOIN articles a ON a.id=c.article_id
WHERE a.profile_id=? AND c.article_id=?`, database.profileID, articleID).Scan(&total); err != nil {
		return domain.Page[CommentRecord]{}, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT c.id, c.article_id, c.upstream_id, c.author_name, c.content,
c.like_count, c.created_at_upstream, c.raw_object_digest, c.fetched_at,
COALESCE(rc.total_replies, 0), COALESCE(rc.max_reply_id, 0)
FROM comments c JOIN articles a ON a.id=c.article_id
LEFT JOIN reply_checkpoints rc ON rc.article_id=c.article_id AND rc.content_id=c.upstream_id
WHERE a.profile_id=? AND c.article_id=? ORDER BY c.created_at_upstream, c.upstream_id LIMIT ? OFFSET ?`,
		database.profileID, articleID, limit, offset)
	if err != nil {
		return domain.Page[CommentRecord]{}, err
	}
	defer rows.Close()
	items := make([]CommentRecord, 0, limit)
	for rows.Next() {
		var comment CommentRecord
		var created sql.NullInt64
		var fetchedAt int64
		if err := rows.Scan(&comment.ID, &comment.ArticleID, &comment.UpstreamID, &comment.AuthorName, &comment.Content,
			&comment.LikeCount, &created, &comment.RawObjectDigest, &fetchedAt, &comment.ReplyTotal, &comment.ReplyMaxID); err != nil {
			return domain.Page[CommentRecord]{}, err
		}
		comment.CreatedAt = unixMillis(created)
		comment.FetchedAt = time.UnixMilli(fetchedAt)
		items = append(items, comment)
	}
	if err := rows.Err(); err != nil {
		return domain.Page[CommentRecord]{}, err
	}
	return domain.Page[CommentRecord]{Items: items, Total: total, Offset: offset, Limit: limit}, nil
}

func upsertComment(ctx context.Context, transaction *sql.Tx, articleID domain.ArticleID, comment CommentRecord, fetchedAt time.Time) (bool, string, error) {
	commentID := comment.ID
	if commentID == "" {
		commentID = uuid.NewString()
	}
	var existed int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE article_id=? AND upstream_id=?`,
		articleID, strings.TrimSpace(comment.UpstreamID)).Scan(&existed); err != nil {
		return false, "", err
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO comments(
id, article_id, upstream_id, author_name, content, like_count, created_at_upstream, raw_object_digest, fetched_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(article_id, upstream_id) DO UPDATE SET
author_name=excluded.author_name, content=excluded.content, like_count=excluded.like_count,
created_at_upstream=COALESCE(excluded.created_at_upstream, comments.created_at_upstream),
raw_object_digest=CASE WHEN excluded.raw_object_digest<>'' THEN excluded.raw_object_digest ELSE comments.raw_object_digest END,
fetched_at=excluded.fetched_at`, commentID, articleID, strings.TrimSpace(comment.UpstreamID), comment.AuthorName,
		comment.Content, comment.LikeCount, nullableTime(comment.CreatedAt), comment.RawObjectDigest, fetchedAt.UnixMilli())
	if err != nil {
		return false, "", err
	}
	if err := transaction.QueryRowContext(ctx, `SELECT id FROM comments WHERE article_id=? AND upstream_id=?`,
		articleID, strings.TrimSpace(comment.UpstreamID)).Scan(&commentID); err != nil {
		return false, "", err
	}
	return existed == 0, commentID, nil
}

func requireArticle(ctx context.Context, transaction *sql.Tx, profileID domain.ProfileID, articleID domain.ArticleID) error {
	var count int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM articles WHERE profile_id=? AND id=?`,
		profileID, articleID).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}
