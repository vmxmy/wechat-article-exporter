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
