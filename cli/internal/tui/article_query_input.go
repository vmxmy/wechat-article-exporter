package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func articleQueryPrompt() string {
	return "Article filter as JSON or key=value pairs separated by ; (keyword, author, state, account, album, from, to, deleted, content, comments, original, paid, types, read, oldLike, share, like, comment, wecoin, mediaSeconds, sort)"
}

func parseArticleQueryInput(value string, limit int) (domain.ArticleQuery, error) {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		limit = 20
	}
	if value == "" {
		query := domain.ArticleQuery{Limit: limit}
		if err := validateArticleQuery(&query, limit); err != nil {
			return domain.ArticleQuery{}, err
		}
		return query, nil
	}
	if strings.HasPrefix(value, "{") {
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.DisallowUnknownFields()
		var query domain.ArticleQuery
		if err := decoder.Decode(&query); err != nil {
			return domain.ArticleQuery{}, fmt.Errorf("decode article query JSON: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return domain.ArticleQuery{}, errors.New("decode article query JSON: multiple JSON values are not allowed")
			}
			return domain.ArticleQuery{}, fmt.Errorf("decode article query JSON trailing data: %w", err)
		}
		query.Offset = 0
		if query.Limit <= 0 {
			query.Limit = limit
		}
		if err := validateArticleQuery(&query, limit); err != nil {
			return domain.ArticleQuery{}, err
		}
		return query, nil
	}

	query := domain.ArticleQuery{Limit: limit}
	values := make(map[string]string)
	for _, field := range strings.Split(value, ";") {
		key, raw, ok := strings.Cut(field, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return domain.ArticleQuery{}, fmt.Errorf("invalid article filter %q; use key=value", field)
		}
		values[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(raw)
	}
	input := articleQueryInput{
		Keyword: values["keyword"], Author: values["author"], State: values["state"], AccountID: values["account"],
		AlbumID: values["album"], PublishedFrom: values["from"], PublishedTo: values["to"], Deleted: values["deleted"],
		HasContent: values["content"], HasComments: values["comments"], Original: values["original"], Paid: values["paid"],
		MessageTypes: values["types"], Read: values["read"], OldLike: values["oldlike"], Share: values["share"],
		Like: values["like"], Comment: values["comment"], WeCoin: values["wecoin"], MediaSeconds: values["mediaseconds"], Sort: values["sort"],
	}
	if err := input.apply(&query); err != nil {
		return domain.ArticleQuery{}, err
	}
	for key := range values {
		switch key {
		case "keyword", "author", "state", "account", "album", "from", "to", "deleted", "content", "comments", "original", "paid",
			"types", "read", "oldlike", "share", "like", "comment", "wecoin", "mediaseconds", "sort":
		default:
			return domain.ArticleQuery{}, fmt.Errorf("unsupported article filter key %q", key)
		}
	}
	if err := validateArticleQuery(&query, limit); err != nil {
		return domain.ArticleQuery{}, err
	}
	return query, nil
}

// articleQueryInput reuses the Cobra decoder's compact range, bool, time, and
// stable multi-sort syntax without pulling those presentation rules into the
// persistence layer.
type articleQueryInput struct {
	Keyword       string
	Author        string
	State         string
	AccountID     string
	AlbumID       string
	PublishedFrom string
	PublishedTo   string
	Deleted       string
	HasContent    string
	HasComments   string
	Original      string
	Paid          string
	MessageTypes  string
	Read          string
	OldLike       string
	Share         string
	Like          string
	Comment       string
	WeCoin        string
	MediaSeconds  string
	Sort          string
}

func (input articleQueryInput) apply(query *domain.ArticleQuery) error {
	query.Keyword = input.Keyword
	query.Author = input.Author
	query.State = input.State
	query.AccountID = domain.AccountID(input.AccountID)
	query.AlbumID = domain.AlbumID(input.AlbumID)
	var err error
	if query.PublishedFrom, err = parseQueryTime(input.PublishedFrom); err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if query.PublishedTo, err = parseQueryEndTime(input.PublishedTo); err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if !query.PublishedTo.IsZero() && query.PublishedFrom.After(query.PublishedTo) {
		return fmt.Errorf("from must not be after to")
	}
	for _, item := range []struct {
		raw         string
		destination **bool
	}{
		{input.Deleted, &query.Deleted}, {input.HasContent, &query.HasContent}, {input.HasComments, &query.HasComments},
		{input.Original, &query.Original}, {input.Paid, &query.Paid},
	} {
		raw, destination := item.raw, item.destination
		if *destination, err = parseQueryBool(raw); err != nil {
			return err
		}
	}
	if query.MessageTypes, err = parseQueryInts(input.MessageTypes); err != nil {
		return fmt.Errorf("types: %w", err)
	}
	for _, item := range []struct {
		raw              string
		minimum, maximum **int
	}{
		{input.Read, &query.ReadMin, &query.ReadMax}, {input.OldLike, &query.OldLikeMin, &query.OldLikeMax},
		{input.Share, &query.ShareMin, &query.ShareMax}, {input.Like, &query.LikeMin, &query.LikeMax},
		{input.Comment, &query.CommentMin, &query.CommentMax}, {input.WeCoin, &query.WeCoinMin, &query.WeCoinMax},
		{input.MediaSeconds, &query.MediaSecondsMin, &query.MediaSecondsMax},
	} {
		raw, minimum, maximum := item.raw, item.minimum, item.maximum
		if *minimum, *maximum, err = parseQueryRange(raw); err != nil {
			return err
		}
	}
	if query.Sorts, err = parseQuerySorts(input.Sort); err != nil {
		return err
	}
	return nil
}

