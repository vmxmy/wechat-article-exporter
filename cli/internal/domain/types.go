package domain

import "time"

type ProfileID string
type AccountID string
type ArticleID string
type AlbumID string
type JobID string
type ExportID string

type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type RuntimePaths struct {
	Config string `json:"config"`
	Data   string `json:"data"`
	Cache  string `json:"cache"`
	State  string `json:"state"`
}

type RuntimeStatus struct {
	Version       string        `json:"version"`
	Profile       ProfileID     `json:"profile"`
	Paths         RuntimePaths  `json:"paths"`
	Portable      bool          `json:"portable"`
	OfflineReady  bool          `json:"offlineReady"`
	SecretBackend string        `json:"secretBackend"`
	Storage       StorageStatus `json:"storage"`
	CheckedAt     time.Time     `json:"checkedAt"`
}

type Account struct {
	ID            AccountID `json:"id"`
	FakeID        string    `json:"fakeid,omitempty"`
	Name          string    `json:"name"`
	Alias         string    `json:"alias,omitempty"`
	Description   string    `json:"description,omitempty"`
	AvatarURL     string    `json:"avatarUrl,omitempty"`
	ServiceType   int       `json:"serviceType,omitempty"`
	LastSyncAt    time.Time `json:"lastSyncAt,omitempty"`
	ArticleCount  int       `json:"articleCount,omitempty"`
	MessageCount  int       `json:"messageCount,omitempty"`
	UpstreamTotal int       `json:"upstreamTotal,omitempty"`
	SyncCursor    int       `json:"syncCursor,omitempty"`
	SyncCompleted bool      `json:"syncCompleted,omitempty"`
}

type Article struct {
	ID                   ArticleID `json:"id"`
	AccountID            AccountID `json:"accountId,omitempty"`
	Aid                  string    `json:"aid,omitempty"`
	AppMsgID             int64     `json:"appmsgId,omitempty"`
	ItemIndex            int       `json:"itemIndex,omitempty"`
	Title                string    `json:"title"`
	Author               string    `json:"author,omitempty"`
	Digest               string    `json:"digest,omitempty"`
	CanonicalURL         string    `json:"canonicalUrl,omitempty"`
	CoverURL             string    `json:"coverUrl,omitempty"`
	PublishedAt          time.Time `json:"publishedAt,omitempty"`
	UpdatedAt            time.Time `json:"updatedAt,omitempty"`
	MessageType          int       `json:"messageType,omitempty"`
	State                string    `json:"state,omitempty"`
	Deleted              bool      `json:"deleted,omitempty"`
	Paid                 bool      `json:"paid,omitempty"`
	Original             bool      `json:"original,omitempty"`
	Single               bool      `json:"single,omitempty"`
	HasContent           bool      `json:"hasContent"`
	HasComments          bool      `json:"hasComments"`
	WeCoinCount          int       `json:"weCoinCount,omitempty"`
	MediaDurationSeconds int       `json:"mediaDurationSeconds,omitempty"`
	ReadCount            int       `json:"readCount,omitempty"`
	OldLikeCount         int       `json:"oldLikeCount,omitempty"`
	ShareCount           int       `json:"shareCount,omitempty"`
	LikeCount            int       `json:"likeCount,omitempty"`
	CommentCount         int       `json:"commentCount,omitempty"`
	Albums               []Album   `json:"albums,omitempty"`
}

type Album struct {
	ID           AlbumID   `json:"id"`
	AccountID    AccountID `json:"accountId,omitempty"`
	UpstreamID   string    `json:"upstreamId,omitempty"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	ArticleCount int       `json:"articleCount"`
	Paid         bool      `json:"paid,omitempty"`
}

type JobState string

const (
	JobQueued      JobState = "queued"
	JobRunning     JobState = "running"
	JobCompleted   JobState = "completed"
	JobPartial     JobState = "partial"
	JobFailed      JobState = "failed"
	JobCancelled   JobState = "cancelled"
	JobBlockedAuth JobState = "blocked_auth"
	JobPaused      JobState = "paused"
)

type Job struct {
	ID        JobID          `json:"id"`
	Kind      string         `json:"kind"`
	State     JobState       `json:"state"`
	Profile   ProfileID      `json:"profile"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Counts    map[string]int `json:"counts,omitempty"`
}

