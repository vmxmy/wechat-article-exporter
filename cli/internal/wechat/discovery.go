package wechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

const (
	defaultAccountPageSize = 5
	defaultArticlePageSize = 5
	maximumDiscoveryPage   = 50
)

var (
	ErrDiscoveryAuthentication = errors.New("WeChat discovery session expired; run login again")
	ErrDiscoveryProtocol       = errors.New("unsupported WeChat discovery response")
	articleAccountNamePattern  = regexp.MustCompile(`(?is)<[^>]*class=["'][^"']*wx_follow_nickname[^"']*["'][^>]*>(.*?)</[^>]+>`)
	htmlTagPattern             = regexp.MustCompile(`(?s)<[^>]*>`)
)

type AccountSearchRequest struct {
	Keyword string `json:"keyword"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type AccountDetails struct {
	Account domain.Account `json:"account"`
	Author  AuthorInfo     `json:"author"`
}

type DiscoveryGateway interface {
	SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
	ResolveAccountName(context.Context, string) (string, error)
	ResolveAccountFromArticle(context.Context, string) (domain.Account, error)
	AccountDetails(context.Context, string) (AccountDetails, error)
	AuthorInfo(context.Context, string) (AuthorInfo, error)
	ListArticles(context.Context, ArticleListRequest) (ArticlePage, error)
}

type AlbumDiscoveryGateway interface {
	ListAlbumArticles(context.Context, AlbumListRequest) (AlbumPage, error)
}

type AuthorInfo struct {
	AccountID        string `json:"accountId,omitempty"`
	Name             string `json:"name,omitempty"`
	Alias            string `json:"alias,omitempty"`
	Description      string `json:"description,omitempty"`
	AvatarURL        string `json:"avatarUrl,omitempty"`
	PrincipalName    string `json:"principalName,omitempty"`
	VerificationInfo string `json:"verificationInfo,omitempty"`
}

type ArticleListRequest struct {
	FakeID  string `json:"fakeid"`
	Keyword string `json:"keyword,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type ArticlePage struct {
	Items     []domain.Article `json:"items"`
	Total     int              `json:"total"`
	Offset    int              `json:"offset"`
	Limit     int              `json:"limit"`
	Next      int              `json:"next"`
	Completed bool             `json:"completed"`
}

func (client *Client) SearchAccounts(ctx context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	return client.SearchAccountPage(ctx, AccountSearchRequest{Keyword: query.Keyword, Offset: query.Offset, Limit: query.Limit})
}

func (client *Client) SearchAccountPage(ctx context.Context, request AccountSearchRequest) (domain.Page[domain.Account], error) {
	keyword := strings.TrimSpace(request.Keyword)
	if keyword == "" {
		return domain.Page[domain.Account]{}, errors.New("account search keyword is required")
	}
	limit, offset := discoveryPage(request.Limit, request.Offset, defaultAccountPageSize)
	session, err := client.discoverySession(ctx)
	if err != nil {
		return domain.Page[domain.Account]{}, err
	}
	response, err := client.request(ctx, http.MethodGet, "/cgi-bin/searchbiz", url.Values{
		"action": {"search_biz"}, "begin": {strconv.Itoa(offset)}, "count": {strconv.Itoa(limit)},
		"query": {keyword}, "token": {session.Token}, "lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"},
	}, nil)
	if err != nil {
		return domain.Page[domain.Account]{}, err
	}
	defer response.Body.Close()
	var payload searchAccountsPayload
	if err := decodeDiscoveryJSON(response, &payload); err != nil {
		return domain.Page[domain.Account]{}, err
	}
	if err := discoveryBaseError(payload.BaseResp); err != nil {
		return domain.Page[domain.Account]{}, err
	}
	if payload.List == nil || payload.Total == nil || *payload.Total < 0 {
		return domain.Page[domain.Account]{}, fmt.Errorf("%w: account search payload lacks list or total", ErrDiscoveryProtocol)
	}
	accounts := make([]domain.Account, 0, len(payload.List))
	for index, item := range payload.List {
		account, err := normalizeDiscoveredAccount(item)
		if err != nil {
			return domain.Page[domain.Account]{}, fmt.Errorf("%w: account %d: %v", ErrDiscoveryProtocol, index, err)
		}
		accounts = append(accounts, account)
	}
	return domain.Page[domain.Account]{Items: accounts, Total: *payload.Total, Offset: offset, Limit: limit}, nil
}