func validateArticleQuery(query *domain.ArticleQuery, pageLimit int) error {
	if query == nil {
		return errors.New("article query is required")
	}
	if pageLimit <= 0 {
		pageLimit = 20
	}
	if pageLimit > 500 {
		pageLimit = 500
	}
	if query.Offset < 0 {
		return errors.New("article query offset must be non-negative")
	}
	query.Offset = 0
	if query.Limit <= 0 {
		query.Limit = pageLimit
	}
	if query.Limit > 500 {
		query.Limit = 500
	}
	if !query.PublishedTo.IsZero() && query.PublishedFrom.After(query.PublishedTo) {
		return errors.New("from must not be after to")
	}
	for _, item := range []struct {
		name             string
		minimum, maximum *int
	}{
		{"read", query.ReadMin, query.ReadMax}, {"oldLike", query.OldLikeMin, query.OldLikeMax},
		{"share", query.ShareMin, query.ShareMax}, {"like", query.LikeMin, query.LikeMax},
		{"comment", query.CommentMin, query.CommentMax}, {"wecoin", query.WeCoinMin, query.WeCoinMax},
		{"mediaSeconds", query.MediaSecondsMin, query.MediaSecondsMax},
	} {
		if item.minimum != nil && *item.minimum < 0 || item.maximum != nil && *item.maximum < 0 {
			return fmt.Errorf("%s bounds must be non-negative", item.name)
		}
		if item.minimum != nil && item.maximum != nil && *item.minimum > *item.maximum {
			return fmt.Errorf("%s minimum must not exceed maximum", item.name)
		}
	}
	for _, messageType := range query.MessageTypes {
		if messageType < 0 {
			return errors.New("message types must be non-negative")
		}
	}
	if query.Sort != "" && len(query.Sorts) > 0 {
		return errors.New("article query must use either sort or sorts, not both")
	}
	if query.Sort != "" {
		switch query.Sort {
		case "published_desc", "published_asc", "title":
		default:
			return fmt.Errorf("article query has unsupported legacy sort %q", query.Sort)
		}
	}
	allowedSorts := map[string]struct{}{
		"aid": {}, "url": {}, "title": {}, "digest": {}, "published": {}, "created": {}, "updated": {},
		"deleted": {}, "state": {}, "content": {}, "comments_downloaded": {}, "read": {}, "old_like": {},
		"share": {}, "like": {}, "comment": {}, "author": {}, "original": {}, "paid": {}, "wecoin": {},
		"message_type": {}, "media_duration": {}, "single": {},
	}
	for _, sort := range query.Sorts {
		if _, ok := allowedSorts[strings.TrimSpace(sort.Field)]; !ok {
			return fmt.Errorf("sort %q has an unsupported field", sort.Field)
		}
		if sort.Direction != domain.SortAscending && sort.Direction != domain.SortDescending {
			return fmt.Errorf("sort %q has an unsupported direction", sort.Field)
		}
	}
	return nil
}

func parseQueryTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("must be RFC3339 or YYYY-MM-DD")
}

func parseQueryEndTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Add(24*time.Hour - time.Nanosecond), nil
	}
	return time.Time{}, fmt.Errorf("must be RFC3339 or YYYY-MM-DD")
}

func parseQueryBool(value string) (*bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, fmt.Errorf("boolean filters must be true or false")
	}
	return &parsed, nil
}

func parseQueryInts(value string) ([]int, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	result := []int{}
	for _, part := range strings.Split(value, ",") {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", part)
		}
		result = append(result, parsed)
	}
	return result, nil
}

func parseQueryRange(value string) (*int, *int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil, nil
	}
	if raw, ok := strings.CutPrefix(value, ">="); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("range %q has an invalid minimum", value)
		}
		return intPointer(parsed), nil, nil
	}
	if raw, ok := strings.CutPrefix(value, "<="); ok {
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, fmt.Errorf("range %q has an invalid maximum", value)
		}
		return nil, intPointer(parsed), nil
	}
	if minimum, maximum, ok := strings.Cut(value, ".."); ok {
		low, lowErr := strconv.Atoi(strings.TrimSpace(minimum))
		high, highErr := strconv.Atoi(strings.TrimSpace(maximum))
		if lowErr != nil || highErr != nil || low > high {
			return nil, nil, fmt.Errorf("range %q must be min..max", value)
		}
		return intPointer(low), intPointer(high), nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, nil, fmt.Errorf("range %q must be an integer, >=min, <=max, or min..max", value)
	}
	return intPointer(parsed), intPointer(parsed), nil
}

func parseQuerySorts(value string) ([]domain.ArticleSort, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	result := []domain.ArticleSort{}
	for _, part := range strings.Split(value, ",") {
		field, direction, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok || strings.TrimSpace(field) == "" {
			return nil, fmt.Errorf("sort %q must be field:asc or field:desc", part)
		}
		direction = strings.ToLower(strings.TrimSpace(direction))
		if direction != string(domain.SortAscending) && direction != string(domain.SortDescending) {
			return nil, fmt.Errorf("sort %q has an unsupported direction", part)
		}
		result = append(result, domain.ArticleSort{Field: strings.TrimSpace(field), Direction: domain.SortDirection(direction)})
	}
	return result, nil
}

func intPointer(value int) *int { return &value }

func formatArticleQueryInput(query domain.ArticleQuery) string {
	encoded, err := json.Marshal(query)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
