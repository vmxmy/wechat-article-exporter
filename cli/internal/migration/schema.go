package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	ArchiveFormat        = "wechat-article-exporter-legacy-archive"
	CurrentSchemaVersion = 1
	ManifestPath         = "manifest.json"
)

type Dataset string

const (
	DatasetAccounts     Dataset = "accounts"
	DatasetArticles     Dataset = "articles"
	DatasetHTML         Dataset = "html"
	DatasetMetadata     Dataset = "metadata"
	DatasetMetrics      Dataset = "metrics"
	DatasetComments     Dataset = "comments"
	DatasetReplies      Dataset = "replies"
	DatasetResourceMaps Dataset = "resource-maps"
	DatasetResources    Dataset = "resources"
	DatasetAssets       Dataset = "assets"
)

var supportedDatasets = map[Dataset]struct{}{
	DatasetAccounts: {}, DatasetArticles: {}, DatasetHTML: {}, DatasetMetadata: {}, DatasetMetrics: {},
	DatasetComments: {}, DatasetReplies: {}, DatasetResourceMaps: {}, DatasetResources: {},
	DatasetAssets: {},
}

type FileKind string

const (
	FileRecords FileKind = "records"
	FileObject  FileKind = "object"
)

type SourceInfo struct {
	Application        string `json:"application,omitempty"`
	DexieDatabase      string `json:"dexieDatabase,omitempty"`
	DexieSchemaVersion int    `json:"dexieSchemaVersion,omitempty"`
	Database           string `json:"database,omitempty"`
	DexieVersion       int    `json:"dexieVersion,omitempty"`
}

func (source SourceInfo) DatabaseName() string {
	if source.DexieDatabase != "" {
		return source.DexieDatabase
	}
	return source.Database
}

func (source SourceInfo) SchemaVersion() int {
	if source.DexieSchemaVersion != 0 {
		return source.DexieSchemaVersion
	}
	return source.DexieVersion
}

type TableManifest struct {
	SourceTable string `json:"sourceTable"`
	Path        string `json:"path"`
	Records     int    `json:"records"`
}

type MissingResource struct {
	ArticleURL  string `json:"articleUrl"`
	ResourceURL string `json:"resourceUrl"`
	Reason      string `json:"reason"`
}

type ManifestWarning struct {
	Code    string `json:"code"`
	Table   string `json:"table"`
	Key     string `json:"key"`
	Message string `json:"message"`
}

type ManifestFile struct {
	Path      string   `json:"path"`
	Kind      FileKind `json:"kind"`
	Dataset   Dataset  `json:"dataset,omitempty"`
	Size      int64    `json:"size"`
	SHA256    string   `json:"sha256"`
	MediaType string   `json:"mediaType,omitempty"`
}

type Manifest struct {
	Format           string                   `json:"format"`
	SchemaVersion    int                      `json:"schemaVersion"`
	CreatedAt        time.Time                `json:"createdAt"`
	Status           string                   `json:"status,omitempty"`
	Source           SourceInfo               `json:"source"`
	Counts           map[Dataset]int          `json:"counts,omitempty"`
	Tables           map[string]TableManifest `json:"tables,omitempty"`
	MissingResources []MissingResource        `json:"missingResources,omitempty"`
	Warnings         []ManifestWarning        `json:"warnings,omitempty"`
	ChecksumFile     string                   `json:"checksumFile,omitempty"`
	Files            []ManifestFile           `json:"files,omitempty"`
}

func (manifest Manifest) Validate() error {
	if manifest.Format != ArchiveFormat {
		return fmt.Errorf("unsupported archive format %q", manifest.Format)
	}
	if manifest.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported archive schema version %d", manifest.SchemaVersion)
	}
	if manifest.Source.DatabaseName() != "exporter.wxdown.online" {
		return fmt.Errorf("unsupported source database %q", manifest.Source.DatabaseName())
	}
	if manifest.Source.SchemaVersion() < 1 || manifest.Source.SchemaVersion() > 3 {
		return fmt.Errorf("unsupported Dexie schema version %d", manifest.Source.SchemaVersion())
	}
	if len(manifest.Files) == 0 && manifest.ChecksumFile == "" {
		return errors.New("archive must declare files or a checksum file")
	}
	for _, file := range manifest.Files {
		if file.Kind != FileRecords && file.Kind != FileObject {
			return fmt.Errorf("unsupported file kind %q", file.Kind)
		}
		if file.Kind == FileRecords {
			if _, ok := supportedDatasets[file.Dataset]; !ok {
				return fmt.Errorf("unsupported dataset %q", file.Dataset)
			}
		}
	}
	return nil
}

