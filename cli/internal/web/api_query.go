package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

// maximumWorkspaceQueryTextLength bounds local presentation queries before
// they reach storage adapters. It complements the HTTP header limit and keeps
// all text-backed list and selector queries within a predictable cost.
const maximumWorkspaceQueryTextLength = 512

func parseAccountQuery(request *http.Request) (application.WorkspaceAccountQuery, error) {
	values, err := strictQuery(request.URL.Query(), nil, "keyword", "search", "offset", "limit", "page", "page_size")
	if err != nil {
		return application.WorkspaceAccountQuery{}, err
	}
	page, err := parsePageValues(values)
	if err != nil {
		return application.WorkspaceAccountQuery{}, err
	}
	return application.WorkspaceAccountQuery{Keyword: queryText(values, "keyword"), Page: page}, nil
}

func parseArticleQuery(request *http.Request) (application.WorkspaceArticleQuery, error) {
	values, err := strictQuery(request.URL.Query(), map[string]bool{"messageType": true, "sort": true}, "accountId", "albumId", "keyword", "search", "author", "state", "publishedFrom", "publishedTo",
		"deleted", "hasContent", "hasComments", "original", "paid", "messageType", "readMin", "readMax", "oldLikeMin", "oldLikeMax",
		"shareMin", "shareMax", "likeMin", "likeMax", "commentMin", "commentMax", "weCoinMin", "weCoinMax", "mediaSecondsMin",
		"mediaSecondsMax", "sort", "direction", "offset", "limit", "page", "page_size")
	if err != nil {
		return application.WorkspaceArticleQuery{}, err
	}
	page, err := parsePageValues(values)
	if err != nil {
		return application.WorkspaceArticleQuery{}, err
	}
	query := application.WorkspaceArticleQuery{
		AccountID: strings.TrimSpace(values.Get("accountId")), AlbumID: strings.TrimSpace(values.Get("albumId")), Keyword: queryText(values, "keyword"),
		Author: strings.TrimSpace(values.Get("author")), State: strings.TrimSpace(values.Get("state")), Page: page,
	}
	if query.PublishedFrom, err = parseTime(values, "publishedFrom"); err != nil {
		return application.WorkspaceArticleQuery{}, err
	}
	if query.PublishedTo, err = parseTime(values, "publishedTo"); err != nil {
		return application.WorkspaceArticleQuery{}, err
	}
	if !query.PublishedFrom.IsZero() && !query.PublishedTo.IsZero() && query.PublishedFrom.After(query.PublishedTo) {
		return application.WorkspaceArticleQuery{}, invalidArgument("publishedFrom must not be after publishedTo")
	}
	for _, destination := range []struct {
		name string
		out  **bool
	}{{"deleted", &query.Deleted}, {"hasContent", &query.HasContent}, {"hasComments", &query.HasComments}, {"original", &query.Original}, {"paid", &query.Paid}} {
		if *destination.out, err = parseOptionalBool(values, destination.name); err != nil {
			return application.WorkspaceArticleQuery{}, err
		}
	}
	query.MessageTypes, err = parseRepeatedInts(values, "messageType")
	if err != nil {
		return application.WorkspaceArticleQuery{}, err
	}
	for _, destination := range []struct {
		name string
		out  **int
	}{{"readMin", &query.ReadMin}, {"readMax", &query.ReadMax}, {"oldLikeMin", &query.OldLikeMin}, {"oldLikeMax", &query.OldLikeMax},
		{"shareMin", &query.ShareMin}, {"shareMax", &query.ShareMax}, {"likeMin", &query.LikeMin}, {"likeMax", &query.LikeMax},
		{"commentMin", &query.CommentMin}, {"commentMax", &query.CommentMax}, {"weCoinMin", &query.WeCoinMin}, {"weCoinMax", &query.WeCoinMax},
		{"mediaSecondsMin", &query.MediaSecondsMin}, {"mediaSecondsMax", &query.MediaSecondsMax}} {
		if *destination.out, err = parseOptionalNonNegativeInt(values, destination.name); err != nil {
			return application.WorkspaceArticleQuery{}, err
		}
	}
	for _, pair := range [][2]*int{{query.ReadMin, query.ReadMax}, {query.OldLikeMin, query.OldLikeMax}, {query.ShareMin, query.ShareMax}, {query.LikeMin, query.LikeMax}, {query.CommentMin, query.CommentMax}, {query.WeCoinMin, query.WeCoinMax}, {query.MediaSecondsMin, query.MediaSecondsMax}} {
		if pair[0] != nil && pair[1] != nil && *pair[0] > *pair[1] {
			return application.WorkspaceArticleQuery{}, invalidArgument("minimum filter must not exceed maximum filter")
		}
	}
	query.Sorts, err = parseSorts(values["sort"], values.Get("direction"))
	if err != nil {
		return application.WorkspaceArticleQuery{}, err
	}
	return query, nil
}

