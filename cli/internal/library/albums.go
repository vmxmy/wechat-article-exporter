package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

type AlbumArticleCommit struct {
	Article domain.Article
	Ordinal int
	Key     string
}

type AlbumPageCommit struct {
	Album      domain.Album
	Articles   []AlbumArticleCommit
	Checkpoint domain.AlbumCheckpoint
	Completed  bool
	FetchedAt  time.Time
}

type AlbumCommitResult struct {
	Stored        int                    `json:"stored"`
	DuplicateKeys []string               `json:"duplicateKeys,omitempty"`
	Checkpoint    domain.AlbumCheckpoint `json:"checkpoint"`
	Completed     bool                   `json:"completed"`
}

func (database *Database) SaveAlbumPage(ctx context.Context, page AlbumPageCommit) (AlbumCommitResult, error) {
	page.Album.UpstreamID = strings.TrimSpace(page.Album.UpstreamID)
	page.Album.Name = strings.TrimSpace(page.Album.Name)
	if page.Album.UpstreamID == "" {
		return AlbumCommitResult{}, errors.New("album upstream ID is required")
	}
	if page.Album.ID == "" {
		page.Album.ID = domain.AlbumID("album:" + localStableDigest(page.Album.UpstreamID))
	}
	fetchedAt := page.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}
	seen := make(map[string]struct{}, len(page.Checkpoint.SeenKeys))
	for _, key := range page.Checkpoint.SeenKeys {
		seen[key] = struct{}{}
	}
	result := AlbumCommitResult{Checkpoint: page.Checkpoint, Completed: page.Completed}
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO albums(
id, profile_id, account_id, upstream_id, title, description, article_count, is_paid, created_at, updated_at)
VALUES(?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, upstream_id) DO UPDATE SET account_id=COALESCE(excluded.account_id, albums.account_id),
title=CASE WHEN excluded.title<>'' THEN excluded.title ELSE albums.title END,
description=CASE WHEN excluded.description<>'' THEN excluded.description ELSE albums.description END,
article_count=MAX(albums.article_count, excluded.article_count), is_paid=MAX(albums.is_paid, excluded.is_paid),
updated_at=excluded.updated_at`, page.Album.ID, database.profileID, page.Album.AccountID, page.Album.UpstreamID,
			page.Album.Name, page.Album.Description, page.Album.ArticleCount, page.Album.Paid,
			fetchedAt.UnixMilli(), fetchedAt.UnixMilli()); err != nil {
			return err
		}
		for _, entry := range page.Articles {
			key := strings.TrimSpace(entry.Key)
			if key == "" {
				key = string(entry.Article.ID)
			}
			if _, duplicate := seen[key]; duplicate {
				result.DuplicateKeys = append(result.DuplicateKeys, key)
				continue
			}
			article, err := validateArticleForCommit(entry.Article)
			if err != nil {
				return fmt.Errorf("album article %q: %w", key, err)
			}
			if article.AccountID == "" {
				article.AccountID = page.Album.AccountID
			}
			if err := upsertArticleTx(ctx, transaction, database.profileID, article, fetchedAt); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, `INSERT INTO article_albums(article_id, album_id, ordinal)
VALUES(?, ?, ?) ON CONFLICT(article_id, album_id) DO UPDATE SET ordinal=excluded.ordinal`,
				article.ID, page.Album.ID, entry.Ordinal); err != nil {
				return err
			}
			seen[key] = struct{}{}
			result.Stored++
		}
		return nil
	})
	if err != nil {
		return AlbumCommitResult{}, err
	}
	result.Checkpoint.SeenKeys = sortedStringSet(seen)
	result.Checkpoint.PagesCommitted++
	result.Checkpoint.ItemsCommitted += result.Stored
	return result, nil
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sortStrings(result)
	return result
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for cursor := index; cursor > 0 && values[cursor] < values[cursor-1]; cursor-- {
			values[cursor], values[cursor-1] = values[cursor-1], values[cursor]
		}
	}
}
