package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
)

const contentResponseLimit = 32 << 20

var (
	ErrContentProtocol = errors.New("unsupported WeChat content response")
	// ErrContentThrottled is WeChat rate limiting the content surface. It is
	// retryable, unlike an expired credential.
	ErrContentThrottled = errors.New("WeChat content rate limited this request; retry later")
)

type ContentEndpoint struct {
	Network network.Client
	BaseURL *url.URL
}

type CredentialArticleRequest struct {
	URL        string
	Credential credentials.Record
	Class      network.RequestClass
}

type CredentialValidationRequest struct {
	BusinessID string
	Credential credentials.Record
}

type ContentResponse struct {
	Body      []byte
	MediaType string
	Route     string
	RequestID string
}

type RequestProvenance struct {
	Route     string
	RequestID string
}

type Engagement struct {
	ReadCount    int
	OldLikeCount int
	ShareCount   int
	LikeCount    int
	CommentCount int
}

type CommentPageRequest struct {
	BusinessID   string
	AppMessageID int64
	ItemIndex    int
	CommentID    string
	Buffer       string
	Credential   credentials.Record
}

type ReplyPageRequest struct {
	BusinessID   string
	AppMessageID int64
	ItemIndex    int
	CommentID    string
	ContentID    string
	MaxReplyID   int64
	Credential   credentials.Record
}

type Comment struct {
	ID              string
	Author          string
	Content         string
	LikeCount       int
	CreatedAt       time.Time
	ReplyTotal      int
	ReplyMaxID      int64
	EmbeddedReplies []Reply
}

type Reply struct {
	ID        string
	Author    string
	Content   string
	LikeCount int
	CreatedAt time.Time
}

type CommentPage struct {
	Comments []Comment
	Buffer   string
	Continue bool
	Raw      []byte
}

type ReplyPage struct {
	ContentID  string
	Replies    []Reply
	MaxReplyID int64
	Raw        []byte
}

func (endpoint ContentEndpoint) FetchArticle(ctx context.Context, request CredentialArticleRequest) (ContentResponse, error) {
	if endpoint.Network == nil {
		return ContentResponse{}, errors.New("content network client is required")
	}
	target, err := parseContentURL(request.URL)
	if err != nil {
		return ContentResponse{}, err
	}
	class := request.Class
	if class != network.EngagementMetrics && class != network.PaidContent && class != network.ArticleCredential {
		return ContentResponse{}, fmt.Errorf("unsupported credential article request class %q", class)
	}
	header := credentialHeaders(request.Credential)
	result, err := endpoint.Network.Do(ctx, network.Request{
		Class: class, Method: http.MethodGet, URL: target, Header: header, MaxResponseBytes: contentResponseLimit,
	})
	if err != nil {
		return ContentResponse{}, err
	}
	body, mediaType, err := readContentResponse(result.Response)
	if err != nil {
		return ContentResponse{}, err
	}
	if looksLikeExpiredCredential(body) {
		return ContentResponse{}, credentials.ErrCredentialExpired
	}
	return ContentResponse{Body: body, MediaType: mediaType, Route: result.Route, RequestID: result.RequestID}, nil
}

