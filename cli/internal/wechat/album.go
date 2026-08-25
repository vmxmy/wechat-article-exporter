package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
)

type AlbumOrder string

const (
	AlbumForward AlbumOrder = "forward"
	AlbumReverse AlbumOrder = "reverse"
)

type AlbumListRequest struct {
	FakeID         string     `json:"fakeid"`
	AlbumID        string     `json:"albumId"`
	Order          AlbumOrder `json:"order,omitempty"`
	BeginMessageID string     `json:"beginMessageId,omitempty"`
	BeginItemIndex string     `json:"beginItemIndex,omitempty"`
	Limit          int        `json:"limit,omitempty"`
}

type AlbumArticle struct {
	Key          string         `json:"key"`
	MessageID    string         `json:"messageId"`
	ItemIndex    string         `json:"itemIndex"`
	Title        string         `json:"title"`
	CanonicalURL string         `json:"canonicalUrl"`
	CoverURL     string         `json:"coverUrl,omitempty"`
	PublishedAt  time.Time      `json:"publishedAt,omitempty"`
	MessageType  int            `json:"messageType,omitempty"`
	Paid         bool           `json:"paid,omitempty"`
	Article      domain.Article `json:"article"`
}

type AlbumPage struct {
	Album      domain.Album           `json:"album"`
	Account    domain.Account         `json:"account"`
	Items      []AlbumArticle         `json:"items"`
	Order      AlbumOrder             `json:"order"`
	Next       domain.AlbumCheckpoint `json:"next"`
	Completed  bool                   `json:"completed"`
	Duplicates []string               `json:"duplicates,omitempty"`
}

type AlbumTraverseOptions struct {
	Request    AlbumListRequest
	Checkpoint domain.AlbumCheckpoint
	Delay      time.Duration
	Sleep      func(context.Context, time.Duration) error
	OnPage     func(AlbumPage, domain.AlbumCheckpoint) error
}

type AlbumTraversal struct {
	Album      domain.Album           `json:"album"`
	Items      []AlbumArticle         `json:"items"`
	Checkpoint domain.AlbumCheckpoint `json:"checkpoint"`
	Completed  bool                   `json:"completed"`
}

func (client *Client) ListAlbumArticles(ctx context.Context, request AlbumListRequest) (AlbumPage, error) {
	fakeID := strings.TrimSpace(request.FakeID)
	albumID := strings.TrimSpace(request.AlbumID)
	if fakeID == "" || albumID == "" {
		return AlbumPage{}, errors.New("album fakeid and album ID are required")
	}
	limit, _ := discoveryPage(request.Limit, 0, 20)
	order := request.Order
	if order == "" {
		order = AlbumForward
	}
	if order != AlbumForward && order != AlbumReverse {
		return AlbumPage{}, fmt.Errorf("unsupported album order %q", order)
	}
	query := BuildAlbumQuery(AlbumListRequest{
		FakeID: fakeID, AlbumID: albumID, Order: order, BeginMessageID: request.BeginMessageID,
		BeginItemIndex: request.BeginItemIndex, Limit: limit,
	})
	response, err := client.request(ctx, http.MethodGet, "/mp/appmsgalbum", query, nil)
	if err != nil {
		return AlbumPage{}, err
	}
	defer response.Body.Close()
	var payload albumPayload
	if err := decodeDiscoveryJSON(response, &payload); err != nil {
		return AlbumPage{}, err
	}
	if err := discoveryBaseError(payload.BaseResp); err != nil {
		return AlbumPage{}, err
	}
	if payload.Response == nil {
		return AlbumPage{}, fmt.Errorf("%w: album response omitted getalbum_resp", ErrDiscoveryProtocol)
	}
	items, duplicates, err := normalizeAlbumArticles(fakeID, client.baseURL, payload.Response.Articles)
	if err != nil {
		return AlbumPage{}, err
	}
	metadata, account, err := normalizeAlbumMetadata(fakeID, albumID, payload.Response.BaseInfo, len(items))
	if err != nil {
		return AlbumPage{}, err
	}
	continueValue := payload.Response.ContinueFlag
	if order == AlbumReverse {
		continueValue = payload.Response.ReverseContinueFlag
	}
	completed := continueValue != "1" || len(items) == 0
	next := domain.AlbumCheckpoint{}
	if !completed {
		last := items[len(items)-1]
		next.BeginMessageID = last.MessageID
		next.BeginItemIndex = last.ItemIndex
	}
	return AlbumPage{Album: metadata, Account: account, Items: items, Order: order, Next: next,
		Completed: completed, Duplicates: duplicates}, nil
}

