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

type ReplyPageCommit struct {
	Replies    []ReplyRecord
	MaxReplyID int64
	FetchedAt  time.Time
}

type ReplyPageResult struct {
	Received   int
	Stored     int
	Duplicates int
}

type ReplyThread struct {
	ArticleID  domain.ArticleID
	ContentID  string
	MaxReplyID int64
	Total      int
	Fetched    int
	Complete   bool
	Attempts   int
	LastError  string
	UpdatedAt  time.Time
}

func (database *Database) CommitReplyPage(ctx context.Context, articleID domain.ArticleID, contentID string, page ReplyPageCommit) (ReplyPageResult, error) {
	if articleID == "" || strings.TrimSpace(contentID) == "" {
		return ReplyPageResult{}, errors.New("article ID and content ID are required")
	}
	if err := database.ensureReplyCheckpointTable(ctx); err != nil {
		return ReplyPageResult{}, err
	}
	fetchedAt := page.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}
	result := ReplyPageResult{Received: len(page.Replies)}
	seen := make(map[string]struct{}, len(page.Replies))
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		commentID, total, err := commentIdentity(ctx, transaction, database.profileID, articleID, contentID)
		if err != nil {
			return err
		}
		for _, reply := range page.Replies {
			upstreamID := strings.TrimSpace(reply.UpstreamID)
			if upstreamID == "" {
				return errors.New("reply upstream ID is required")
			}
			if _, duplicate := seen[upstreamID]; duplicate {
				result.Duplicates++
				continue
			}
			seen[upstreamID] = struct{}{}
			stored, err := upsertReply(ctx, transaction, commentID, reply, fetchedAt)
			if err != nil {
				return err
			}
			if stored {
				result.Stored++
			} else {
				result.Duplicates++
			}
		}
		var fetched int
		if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM replies WHERE comment_id=?`, commentID).Scan(&fetched); err != nil {
			return err
		}
		complete := total == 0 || fetched >= total
		return upsertReplyCheckpoint(ctx, transaction, articleID, contentID, page.MaxReplyID, total, complete, "", fetchedAt)
	})
	if err != nil {
		return ReplyPageResult{}, fmt.Errorf("commit reply page: %w", err)
	}
	return result, nil
}

func (database *Database) RecordReplyFailure(ctx context.Context, articleID domain.ArticleID, contentID, message string) error {
	if err := database.ensureReplyCheckpointTable(ctx); err != nil {
		return err
	}
	now := time.Now()
	return database.WithTx(ctx, func(transaction *sql.Tx) error {
		_, total, err := commentIdentity(ctx, transaction, database.profileID, articleID, contentID)
		if err != nil {
			return err
		}
		_, err = transaction.ExecContext(ctx, `INSERT INTO reply_checkpoints(
article_id, content_id, max_reply_id, total_replies, complete, attempt_count, last_error, updated_at)
VALUES(?, ?, 0, ?, 0, 1, ?, ?) ON CONFLICT(article_id, content_id) DO UPDATE SET
complete=0, attempt_count=reply_checkpoints.attempt_count+1, last_error=excluded.last_error, updated_at=excluded.updated_at`,
			articleID, contentID, total, strings.TrimSpace(message), now.UnixMilli())
		return err
	})
}

func (database *Database) PendingReplyThreads(ctx context.Context, articleID domain.ArticleID) ([]ReplyThread, error) {
	if err := database.ensureReplyCheckpointTable(ctx); err != nil {
		return nil, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT rc.article_id, rc.content_id, rc.max_reply_id,
rc.total_replies, (SELECT COUNT(*) FROM replies r JOIN comments c ON c.id=r.comment_id
WHERE c.article_id=rc.article_id AND c.upstream_id=rc.content_id), rc.complete, rc.attempt_count,
rc.last_error, rc.updated_at
FROM reply_checkpoints rc JOIN articles a ON a.id=rc.article_id
WHERE a.profile_id=? AND rc.article_id=? AND rc.complete=0 ORDER BY rc.updated_at, rc.content_id`,
		database.profileID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	threads := make([]ReplyThread, 0)
	for rows.Next() {
		var thread ReplyThread
		var updatedAt int64
		if err := rows.Scan(&thread.ArticleID, &thread.ContentID, &thread.MaxReplyID, &thread.Total, &thread.Fetched,
			&thread.Complete, &thread.Attempts, &thread.LastError, &updatedAt); err != nil {
			return nil, err
		}
		thread.UpdatedAt = time.UnixMilli(updatedAt)
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func (database *Database) RepliesForComment(ctx context.Context, articleID domain.ArticleID, contentID string) ([]ReplyRecord, error) {
	var commentID string
	err := database.db.QueryRowContext(ctx, `SELECT c.id FROM comments c JOIN articles a ON a.id=c.article_id
WHERE a.profile_id=? AND c.article_id=? AND c.upstream_id=?`, database.profileID, articleID, contentID).Scan(&commentID)
	if err != nil {
		return nil, err
	}
	return repliesForCommentID(ctx, database.db, commentID)
}

func (database *Database) ensureReplyCheckpointTable(ctx context.Context) error {
	var count int
	if err := database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
WHERE type='table' AND name='reply_checkpoints'`).Scan(&count); err != nil {
		return fmt.Errorf("inspect reply checkpoint schema: %w", err)
	}
	if count != 1 {
		return errors.New("reply checkpoint schema is unavailable; reopen the database to apply migrations")
	}
	return nil
}

type replyQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func repliesForCommentID(ctx context.Context, queryer replyQueryer, commentID string) ([]ReplyRecord, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id, comment_id, upstream_id, author_name, content, like_count,
created_at_upstream, raw_object_digest, fetched_at FROM replies WHERE comment_id=?
ORDER BY created_at_upstream, upstream_id`, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	replies := make([]ReplyRecord, 0)
	for rows.Next() {
		var reply ReplyRecord
		var created sql.NullInt64
		var fetchedAt int64
		if err := rows.Scan(&reply.ID, &reply.CommentID, &reply.UpstreamID, &reply.AuthorName, &reply.Content,
			&reply.LikeCount, &created, &reply.RawObjectDigest, &fetchedAt); err != nil {
			return nil, err
		}
		reply.CreatedAt = unixMillis(created)
		reply.FetchedAt = time.UnixMilli(fetchedAt)
		replies = append(replies, reply)
	}
	return replies, rows.Err()
}

func upsertReply(ctx context.Context, transaction *sql.Tx, commentID string, reply ReplyRecord, fetchedAt time.Time) (bool, error) {
	id := reply.ID
	if id == "" {
		id = uuid.NewString()
	}
	var existed int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM replies WHERE comment_id=? AND upstream_id=?`,
		commentID, strings.TrimSpace(reply.UpstreamID)).Scan(&existed); err != nil {
		return false, err
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO replies(
id, comment_id, upstream_id, author_name, content, like_count, created_at_upstream, raw_object_digest, fetched_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(comment_id, upstream_id) DO UPDATE SET
author_name=excluded.author_name, content=excluded.content, like_count=excluded.like_count,
created_at_upstream=COALESCE(excluded.created_at_upstream, replies.created_at_upstream),
raw_object_digest=CASE WHEN excluded.raw_object_digest<>'' THEN excluded.raw_object_digest ELSE replies.raw_object_digest END,
fetched_at=excluded.fetched_at`, id, commentID, strings.TrimSpace(reply.UpstreamID), reply.AuthorName, reply.Content,
		reply.LikeCount, nullableTime(reply.CreatedAt), reply.RawObjectDigest, fetchedAt.UnixMilli())
	return existed == 0, err
}