func (endpoint ContentEndpoint) ValidateCredential(ctx context.Context, request CredentialValidationRequest) (RequestProvenance, error) {
	query := url.Values{
		"action": {"getcomment"}, "__biz": {strings.TrimSpace(request.BusinessID)},
		"appmsgid": {"0"}, "idx": {"1"}, "comment_id": {"0"}, "scene": {"0"},
		"offset": {"0"}, "limit": {"1"}, "buffer": {""}, "f": {"json"},
	}
	body, provenance, err := endpoint.fetchJSON(ctx, query, request.Credential)
	if err != nil {
		return provenance, err
	}
	var payload struct {
		BaseResp        *rawBaseResponse `json:"base_resp"`
		Comments        json.RawMessage  `json:"elected_comment"`
		ContinueFlag    json.RawMessage  `json:"continue_flag"`
		Buffer          json.RawMessage  `json:"buffer"`
		CommentIdentity json.RawMessage  `json:"comment_id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provenance, fmt.Errorf("%w: decode credential validation response: %v", ErrContentProtocol, err)
	}
	if payload.BaseResp == nil {
		return provenance, fmt.Errorf("%w: credential validation response is missing base_resp", ErrContentProtocol)
	}
	if err := contentBaseError(*payload.BaseResp); err != nil {
		return provenance, err
	}
	if !validCredentialValidationShape(payload.Comments, payload.ContinueFlag, payload.Buffer, payload.CommentIdentity) {
		return provenance, fmt.Errorf("%w: credential validation response did not contain an authenticated comment-data shape",
			ErrContentProtocol)
	}
	return provenance, nil
}

func validCredentialValidationShape(comments, continueFlag, buffer, commentIdentity json.RawMessage) bool {
	if len(comments) > 0 {
		var value []json.RawMessage
		if json.Unmarshal(comments, &value) == nil && value != nil {
			return true
		}
	}
	if len(continueFlag) > 0 {
		var value int
		if json.Unmarshal(continueFlag, &value) == nil {
			return true
		}
	}
	for _, raw := range []json.RawMessage{buffer, commentIdentity} {
		if len(raw) == 0 {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) == nil {
			return true
		}
	}
	return false
}

func (endpoint ContentEndpoint) FetchComments(ctx context.Context, request CommentPageRequest) (CommentPage, RequestProvenance, error) {
	query := contentIdentityQuery(request.BusinessID, request.AppMessageID, request.ItemIndex, request.CommentID)
	query.Set("action", "getcomment")
	query.Set("comment_scene", "0")
	query.Set("buffer", request.Buffer)
	query.Set("offset", "0")
	query.Set("limit", "100")
	body, provenance, err := endpoint.fetchJSON(ctx, query, request.Credential)
	if err != nil {
		return CommentPage{}, provenance, err
	}
	page, err := decodeCommentPage(body)
	return page, provenance, err
}

func (endpoint ContentEndpoint) FetchReplies(ctx context.Context, request ReplyPageRequest) (ReplyPage, RequestProvenance, error) {
	query := contentIdentityQuery(request.BusinessID, request.AppMessageID, request.ItemIndex, request.CommentID)
	query.Set("action", "getcommentreply")
	query.Set("content_id", strings.TrimSpace(request.ContentID))
	query.Set("max_reply_id", strconv.FormatInt(request.MaxReplyID, 10))
	query.Set("limit", "100")
	body, provenance, err := endpoint.fetchJSON(ctx, query, request.Credential)
	if err != nil {
		return ReplyPage{}, provenance, err
	}
	page, err := decodeReplyPage(body)
	return page, provenance, err
}

func (endpoint ContentEndpoint) fetchJSON(ctx context.Context, query url.Values, credential credentials.Record) ([]byte, RequestProvenance, error) {
	if endpoint.Network == nil {
		return nil, RequestProvenance{}, errors.New("content network client is required")
	}
	base := endpoint.BaseURL
	if base == nil {
		base, _ = url.Parse(upstreamOrigin)
	}
	target := *base
	target.Path = "/mp/appmsg_comment"
	target.RawQuery = query.Encode()
	result, err := endpoint.Network.Do(ctx, network.Request{
		Class: network.Comments, Method: http.MethodGet, URL: &target,
		Header: credentialHeaders(credential), MaxResponseBytes: 8 << 20,
	})
	provenance := RequestProvenance{Route: result.Route, RequestID: result.RequestID}
	if err != nil {
		return nil, provenance, err
	}
	body, _, err := readContentResponse(result.Response)
	if err != nil {
		return nil, provenance, err
	}
	if looksLikeExpiredCredential(body) {
		return nil, provenance, credentials.ErrCredentialExpired
	}
	return body, provenance, nil
}

func contentIdentityQuery(businessID string, appMessageID int64, itemIndex int, commentID string) url.Values {
	return url.Values{
		"scene": {"0"}, "appmsgid": {strconv.FormatInt(appMessageID, 10)}, "idx": {strconv.Itoa(itemIndex)},
		"__biz": {strings.TrimSpace(businessID)}, "comment_id": {strings.TrimSpace(commentID)},
		"wxtoken": {"777"}, "devicetype": {"UnifiedPCMac"},
		"x5": {"0"}, "f": {"json"},
	}
}

func credentialHeaders(credential credentials.Record) http.Header {
	header := make(http.Header)
	header.Set("User-Agent", "Mozilla/5.0 MicroMessenger/8.0 wechat-article-local/2")
	header.Set("Referer", upstreamOrigin+"/")
	cookie := strings.TrimSpace(credential.Cookie)
	if cookie == "" {
		cookie = "pass_ticket=" + url.QueryEscape(credential.PassTicket) + "; wap_sid2=" + url.QueryEscape(credential.WapSID2)
	}
	if cookie != "" {
		header.Set("Cookie", cookie)
	}
	// Query strings are routinely logged by HTTP infrastructure. Keep article
	// credential fields in headers/cookies so the route can redact them as one
	// sensitive envelope and URL provenance remains safe to persist.
	header.Set("X-WeChat-UIN", credential.UIN)
	header.Set("X-WeChat-Key", credential.Key)
	header.Set("X-WeChat-Pass-Ticket", credential.PassTicket)
	header.Set("X-WeChat-AppMsg-Token", credential.AppMsgToken)
	return header
}

func parseContentURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("content URL must be an absolute URL without user information")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, errors.New("content URL must use HTTPS or approved loopback HTTP")
	}
	parsed.Fragment = ""
	return parsed, nil
}

func readContentResponse(response *http.Response) ([]byte, string, error) {
	if response == nil || response.Body == nil {
		return nil, "", errors.New("WeChat content request returned no response")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, "", credentials.ErrCredentialExpired
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("WeChat content request returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, contentResponseLimit+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > contentResponseLimit {
		return nil, "", fmt.Errorf("WeChat content response exceeded %d bytes", contentResponseLimit)
	}
	return body, response.Header.Get("Content-Type"), nil
}

func DecodeEngagement(body []byte) (Engagement, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return Engagement{}, fmt.Errorf("%w: decode engagement JSON: %v", ErrContentProtocol, err)
	}
	bar := nestedMap(payload, "user_info", "appmsg_bar_data")
	if len(bar) == 0 {
		bar = nestedMap(payload, "appmsg_bar_data")
	}
	if len(bar) == 0 {
		bar = nestedMap(payload, "user_info")
	}
	if len(bar) == 0 {
		return Engagement{}, fmt.Errorf("%w: engagement fields are missing", ErrContentProtocol)
	}
	return Engagement{
		ReadCount: intValue(bar["read_num"]), OldLikeCount: intValue(bar["old_like_count"]),
		ShareCount: intValue(bar["share_count"]), LikeCount: intValue(bar["like_count"]),
		CommentCount: intValue(bar["comment_count"]),
	}, nil
}

type rawBaseResponse struct {
	Ret    int    `json:"ret"`
	ErrMsg string `json:"err_msg"`
}

type rawReply struct {
	ID        any    `json:"reply_id"`
	Author    string `json:"nick_name"`
	Content   string `json:"content"`
	LikeCount int    `json:"reply_like_num"`
	CreatedAt int64  `json:"create_time"`
}

type rawReplyList struct {
	MaxReplyID int64      `json:"max_reply_id"`
	Replies    []rawReply `json:"reply_list"`
}

type rawComment struct {
	ID         string       `json:"content_id"`
	Author     string       `json:"nick_name"`
	Content    string       `json:"content"`
	LikeCount  int          `json:"like_num"`
	CreatedAt  int64        `json:"create_time"`
	Replies    rawReplyList `json:"reply_new"`
	ReplyTotal int          `json:"-"`
}

func decodeCommentPage(body []byte) (CommentPage, error) {
	var payload struct {
		BaseResp    rawBaseResponse   `json:"base_resp"`
		ContinueRaw any               `json:"continue_flag"`
		Buffer      string            `json:"buffer"`
		Comments    []json.RawMessage `json:"elected_comment"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return CommentPage{}, fmt.Errorf("%w: decode comments: %v", ErrContentProtocol, err)
	}
	if err := contentBaseError(payload.BaseResp); err != nil {
		return CommentPage{}, err
	}
	page := CommentPage{Buffer: payload.Buffer, Continue: boolValue(payload.ContinueRaw), Raw: append([]byte(nil), body...)}
	for _, encoded := range payload.Comments {
		var commentPayload struct {
			ID        string `json:"content_id"`
			Author    string `json:"nick_name"`
			Content   string `json:"content"`
			LikeCount int    `json:"like_num"`
			CreatedAt int64  `json:"create_time"`
			Replies   struct {
				ReplyTotal int        `json:"reply_total_cnt"`
				MaxReplyID int64      `json:"max_reply_id"`
				Replies    []rawReply `json:"reply_list"`
			} `json:"reply_new"`
		}
		if err := json.Unmarshal(encoded, &commentPayload); err != nil {
			return CommentPage{}, fmt.Errorf("%w: decode comment: %v", ErrContentProtocol, err)
		}
		if strings.TrimSpace(commentPayload.ID) == "" {
			return CommentPage{}, fmt.Errorf("%w: comment identifier is missing", ErrContentProtocol)
		}
		comment := Comment{ID: commentPayload.ID, Author: commentPayload.Author, Content: commentPayload.Content,
			LikeCount: commentPayload.LikeCount, CreatedAt: unixContentTime(commentPayload.CreatedAt),
			ReplyTotal: commentPayload.Replies.ReplyTotal, ReplyMaxID: commentPayload.Replies.MaxReplyID}
		comment.EmbeddedReplies = normalizeReplies(commentPayload.Replies.Replies)
		page.Comments = append(page.Comments, comment)
	}
	return page, nil
}