func parseAlbumQuery(request *http.Request) (application.WorkspaceAlbumQuery, error) {
	values, err := strictQuery(request.URL.Query(), nil, "accountId", "keyword", "search", "offset", "limit", "page", "page_size")
	if err != nil {
		return application.WorkspaceAlbumQuery{}, err
	}
	page, err := parsePageValues(values)
	if err != nil {
		return application.WorkspaceAlbumQuery{}, err
	}
	return application.WorkspaceAlbumQuery{AccountID: strings.TrimSpace(values.Get("accountId")), Keyword: queryText(values, "keyword"), Page: page}, nil
}

func parseJobQuery(request *http.Request) (application.WorkspaceJobQuery, error) {
	values, err := strictQuery(request.URL.Query(), map[string]bool{"state": true}, "kind", "state", "offset", "limit", "page", "page_size")
	if err != nil {
		return application.WorkspaceJobQuery{}, err
	}
	page, err := parsePageValues(values)
	if err != nil {
		return application.WorkspaceJobQuery{}, err
	}
	states := make([]domain.JobState, 0, len(values["state"]))
	for _, raw := range values["state"] {
		state := domain.JobState(raw)
		if !validJobState(state) {
			return application.WorkspaceJobQuery{}, invalidArgument("job state is not supported")
		}
		states = append(states, state)
	}
	return application.WorkspaceJobQuery{Kind: strings.TrimSpace(values.Get("kind")), States: states, Page: page}, nil
}

func parsePage(request *http.Request) (application.WorkspacePageRequest, error) {
	values, err := strictQuery(request.URL.Query(), nil, "offset", "limit", "page", "page_size")
	if err != nil {
		return application.WorkspacePageRequest{}, err
	}
	return parsePageValues(values)
}

func parsePageValues(values url.Values) (application.WorkspacePageRequest, error) {
	if values.Has("offset") != values.Has("limit") && (values.Has("page") || values.Has("page_size")) {
		return application.WorkspacePageRequest{}, invalidArgument("use either offset and limit or page and page_size")
	}
	if values.Has("offset") || values.Has("limit") {
		if values.Has("page") || values.Has("page_size") {
			return application.WorkspacePageRequest{}, invalidArgument("use either offset and limit or page and page_size")
		}
		return parseOffsetPage(values)
	}
	if values.Has("page") || values.Has("page_size") {
		return parseNumberedPage(values)
	}
	return application.WorkspacePageRequest{}, nil
}

func parseOffsetPage(values url.Values) (application.WorkspacePageRequest, error) {
	offset, err := parseOptionalNonNegativeInt(values, "offset")
	if err != nil {
		return application.WorkspacePageRequest{}, err
	}
	limit, err := parseOptionalNonNegativeInt(values, "limit")
	if err != nil {
		return application.WorkspacePageRequest{}, err
	}
	page := application.WorkspacePageRequest{}
	if offset != nil {
		page.Offset = *offset
	}
	if limit != nil {
		page.Limit = *limit
	}
	if page.Limit > application.WorkspaceMaximumPageLimit {
		return application.WorkspacePageRequest{}, invalidArgument(fmt.Sprintf("page limit must not exceed %d", application.WorkspaceMaximumPageLimit))
	}
	if page.Offset > application.WorkspaceMaximumPageOffset {
		return application.WorkspacePageRequest{}, invalidArgument(fmt.Sprintf("page offset must not exceed %d", application.WorkspaceMaximumPageOffset))
	}
	return page, nil
}

func parseNumberedPage(values url.Values) (application.WorkspacePageRequest, error) {
	pageNumber, err := parseRequiredPositiveInt(values, "page")
	if err != nil {
		return application.WorkspacePageRequest{}, err
	}
	pageSize, err := parseOptionalNonNegativeInt(values, "page_size")
	if err != nil {
		return application.WorkspacePageRequest{}, err
	}
	limit := application.WorkspaceDefaultPageLimit
	if pageSize != nil {
		limit = *pageSize
	}
	if limit == 0 || limit > application.WorkspaceMaximumPageLimit {
		return application.WorkspacePageRequest{}, invalidArgument(fmt.Sprintf("page limit must be between 1 and %d", application.WorkspaceMaximumPageLimit))
	}
	pageOffset := *pageNumber - 1
	if pageOffset > int(^uint(0)>>1)/limit {
		return application.WorkspacePageRequest{}, invalidArgument("page is too large")
	}
	offset := pageOffset * limit
	if offset > application.WorkspaceMaximumPageOffset {
		return application.WorkspacePageRequest{}, invalidArgument(fmt.Sprintf("page offset must not exceed %d", application.WorkspaceMaximumPageOffset))
	}
	return application.WorkspacePageRequest{Offset: offset, Limit: limit}, nil
}

