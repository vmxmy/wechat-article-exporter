package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

// ImportLegacyArticle applies the conservative legacy migration policy:
// browser records may fill or refresh an older local record, but they never
// overwrite a locally newer normalized record.
func (database *Database) ImportLegacyArticle(ctx context.Context, article domain.Article) (string, error) {
	canonical, err := NormalizeArticleURL(article.CanonicalURL)
	if err != nil {
		return "", err
	}
	article.CanonicalURL = canonical
	if article.ID == "" {
		article.ID = stableLegacyArticleID(canonical)
	}
	if strings.TrimSpace(article.Aid) == "" {
		article.Aid = "legacy:" + strings.TrimPrefix(string(article.ID), "article:")
	}
	if strings.TrimSpace(article.Title) == "" {
		article.Title = canonical
	}
	local, lookupErr := database.GetArticleByCanonicalURL(ctx, canonical)
	if lookupErr == nil {
		if !local.UpdatedAt.IsZero() && (article.UpdatedAt.IsZero() || local.UpdatedAt.After(article.UpdatedAt)) {
			return "skipped_newer", nil
		}
		article.ID = local.ID
		if article.AccountID == "" {
			article.AccountID = local.AccountID
		}
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return "", lookupErr
	}
	if err := database.UpsertArticle(ctx, ArticleRecord{
		ID: article.ID, AccountID: article.AccountID, Aid: article.Aid, Title: article.Title, Author: article.Author,
		Digest: article.Digest, CanonicalURL: article.CanonicalURL, CoverURL: article.CoverURL,
		PublishedAt: article.PublishedAt, UpdatedAt: article.UpdatedAt, MessageType: article.MessageType, State: article.State,
		Deleted: article.Deleted, Paid: article.Paid, Single: article.Single,
		ContentStatus: map[bool]string{true: "available", false: "missing"}[article.HasContent],
	}); err != nil {
		return "", err
	}
	if lookupErr == nil {
		return "updated", nil
	}
	return "added", nil
}

func stableLegacyArticleID(canonical string) domain.ArticleID {
	digest := sha256.Sum256([]byte(canonical))
	return domain.ArticleID("article:" + hex.EncodeToString(digest[:16]))
}

// ImportLegacyAccount preserves browser synchronization counters and timestamps
// that the generic account manifest intentionally omits from its merge policy.
func (database *Database) ImportLegacyAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	saved, err := database.SaveAccount(ctx, account)
	if err != nil {
		return domain.Account{}, err
	}
	lastSync := account.LastSyncAt
	if lastSync.IsZero() {
		lastSync = time.Time{}
	}
	_, err = database.db.ExecContext(ctx, `UPDATE accounts SET completed=?, message_count=MAX(message_count, ?),
article_count=MAX(article_count, ?), upstream_total=MAX(upstream_total, ?), sync_cursor=?,
last_sync_at=CASE WHEN last_sync_at IS NULL OR last_sync_at<? THEN ? ELSE last_sync_at END, updated_at=?
WHERE profile_id=? AND id=?`, account.SyncCompleted, account.MessageCount, account.ArticleCount,
		account.UpstreamTotal, strconv.Itoa(account.SyncCursor), nullableTime(lastSync),
		nullableTime(lastSync), time.Now().UnixMilli(), database.profileID, saved.ID)
	if err != nil {
		return domain.Account{}, err
	}
	return database.GetAccount(ctx, saved.ID)
}