func upsertReplyCheckpoint(ctx context.Context, transaction *sql.Tx, articleID domain.ArticleID, contentID string,
	maxReplyID int64, total int, complete bool, lastError string, updatedAt time.Time) error {
	if total <= 0 {
		complete = true
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO reply_checkpoints(
article_id, content_id, max_reply_id, total_replies, complete, attempt_count, last_error, updated_at)
VALUES(?, ?, ?, ?, ?, 0, ?, ?) ON CONFLICT(article_id, content_id) DO UPDATE SET
max_reply_id=MAX(reply_checkpoints.max_reply_id, excluded.max_reply_id),
total_replies=MAX(reply_checkpoints.total_replies, excluded.total_replies), complete=excluded.complete,
last_error=excluded.last_error, updated_at=excluded.updated_at`, articleID, contentID, maxReplyID, total,
		complete, lastError, updatedAt.UnixMilli())
	return err
}

func commentIdentity(ctx context.Context, transaction *sql.Tx, profileID domain.ProfileID,
	articleID domain.ArticleID, contentID string) (string, int, error) {
	var commentID string
	var total int
	err := transaction.QueryRowContext(ctx, `SELECT c.id, COALESCE(rc.total_replies, 0)
FROM comments c JOIN articles a ON a.id=c.article_id
LEFT JOIN reply_checkpoints rc ON rc.article_id=c.article_id AND rc.content_id=c.upstream_id
WHERE a.profile_id=? AND c.article_id=? AND c.upstream_id=?`, profileID, articleID, contentID).Scan(&commentID, &total)
	return commentID, total, err
}
