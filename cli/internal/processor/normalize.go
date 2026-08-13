package processor

import (
	"encoding/json"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func normalizeArticle(payload map[string]any, location *time.Location) (Article, error) {
	title := stringValue(payload["title"])
	account := Account{
		Username:    stringValue(payload["user_name"]),
		BusinessID:  stringValue(payload["bizuin"]),
		Nickname:    stringValue(payload["nick_name"]),
		Alias:       stringValue(payload["alias"]),
		AvatarURL:   normalizeURL(stringValue(payload["round_head_img"])),
		HDAvatarURL: normalizeURL(stringValue(payload["hd_head_img"])),
		Signature:   stringValue(payload["signature"]),
	}
	if title == "" {
		return Article{}, processError(ErrorInvalid, ReasonInvalidArticle, 0, "normalized article is missing a title")
	}
	if account.Username == "" && account.BusinessID == "" && account.Nickname == "" {
		return Article{}, processError(ErrorInvalid, ReasonInvalidArticle, 0, "normalized article is missing account identity")
	}

	messageCode := int64Value(payload["real_item_show_type"])
	if messageCode == 0 {
		messageCode = int64Value(payload["item_show_type"])
	}
	article := Article{
		SchemaVersion: NormalizedArticleSchemaVersion,
		Identity: Identity{
			BusinessID: account.BusinessID,
			MessageID:  stringValue(payload["mid"]),
			Index:      int64Value(payload["idx"]),
			Signature:  stringValue(payload["sn"]),
			AppMessage: firstString(payload, "appmsgid", "appmsg_id", "aid"),
		},
		Title:        title,
		Description:  stringValue(payload["desc"]),
		Author:       stringValue(payload["author"]),
		Account:      account,
		CanonicalURL: normalizeURL(stringValue(payload["link"])),
		SourceURL:    normalizeURL(stringValue(payload["source_url"])),
		Content:      normalizeContent(payload),
		Timestamps: Timestamps{
			PublishedAt: firstTimestamp(payload, location, "ori_create_time", "ori_send_time", "create_timestamp"),
			DisplayedAt: parseDisplayTimestamp(stringValue(payload["create_time"]), location),
			ServerAt:    unixTimestamp(payload["svr_time"]),
			FilteredAt:  unixTimestamp(payload["filter_time"]),
		},
		Message: Message{Type: normalizeMessageType(messageCode), UpstreamCode: messageCode},
		Payment: normalizePayment(payload),
		Media: Media{
			CoverURL: normalizeURL(firstString(payload, "cdn_url", "cdn_url_16_9", "cdn_url_3_4")),
			Images:   normalizeImages(arrayValue(payload["picture_page_info_list"]), stringValue(payload["img_format"])),
			Audio:    normalizeAudio(arrayValue(payload["voice_in_appmsg"])),
			Videos:   normalizeVideos(payload),
		},
		Albums:     normalizeAlbums(payload),
		Comments:   normalizeComments(payload),
		Engagement: normalizeEngagement(payload),
		Copyright: Copyright{
			Status:           int64Value(nestedValue(payload, "copyright_info", "copyright_stat")),
			CartoonCopyright: boolValue(nestedValue(payload, "copyright_info", "is_cartoon_copyright")),
		},
		Language: stringValue(payload["lang"]),
		IPLocation: IPLocation{
			Country:  stringValue(nestedValue(payload, "ip_wording", "country_name")),
			Province: stringValue(nestedValue(payload, "ip_wording", "province_name")),
			City:     stringValue(nestedValue(payload, "ip_wording", "city_name")),
		},
	}
	return article, nil
}

func normalizeContent(payload map[string]any) string {
	if textInfo := objectValue(payload["text_page_info"]); textInfo != nil {
		if content := firstString(textInfo, "content_noencode", "content"); content != "" {
			return content
		}
	}
	return stringValue(payload["content_noencode"])
}

func normalizeMessageType(code int64) MessageType {
	switch code {
	case 0:
		return MessageTypeGraphic
	case 5:
		return MessageTypeVideo
	case 6:
		return MessageTypeMusic
	case 7:
		return MessageTypeAudio
	case 8:
		return MessageTypeImageShare
	case 10:
		return MessageTypeTextShare
	case 11:
		return MessageTypeArticleShare
	case 17:
		return MessageTypeShortPost
	default:
		return MessageTypeUnknown
	}
}

func normalizePayment(payload map[string]any) Payment {
	info := objectValue(payload["pay_subscribe_info"])
	return Payment{
		Required:       boolValue(payload["is_pay_subscribe"]),
		Description:    stringValue(info["desc"]),
		Fee:            int64Value(info["fee"]),
		WeCoinAmount:   int64Value(info["wecoin_amount"]),
		PreviewPercent: int64Value(info["preview_percent"]),
		GiftCount:      int64Value(info["gifts_count"]),
	}
}

func normalizeImages(values []any, fallbackFormat string) []Image {
	images := make([]Image, 0, len(values))
	for _, value := range values {
		item := objectValue(value)
		imageURL := normalizeURL(firstString(item, "cdn_url", "url"))
		if imageURL == "" {
			continue
		}
		format := firstString(item, "format", "img_format")
		if format == "" {
			format = fallbackFormat
		}
		images = append(images, Image{
			URL:       imageURL,
			Caption:   firstString(item, "caption", "desc", "title"),
			Width:     int64Value(item["width"]),
			Height:    int64Value(item["height"]),
			Format:    format,
			Watermark: boolValue(item["show_watermark"]),
		})
	}
	return images
}

func normalizeAudio(values []any) []Audio {
	audio := make([]Audio, 0, len(values))
	for _, value := range values {
		item := objectValue(value)
		entry := Audio{
			ID:         firstString(item, "voice_id", "voiceid", "id"),
			ListenID:   firstString(item, "listen_id", "listenid"),
			Title:      firstString(item, "title", "music_name", "name"),
			URL:        normalizeURL(firstString(item, "voice_url", "url", "play_url")),
			DurationMS: durationMilliseconds(item, "duration_ms", "duration", "play_length", "play_length_ms"),
			FileSize:   firstInt64(item, "file_size", "filesize", "size"),
		}
		if entry.ID != "" || entry.ListenID != "" || entry.URL != "" || entry.Title != "" {
			audio = append(audio, entry)
		}
	}
	return audio
}

func normalizeVideos(payload map[string]any) []Video {
	values := append([]any{}, arrayValue(payload["video_page_infos"])...)
	pageInfo := objectValue(payload["video_page_info"])
	if len(pageInfo) > 0 {
		values = append(values, pageInfo)
	}
	values = append(values, arrayValue(payload["video_in_article"])...)

	videos := make([]Video, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		item := objectValue(value)
		transcodes := arrayValue(item["mp_video_trans_info"])
		if len(transcodes) == 0 {
			transcodes = []any{item}
		}
		for _, transcodeValue := range transcodes {
			transcode := objectValue(transcodeValue)
			entry := Video{
				ID:           firstString(item, "video_id", "vid", "id"),
				Title:        firstString(item, "title", "video_title"),
				URL:          normalizeURL(firstNonEmpty(firstString(transcode, "url", "video_url", "play_url"), firstString(item, "url", "video_url", "play_url"))),
				CoverURL:     normalizeURL(firstString(item, "cover_url", "cover", "cdn_url")),
				DurationMS:   firstNonZero(durationMilliseconds(transcode, "duration_ms", "duration"), durationMilliseconds(item, "duration_ms", "duration")),
				FileSize:     firstNonZero(firstInt64(transcode, "file_size", "filesize", "size"), firstInt64(item, "file_size", "filesize", "size")),
				Width:        firstNonZero(firstInt64(transcode, "width"), firstInt64(item, "width")),
				Height:       firstNonZero(firstInt64(transcode, "height"), firstInt64(item, "height")),
				FormatID:     firstInt64(transcode, "format_id"),
				QualityLevel: firstInt64(transcode, "video_quality_level", "quality_level"),
				Quality:      firstString(transcode, "video_quality_wording", "quality", "format", "resolution"),
				MPVideo:      boolValue(item["is_mp_video"]),
			}
			if entry.ID == "" && entry.URL == "" && entry.CoverURL == "" {
				continue
			}
			key := entry.ID + "\x00" + entry.URL + "\x00" + entry.Quality
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			videos = append(videos, entry)
		}
	}
	return videos
}

func normalizeAlbums(payload map[string]any) []Album {
	values := make([]any, 0)
	if album := objectValue(payload["appmsgalbuminfo"]); len(album) > 0 {
		values = append(values, album)
	}
	values = append(values, arrayValue(payload["appmsg_album_infos"])...)
	for _, tagValue := range arrayValue(nestedValue(payload, "public_tag_info", "tags")) {
		tag := objectValue(tagValue)
		if album := objectValue(tag["album_info"]); len(album) > 0 {
			values = append(values, album)
		}
	}

	albums := make([]Album, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		item := objectValue(value)
		id := firstString(item, "album_id_str", "album_id", "id")
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		albums = append(albums, Album{
			ID:           id,
			Title:        firstString(item, "title", "name"),
			URL:          normalizeURL(firstString(item, "link", "url")),
			ContentCount: firstInt64(item, "content_size", "content_count", "tag_content_num"),
			Paid:         boolValue(item["album_needpay"]) || boolValue(item["appmsg_needpay"]),
			Updating:     boolValue(item["isupdating"]),
		})
	}
	return albums
}

func normalizeComments(payload map[string]any) Comments {
	selected := optionalInt64(nestedValue(payload, "rt_biz_info", "elected_comment_total_cnt"))
	return Comments{
		ID:            stringValue(payload["comment_id"]),
		SegmentID:     stringValue(payload["segment_comment_id"]),
		ExtraID:       stringValue(payload["extra_comment_id"]),
		Enabled:       boolValue(payload["show_comment"]) || selected != nil,
		SelectedCount: selected,
	}
}

func normalizeEngagement(payload map[string]any) Engagement {
	bar := objectValue(nestedValue(payload, "user_info", "appmsg_bar_data"))
	if len(bar) == 0 {
		bar = objectValue(payload["appmsg_bar_data"])
	}
	return Engagement{
		Reads:       firstOptionalInt64(payload["read_num_new"], payload["read_num"], bar["read_num"]),
		Likes:       firstOptionalInt64(payload["like_num"], bar["like_count"]),
		OldLikes:    firstOptionalInt64(payload["old_like_num"], bar["old_like_count"]),
		Shares:      firstOptionalInt64(payload["share_num"], bar["share_count"]),
		Collections: firstOptionalInt64(payload["collect_num"], bar["collect_count"]),
		Comments:    firstOptionalInt64(payload["comment_num"], bar["comment_count"]),
	}
}

func parseDisplayTimestamp(value string, location *time.Location) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", time.RFC3339} {
		var parsed time.Time
		var err error
		if layout == time.RFC3339 {
			parsed, err = time.Parse(layout, value)
		} else {
			parsed, err = time.ParseInLocation(layout, value, location)
		}
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func unixTimestamp(value any) *time.Time {
	seconds, ok := int64OK(value)
	if !ok || seconds <= 0 {
		return nil
	}
	parsed := time.Unix(seconds, 0).UTC()
	return &parsed
}

func firstTimestamp(payload map[string]any, location *time.Location, keys ...string) *time.Time {
	for _, key := range keys {
		if parsed := unixTimestamp(payload[key]); parsed != nil {
			return parsed
		}
		if parsed := parseDisplayTimestamp(stringValue(payload[key]), location); parsed != nil {
			return parsed
		}
	}
	return nil
}

func durationMilliseconds(item map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := int64OK(item[key])
		if !ok || value <= 0 {
			continue
		}
		if strings.Contains(key, "_ms") || value > 100_000 {
			return value
		}
		return value * 1000
	}
	return 0
}