func BuildAlbumQuery(request AlbumListRequest) url.Values {
	limit, _ := discoveryPage(request.Limit, 0, 20)
	reverse := "0"
	if request.Order == AlbumReverse {
		reverse = "1"
	}
	return url.Values{
		"action": {"getalbum"}, "__biz": {strings.TrimSpace(request.FakeID)},
		"album_id": {strings.TrimSpace(request.AlbumID)}, "begin_msgid": {strings.TrimSpace(request.BeginMessageID)},
		"begin_itemidx": {strings.TrimSpace(request.BeginItemIndex)}, "count": {strconv.Itoa(limit)},
		"is_reverse": {reverse}, "f": {"json"},
	}
}

func (client *Client) TraverseAlbum(ctx context.Context, options AlbumTraverseOptions) (AlbumTraversal, error) {
	sleep := options.Sleep
	if sleep == nil {
		sleep = sleepAlbumContext
	}
	checkpoint := options.Checkpoint
	request := options.Request
	request.BeginMessageID = checkpoint.BeginMessageID
	request.BeginItemIndex = checkpoint.BeginItemIndex
	seen := make(map[string]struct{}, len(checkpoint.SeenKeys))
	for _, key := range checkpoint.SeenKeys {
		seen[key] = struct{}{}
	}
	result := AlbumTraversal{Checkpoint: checkpoint}
	for {
		page, err := client.ListAlbumArticles(ctx, request)
		if err != nil {
			return result, err
		}
		if result.Album.ID == "" {
			result.Album = page.Album
		}
		fresh := make([]AlbumArticle, 0, len(page.Items))
		for _, item := range page.Items {
			if _, duplicate := seen[item.Key]; duplicate {
				continue
			}
			seen[item.Key] = struct{}{}
			fresh = append(fresh, item)
			result.Items = append(result.Items, item)
		}
		checkpoint.BeginMessageID = page.Next.BeginMessageID
		checkpoint.BeginItemIndex = page.Next.BeginItemIndex
		checkpoint.PagesCommitted++
		checkpoint.ItemsCommitted += len(fresh)
		checkpoint.SeenKeys = sortedKeys(seen)
		result.Checkpoint = checkpoint
		result.Completed = page.Completed
		page.Items = fresh
		if options.OnPage != nil {
			if err := options.OnPage(page, checkpoint); err != nil {
				return result, err
			}
		}
		if page.Completed {
			return result, nil
		}
		if checkpoint.BeginMessageID == "" {
			return result, fmt.Errorf("%w: album continuation did not include a message ID", ErrDiscoveryProtocol)
		}
		request.BeginMessageID = checkpoint.BeginMessageID
		request.BeginItemIndex = checkpoint.BeginItemIndex
		if err := sleep(ctx, options.Delay); err != nil {
			return result, err
		}
	}
}

type albumPayload struct {
	BaseResp baseResponse       `json:"base_resp"`
	Response *albumResponseData `json:"getalbum_resp"`
}

type albumResponseData struct {
	BaseInfo            albumBaseInfo   `json:"base_info"`
	Articles            json.RawMessage `json:"article_list"`
	ContinueFlag        string          `json:"continue_flag"`
	ReverseContinueFlag string          `json:"reverse_continue_flag"`
}

type albumBaseInfo struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Nickname     string `json:"nickname"`
	Username     string `json:"username"`
	ArticleCount string `json:"article_count"`
	Paid         string `json:"is_paid"`
}