func decodeReplyPage(body []byte) (ReplyPage, error) {
	var payload struct {
		BaseResp  rawBaseResponse `json:"base_resp"`
		ContentID string          `json:"content_id"`
		Replies   rawReplyList    `json:"reply_list"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ReplyPage{}, fmt.Errorf("%w: decode replies: %v", ErrContentProtocol, err)
	}
	if err := contentBaseError(payload.BaseResp); err != nil {
		return ReplyPage{}, err
	}
	return ReplyPage{ContentID: payload.ContentID, Replies: normalizeReplies(payload.Replies.Replies),
		MaxReplyID: payload.Replies.MaxReplyID, Raw: append([]byte(nil), body...)}, nil
}

func normalizeReplies(values []rawReply) []Reply {
	result := make([]Reply, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(fmt.Sprint(value.ID))
		if id == "" || id == "<nil>" {
			continue
		}
		result = append(result, Reply{ID: id, Author: value.Author, Content: value.Content,
			LikeCount: value.LikeCount, CreatedAt: unixContentTime(value.CreatedAt)})
	}
	return result
}

func contentBaseError(response rawBaseResponse) error {
	if response.Ret == 0 {
		return nil
	}
	// Frequency control is transient and must not be reported as an expired
	// credential: that classification blocks the job and asks the user to sign in
	// again for something only a retry can clear.
	switch response.Ret {
	case retInvalidSession, retSecurityVerification:
		return fmt.Errorf("%w (%d)", credentials.ErrCredentialExpired, response.Ret)
	case retFrequencyControl:
		return fmt.Errorf("%w (%d)", ErrContentThrottled, response.Ret)
	}
	message := strings.TrimSpace(response.ErrMsg)
	if message == "" {
		message = "unknown upstream error"
	}
	return fmt.Errorf("WeChat content request failed (%d): %s", response.Ret, message)
}

func looksLikeExpiredCredential(body []byte) bool {
	lower := bytes.ToLower(body)
	return bytes.Contains(lower, []byte("/cgi-bin/bizlogin")) ||
		bytes.Contains(lower, []byte("login_page")) ||
		bytes.Contains(body, []byte("登录后可访问"))
}

func nestedMap(value map[string]any, path ...string) map[string]any {
	current := value
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	case int:
		return typed
	case int64:
		return int(typed)
	}
	return 0
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	}
	return false
}

func unixContentTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(value, 0)
}