func (client *Client) ResolveAccountName(ctx context.Context, rawURL string) (string, error) {
	target, err := client.validateArticleURL(rawURL)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 wechat-article-local/2")
	request.Header.Set("Referer", upstreamOrigin+"/")
	request.Header.Set("Origin", upstreamOrigin)
	httpClient := *client.http
	previousRedirectCheck := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if _, err := client.validateArticleURL(request.URL.String()); err != nil {
			return err
		}
		if previousRedirectCheck != nil {
			return previousRedirectCheck(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("article account resolution returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return "", err
	}
	match := articleAccountNamePattern.FindSubmatch(body)
	if len(match) < 2 {
		return "", fmt.Errorf("%w: article did not expose an account name", ErrDiscoveryProtocol)
	}
	name := strings.TrimSpace(html.UnescapeString(htmlTagPattern.ReplaceAllString(string(match[1]), "")))
	if name == "" {
		return "", fmt.Errorf("%w: article account name was empty", ErrDiscoveryProtocol)
	}
	return name, nil
}

func (client *Client) ResolveAccountFromArticle(ctx context.Context, rawURL string) (domain.Account, error) {
	name, err := client.ResolveAccountName(ctx, rawURL)
	if err != nil {
		return domain.Account{}, err
	}
	page, err := client.SearchAccountPage(ctx, AccountSearchRequest{Keyword: name, Limit: 20})
	if err != nil {
		return domain.Account{}, err
	}
	for _, account := range page.Items {
		if account.Name == name {
			return account, nil
		}
	}
	return domain.Account{}, fmt.Errorf("account %q resolved from article but was not present in authenticated search results", name)
}

func (client *Client) AccountDetails(ctx context.Context, fakeID string) (AccountDetails, error) {
	fakeID = strings.TrimSpace(fakeID)
	if fakeID == "" {
		return AccountDetails{}, errors.New("account fakeid is required")
	}
	page, err := client.SearchAccountPage(ctx, AccountSearchRequest{Keyword: fakeID, Limit: 20})
	if err != nil {
		return AccountDetails{}, err
	}
	var account domain.Account
	for _, candidate := range page.Items {
		if candidate.FakeID == fakeID {
			account = candidate
			break
		}
	}
	if account.FakeID == "" {
		return AccountDetails{}, fmt.Errorf("account %q was not found", fakeID)
	}
	author, err := client.AuthorInfo(ctx, fakeID)
	if err != nil {
		return AccountDetails{}, err
	}
	if account.Alias == "" {
		account.Alias = author.Alias
	}
	if account.Description == "" {
		account.Description = author.Description
	}
	if account.AvatarURL == "" {
		account.AvatarURL = author.AvatarURL
	}
	return AccountDetails{Account: account, Author: author}, nil
}

func (client *Client) AuthorInfo(ctx context.Context, fakeID string) (AuthorInfo, error) {
	fakeID = strings.TrimSpace(fakeID)
	if fakeID == "" {
		return AuthorInfo{}, errors.New("author fakeid is required")
	}
	if _, err := client.discoverySession(ctx); err != nil {
		return AuthorInfo{}, err
	}
	response, err := client.request(ctx, http.MethodGet, "/mp/authorinfo", url.Values{
		"wxtoken": {"777"}, "biz": {fakeID}, "__biz": {fakeID}, "x5": {"0"}, "f": {"json"},
	}, nil)
	if err != nil {
		return AuthorInfo{}, err
	}
	defer response.Body.Close()
	var payload authorInfoPayload
	if err := decodeDiscoveryJSON(response, &payload); err != nil {
		return AuthorInfo{}, err
	}
	if err := discoveryBaseError(payload.BaseResp); err != nil {
		return AuthorInfo{}, err
	}
	author := payload.normalized()
	if author.Name == "" && author.PrincipalName == "" && author.VerificationInfo == "" {
		return AuthorInfo{}, fmt.Errorf("%w: author info contained no recognizable fields", ErrDiscoveryProtocol)
	}
	if author.AccountID == "" {
		author.AccountID = fakeID
	}
	return author, nil
}

func (client *Client) ListArticles(ctx context.Context, request ArticleListRequest) (ArticlePage, error) {
	fakeID := strings.TrimSpace(request.FakeID)
	if fakeID == "" {
		return ArticlePage{}, errors.New("article-list fakeid is required")
	}
	limit, offset := discoveryPage(request.Limit, request.Offset, defaultArticlePageSize)
	session, err := client.discoverySession(ctx)
	if err != nil {
		return ArticlePage{}, err
	}
	query := BuildArticleListQuery(session.Token, ArticleListRequest{
		FakeID: fakeID, Keyword: strings.TrimSpace(request.Keyword), Offset: offset, Limit: limit,
	})
	response, err := client.request(ctx, http.MethodGet, "/cgi-bin/appmsgpublish", query, nil)
	if err != nil {
		return ArticlePage{}, err
	}
	defer response.Body.Close()
	var payload articleListPayload
	if err := decodeDiscoveryJSON(response, &payload); err != nil {
		return ArticlePage{}, err
	}
	if err := discoveryBaseError(payload.BaseResp); err != nil {
		return ArticlePage{}, err
	}
	if strings.TrimSpace(payload.PublishPage) == "" {
		return ArticlePage{}, fmt.Errorf("%w: article-list payload omitted publish_page", ErrDiscoveryProtocol)
	}
	var publishPage publishPagePayload
	if err := json.Unmarshal([]byte(payload.PublishPage), &publishPage); err != nil {
		return ArticlePage{}, fmt.Errorf("%w: decode publish_page: %v", ErrDiscoveryProtocol, err)
	}
	if publishPage.PublishList == nil {
		return ArticlePage{}, fmt.Errorf("%w: publish_page lacks publish_list", ErrDiscoveryProtocol)
	}
	var total int
	if payload.TotalCount != nil {
		total = *payload.TotalCount
	} else if publishPage.TotalCount != nil {
		total = *publishPage.TotalCount
	} else {
		return ArticlePage{}, fmt.Errorf("%w: article-list payload lacks total_count", ErrDiscoveryProtocol)
	}
	if total < 0 {
		return ArticlePage{}, fmt.Errorf("%w: article-list total_count is negative", ErrDiscoveryProtocol)
	}
	articles := []domain.Article{}
	messageCount := 0
	for index, item := range publishPage.PublishList {
		if strings.TrimSpace(item.PublishInfo) == "" {
			continue
		}
		messageCount++
		var info publishInfoPayload
		if err := json.Unmarshal([]byte(item.PublishInfo), &info); err != nil {
			return ArticlePage{}, fmt.Errorf("%w: decode publish_list[%d]: %v", ErrDiscoveryProtocol, index, err)
		}
		if info.AppMsgEx == nil {
			return ArticlePage{}, fmt.Errorf("%w: publish_list[%d] lacks appmsgex", ErrDiscoveryProtocol, index)
		}
		for articleIndex, item := range info.AppMsgEx {
			article, err := normalizeDiscoveredArticleForOrigin(fakeID, item, client.baseURL)
			if err != nil {
				return ArticlePage{}, fmt.Errorf("%w: publish_list[%d].appmsgex[%d]: %v", ErrDiscoveryProtocol, index, articleIndex, err)
			}
			articles = append(articles, article)
		}
	}
	next := offset + messageCount
	completed := messageCount == 0 || next >= total
	return ArticlePage{
		Items: articles, Total: total, Offset: offset, Limit: limit, Next: next, Completed: completed,
	}, nil
}

func BuildArticleListQuery(token string, request ArticleListRequest) url.Values {
	limit, offset := discoveryPage(request.Limit, request.Offset, defaultArticlePageSize)
	keyword := strings.TrimSpace(request.Keyword)
	sub := "list"
	searchField := "null"
	if keyword != "" {
		sub = "search"
		searchField = "7"
	}
	return url.Values{
		"sub": {sub}, "search_field": {searchField}, "begin": {strconv.Itoa(offset)}, "count": {strconv.Itoa(limit)},
		"query": {keyword}, "fakeid": {strings.TrimSpace(request.FakeID)}, "type": {"101_1"},
		"free_publish_type": {"1"}, "sub_action": {"list_ex"}, "token": {token}, "lang": {"zh_CN"},
		"f": {"json"}, "ajax": {"1"},
	}
}

func (client *Client) discoverySession(ctx context.Context) (Session, error) {
	session, err := client.loadSession(ctx)
	if errors.Is(err, secrets.ErrNotFound) {
		return Session{}, ErrDiscoveryAuthentication
	}
	if err != nil {
		return Session{}, err
	}
	if session.Token == "" || (!session.ExpiresAt.IsZero() && !client.now().Before(session.ExpiresAt)) {
		return Session{}, ErrDiscoveryAuthentication
	}
	client.restoreCookies(session.Cookies)
	return session, nil
}

func (client *Client) validateArticleURL(rawURL string) (*url.URL, error) {
	value := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(value)
	if err == nil && matchesControlledArticleOrigin(parsed, client.baseURL) {
		return parsed, nil
	}
	if err != nil || parsed.User != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.Port() != "" {
		return nil, errors.New("article URL must use HTTPS and an allowed WeChat host")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "mp.weixin.qq.com" && host != "weixin.qq.com" {
		return nil, errors.New("article URL must use HTTPS and an allowed WeChat host")
	}
	return parsed, nil
}

func matchesControlledArticleOrigin(target, origin *url.URL) bool {
	return matchesControlledAuthority(target, origin)
}

func discoveryPage(limit, offset, defaultLimit int) (int, int) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maximumDiscoveryPage {
		limit = maximumDiscoveryPage
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

type baseResponse struct {
	Ret    int    `json:"ret"`
	ErrMsg string `json:"err_msg"`
}

type searchAccountsPayload struct {
	BaseResp baseResponse        `json:"base_resp"`
	List     []searchAccountItem `json:"list"`
	Total    *int                `json:"total"`
}

type searchAccountItem struct {
	FakeID       string `json:"fakeid"`
	Nickname     string `json:"nickname"`
	Alias        string `json:"alias"`
	Signature    string `json:"signature"`
	RoundHeadImg string `json:"round_head_img"`
	ServiceType  int    `json:"service_type"`
}

type authorInfoPayload struct {
	BaseResp      baseResponse `json:"base_resp"`
	FakeID        string       `json:"fakeid"`
	Nickname      string       `json:"nickname"`
	Alias         string       `json:"alias"`
	Signature     string       `json:"signature"`
	RoundHeadImg  string       `json:"round_head_img"`
	PrincipalName string       `json:"principal_name"`
	VerifyInfo    string       `json:"verify_info"`
	Data          *struct {
		FakeID        string `json:"fakeid"`
		Nickname      string `json:"nickname"`
		Alias         string `json:"alias"`
		Signature     string `json:"signature"`
		RoundHeadImg  string `json:"round_head_img"`
		PrincipalName string `json:"principal_name"`
		VerifyInfo    string `json:"verify_info"`
	} `json:"data"`
}

func (payload authorInfoPayload) normalized() AuthorInfo {
	author := AuthorInfo{AccountID: payload.FakeID, Name: payload.Nickname, Alias: payload.Alias,
		Description: payload.Signature, AvatarURL: payload.RoundHeadImg, PrincipalName: payload.PrincipalName,
		VerificationInfo: payload.VerifyInfo}
	if payload.Data != nil {
		if author.AccountID == "" {
			author.AccountID = payload.Data.FakeID
		}
		if author.Name == "" {
			author.Name = payload.Data.Nickname
		}
		if author.Alias == "" {
			author.Alias = payload.Data.Alias
		}
		if author.Description == "" {
			author.Description = payload.Data.Signature
		}
		if author.AvatarURL == "" {
			author.AvatarURL = payload.Data.RoundHeadImg
		}
		if author.PrincipalName == "" {
			author.PrincipalName = payload.Data.PrincipalName
		}
		if author.VerificationInfo == "" {
			author.VerificationInfo = payload.Data.VerifyInfo
		}
	}
	return author
}

type articleListPayload struct {
	BaseResp    baseResponse `json:"base_resp"`
	TotalCount  *int         `json:"total_count"`
	PublishPage string       `json:"publish_page"`
}

type publishPagePayload struct {
	TotalCount  *int              `json:"total_count"`
	PublishList []publishListItem `json:"publish_list"`
}

type publishListItem struct {
	PublishInfo string `json:"publish_info"`
}

type publishInfoPayload struct {
	AppMsgEx []articleItem `json:"appmsgex"`
}

type articleItem struct {
	Aid           string             `json:"aid"`
	AppMsgID      int64              `json:"appmsgid"`
	ItemIndex     int                `json:"itemidx"`
	Title         string             `json:"title"`
	AuthorName    string             `json:"author_name"`
	Digest        string             `json:"digest"`
	Link          string             `json:"link"`
	Cover         string             `json:"cover"`
	CreateTime    int64              `json:"create_time"`
	UpdateTime    int64              `json:"update_time"`
	ItemShowType  int                `json:"item_show_type"`
	Deleted       bool               `json:"is_deleted"`
	Paid          int                `json:"is_pay_subscribe"`
	CopyrightStat int                `json:"copyright_stat"`
	CopyrightType int                `json:"copyright_type"`
	WeCoinCount   int                `json:"wecoin_count"`
	MediaDuration any                `json:"media_duration"`
	AlbumID       string             `json:"album_id"`
	AlbumInfos    []articleAlbumInfo `json:"appmsg_album_infos"`
}

type articleAlbumInfo struct {
	ID      string `json:"id"`
	AlbumID any    `json:"album_id"`
	Title   string `json:"title"`
}

func normalizeDiscoveredAccount(item searchAccountItem) (domain.Account, error) {
	fakeID := strings.TrimSpace(item.FakeID)
	name := strings.TrimSpace(item.Nickname)
	if fakeID == "" || name == "" {
		return domain.Account{}, errors.New("fakeid and nickname are required")
	}
	return domain.Account{ID: domain.AccountID(identity.AccountID(fakeID)), FakeID: fakeID, Name: name, Alias: strings.TrimSpace(item.Alias),
		Description: strings.TrimSpace(item.Signature), AvatarURL: strings.TrimSpace(item.RoundHeadImg), ServiceType: item.ServiceType}, nil
}

func normalizeDiscoveredArticle(fakeID string, item articleItem) (domain.Article, error) {
	return normalizeDiscoveredArticleForOrigin(fakeID, item, nil)
}

func normalizeDiscoveredArticleForOrigin(fakeID string, item articleItem, controlledOrigin *url.URL) (domain.Article, error) {
	link := strings.TrimSpace(html.UnescapeString(item.Link))
	if item.Aid == "" || strings.TrimSpace(item.Title) == "" || link == "" {
		return domain.Article{}, errors.New("aid, title, and link are required")
	}
	parsed, err := url.Parse(link)
	if err != nil || parsed == nil || parsed.User != nil {
		return domain.Article{}, errors.New("article link is not a canonical HTTPS WeChat URL")
	}
	if !matchesControlledArticleOrigin(parsed, controlledOrigin) &&
		(parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "mp.weixin.qq.com")) {
		return domain.Article{}, errors.New("article link is not a canonical HTTPS WeChat URL")
	}
	article := domain.Article{
		ID: domain.ArticleID(identity.ArticleID(fakeID, item.Aid)), AccountID: domain.AccountID(identity.AccountID(fakeID)), Aid: item.Aid,
		AppMsgID: item.AppMsgID, ItemIndex: item.ItemIndex, Title: strings.TrimSpace(item.Title),
		Author: strings.TrimSpace(item.AuthorName), Digest: strings.TrimSpace(item.Digest), CanonicalURL: parsed.String(),
		CoverURL: strings.TrimSpace(html.UnescapeString(item.Cover)), PublishedAt: unixSeconds(item.CreateTime),
		UpdatedAt: unixSeconds(item.UpdateTime), MessageType: item.ItemShowType, Deleted: item.Deleted, Paid: item.Paid != 0,
		Original: item.CopyrightStat == 1 && item.CopyrightType == 1, WeCoinCount: item.WeCoinCount,
		MediaDurationSeconds: mediaDurationSeconds(item.MediaDuration),
	}
	seenAlbums := make(map[string]struct{})
	for _, info := range item.AlbumInfos {
		upstreamID := strings.TrimSpace(info.ID)
		if upstreamID == "" {
			upstreamID = strings.TrimSpace(fmt.Sprint(info.AlbumID))
		}
		if upstreamID == "" || upstreamID == "<nil>" {
			continue
		}
		if _, duplicate := seenAlbums[upstreamID]; duplicate {
			continue
		}
		seenAlbums[upstreamID] = struct{}{}
		article.Albums = append(article.Albums, domain.Album{
			ID: domain.AlbumID("album:" + stableDigest(upstreamID)), AccountID: article.AccountID,
			UpstreamID: upstreamID, Name: strings.TrimSpace(info.Title),
		})
	}
	if len(article.Albums) == 0 && strings.TrimSpace(item.AlbumID) != "" {
		upstreamID := strings.TrimSpace(item.AlbumID)
		article.Albums = []domain.Album{{
			ID: domain.AlbumID("album:" + stableDigest(upstreamID)), AccountID: article.AccountID, UpstreamID: upstreamID,
		}}
	}
	return article, nil
}

func mediaDurationSeconds(value any) int {
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case string:
		text := strings.TrimSpace(typed)
		if seconds, err := strconv.Atoi(text); err == nil && seconds > 0 {
			return seconds
		}
		parts := strings.Split(text, ":")
		if len(parts) == 2 || len(parts) == 3 {
			total := 0
			for _, part := range parts {
				component, err := strconv.Atoi(part)
				if err != nil || component < 0 {
					return 0
				}
				total = total*60 + component
			}
			return total
		}
	}
	return 0
}

func stableDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}

func unixSeconds(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}

func decodeDiscoveryJSON(response *http.Response, destination any) error {
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ErrDiscoveryAuthentication
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("WeChat discovery returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: decode JSON: %v", ErrDiscoveryProtocol, err)
	}
	return nil
}

func discoveryBaseError(response baseResponse) error {
	if response.Ret == 0 {
		return nil
	}
	if response.Ret == 200003 || response.Ret == 200013 || response.Ret == -6 {
		return ErrDiscoveryAuthentication
	}
	message := strings.TrimSpace(response.ErrMsg)
	if message == "" {
		message = "unknown upstream error"
	}
	return fmt.Errorf("WeChat discovery failed (%d): %s", response.Ret, message)
}