type RecordKind string

const (
	RecordAccount     RecordKind = "account"
	RecordArticle     RecordKind = "article"
	RecordHTML        RecordKind = "html"
	RecordMetric      RecordKind = "metric"
	RecordComment     RecordKind = "comment"
	RecordReply       RecordKind = "reply"
	RecordResourceMap RecordKind = "resource-map"
	RecordResource    RecordKind = "resource"
)

type RecordIdentity struct {
	Kind RecordKind
	Key  string
}

func (identity RecordIdentity) String() string { return string(identity.Kind) + ":" + identity.Key }

type Account struct {
	FakeID        string
	Name          string
	AvatarURL     string
	Completed     bool
	MessageCount  int
	ArticleCount  int
	UpstreamTotal int
	UpdatedAt     time.Time
}

type Article struct {
	FakeID       string
	Aid          string
	AppMsgID     int64
	ItemIndex    int
	Title        string
	Author       string
	Digest       string
	CanonicalURL string
	CoverURL     string
	PublishedAt  time.Time
	UpdatedAt    time.Time
	MessageType  int
	State        string
	Deleted      bool
	Paid         bool
	Single       bool
}

type HTML struct {
	FakeID       string
	URL          string
	Title        string
	CommentID    string
	ObjectDigest string
	MediaType    string
	UpdatedAt    time.Time
}

type Metric struct {
	FakeID       string
	URL          string
	ReadCount    int
	OldLikeCount int
	ShareCount   int
	LikeCount    int
	CommentCount int
	CapturedAt   time.Time
}

type Comment struct {
	FakeID     string
	URL        string
	UpstreamID string
	AuthorName string
	Content    string
	LikeCount  int
	CreatedAt  time.Time
	FetchedAt  time.Time
	ReplyTotal int
	ReplyMaxID int64
	Complete   bool
	Buffer     string
}

type Reply struct {
	FakeID            string
	URL               string
	CommentUpstreamID string
	UpstreamID        string
	AuthorName        string
	Content           string
	LikeCount         int
	CreatedAt         time.Time
	FetchedAt         time.Time
	MaxReplyID        int64
}

type ResourceMap struct {
	FakeID    string
	URL       string
	Resources []string
	UpdatedAt time.Time
}

type Resource struct {
	FakeID       string
	URL          string
	ObjectDigest string
	MediaType    string
	UpdatedAt    time.Time
}

type ContentReference struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType"`
}

type RecordData struct {
	Account     *Account
	Article     *Article
	HTML        *HTML
	Metric      *Metric
	Comment     *Comment
	Reply       *Reply
	ResourceMap *ResourceMap
	Resource    *Resource
}

type Record struct {
	Kind        RecordKind
	Key         string
	UpdatedAt   time.Time
	Fingerprint string
	Data        RecordData
}

func (record Record) Identity() RecordIdentity {
	return RecordIdentity{Kind: record.Kind, Key: record.Key}
}

func recordKey(kind RecordKind, parts ...string) string {
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return strings.Join(parts, "\x00")
}

type wireAccount struct {
	FakeID         string `json:"fakeid"`
	Nickname       string `json:"nickname"`
	AvatarURL      string `json:"round_head_img"`
	Completed      bool   `json:"completed"`
	MessageCount   int    `json:"count"`
	ArticleCount   int    `json:"articles"`
	UpstreamTotal  int    `json:"total_count"`
	UpdateTime     int64  `json:"update_time"`
	LastUpdateTime int64  `json:"last_update_time"`
}