type albumArticlePayload struct {
	MessageID   string `json:"msgid"`
	ItemIndex   string `json:"itemidx"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	CoverURL    string `json:"cover_img_1_1"`
	PublishedAt string `json:"create_time"`
	MessageType string `json:"item_show_type"`
	Paid        string `json:"is_pay_subscribe"`
}

func normalizeAlbumMetadata(fakeID, albumID string, info albumBaseInfo, fallbackCount int) (domain.Album, domain.Account, error) {
	count := fallbackCount
	if info.ArticleCount != "" {
		parsed, err := strconv.Atoi(info.ArticleCount)
		if err != nil || parsed < 0 {
			return domain.Album{}, domain.Account{}, fmt.Errorf("%w: album article_count is invalid", ErrDiscoveryProtocol)
		}
		count = parsed
	}
	accountName := strings.TrimSpace(info.Nickname)
	if accountName == "" {
		accountName = strings.TrimSpace(info.Username)
	}
	account := domain.Account{ID: domain.AccountID(identity.AccountID(fakeID)), FakeID: fakeID, Name: accountName}
	album := domain.Album{ID: domain.AlbumID("album:" + stableDigest(albumID)), AccountID: account.ID,
		UpstreamID: albumID, Name: strings.TrimSpace(info.Title), Description: strings.TrimSpace(info.Description),
		ArticleCount: count, Paid: info.Paid == "1"}
	return album, account, nil
}

func normalizeAlbumArticles(fakeID string, controlledOrigin *url.URL, raw json.RawMessage) ([]AlbumArticle, []string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return []AlbumArticle{}, nil, nil
	}
	var payloads []albumArticlePayload
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(raw, &payloads); err != nil {
			return nil, nil, fmt.Errorf("%w: decode album article list: %v", ErrDiscoveryProtocol, err)
		}
	} else {
		var payload albumArticlePayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, nil, fmt.Errorf("%w: decode album article item: %v", ErrDiscoveryProtocol, err)
		}
		payloads = []albumArticlePayload{payload}
	}
	items := make([]AlbumArticle, 0, len(payloads))
	seen := make(map[string]struct{})
	duplicates := make([]string, 0)
	for index, payload := range payloads {
		messageID := strings.TrimSpace(payload.MessageID)
		itemIndex := strings.TrimSpace(payload.ItemIndex)
		if messageID == "" || itemIndex == "" || strings.TrimSpace(payload.Title) == "" || strings.TrimSpace(payload.URL) == "" {
			return nil, nil, fmt.Errorf("%w: album item %d lacks message ID, item index, title, or URL", ErrDiscoveryProtocol, index)
		}
		target, err := url.Parse(strings.TrimSpace(html.UnescapeString(payload.URL)))
		if err != nil {
			return nil, nil, fmt.Errorf("%w: album item %d contains an invalid article URL", ErrDiscoveryProtocol, index)
		}
		if !matchesControlledArticleOrigin(target, controlledOrigin) {
			if target, err = upgradeWeChatArticleURL(target); err != nil {
				return nil, nil, fmt.Errorf("%w: album item %d contains an invalid article URL", ErrDiscoveryProtocol, index)
			}
		}
		key := messageID + ":" + itemIndex
		if _, duplicate := seen[key]; duplicate {
			duplicates = append(duplicates, key)
			continue
		}
		seen[key] = struct{}{}
		publishedUnix, _ := strconv.ParseInt(payload.PublishedAt, 10, 64)
		messageType, _ := strconv.Atoi(payload.MessageType)
		// The article-list surface spells this identity "{appmsgid}_{itemidx}"
		// and identity.ArticleID hashes it verbatim, so the album surface has
		// to spell it the same way. Using the ":" separated key here made the
		// same article land twice, once per discovery channel.
		aid := messageID + "_" + itemIndex
		article := domain.Article{ID: domain.ArticleID(identity.ArticleID(fakeID, aid)), AccountID: domain.AccountID(identity.AccountID(fakeID)),
			Aid: aid, Title: strings.TrimSpace(payload.Title), CanonicalURL: target.String(),
			CoverURL: strings.TrimSpace(html.UnescapeString(payload.CoverURL)), PublishedAt: unixSeconds(publishedUnix),
			MessageType: messageType, Paid: payload.Paid == "1"}
		items = append(items, AlbumArticle{Key: key, MessageID: messageID, ItemIndex: itemIndex,
			Title: article.Title, CanonicalURL: article.CanonicalURL, CoverURL: article.CoverURL,
			PublishedAt: article.PublishedAt, MessageType: article.MessageType, Paid: article.Paid, Article: article})
	}
	return items, duplicates, nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sleepAlbumContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
