package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func (database *Database) SaveArticleQuery(
	ctx context.Context,
	name string,
	query domain.ArticleQuery,
) (domain.SavedArticleQuery, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.SavedArticleQuery{}, errors.New("saved article query name is required")
	}
	if len(name) > 256 {
		return domain.SavedArticleQuery{}, errors.New("saved article query name exceeds supported length")
	}
	if _, err := articleOrder(query); err != nil {
		return domain.SavedArticleQuery{}, err
	}
	encoded, err := json.Marshal(query)
	if err != nil {
		return domain.SavedArticleQuery{}, fmt.Errorf("encode saved article query: %w", err)
	}
	now := time.Now().UnixMilli()
	_, err = database.db.ExecContext(ctx, `INSERT INTO saved_article_queries(profile_id, name, query_json, created_at, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(profile_id, name) DO UPDATE SET query_json=excluded.query_json, updated_at=excluded.updated_at`,
		database.profileID, name, string(encoded), now, now)
	if err != nil {
		return domain.SavedArticleQuery{}, fmt.Errorf("save article query: %w", err)
	}
	return database.GetSavedArticleQuery(ctx, name)
}

func (database *Database) GetSavedArticleQuery(ctx context.Context, name string) (domain.SavedArticleQuery, error) {
	return scanSavedArticleQuery(database.db.QueryRowContext(ctx, `SELECT name, query_json, created_at, updated_at
FROM saved_article_queries WHERE profile_id=? AND name=?`, database.profileID, strings.TrimSpace(name)))
}

func (database *Database) ListSavedArticleQueries(ctx context.Context) ([]domain.SavedArticleQuery, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT name, query_json, created_at, updated_at
FROM saved_article_queries WHERE profile_id=? ORDER BY name COLLATE NOCASE, name`, database.profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.SavedArticleQuery, 0)
	for rows.Next() {
		item, err := scanSavedArticleQuery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) DeleteSavedArticleQuery(ctx context.Context, name string) (bool, error) {
	result, err := database.db.ExecContext(ctx, `DELETE FROM saved_article_queries WHERE profile_id=? AND name=?`,
		database.profileID, strings.TrimSpace(name))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

type savedQueryScanner interface{ Scan(...any) error }

func scanSavedArticleQuery(scanner savedQueryScanner) (domain.SavedArticleQuery, error) {
	var item domain.SavedArticleQuery
	var encoded string
	var createdAt, updatedAt int64
	if err := scanner.Scan(&item.Name, &encoded, &createdAt, &updatedAt); err != nil {
		return domain.SavedArticleQuery{}, err
	}
	if err := json.Unmarshal([]byte(encoded), &item.Query); err != nil {
		return domain.SavedArticleQuery{}, fmt.Errorf("decode saved article query %q: %w", item.Name, err)
	}
	item.CreatedAt = time.UnixMilli(createdAt)
	item.UpdatedAt = time.UnixMilli(updatedAt)
	return item, nil
}

var _ savedQueryScanner = (*sql.Row)(nil)
