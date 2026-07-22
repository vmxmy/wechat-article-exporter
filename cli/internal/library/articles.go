package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

type ArticlePageCommit struct {
	AccountFakeID string
	Articles      []domain.Article
	UpstreamTotal int
	NextOffset    int
	MessageCount  int
	Completed     bool
	FetchedAt     time.Time
}

func (database *Database) SaveArticlePage(ctx context.Context, page ArticlePageCommit) error {
	fakeID := strings.TrimSpace(page.AccountFakeID)
	if fakeID == "" {
		return errors.New("article page account fakeid is required")
	}
	if page.UpstreamTotal < 0 || page.NextOffset < 0 {
		return errors.New("article page totals and offsets cannot be negative")
	}
	validated := make([]domain.Article, len(page.Articles))
	for index, article := range page.Articles {
		value, err := validateArticleForCommit(article)
		if err != nil {
			return fmt.Errorf("article page item %d: %w", index, err)
		}
		validated[index] = value
	}
	fetchedAt := page.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}
	return database.WithTx(ctx, func(transaction *sql.Tx) error {
		var accountID domain.AccountID
		if err := transaction.QueryRowContext(ctx, "SELECT id FROM accounts WHERE profile_id=? AND fakeid=?",
			database.profileID, fakeID).Scan(&accountID); err != nil {
			return fmt.Errorf("resolve article page account: %w", err)
		}
		for _, article := range validated {
			article.AccountID = accountID
			if err := upsertArticleTx(ctx, transaction, database.profileID, article, fetchedAt); err != nil {
				return err
			}
			if err := upsertArticleAlbumsTx(ctx, transaction, database.profileID, accountID, article, fetchedAt); err != nil {
				return err
			}
		}
		messageCount := page.MessageCount
		if messageCount <= 0 {
			messageCount = page.NextOffset
		}
		_, err := transaction.ExecContext(ctx, `UPDATE accounts SET upstream_total=?,
message_count=?, article_count=(SELECT COUNT(*) FROM articles WHERE account_id=?), sync_cursor=?,
completed=?, last_sync_at=?, updated_at=? WHERE profile_id=? AND id=?`,
			page.UpstreamTotal, messageCount, accountID, fmt.Sprintf("%d", page.NextOffset), page.Completed,
			fetchedAt.UnixMilli(), fetchedAt.UnixMilli(), database.profileID, accountID)
		return err
	})
}

func validateArticleForCommit(article domain.Article) (domain.Article, error) {
	article.Aid = strings.TrimSpace(article.Aid)
	article.Title = strings.TrimSpace(article.Title)
	article.CanonicalURL = strings.TrimSpace(article.CanonicalURL)
	if article.ID == "" || article.Aid == "" || article.Title == "" || article.CanonicalURL == "" {
		return domain.Article{}, errors.New("article id, aid, title, and canonical URL are required")
	}
	if len(article.Title) > 4096 || len(article.CanonicalURL) > 8192 {
		return domain.Article{}, errors.New("article fields exceed supported limits")
	}
	return article, nil
}

func upsertArticleTx(ctx context.Context, tx *sql.Tx, profileID domain.ProfileID, article domain.Article, now time.Time) error {
	contentStatus := "missing"
	if article.HasContent {
		contentStatus = "available"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO articles(id, profile_id, account_id, aid, appmsg_id, item_index, title,
author, digest, canonical_url, cover_url, published_at, updated_at_upstream, message_type, state, is_deleted,
is_paid, is_single, is_original, wecoin_count, media_duration_seconds, content_status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, canonical_url) DO UPDATE SET
account_id=excluded.account_id, aid=excluded.aid, appmsg_id=excluded.appmsg_id, item_index=excluded.item_index,
title=excluded.title, author=excluded.author, digest=excluded.digest, cover_url=excluded.cover_url,
published_at=excluded.published_at, updated_at_upstream=excluded.updated_at_upstream,
message_type=excluded.message_type, state=excluded.state, is_deleted=excluded.is_deleted, is_paid=excluded.is_paid,
is_single=CASE WHEN articles.is_single=1 THEN articles.is_single ELSE excluded.is_single END,
is_original=excluded.is_original, wecoin_count=excluded.wecoin_count,
media_duration_seconds=excluded.media_duration_seconds,
content_status=CASE WHEN articles.content_status='available' THEN articles.content_status ELSE excluded.content_status END,
updated_at=excluded.updated_at`, article.ID, profileID, article.AccountID, article.Aid, nullableInt64(article.AppMsgID),
		nullableInt(article.ItemIndex), article.Title, article.Author, article.Digest, article.CanonicalURL, article.CoverURL,
		nullableTime(article.PublishedAt), nullableTime(article.UpdatedAt), article.MessageType, article.State, article.Deleted,
		article.Paid, article.Single, article.Original, article.WeCoinCount, article.MediaDurationSeconds,
		contentStatus, now.UnixMilli(), now.UnixMilli())
	return err
}

func upsertArticleAlbumsTx(
	ctx context.Context,
	tx *sql.Tx,
	profileID domain.ProfileID,
	accountID domain.AccountID,
	article domain.Article,
	now time.Time,
) error {
	for ordinal, album := range article.Albums {
		upstreamID := strings.TrimSpace(album.UpstreamID)
		if upstreamID == "" {
			upstreamID = strings.TrimPrefix(string(album.ID), "album:")
		}
		if upstreamID == "" {
			continue
		}
		albumID := album.ID
		if albumID == "" {
			albumID = domain.AlbumID("album:" + localStableDigest(upstreamID))
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO albums(
id, profile_id, account_id, upstream_id, title, description, article_count, is_paid, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, upstream_id) DO UPDATE SET
account_id=COALESCE(excluded.account_id, albums.account_id),
title=CASE WHEN excluded.title<>'' THEN excluded.title ELSE albums.title END,
description=CASE WHEN excluded.description<>'' THEN excluded.description ELSE albums.description END,
article_count=MAX(albums.article_count, excluded.article_count), is_paid=MAX(albums.is_paid, excluded.is_paid),
updated_at=excluded.updated_at`, albumID, profileID, accountID, upstreamID, strings.TrimSpace(album.Name),
			strings.TrimSpace(album.Description), album.ArticleCount, album.Paid, now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO article_albums(article_id, album_id, ordinal) VALUES(?, ?, ?)
ON CONFLICT(article_id, album_id) DO UPDATE SET ordinal=excluded.ordinal`, article.ID, albumID, ordinal); err != nil {
			return err
		}
	}
	return nil
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func localStableDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}
