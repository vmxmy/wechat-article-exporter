package library

import (
	"context"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

// Queries is the shared local-library read seam used by every presentation adapter.
type Queries interface {
	QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
	QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error)
	QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error)
	StorageStatus(context.Context) (domain.StorageStatus, error)
}

// ArticleSelection freezes export selections against the active local
// library. The exporter package consumes the same behavior through its small
// SelectionSource interface without depending on SQLite details.
func (database *Database) ResolveArticleURL(ctx context.Context, rawURL string) (domain.ArticleID, error) {
	article, err := database.GetArticleByCanonicalURL(ctx, rawURL)
	return article.ID, err
}

func (database *Database) LoadSavedArticleQuery(ctx context.Context, name string) (domain.ArticleQuery, error) {
	saved, err := database.GetSavedArticleQuery(ctx, name)
	return saved.Query, err
}

func (database *Database) QueryArticleIDs(ctx context.Context, query domain.ArticleQuery) ([]domain.ArticleID, error) {
	if query.AlbumID != "" && !articleQueryHasFiltersBeyondAlbum(query) {
		return database.queryAlbumArticleIDs(ctx, query.AlbumID)
	}
	query.Offset = 0
	query.Limit = 500
	ids := make([]domain.ArticleID, 0)
	for {
		page, err := database.QueryArticles(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, article := range page.Items {
			ids = append(ids, article.ID)
		}
		if len(ids) >= page.Total || len(page.Items) == 0 {
			return ids, nil
		}
		query.Offset += len(page.Items)
	}
}

func (database *Database) queryAlbumArticleIDs(ctx context.Context, albumID domain.AlbumID) ([]domain.ArticleID, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT aa.article_id
FROM article_albums aa
JOIN articles a ON a.id=aa.article_id
WHERE aa.album_id=? AND a.profile_id=?
ORDER BY aa.ordinal ASC, a.id ASC`, albumID, database.profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]domain.ArticleID, 0)
	for rows.Next() {
		var id domain.ArticleID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func articleQueryHasFiltersBeyondAlbum(query domain.ArticleQuery) bool {
	query.AlbumID = ""
	return query.AccountID != "" || query.Keyword != "" || query.Author != "" || query.State != "" ||
		!query.PublishedFrom.IsZero() || !query.PublishedTo.IsZero() || query.Deleted != nil || query.HasContent != nil ||
		query.HasComments != nil || query.Original != nil || query.Paid != nil || len(query.MessageTypes) > 0 ||
		query.ReadMin != nil || query.ReadMax != nil || query.OldLikeMin != nil || query.OldLikeMax != nil ||
		query.ShareMin != nil || query.ShareMax != nil || query.LikeMin != nil || query.LikeMax != nil ||
		query.CommentMin != nil || query.CommentMax != nil || query.WeCoinMin != nil || query.WeCoinMax != nil ||
		query.MediaSecondsMin != nil || query.MediaSecondsMax != nil ||
		(query.Sort != "" && query.Sort != "published_desc") || len(query.Sorts) > 0
}

// Accounts is the shared local-account write seam. Presentation adapters use
// it through application.Service so account merge and deletion rules remain
// identical across Cobra, Bubble Tea, and MCP.
type Accounts interface {
	SaveAccount(context.Context, domain.Account) (domain.Account, error)
	UpdateAccount(context.Context, domain.Account) (domain.Account, error)
	GetAccount(context.Context, domain.AccountID) (domain.Account, error)
	GetAccountByFakeID(context.Context, string) (domain.Account, error)
	ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error)
	ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error)
	DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error)
}
