package library

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func (database *Database) GetAccountSyncState(ctx context.Context, id domain.AccountID) (domain.AccountSyncState, error) {
	var state domain.AccountSyncState
	var cursor string
	var lastSync sql.NullInt64
	var latest sql.NullInt64
	err := database.db.QueryRowContext(ctx, `SELECT id, fakeid, nickname, alias, signature, avatar_url, service_type,
last_sync_at, article_count, sync_cursor, upstream_total, message_count, completed,
(SELECT MAX(published_at) FROM articles WHERE profile_id=accounts.profile_id AND account_id=accounts.id)
FROM accounts WHERE profile_id=? AND id=?`, database.profileID, id).Scan(
		&state.Account.ID, &state.Account.FakeID, &state.Account.Name, &state.Account.Alias,
		&state.Account.Description, &state.Account.AvatarURL, &state.Account.ServiceType, &lastSync,
		&state.Account.ArticleCount, &cursor, &state.UpstreamTotal, &state.MessageCount, &state.Completed, &latest,
	)
	if err != nil {
		return domain.AccountSyncState{}, fmt.Errorf("get account sync state: %w", err)
	}
	state.Account.LastSyncAt = unixMillis(lastSync)
	state.LatestArticle = unixMillis(latest)
	state.Cursor, _ = strconv.Atoi(cursor)
	if state.Cursor < 0 {
		state.Cursor = 0
	}
	state.Account.SyncCursor = state.Cursor
	state.Account.UpstreamTotal = state.UpstreamTotal
	state.Account.MessageCount = state.MessageCount
	state.Account.SyncCompleted = state.Completed
	return state, nil
}
