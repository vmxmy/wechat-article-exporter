package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

var ErrInvalidArticleSort = errors.New("unsupported article sort field or direction")

func (database *Database) queryArticlesAdvanced(ctx context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	limit, offset := normalizePage(query.Limit, query.Offset)
	where := []string{"a.profile_id = ?"}
	arguments := []any{database.profileID}
	if query.AccountID != "" {
		where = append(where, "a.account_id = ?")
		arguments = append(arguments, query.AccountID)
	}
	if query.AlbumID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM article_albums aa WHERE aa.article_id=a.id AND aa.album_id=?)")
		arguments = append(arguments, query.AlbumID)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		value := "%" + keyword + "%"
		where = append(where, `(a.aid LIKE ? OR a.canonical_url LIKE ? OR a.title LIKE ? OR a.author LIKE ? OR
a.digest LIKE ? OR a.cover_url LIKE ? OR a.state LIKE ? OR EXISTS (
  SELECT 1 FROM article_albums aa JOIN albums al ON al.id=aa.album_id
  WHERE aa.article_id=a.id AND (al.title LIKE ? OR al.upstream_id LIKE ?)
))`)
		arguments = append(arguments, value, value, value, value, value, value, value, value, value)
	}
	if author := strings.TrimSpace(query.Author); author != "" {
		where = append(where, "a.author LIKE ?")
		arguments = append(arguments, "%"+author+"%")
	}
	if query.State != "" {
		where = append(where, "a.state = ?")
		arguments = append(arguments, query.State)
	}
	if !query.PublishedFrom.IsZero() {
		where = append(where, "a.published_at >= ?")
		arguments = append(arguments, query.PublishedFrom.UnixMilli())
	}
	if !query.PublishedTo.IsZero() {
		where = append(where, "a.published_at <= ?")
		arguments = append(arguments, query.PublishedTo.UnixMilli())
	}
	appendBoolFilter(&where, &arguments, "a.is_deleted", query.Deleted)
	appendBoolFilter(&where, &arguments, "CASE WHEN a.content_status='available' THEN 1 ELSE 0 END", query.HasContent)
	appendBoolFilter(&where, &arguments, "CASE WHEN EXISTS (SELECT 1 FROM comments c WHERE c.article_id=a.id) THEN 1 ELSE 0 END", query.HasComments)
	appendBoolFilter(&where, &arguments, "a.is_original", query.Original)
	appendBoolFilter(&where, &arguments, "a.is_paid", query.Paid)
	if len(query.MessageTypes) > 0 {
		placeholders := make([]string, len(query.MessageTypes))
		for index, value := range query.MessageTypes {
			placeholders[index] = "?"
			arguments = append(arguments, value)
		}
		where = append(where, "a.message_type IN ("+strings.Join(placeholders, ",")+")")
	}
	appendRangeFilter(&where, &arguments, "COALESCE(ms.read_count, 0)", query.ReadMin, query.ReadMax)
	appendRangeFilter(&where, &arguments, "COALESCE(ms.old_like_count, 0)", query.OldLikeMin, query.OldLikeMax)
	appendRangeFilter(&where, &arguments, "COALESCE(ms.share_count, 0)", query.ShareMin, query.ShareMax)
	appendRangeFilter(&where, &arguments, "COALESCE(ms.like_count, 0)", query.LikeMin, query.LikeMax)
	appendRangeFilter(&where, &arguments, "COALESCE(ms.comment_count, 0)", query.CommentMin, query.CommentMax)
	appendRangeFilter(&where, &arguments, "a.wecoin_count", query.WeCoinMin, query.WeCoinMax)
	appendRangeFilter(&where, &arguments, "a.media_duration_seconds", query.MediaSecondsMin, query.MediaSecondsMax)

	order, err := articleOrder(query)
	if err != nil {
		return domain.Page[domain.Article]{}, err
	}
	predicate := strings.Join(where, " AND ")
	from := ` FROM articles a
LEFT JOIN metric_snapshots ms ON ms.id=(
  SELECT id FROM metric_snapshots latest WHERE latest.article_id=a.id ORDER BY captured_at DESC, id DESC LIMIT 1
)`
	var total int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*)"+from+" WHERE "+predicate, arguments...).Scan(&total); err != nil {
		return domain.Page[domain.Article]{}, err
	}
	rows, err := database.db.QueryContext(ctx, `SELECT a.id, COALESCE(a.account_id, ''), a.aid,
COALESCE(a.appmsg_id, 0), COALESCE(a.item_index, 0), a.title, a.author, a.digest,
a.canonical_url, a.cover_url, a.published_at, a.updated_at_upstream, a.message_type, a.state,
a.is_deleted, a.is_paid, a.is_original, a.is_single,
CASE WHEN a.content_status='available' THEN 1 ELSE 0 END,
CASE WHEN EXISTS (SELECT 1 FROM comments c WHERE c.article_id=a.id) THEN 1 ELSE 0 END,
a.wecoin_count, a.media_duration_seconds,
COALESCE(ms.read_count, 0), COALESCE(ms.old_like_count, 0), COALESCE(ms.share_count, 0),
COALESCE(ms.like_count, 0), COALESCE(ms.comment_count, 0)`+from+` WHERE `+predicate+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(arguments, limit, offset)...)
	if err != nil {
		return domain.Page[domain.Article]{}, err
	}
	defer rows.Close()
	items := make([]domain.Article, 0)
	for rows.Next() {
		var item domain.Article
		var published, updated sql.NullInt64
		if err := rows.Scan(&item.ID, &item.AccountID, &item.Aid, &item.AppMsgID, &item.ItemIndex, &item.Title,
			&item.Author, &item.Digest, &item.CanonicalURL, &item.CoverURL, &published, &updated,
			&item.MessageType, &item.State, &item.Deleted, &item.Paid, &item.Original, &item.Single,
			&item.HasContent, &item.HasComments, &item.WeCoinCount, &item.MediaDurationSeconds,
			&item.ReadCount, &item.OldLikeCount, &item.ShareCount, &item.LikeCount, &item.CommentCount); err != nil {
			return domain.Page[domain.Article]{}, err
		}
		item.PublishedAt = unixMillis(published)
		item.UpdatedAt = unixMillis(updated)
		albums, err := database.articleAlbums(ctx, item.ID)
		if err != nil {
			return domain.Page[domain.Article]{}, err
		}
		item.Albums = albums
		items = append(items, item)
	}
	return domain.Page[domain.Article]{Items: items, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

func appendBoolFilter(where *[]string, arguments *[]any, expression string, value *bool) {
	if value == nil {
		return
	}
	*where = append(*where, expression+" = ?")
	*arguments = append(*arguments, *value)
}

func appendRangeFilter(where *[]string, arguments *[]any, expression string, minimum, maximum *int) {
	if minimum != nil {
		*where = append(*where, expression+" >= ?")
		*arguments = append(*arguments, *minimum)
	}
	if maximum != nil {
		*where = append(*where, expression+" <= ?")
		*arguments = append(*arguments, *maximum)
	}
}

func articleOrder(query domain.ArticleQuery) (string, error) {
	sorts := append([]domain.ArticleSort(nil), query.Sorts...)
	if len(sorts) == 0 {
		switch query.Sort {
		case "", "published_desc":
			sorts = []domain.ArticleSort{{Field: "published", Direction: domain.SortDescending}}
		case "published_asc":
			sorts = []domain.ArticleSort{{Field: "published", Direction: domain.SortAscending}}
		case "title":
			sorts = []domain.ArticleSort{{Field: "title", Direction: domain.SortAscending}}
		default:
			return "", fmt.Errorf("%w: %s", ErrInvalidArticleSort, query.Sort)
		}
	}
	fields := map[string]string{
		"aid": "a.aid", "url": "a.canonical_url", "title": "a.title COLLATE NOCASE", "digest": "a.digest COLLATE NOCASE",
		"published": "a.published_at", "created": "a.published_at", "updated": "a.updated_at_upstream",
		"deleted": "a.is_deleted", "state": "a.state COLLATE NOCASE", "content": "a.content_status",
		"comments_downloaded": "CASE WHEN EXISTS (SELECT 1 FROM comments c WHERE c.article_id=a.id) THEN 1 ELSE 0 END",
		"read":                "COALESCE(ms.read_count, 0)", "old_like": "COALESCE(ms.old_like_count, 0)",
		"share": "COALESCE(ms.share_count, 0)", "like": "COALESCE(ms.like_count, 0)",
		"comment": "COALESCE(ms.comment_count, 0)", "author": "a.author COLLATE NOCASE", "original": "a.is_original",
		"paid": "a.is_paid", "wecoin": "a.wecoin_count", "message_type": "a.message_type",
		"media_duration": "a.media_duration_seconds", "single": "a.is_single",
	}
	parts := make([]string, 0, len(sorts)+1)
	for _, sort := range sorts {
		field, ok := fields[strings.TrimSpace(sort.Field)]
		direction := strings.ToUpper(string(sort.Direction))
		if direction == "" {
			direction = "ASC"
		}
		if !ok || (direction != "ASC" && direction != "DESC") {
			return "", fmt.Errorf("%w: %s %s", ErrInvalidArticleSort, sort.Field, sort.Direction)
		}
		parts = append(parts, field+" "+direction)
	}
	parts = append(parts, "a.id ASC")
	return strings.Join(parts, ", "), nil
}

func (database *Database) articleAlbums(ctx context.Context, articleID domain.ArticleID) ([]domain.Album, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT al.id, COALESCE(al.account_id, ''), al.upstream_id,
al.title, al.description, al.article_count, al.is_paid
FROM article_albums aa JOIN albums al ON al.id=aa.album_id
WHERE aa.article_id=? ORDER BY aa.ordinal, al.id`, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Album, 0)
	for rows.Next() {
		var album domain.Album
		if err := rows.Scan(&album.ID, &album.AccountID, &album.UpstreamID, &album.Name, &album.Description,
			&album.ArticleCount, &album.Paid); err != nil {
			return nil, err
		}
		items = append(items, album)
	}
	return items, rows.Err()
}
