package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
)

const ProvisionalSingleFakeID = "SINGLE_ARTICLE_FAKEID"

type SingleArticleInput struct {
	URL         string
	Title       string
	Author      string
	Digest      string
	CoverURL    string
	PublishedAt time.Time
	UpdatedAt   time.Time
	MessageType int
	Paid        bool
}

type SingleArticleRepair struct {
	ArticleID    domain.ArticleID
	CanonicalURL string
	RealFakeID   string
	AccountName  string
	Aid          string
	Title        string
	Author       string
}

func NormalizeArticleURL(rawURL string) (string, error) {
	value := strings.TrimSpace(html.UnescapeString(rawURL))
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", errors.New("article URL must use HTTPS and an allowed WeChat host")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "mp.weixin.qq.com" && host != "weixin.qq.com" {
		return "", errors.New("article URL must use HTTPS and an allowed WeChat host")
	}
	parsed.Scheme = "https"
	parsed.Host = "mp.weixin.qq.com"
	parsed.Fragment = ""
	parsed.RawFragment = ""
	query := parsed.Query()
	if query.Get("url") != "" && strings.Contains(parsed.Path, "redirect") {
		return NormalizeArticleURL(query.Get("url"))
	}
	allowed := map[string]struct{}{"__biz": {}, "mid": {}, "idx": {}, "sn": {}, "chksm": {}}
	filtered := make(url.Values)
	keys := make([]string, 0, len(query))
	for key := range query {
		if _, ok := allowed[key]; ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, item := range query[key] {
			filtered.Add(key, item)
		}
	}
	parsed.RawQuery = filtered.Encode()
	parsed.ForceQuery = false
	parsed.Path = strings.TrimSuffix(parsed.EscapedPath(), "/")
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	if parsed.Path != "/s" && !strings.HasPrefix(parsed.Path, "/s/") {
		return "", errors.New("article URL is not a supported WeChat article path")
	}
	parsed.RawPath = ""
	return parsed.String(), nil
}

func (database *Database) SaveProvisionalArticle(ctx context.Context, input SingleArticleInput) (domain.Article, error) {
	canonicalURL, err := NormalizeArticleURL(input.URL)
	if err != nil {
		return domain.Article{}, err
	}
	account, err := database.ensureProvisionalSingleAccount(ctx)
	if err != nil {
		return domain.Article{}, err
	}
	article := domain.Article{
		ID: domain.ArticleID("article:" + localStableDigest(canonicalURL)), AccountID: account.ID,
		Aid: "provisional:" + localStableDigest(canonicalURL), Title: strings.TrimSpace(input.Title),
		Author: strings.TrimSpace(input.Author), Digest: strings.TrimSpace(input.Digest), CanonicalURL: canonicalURL,
		CoverURL: strings.TrimSpace(input.CoverURL), PublishedAt: input.PublishedAt, UpdatedAt: input.UpdatedAt,
		MessageType: input.MessageType, Paid: input.Paid, Single: true,
	}
	if article.Title == "" {
		article.Title = canonicalURL
	}
	if err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		return upsertArticleTx(ctx, transaction, database.profileID, article, time.Now())
	}); err != nil {
		return domain.Article{}, err
	}
	return database.GetArticleByCanonicalURL(ctx, canonicalURL)
}

func (database *Database) RepairSingleArticle(ctx context.Context, repair SingleArticleRepair) (domain.Article, error) {
	fakeID := strings.TrimSpace(repair.RealFakeID)
	if fakeID == "" {
		return domain.Article{}, errors.New("real fakeid is required")
	}
	canonicalURL := strings.TrimSpace(repair.CanonicalURL)
	if canonicalURL != "" {
		var err error
		canonicalURL, err = NormalizeArticleURL(canonicalURL)
		if err != nil {
			return domain.Article{}, err
		}
	}
	if repair.ArticleID == "" && canonicalURL == "" {
		return domain.Article{}, errors.New("article ID or canonical URL is required")
	}
	var result domain.Article
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		accountID := domain.AccountID(identity.AccountID(fakeID))
		accountName := strings.TrimSpace(repair.AccountName)
		if accountName == "" {
			accountName = fakeID
		}
		now := time.Now().UnixMilli()
		if _, err := transaction.ExecContext(ctx, `INSERT INTO accounts(id, profile_id, fakeid, nickname, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(profile_id, fakeid) DO UPDATE SET
nickname=CASE WHEN accounts.nickname='' THEN excluded.nickname ELSE accounts.nickname END, updated_at=excluded.updated_at`,
			accountID, database.profileID, fakeID, accountName, now, now); err != nil {
			return err
		}
		provisional, err := findArticleTx(ctx, transaction, database.profileID, repair.ArticleID, canonicalURL)
		if err != nil {
			return err
		}
		existing, exists, err := articleByIdentityTx(ctx, transaction, database.profileID, accountID, strings.TrimSpace(repair.Aid))
		if err != nil {
			return err
		}
		if exists && existing.ID != provisional.ID {
			if err := mergeArticleDependentsTx(ctx, transaction, provisional.ID, existing.ID); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, `UPDATE articles SET
title=CASE WHEN title='' THEN ? ELSE title END,
author=CASE WHEN author='' THEN ? ELSE author END,
content_status=CASE WHEN content_status='available' OR ?<>'available' THEN content_status ELSE ? END,
is_single=1, updated_at=? WHERE id=?`, repair.Title, repair.Author, provisional.contentStatus,
				provisional.contentStatus, now, existing.ID); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, "DELETE FROM articles WHERE id=?", provisional.ID); err != nil {
				return err
			}
			result = existing.Article
			result.ID = existing.ID
			return nil
		}
		aid := strings.TrimSpace(repair.Aid)
		if aid == "" {
			aid = provisional.Aid
		}
		_, err = transaction.ExecContext(ctx, `UPDATE articles SET account_id=?, aid=?,
title=CASE WHEN ?<>'' THEN ? ELSE title END, author=CASE WHEN ?<>'' THEN ? ELSE author END,
is_single=1, updated_at=? WHERE id=?`, accountID, aid, strings.TrimSpace(repair.Title), strings.TrimSpace(repair.Title),
			strings.TrimSpace(repair.Author), strings.TrimSpace(repair.Author), now, provisional.ID)
		if err != nil {
			return err
		}
		result = provisional.Article
		result.AccountID = accountID
		result.Aid = aid
		return nil
	})
	if err != nil {
		return domain.Article{}, fmt.Errorf("repair single article: %w", err)
	}
	if canonicalURL == "" {
		canonicalURL = result.CanonicalURL
	}
	return database.GetArticleByCanonicalURL(ctx, canonicalURL)
}