func normalizeURL(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	for strings.Contains(value, "&amp;") {
		value = strings.ReplaceAll(value, "&amp;", "&")
	}
	// Raw whitespace, quotes, or angle brackets never appear in a legitimate
	// resource URL (they must be percent-encoded), and they are exactly the
	// characters that let a crafted value smuggle attribute- or tag-shaped
	// text into rendered markup. url.Parse tolerates them, so reject here.
	if strings.ContainsAny(value, " \t\r\n\"'<>") {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return value
}

func objectValue(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func arrayValue(value any) []any {
	if array, ok := value.([]any); ok {
		return array
	}
	return nil
}

func nestedValue(object map[string]any, path ...string) any {
	var value any = object
	for _, key := range path {
		current, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = current[key]
	}
	return value
}

func stringValue(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case json.Number:
		return item.String()
	case float64:
		return strconv.FormatFloat(item, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(item), 'f', -1, 32)
	case int:
		return strconv.Itoa(item)
	case int64:
		return strconv.FormatInt(item, 10)
	case int32:
		return strconv.FormatInt(int64(item), 10)
	default:
		return ""
	}
}

func boolValue(value any) bool {
	switch item := value.(type) {
	case bool:
		return item
	case json.Number:
		parsed, err := item.Float64()
		return err == nil && parsed != 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		return err == nil && parsed != 0 || strings.EqualFold(strings.TrimSpace(item), "true")
	default:
		return false
	}
}

func int64Value(value any) int64 {
	parsed, _ := int64OK(value)
	return parsed
}

func int64OK(value any) (int64, bool) {
	switch item := value.(type) {
	case json.Number:
		if parsed, err := item.Int64(); err == nil {
			return parsed, true
		}
		parsed, err := item.Float64()
		return int64(parsed), err == nil
	case string:
		if strings.TrimSpace(item) == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64); err == nil {
			return parsed, true
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		return int64(parsed), err == nil
	case float64:
		return int64(item), true
	case float32:
		return int64(item), true
	case int:
		return int64(item), true
	case int64:
		return item, true
	case int32:
		return int64(item), true
	case bool:
		if item {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func optionalInt64(value any) *int64 {
	parsed, ok := int64OK(value)
	if !ok {
		return nil
	}
	return &parsed
}

func firstOptionalInt64(values ...any) *int64 {
	for _, value := range values {
		if parsed := optionalInt64(value); parsed != nil {
			return parsed
		}
	}
	return nil
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstInt64(object map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := int64OK(object[key]); ok {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