type wireArticle struct {
	FakeID      string `json:"fakeid"`
	Aid         string `json:"aid"`
	AppMsgID    int64  `json:"appmsgid"`
	ItemIndex   int    `json:"itemidx"`
	Title       string `json:"title"`
	Author      string `json:"author_name"`
	Digest      string `json:"digest"`
	Link        string `json:"link"`
	Cover       string `json:"cover"`
	CreateTime  int64  `json:"create_time"`
	UpdateTime  int64  `json:"update_time"`
	MessageType int    `json:"item_show_type"`
	State       string `json:"_status"`
	Deleted     bool   `json:"is_deleted"`
	Paid        int    `json:"is_pay_subscribe"`
	Single      bool   `json:"_single"`
}

type wireHTML struct {
	FakeID       string           `json:"fakeid"`
	URL          string           `json:"url"`
	Title        string           `json:"title"`
	CommentID    string           `json:"commentID"`
	ObjectDigest string           `json:"objectDigest"`
	MediaType    string           `json:"mediaType"`
	UpdatedAt    int64            `json:"updatedAt"`
	Content      ContentReference `json:"content"`
}

type wireMetric struct {
	FakeID       string `json:"fakeid"`
	URL          string `json:"url"`
	ReadNum      int    `json:"readNum"`
	OldLikeNum   int    `json:"oldLikeNum"`
	ShareNum     int    `json:"shareNum"`
	LikeNum      int    `json:"likeNum"`
	CommentNum   int    `json:"commentNum"`
	ReadCount    int    `json:"readCount"`
	OldLikeCount int    `json:"oldLikeCount"`
	ShareCount   int    `json:"shareCount"`
	LikeCount    int    `json:"likeCount"`
	CommentCount int    `json:"commentCount"`
	UpdatedAt    int64  `json:"updatedAt"`
	CapturedAt   int64  `json:"capturedAt"`
}

type wireResourceMap struct {
	FakeID    string   `json:"fakeid"`
	URL       string   `json:"url"`
	Resources []string `json:"resources"`
	UpdatedAt int64    `json:"updatedAt"`
}

type wireResource struct {
	FakeID       string           `json:"fakeid"`
	URL          string           `json:"url"`
	ObjectDigest string           `json:"objectDigest"`
	MediaType    string           `json:"mediaType"`
	UpdatedAt    int64            `json:"updatedAt"`
	Content      ContentReference `json:"content"`
}

type wireCommentAsset struct {
	FakeID    string          `json:"fakeid"`
	URL       string          `json:"url"`
	UpdatedAt int64           `json:"updatedAt"`
	Data      json.RawMessage `json:"data"`
}

type wireReplyAsset struct {
	FakeID    string          `json:"fakeid"`
	URL       string          `json:"url"`
	ContentID string          `json:"contentID"`
	UpdatedAt int64           `json:"updatedAt"`
	Data      json.RawMessage `json:"data"`
}

type wireCommentResponse struct {
	Buffer          string        `json:"buffer"`
	ContinueFlag    bool          `json:"continue_flag"`
	ElectedComments []wireComment `json:"elected_comment"`
}

type wireComment struct {
	ContentID  string     `json:"content_id"`
	AuthorName string     `json:"nick_name"`
	Content    string     `json:"content"`
	LikeCount  int        `json:"like_num"`
	CreatedAt  int64      `json:"create_time"`
	ReplyNew   wireThread `json:"reply_new"`
}

type wireThread struct {
	MaxReplyID int64       `json:"max_reply_id"`
	ReplyTotal int         `json:"reply_total_cnt"`
	Replies    []wireReply `json:"reply_list"`
}

type wireReplyResponse struct {
	ReplyList wireThread `json:"reply_list"`
}

type wireReply struct {
	ReplyID    json.RawMessage `json:"reply_id"`
	AuthorName string          `json:"nick_name"`
	Content    string          `json:"content"`
	LikeCount  int             `json:"reply_like_num"`
	CreatedAt  int64           `json:"create_time"`
}

func parseUnix(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	if value < 1_000_000_000_000 {
		return time.Unix(value, 0).UTC()
	}
	return time.UnixMilli(value).UTC()
}

func replyID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func canonicalURLKey(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return strings.TrimSpace(raw)
	}
	parsed.Fragment = ""
	return parsed.String()
}

func replyFallbackID(reply wireReply, index int) string {
	if id := replyID(reply.ReplyID); id != "" {
		return id
	}
	return strconv.FormatInt(reply.CreatedAt, 10) + ":" + strconv.Itoa(index)
}