func (database *Database) GetArticleByCanonicalURL(ctx context.Context, rawURL string) (domain.Article, error) {
	canonicalURL, err := NormalizeArticleURL(rawURL)
	if err != nil {
		return domain.Article{}, err
	}
	return database.getArticle(ctx, "a.canonical_url=?", canonicalURL)
}

// GetArticle resolves one stable article ID inside the active profile without
// relying on full-text filters. Download/export job construction uses this
// path so IDs returned by queries remain valid automation inputs.
func (database *Database) GetArticle(ctx context.Context, id domain.ArticleID) (domain.Article, error) {
	if strings.TrimSpace(string(id)) == "" {
		return domain.Article{}, errors.New("article ID is required")
	}
	return database.getArticle(ctx, "a.id=?", id)
}

func (database *Database) getArticle(ctx context.Context, condition string, argument any) (domain.Article, error) {
	var item domain.Article
	var published, updated sql.NullInt64
	err := database.db.QueryRowContext(ctx, `SELECT a.id, COALESCE(a.account_id, ''), a.aid,
COALESCE(a.appmsg_id, 0), COALESCE(a.item_index, 0), a.title, a.author, a.digest,
a.canonical_url, a.cover_url, a.published_at, a.updated_at_upstream, a.message_type, a.state,
a.is_deleted, a.is_paid, a.is_original, a.is_single,
CASE WHEN a.content_status='available' THEN 1 ELSE 0 END,
CASE WHEN EXISTS (SELECT 1 FROM comments c WHERE c.article_id=a.id) THEN 1 ELSE 0 END,
a.wecoin_count, a.media_duration_seconds,
COALESCE(ms.read_count, 0), COALESCE(ms.old_like_count, 0), COALESCE(ms.share_count, 0),
COALESCE(ms.like_count, 0), COALESCE(ms.comment_count, 0)
FROM articles a LEFT JOIN metric_snapshots ms ON ms.id=(
  SELECT id FROM metric_snapshots latest WHERE latest.article_id=a.id ORDER BY captured_at DESC, id DESC LIMIT 1
) WHERE a.profile_id=? AND `+condition, database.profileID, argument).Scan(
		&item.ID, &item.AccountID, &item.Aid, &item.AppMsgID, &item.ItemIndex, &item.Title, &item.Author, &item.Digest,
		&item.CanonicalURL, &item.CoverURL, &published, &updated, &item.MessageType, &item.State, &item.Deleted,
		&item.Paid, &item.Original, &item.Single, &item.HasContent, &item.HasComments, &item.WeCoinCount,
		&item.MediaDurationSeconds, &item.ReadCount, &item.OldLikeCount, &item.ShareCount, &item.LikeCount,
		&item.CommentCount,
	)
	if err != nil {
		return domain.Article{}, err
	}
	item.PublishedAt = unixMillis(published)
	item.UpdatedAt = unixMillis(updated)
	item.Albums, err = database.articleAlbums(ctx, item.ID)
	return item, err
}

func (database *Database) ensureProvisionalSingleAccount(ctx context.Context) (domain.Account, error) {
	account, err := database.GetAccountByFakeID(ctx, ProvisionalSingleFakeID)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, err
	}
	return database.SaveAccount(ctx, domain.Account{FakeID: ProvisionalSingleFakeID, Name: "Single articles"})
}

type articleIdentityRecord struct {
	domain.Article
	contentStatus string
}