type StorageStatus struct {
	DatabaseAvailable bool  `json:"databaseAvailable"`
	ObjectStoreReady  bool  `json:"objectStoreReady"`
	Accounts          int64 `json:"accounts"`
	Articles          int64 `json:"articles"`
	Albums            int64 `json:"albums"`
	Jobs              int64 `json:"jobs"`
	Objects           int64 `json:"objects"`
	ObjectBytes       int64 `json:"objectBytes"`
}

type AccountQuery struct {
	Keyword string `json:"keyword,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type AccountManifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	ExportedAt    time.Time `json:"exportedAt"`
	Accounts      []Account `json:"accounts"`
}

type AccountImportReport struct {
	Added     int `json:"added"`
	Merged    int `json:"merged"`
	Unchanged int `json:"unchanged"`
}

type AccountDeleteReport struct {
	AccountsDeleted        int   `json:"accountsDeleted"`
	ArticlesDeleted        int   `json:"articlesDeleted"`
	AlbumsDeleted          int   `json:"albumsDeleted"`
	ObjectsGarbageEligible int64 `json:"objectsGarbageEligible"`
}

type ArticleQuery struct {
	AccountID       AccountID     `json:"accountId,omitempty"`
	AlbumID         AlbumID       `json:"albumId,omitempty"`
	Keyword         string        `json:"keyword,omitempty"`
	Author          string        `json:"author,omitempty"`
	State           string        `json:"state,omitempty"`
	PublishedFrom   time.Time     `json:"publishedFrom,omitempty"`
	PublishedTo     time.Time     `json:"publishedTo,omitempty"`
	Deleted         *bool         `json:"deleted,omitempty"`
	HasContent      *bool         `json:"hasContent,omitempty"`
	HasComments     *bool         `json:"hasComments,omitempty"`
	Original        *bool         `json:"original,omitempty"`
	Paid            *bool         `json:"paid,omitempty"`
	MessageTypes    []int         `json:"messageTypes,omitempty"`
	ReadMin         *int          `json:"readMin,omitempty"`
	ReadMax         *int          `json:"readMax,omitempty"`
	OldLikeMin      *int          `json:"oldLikeMin,omitempty"`
	OldLikeMax      *int          `json:"oldLikeMax,omitempty"`
	ShareMin        *int          `json:"shareMin,omitempty"`
	ShareMax        *int          `json:"shareMax,omitempty"`
	LikeMin         *int          `json:"likeMin,omitempty"`
	LikeMax         *int          `json:"likeMax,omitempty"`
	CommentMin      *int          `json:"commentMin,omitempty"`
	CommentMax      *int          `json:"commentMax,omitempty"`
	WeCoinMin       *int          `json:"weCoinMin,omitempty"`
	WeCoinMax       *int          `json:"weCoinMax,omitempty"`
	MediaSecondsMin *int          `json:"mediaSecondsMin,omitempty"`
	MediaSecondsMax *int          `json:"mediaSecondsMax,omitempty"`
	Sort            string        `json:"sort,omitempty"`
	Sorts           []ArticleSort `json:"sorts,omitempty"`
	Offset          int           `json:"offset,omitempty"`
	Limit           int           `json:"limit,omitempty"`
}

type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

type ArticleSort struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

type SavedArticleQuery struct {
	Name      string       `json:"name"`
	Query     ArticleQuery `json:"query"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

type AlbumQuery struct {
	AccountID AccountID `json:"accountId,omitempty"`
	Keyword   string    `json:"keyword,omitempty"`
	Offset    int       `json:"offset,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

type JobQuery struct {
	Kind   string     `json:"kind,omitempty"`
	States []JobState `json:"states,omitempty"`
	Offset int        `json:"offset,omitempty"`
	Limit  int        `json:"limit,omitempty"`
}

type SynchronizeAccountRequest struct {
	AccountID           AccountID     `json:"accountId"`
	AccountIDs          []AccountID   `json:"accountIds,omitempty"`
	Incremental         bool          `json:"incremental"`
	Range               SyncRange     `json:"range,omitempty"`
	NotBefore           time.Time     `json:"notBefore,omitempty"`
	PageSize            int           `json:"pageSize,omitempty"`
	PageDelay           time.Duration `json:"pageDelay,omitempty"`
	Jitter              time.Duration `json:"jitter,omitempty"`
	PersistentPacing    bool          `json:"persistentPacing,omitempty"`
	ConfirmUnsafePacing bool          `json:"confirmUnsafePacing,omitempty"`
}

type SyncRange string

const (
	SyncRange24Hours SyncRange = "24h"
	SyncRange1Day    SyncRange = "1d"
	SyncRange3Days   SyncRange = "3d"
	SyncRange7Days   SyncRange = "7d"
	SyncRange1Month  SyncRange = "1m"
	SyncRange3Months SyncRange = "3m"
	SyncRange6Months SyncRange = "6m"
	SyncRange1Year   SyncRange = "1y"
	SyncRangeAll     SyncRange = "all"
	SyncRangePoint   SyncRange = "point"
)

type AccountSyncState struct {
	Account       Account   `json:"account"`
	Cursor        int       `json:"cursor"`
	UpstreamTotal int       `json:"upstreamTotal"`
	MessageCount  int       `json:"messageCount"`
	LatestArticle time.Time `json:"latestArticle,omitempty"`
	Completed     bool      `json:"completed"`
}

type AlbumCheckpoint struct {
	BeginMessageID string   `json:"beginMessageId,omitempty"`
	BeginItemIndex string   `json:"beginItemIndex,omitempty"`
	SeenKeys       []string `json:"seenKeys,omitempty"`
	PagesCommitted int      `json:"pagesCommitted,omitempty"`
	ItemsCommitted int      `json:"itemsCommitted,omitempty"`
}

type DownloadRequest struct {
	Kind       string      `json:"kind,omitempty"`
	ArticleIDs []ArticleID `json:"articleIds,omitempty"`
	URLs       []string    `json:"urls,omitempty"`
	Force      bool        `json:"force"`
}

type ExportSelectionKind string

const (
	ExportSelectionURLs        ExportSelectionKind = "urls"
	ExportSelectionAccount     ExportSelectionKind = "account"
	ExportSelectionAlbum       ExportSelectionKind = "album"
	ExportSelectionAlbumIDs    ExportSelectionKind = "album_ids"
	ExportSelectionSavedQuery  ExportSelectionKind = "saved_query"
	ExportSelectionExplicitIDs ExportSelectionKind = "explicit_ids"
	ExportSelectionAllMatching ExportSelectionKind = "all_matching"
)

type ExportSelection struct {
	Kind         ExportSelectionKind `json:"kind"`
	URLs         []string            `json:"urls,omitempty"`
	AccountID    AccountID           `json:"accountId,omitempty"`
	AlbumID      AlbumID             `json:"albumId,omitempty"`
	AlbumIDs     []AlbumID           `json:"albumIds,omitempty"`
	SavedQueryID string              `json:"savedQueryId,omitempty"`
	ArticleIDs   []ArticleID         `json:"articleIds,omitempty"`
	Query        ArticleQuery        `json:"query,omitempty"`
}

type ExportOptions struct {
	NamingTemplate   string         `json:"namingTemplate,omitempty"`
	MaximumNameBytes int            `json:"maximumNameBytes,omitempty"`
	CollisionPolicy  string         `json:"collisionPolicy,omitempty"`
	FormatOptions    map[string]any `json:"formatOptions,omitempty"`
}

// ExportOutputAuthorization is an internal, execution-time filesystem
// capability created by a trusted adapter. It is deliberately excluded from
// public JSON request decoding and is persisted only in durable job items.
type ExportOutputAuthorization struct {
	Root         string `json:"root"`
	RelativePath string `json:"relativePath,omitempty"`
	Device       uint64 `json:"device"`
	Inode        uint64 `json:"inode"`
}

type ExportRequest struct {
	ArticleIDs          []ArticleID                `json:"articleIds,omitempty"`
	Selection           ExportSelection            `json:"selection,omitempty"`
	Format              string                     `json:"format"`
	OutputRoot          string                     `json:"outputRoot,omitempty"`
	Options             ExportOptions              `json:"options,omitempty"`
	OutputAuthorization *ExportOutputAuthorization `json:"-"`
}