func strictQuery(values url.Values, repeatable map[string]bool, allowed ...string) (url.Values, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, entries := range values {
		if _, ok := allowedSet[key]; !ok || len(entries) == 0 {
			return nil, invalidArgument("query contains an unsupported parameter")
		}
		if !repeatable[key] && len(entries) != 1 {
			return nil, invalidArgument("query parameter must appear once")
		}
		for _, entry := range entries {
			if len([]rune(entry)) > maximumWorkspaceQueryTextLength {
				return nil, invalidArgument(fmt.Sprintf("query text must not exceed %d characters", maximumWorkspaceQueryTextLength))
			}
		}
	}
	return values, nil
}

func queryText(values url.Values, primary string) string {
	if value := strings.TrimSpace(values.Get(primary)); value != "" {
		return value
	}
	return strings.TrimSpace(values.Get("search"))
}

func parseOptionalNonNegativeInt(values url.Values, key string) (*int, error) {
	raw, present := values[key]
	if !present {
		return nil, nil
	}
	if raw[0] == "" {
		return nil, invalidArgument("query number must not be empty")
	}
	value, err := strconv.Atoi(raw[0])
	if err != nil || value < 0 {
		return nil, invalidArgument("query number must be a non-negative integer")
	}
	return &value, nil
}

func parseRequiredPositiveInt(values url.Values, key string) (*int, error) {
	value, err := parseOptionalNonNegativeInt(values, key)
	if err != nil || value == nil || *value == 0 {
		return nil, invalidArgument("page must be a positive integer")
	}
	return value, nil
}

func parseOptionalBool(values url.Values, key string) (*bool, error) {
	raw, present := values[key]
	if !present {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw[0])
	if err != nil {
		return nil, invalidArgument("query boolean must be true or false")
	}
	return &value, nil
}

func parseRepeatedInts(values url.Values, key string) ([]int, error) {
	result := make([]int, 0, len(values[key]))
	for _, raw := range values[key] {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return nil, invalidArgument("query number must be a non-negative integer")
		}
		result = append(result, value)
	}
	return result, nil
}

func parseTime(values url.Values, key string) (time.Time, error) {
	raw, present := values[key]
	if !present {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339, raw[0])
	if err != nil {
		return time.Time{}, invalidArgument("query time must use RFC3339")
	}
	return value, nil
}

func parseSorts(values []string, direction string) ([]domain.ArticleSort, error) {
	result := make([]domain.ArticleSort, 0, len(values))
	for _, raw := range values {
		field, parsedDirection, hasDirection := strings.Cut(raw, ":")
		field = strings.TrimSpace(field)
		parsedDirection = strings.ToLower(strings.TrimSpace(parsedDirection))
		if hasDirection && direction != "" {
			return nil, invalidArgument("use either sort direction or direction")
		}
		if !hasDirection {
			parsedDirection = strings.ToLower(strings.TrimSpace(direction))
		}
		field = articleSortField(field)
		if !validArticleSortField(field) || (parsedDirection != string(domain.SortAscending) && parsedDirection != string(domain.SortDescending)) {
			return nil, invalidArgument("sort must use field:asc or field:desc")
		}
		result = append(result, domain.ArticleSort{Field: field, Direction: domain.SortDirection(parsedDirection)})
	}
	if direction != "" && len(values) != 1 {
		return nil, invalidArgument("direction requires one sort field")
	}
	return result, nil
}

func validArticleSortField(field string) bool {
	field = articleSortField(field)
	switch field {
	case "aid", "url", "title", "digest", "published", "created", "updated", "deleted", "state", "content", "comments_downloaded",
		"read", "old_like", "share", "like", "comment", "author", "original", "paid", "wecoin", "message_type", "media_duration", "single":
		return true
	default:
		return false
	}
}

func articleSortField(field string) string {
	switch field {
	case "publishedAt":
		return "published"
	case "updatedAt":
		return "updated"
	case "canonicalUrl":
		return "url"
	case "messageType":
		return "message_type"
	case "mediaDurationSeconds":
		return "media_duration"
	default:
		return field
	}
}

func validJobState(state domain.JobState) bool {
	switch state {
	case domain.JobQueued, domain.JobRunning, domain.JobCompleted, domain.JobPartial, domain.JobFailed, domain.JobCancelled, domain.JobBlockedAuth, domain.JobPaused:
		return true
	default:
		return false
	}
}

func invalidArgument(message string) error {
	return &application.WorkspaceError{Code: application.WorkspaceErrorInvalidArgument, Message: message}
}