func findArticleTx(
	ctx context.Context,
	tx *sql.Tx,
	profileID domain.ProfileID,
	id domain.ArticleID,
	canonicalURL string,
) (articleIdentityRecord, error) {
	query := `SELECT id, COALESCE(account_id, ''), aid, title, author, canonical_url, content_status
FROM articles WHERE profile_id=? AND `
	argument := any(id)
	if id != "" {
		query += "id=?"
	} else {
		query += "canonical_url=?"
		argument = canonicalURL
	}
	var record articleIdentityRecord
	err := tx.QueryRowContext(ctx, query, profileID, argument).Scan(&record.ID, &record.AccountID, &record.Aid,
		&record.Title, &record.Author, &record.CanonicalURL, &record.contentStatus)
	return record, err
}

func articleByIdentityTx(
	ctx context.Context,
	tx *sql.Tx,
	profileID domain.ProfileID,
	accountID domain.AccountID,
	aid string,
) (articleIdentityRecord, bool, error) {
	if aid == "" {
		return articleIdentityRecord{}, false, nil
	}
	var record articleIdentityRecord
	err := tx.QueryRowContext(ctx, `SELECT id, account_id, aid, title, author, canonical_url, content_status
FROM articles WHERE profile_id=? AND account_id=? AND aid=?`, profileID, accountID, aid).Scan(&record.ID,
		&record.AccountID, &record.Aid, &record.Title, &record.Author, &record.CanonicalURL, &record.contentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return articleIdentityRecord{}, false, nil
	}
	return record, err == nil, err
}

func mergeArticleDependentsTx(ctx context.Context, tx *sql.Tx, from, to domain.ArticleID) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT OR IGNORE INTO article_albums(article_id, album_id, ordinal)
SELECT ?, album_id, ordinal FROM article_albums WHERE article_id=?`, []any{to, from}},
		{`INSERT OR IGNORE INTO article_resources(article_id, resource_id, role, ordinal, original_url)
SELECT ?, resource_id, role, ordinal, original_url FROM article_resources WHERE article_id=?`, []any{to, from}},
		{`DELETE FROM content_versions AS source WHERE source.article_id=? AND EXISTS (
SELECT 1 FROM content_versions AS target
WHERE target.article_id=? AND target.kind=source.kind AND target.object_digest=source.object_digest
)`, []any{from, to}},
		{`UPDATE content_versions SET article_id=? WHERE article_id=?`, []any{to, from}},
		{`UPDATE metric_snapshots SET article_id=? WHERE article_id=?`, []any{to, from}},
		{`UPDATE OR IGNORE replies SET comment_id=(
SELECT target.id FROM comments AS source
JOIN comments AS target ON target.article_id=? AND target.upstream_id=source.upstream_id
WHERE source.id=replies.comment_id
) WHERE comment_id IN (
SELECT source.id FROM comments AS source
JOIN comments AS target ON target.article_id=? AND target.upstream_id=source.upstream_id
WHERE source.article_id=?
)`, []any{to, to, from}},
		{`DELETE FROM replies WHERE comment_id IN (
SELECT source.id FROM comments AS source
JOIN comments AS target ON target.article_id=? AND target.upstream_id=source.upstream_id
WHERE source.article_id=?
)`, []any{to, from}},
		{`UPDATE comments AS child SET parent_id=(
SELECT target.id FROM comments AS source
JOIN comments AS target ON target.article_id=? AND target.upstream_id=source.upstream_id
WHERE source.id=child.parent_id
) WHERE child.parent_id IN (
SELECT source.id FROM comments AS source
JOIN comments AS target ON target.article_id=? AND target.upstream_id=source.upstream_id
WHERE source.article_id=?
)`, []any{to, to, from}},
		{`DELETE FROM comments AS source WHERE source.article_id=? AND EXISTS (
SELECT 1 FROM comments AS target WHERE target.article_id=? AND target.upstream_id=source.upstream_id
)`, []any{from, to}},
		{`UPDATE comments SET article_id=? WHERE article_id=?`, []any{to, from}},
		{`INSERT INTO comment_checkpoints(article_id, continuation, complete, updated_at)
SELECT ?, continuation, complete, updated_at FROM comment_checkpoints WHERE article_id=?
ON CONFLICT(article_id) DO UPDATE SET
continuation=CASE
  WHEN excluded.updated_at>=comment_checkpoints.updated_at AND excluded.continuation<>'' THEN excluded.continuation
  ELSE comment_checkpoints.continuation
END,
complete=MAX(comment_checkpoints.complete, excluded.complete),
updated_at=MAX(comment_checkpoints.updated_at, excluded.updated_at)`, []any{to, from}},
		{`DELETE FROM comment_checkpoints WHERE article_id=?`, []any{from}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}
	cleanup := []string{
		"DELETE FROM article_albums WHERE article_id=?",
		"DELETE FROM article_resources WHERE article_id=?",
	}
	for _, query := range cleanup {
		if _, err := tx.ExecContext(ctx, query, from); err != nil {
			return err
		}
	}
	return nil
}
