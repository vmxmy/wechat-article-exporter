package processor

import "time"

// NormalizedArticleSchemaVersion is independent from WeChat's browser and CGI
// response shapes. Consumers can branch on it when the normalized model evolves.
const NormalizedArticleSchemaVersion = "wechat-article.normalized/v1"

type MessageType string

const (
	MessageTypeGraphic      MessageType = "graphic"
	MessageTypeVideo        MessageType = "video"
	MessageTypeMusic        MessageType = "music"
	MessageTypeAudio        MessageType = "audio"
	MessageTypeImageShare   MessageType = "image_share"
	MessageTypeTextShare    MessageType = "text_share"
	MessageTypeArticleShare MessageType = "article_share"
	MessageTypeShortPost    MessageType = "short_post"
	MessageTypeUnknown      MessageType = "unknown"
)

type Article struct {
	SchemaVersion string     `json:"schemaVersion"`
	Identity      Identity   `json:"identity"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Author        string     `json:"author,omitempty"`
	Account       Account    `json:"account"`
	CanonicalURL  string     `json:"canonicalUrl,omitempty"`
	SourceURL     string     `json:"sourceUrl,omitempty"`
	Content       string     `json:"content,omitempty"`
	Timestamps    Timestamps `json:"timestamps"`
	Message       Message    `json:"message"`
	Payment       Payment    `json:"payment"`
	Media         Media      `json:"media"`
	Albums        []Album    `json:"albums,omitempty"`
	Comments      Comments   `json:"comments"`
	Engagement    Engagement `json:"engagement"`
	Copyright     Copyright  `json:"copyright"`
	Language      string     `json:"language,omitempty"`
	IPLocation    IPLocation `json:"ipLocation"`
}

type Identity struct {
	BusinessID string `json:"businessId,omitempty"`
	MessageID  string `json:"messageId,omitempty"`
	Index      int64  `json:"index,omitempty"`
	Signature  string `json:"signature,omitempty"`
	AppMessage string `json:"appMessageId,omitempty"`
}

type Account struct {
	Username    string `json:"username,omitempty"`
	BusinessID  string `json:"businessId,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	Alias       string `json:"alias,omitempty"`
	AvatarURL   string `json:"avatarUrl,omitempty"`
	HDAvatarURL string `json:"hdAvatarUrl,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

type Timestamps struct {
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	DisplayedAt *time.Time `json:"displayedAt,omitempty"`
	ServerAt    *time.Time `json:"serverAt,omitempty"`
	FilteredAt  *time.Time `json:"filteredAt,omitempty"`
}

type Message struct {
	Type         MessageType `json:"type"`
	UpstreamCode int64       `json:"upstreamCode"`
}

type Payment struct {
	Required       bool   `json:"required"`
	Description    string `json:"description,omitempty"`
	Fee            int64  `json:"fee,omitempty"`
	WeCoinAmount   int64  `json:"weCoinAmount,omitempty"`
	PreviewPercent int64  `json:"previewPercent,omitempty"`
	GiftCount      int64  `json:"giftCount,omitempty"`
}

type Media struct {
	CoverURL string  `json:"coverUrl,omitempty"`
	Images   []Image `json:"images,omitempty"`
	Audio    []Audio `json:"audio,omitempty"`
	Videos   []Video `json:"videos,omitempty"`
}

type Image struct {
	URL       string `json:"url"`
	Caption   string `json:"caption,omitempty"`
	Width     int64  `json:"width,omitempty"`
	Height    int64  `json:"height,omitempty"`
	Format    string `json:"format,omitempty"`
	Watermark bool   `json:"watermark,omitempty"`
}

type Audio struct {
	ID         string `json:"id,omitempty"`
	ListenID   string `json:"listenId,omitempty"`
	Title      string `json:"title,omitempty"`
	URL        string `json:"url,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
	FileSize   int64  `json:"fileSize,omitempty"`
}

type Video struct {
	ID           string `json:"id,omitempty"`
	Title        string `json:"title,omitempty"`
	URL          string `json:"url,omitempty"`
	CoverURL     string `json:"coverUrl,omitempty"`
	DurationMS   int64  `json:"durationMs,omitempty"`
	FileSize     int64  `json:"fileSize,omitempty"`
	Width        int64  `json:"width,omitempty"`
	Height       int64  `json:"height,omitempty"`
	FormatID     int64  `json:"formatId,omitempty"`
	QualityLevel int64  `json:"qualityLevel,omitempty"`
	Quality      string `json:"quality,omitempty"`
	MPVideo      bool   `json:"mpVideo,omitempty"`
}

type Album struct {
	ID           string `json:"id"`
	Title        string `json:"title,omitempty"`
	URL          string `json:"url,omitempty"`
	ContentCount int64  `json:"contentCount,omitempty"`
	Paid         bool   `json:"paid,omitempty"`
	Updating     bool   `json:"updating,omitempty"`
}

type Comments struct {
	ID            string `json:"id,omitempty"`
	SegmentID     string `json:"segmentId,omitempty"`
	ExtraID       string `json:"extraId,omitempty"`
	Enabled       bool   `json:"enabled,omitempty"`
	SelectedCount *int64 `json:"selectedCount,omitempty"`
}

type Engagement struct {
	Reads       *int64 `json:"reads,omitempty"`
	Likes       *int64 `json:"likes,omitempty"`
	OldLikes    *int64 `json:"oldLikes,omitempty"`
	Shares      *int64 `json:"shares,omitempty"`
	Collections *int64 `json:"collections,omitempty"`
	Comments    *int64 `json:"comments,omitempty"`
}

type Copyright struct {
	Status           int64 `json:"status,omitempty"`
	CartoonCopyright bool  `json:"cartoonCopyright,omitempty"`
}

type IPLocation struct {
	Country  string `json:"country,omitempty"`
	Province string `json:"province,omitempty"`
	City     string `json:"city,omitempty"`
}
