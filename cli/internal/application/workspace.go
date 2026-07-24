package application

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

const (
	// WorkspaceDefaultPageLimit keeps each local-adapter response bounded when
	// a caller does not ask for a particular page size.
	WorkspaceDefaultPageLimit = 50
	// WorkspaceMaximumPageLimit is deliberately lower than repository-internal
	// limits. Adapters must page rather than turn a local browser request into a
	// whole-library read.
	WorkspaceMaximumPageLimit = 100
)

// WorkspaceErrorCode is the stable error vocabulary for local presentation
// adapters. HTTP, TUI, and MCP adapters may map these codes to their native
// protocol errors without exposing persistence or upstream error text.
type WorkspaceErrorCode string

const (
	WorkspaceErrorInvalidArgument WorkspaceErrorCode = "invalid_argument"
	WorkspaceErrorNotFound        WorkspaceErrorCode = "not_found"
	WorkspaceErrorUnavailable     WorkspaceErrorCode = "unavailable"
	WorkspaceErrorAuthentication  WorkspaceErrorCode = "authentication_required"
	WorkspaceErrorCancelled       WorkspaceErrorCode = "cancelled"
	WorkspaceErrorInternal        WorkspaceErrorCode = "internal"
)

// WorkspaceError is safe to serialize. Cause intentionally has no JSON
// representation so callers cannot accidentally return SQLite, filesystem,
// credential, or upstream protocol details.
type WorkspaceError struct {
	Code    WorkspaceErrorCode `json:"code"`
	Message string             `json:"message"`
	Cause   error              `json:"-"`
}

func (err *WorkspaceError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

func (err *WorkspaceError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// WorkspaceErrorResponse is the versioned JSON shape adapters should return
// for an operation failure.
type WorkspaceErrorResponse struct {
	Error WorkspaceError `json:"error"`
}

func workspaceError(err error) error {
	if err == nil {
		return nil
	}
	var existing *WorkspaceError
	if errors.As(err, &existing) {
		return existing
	}

	code, message := WorkspaceErrorInternal, "workspace operation failed"
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code, message = WorkspaceErrorCancelled, "workspace operation was cancelled"
	case errors.Is(err, ErrUnavailable):
		code, message = WorkspaceErrorUnavailable, "workspace capability is not available"
	case errors.Is(err, wechat.ErrLoginExpired), errors.Is(err, wechat.ErrSessionExpired), errors.Is(err, wechat.ErrDiscoveryAuthentication):
		code, message = WorkspaceErrorAuthentication, "workspace session must be authenticated"
	case errors.Is(err, sql.ErrNoRows):
		code, message = WorkspaceErrorNotFound, "workspace item was not found"
	case errors.Is(err, library.ErrInvalidArticleSort):
		code, message = WorkspaceErrorInvalidArgument, "workspace query contains an unsupported sort"
	}
	return &WorkspaceError{Code: code, Message: message, Cause: err}
}

// WorkspacePageRequest is the bounded offset pagination contract shared by
// browser, terminal, and stdio presentation adapters.
type WorkspacePageRequest struct {
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`
}

func (request WorkspacePageRequest) normalize() (WorkspacePageRequest, error) {
	if request.Offset < 0 {
		return WorkspacePageRequest{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "page offset must not be negative"}
	}
	if request.Limit < 0 {
		return WorkspacePageRequest{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "page limit must not be negative"}
	}
	if request.Limit == 0 {
		request.Limit = WorkspaceDefaultPageLimit
	}
	if request.Limit > WorkspaceMaximumPageLimit {
		return WorkspacePageRequest{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument,
			Message: fmt.Sprintf("page limit must not exceed %d", WorkspaceMaximumPageLimit)}
	}
	return request, nil
}

// WorkspacePage is a bounded page. It has no backing-store cursor, path, or
// runtime handle, so it is safe to use in local adapter response DTOs.
type WorkspacePage[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// WorkspaceJobAction is a browser-safe indication that a single job control
// is currently available. It exposes only stable action names, never the
// scheduler, lease, or persistence conditions that produced the decision.
type WorkspaceJobAction string

const (
	WorkspaceJobActionCancel WorkspaceJobAction = "cancel"
	WorkspaceJobActionPause  WorkspaceJobAction = "pause"
	WorkspaceJobActionResume WorkspaceJobAction = "resume"
	WorkspaceJobActionRetry  WorkspaceJobAction = "retry"
)

// WorkspaceJob is the browser-safe persistent job DTO. Domain.Job remains an
// internal record; browser adapters receive only this projection and its
// derived permitted controls.
type WorkspaceJob struct {
	ID               domain.JobID         `json:"id"`
	Kind             string               `json:"kind"`
	State            domain.JobState      `json:"state"`
	Profile          domain.ProfileID     `json:"profile,omitempty"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
	Counts           map[string]int       `json:"counts,omitempty"`
	PermittedActions []WorkspaceJobAction `json:"permittedActions"`
}

type WorkspaceRuntime struct {
	Version       string               `json:"version"`
	Profile       string               `json:"profile"`
	Portable      bool                 `json:"portable"`
	OfflineReady  bool                 `json:"offlineReady"`
	SecretBackend string               `json:"secretBackend"`
	Storage       domain.StorageStatus `json:"storage"`
	CheckedAt     time.Time            `json:"checkedAt"`
}

type WorkspaceSession struct {
	State           wechat.SessionState `json:"state"`
	AccountID       string              `json:"accountId,omitempty"`
	AccountName     string              `json:"accountName,omitempty"`
	AvatarURL       string              `json:"avatarUrl,omitempty"`
	CreatedAt       time.Time           `json:"createdAt,omitempty"`
	ExpiresAt       time.Time           `json:"expiresAt,omitempty"`
	LastValidatedAt time.Time           `json:"lastValidatedAt,omitempty"`
	Validation      string              `json:"validation,omitempty"`
}

// WorkspaceSwitchableAccount is the browser-safe identity projection used by
// account switching. It intentionally excludes the upstream account payload,
// session credentials, resource locations, and any local storage references.
type WorkspaceSwitchableAccount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
}

// WorkspaceSwitchableAccounts makes an unavailable upstream capability
// distinguishable from an available account list without surfacing an
// upstream error or any internal capability detail.
type WorkspaceSwitchableAccounts struct {
	Available bool                         `json:"available"`
	Accounts  []WorkspaceSwitchableAccount `json:"accounts"`
}

type WorkspaceAccountQuery struct {
	Keyword string               `json:"keyword,omitempty"`
	Page    WorkspacePageRequest `json:"page"`
}

type WorkspaceArticleQuery struct {
	AccountID       string               `json:"accountId,omitempty"`
	AlbumID         string               `json:"albumId,omitempty"`
	Keyword         string               `json:"keyword,omitempty"`
	Author          string               `json:"author,omitempty"`
	State           string               `json:"state,omitempty"`
	PublishedFrom   time.Time            `json:"publishedFrom,omitempty"`
	PublishedTo     time.Time            `json:"publishedTo,omitempty"`
	Deleted         *bool                `json:"deleted,omitempty"`
	HasContent      *bool                `json:"hasContent,omitempty"`
	HasComments     *bool                `json:"hasComments,omitempty"`
	Original        *bool                `json:"original,omitempty"`
	Paid            *bool                `json:"paid,omitempty"`
	MessageTypes    []int                `json:"messageTypes,omitempty"`
	ReadMin         *int                 `json:"readMin,omitempty"`
	ReadMax         *int                 `json:"readMax,omitempty"`
	OldLikeMin      *int                 `json:"oldLikeMin,omitempty"`
	OldLikeMax      *int                 `json:"oldLikeMax,omitempty"`
	ShareMin        *int                 `json:"shareMin,omitempty"`
	ShareMax        *int                 `json:"shareMax,omitempty"`
	LikeMin         *int                 `json:"likeMin,omitempty"`
	LikeMax         *int                 `json:"likeMax,omitempty"`
	CommentMin      *int                 `json:"commentMin,omitempty"`
	CommentMax      *int                 `json:"commentMax,omitempty"`
	WeCoinMin       *int                 `json:"weCoinMin,omitempty"`
	WeCoinMax       *int                 `json:"weCoinMax,omitempty"`
	MediaSecondsMin *int                 `json:"mediaSecondsMin,omitempty"`
	MediaSecondsMax *int                 `json:"mediaSecondsMax,omitempty"`
	Sort            string               `json:"sort,omitempty"`
	Sorts           []domain.ArticleSort `json:"sorts,omitempty"`
	Page            WorkspacePageRequest `json:"page"`
}

type WorkspaceAlbumQuery struct {
	AccountID string               `json:"accountId,omitempty"`
	Keyword   string               `json:"keyword,omitempty"`
	Page      WorkspacePageRequest `json:"page"`
}

type WorkspaceJobQuery struct {
	Kind   string               `json:"kind,omitempty"`
	States []domain.JobState    `json:"states,omitempty"`
	Page   WorkspacePageRequest `json:"page"`
}

// WorkspaceAlbumTraversalRequest describes the browser-safe portion of an
// album traversal. The application resolves the saved account and album IDs
// before the durable worker contacts WeChat.
type WorkspaceAlbumTraversalRequest struct {
	AccountID domain.AccountID  `json:"accountId"`
	AlbumID   domain.AlbumID    `json:"albumId"`
	AlbumIDs  []domain.AlbumID  `json:"albumIds,omitempty"`
	Order     wechat.AlbumOrder `json:"order"`
	Download  bool              `json:"download"`
}

// WorkspaceArticlePreview is a small, local handoff descriptor. It does not
// contain a filesystem path, object digest, or arbitrary HTML; a later local
// preview service may resolve the opaque article identity into sanitized HTML.
type WorkspaceArticlePreview struct {
	ArticleID   domain.ArticleID `json:"articleId"`
	Title       string           `json:"title"`
	Available   bool             `json:"available"`
	DocumentURL string           `json:"documentUrl,omitempty"`
}

// WorkspaceRenderedArticlePreview is an already-sanitized, self-contained
// local HTML document. It is deliberately available only to the local web
// adapter, which serves it with a restrictive CSP. No filesystem path,
// object digest, or remote resource URL crosses this boundary.
type WorkspaceRenderedArticlePreview struct {
	ArticleID domain.ArticleID
	HTML      []byte
}

// WorkspaceArticlePreviewRenderer is supplied by the local runtime when it
// can render an article from the object store. The application package owns
// the capability boundary while the runtime retains its filesystem access.
type WorkspaceArticlePreviewRenderer interface {
	RenderArticlePreview(context.Context, domain.ArticleID) (WorkspaceRenderedArticlePreview, error)
}

// WorkspaceArticleLookup is the typed optional application capability needed
// for an article-only local handoff. Keeping it narrow avoids making unrelated
// adapter test doubles pretend they can resolve article bodies.
type WorkspaceArticleLookup interface {
	GetArticle(context.Context, domain.ArticleID) (domain.Article, error)
}

// WorkspaceArticleResourceLookup is the narrow application capability used to
// read resource-completeness aggregates without exposing individual resource
// records to local presentation adapters.
type WorkspaceArticleResourceLookup interface {
	ArticleResourceAvailability(context.Context, domain.ArticleID) (library.ArticleResourceAvailability, error)
}

// WorkspaceArticleResources is the browser-safe aggregate resource state for
// one article. It deliberately omits resource IDs, URLs, digests, and media
// types.
type WorkspaceArticleResources struct {
	ArticleID domain.ArticleID `json:"articleId"`
	Total     int              `json:"total"`
	Available int              `json:"available"`
	Missing   int              `json:"missing"`
	Complete  bool             `json:"complete"`
}

// WorkspaceArticleMetrics is the browser-safe engagement projection. It has
// no snapshot ID or credential identity.
type WorkspaceArticleMetrics struct {
	Available    bool      `json:"available"`
	ReadCount    int       `json:"readCount"`
	OldLikeCount int       `json:"oldLikeCount"`
	LikeCount    int       `json:"likeCount"`
	ShareCount   int       `json:"shareCount"`
	CommentCount int       `json:"commentCount"`
	CapturedAt   time.Time `json:"capturedAt,omitempty"`
}

// WorkspaceArticleResourceDetail contains only a resource's position and
// local availability. It deliberately omits IDs, URLs, digests, media types,
// and filesystem paths.
type WorkspaceArticleResourceDetail struct {
	Role      string `json:"role"`
	Ordinal   int    `json:"ordinal"`
	Available bool   `json:"available"`
}

// WorkspaceArticleDetail combines bounded safe resource state with the
// article's latest persisted engagement snapshot.
type WorkspaceArticleDetail struct {
	ArticleID domain.ArticleID                              `json:"articleId"`
	Metrics   WorkspaceArticleMetrics                       `json:"metrics"`
	Resources WorkspacePage[WorkspaceArticleResourceDetail] `json:"resources"`
}

// WorkspaceArticleComment and WorkspaceArticleReply are deliberately safe
// browser projections. They omit database IDs, object digests, fetch times,
// request provenance, and any continuation state.
type WorkspaceArticleComment struct {
	ID          string    `json:"id"`
	AuthorName  string    `json:"authorName"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"createdAt,omitempty"`
	LikeCount   int       `json:"likeCount"`
	ReplyCount  int       `json:"replyCount"`
	ReplyStatus string    `json:"replyStatus"`
}

type WorkspaceArticleReply struct {
	ID         string    `json:"id"`
	AuthorName string    `json:"authorName"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	LikeCount  int       `json:"likeCount"`
}

type WorkspaceArticleComments struct {
	ArticleID      domain.ArticleID                       `json:"articleId"`
	Comments       WorkspacePage[WorkspaceArticleComment] `json:"comments"`
	PendingReplies int                                    `json:"pendingReplies"`
}

// WorkspaceAlbumController is the typed application capability for the
// persisted album workflow. Both variants return the durable album_sync job.
type WorkspaceAlbumController interface {
	SynchronizeAlbum(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error)
	SynchronizeAlbumAndDownload(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error)
}

// WorkspaceAlbumTraversalController adds explicit traversal order to the
// persisted album workflow. It remains separate from WorkspaceAlbumController
// so older application adapters cannot silently ignore a browser request.
type WorkspaceAlbumTraversalController interface {
	SynchronizeAlbumWithOrder(context.Context, domain.AccountID, domain.AlbumID, wechat.AlbumOrder) (domain.Job, error)
	SynchronizeAlbumWithOrderAndDownload(context.Context, domain.AccountID, domain.AlbumID, wechat.AlbumOrder) (domain.Job, error)
}

type WorkspaceMultiAlbumTraversalController interface {
	SynchronizeAlbumsWithOrder(context.Context, []domain.AlbumID, wechat.AlbumOrder, bool) (domain.Job, error)
}

// WorkspaceReader is the P0 read contract for local browser, TUI, and MCP
// presentation adapters. It exposes only stable, profile-scoped DTOs.
type WorkspaceReader interface {
	Runtime(context.Context) (WorkspaceRuntime, error)
	Session(context.Context) (WorkspaceSession, error)
	Accounts(context.Context, WorkspaceAccountQuery) (WorkspacePage[domain.Account], error)
	Articles(context.Context, WorkspaceArticleQuery) (WorkspacePage[domain.Article], error)
	Albums(context.Context, WorkspaceAlbumQuery) (WorkspacePage[domain.Album], error)
	SavedArticleQueries(context.Context, WorkspacePageRequest) (WorkspacePage[domain.SavedArticleQuery], error)
	Jobs(context.Context, WorkspaceJobQuery) (WorkspacePage[WorkspaceJob], error)
	JobDetails(context.Context, domain.JobID) (WorkspaceJobDetail, error)
	ArticlePreview(context.Context, domain.ArticleID) (WorkspaceArticlePreview, error)
	ArticleResources(context.Context, domain.ArticleID) (WorkspaceArticleResources, error)
	ArticleDetail(context.Context, domain.ArticleID, WorkspacePageRequest) (WorkspaceArticleDetail, error)
	ArticleComments(context.Context, domain.ArticleID, WorkspacePageRequest) (WorkspaceArticleComments, error)
	ArticleCommentReplies(context.Context, domain.ArticleID, string, WorkspacePageRequest) (WorkspacePage[WorkspaceArticleReply], error)
}

// WorkspaceSavedQueryController is the mutable saved-query contract exposed
// to local presentation adapters. Saving an existing name is an intentional
// update, preserving the established library upsert semantics.
type WorkspaceSavedQueryController interface {
	SaveArticleQuery(context.Context, string, domain.ArticleQuery) (domain.SavedArticleQuery, error)
	DeleteSavedArticleQuery(context.Context, string, string) error
}

// WorkspaceController keeps supported controls tied to the same durable jobs
// and session semantics used by the existing adapters. It intentionally omits
// filesystem-bearing export inputs; those belong to WorkspaceFileService.
type WorkspaceController interface {
	BeginLogin(context.Context, string) (wechat.LoginFlow, error)
	PollLogin(context.Context) (wechat.PollResult, error)
	CompleteLogin(context.Context) (WorkspaceSession, error)
	Logout(context.Context) error
	SwitchableAccounts(context.Context) (WorkspaceSwitchableAccounts, error)
	SwitchAccount(context.Context, string) (WorkspaceSession, error)
	SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error)
	SynchronizeAlbum(context.Context, WorkspaceAlbumTraversalRequest) (domain.Job, error)
	StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error)
	CancelJob(context.Context, domain.JobID) (domain.Job, error)
}

// WorkspaceDirectoryHandle is an opaque, server-issued capability. It is not
// a host path and must never be interpreted as one by a presentation adapter.
type WorkspaceDirectoryHandle string

// WorkspaceFileService reserves the future browser file boundary. Implementors
// validate roots, descendants, upload staging, and cleanup; callers never send
// or receive unrestricted host filesystem paths.
type WorkspaceFileService interface {
	ListExportDirectories(context.Context) ([]WorkspaceDirectoryHandle, error)
	AuthorizeExportDestination(context.Context, WorkspaceDirectoryHandle, string) (domain.ExportOutputAuthorization, error)
	StageImport(context.Context, string, int64) (string, error)
}

// WorkspaceMaintenanceService reserves the future maintenance boundary. Its
// confirmation proof remains operation-specific rather than a generic boolean.
type WorkspaceMaintenanceService interface {
	Plan(context.Context, string) (any, error)
	Execute(context.Context, string, string) (domain.Job, error)
}

// Workspace is the application-owned adapter facade. It only delegates to
// Application; it never receives ProfileRuntime, SQLite, or host paths.
type Workspace struct {
	application Application
	preview     WorkspaceArticlePreviewRenderer
}

func NewWorkspace(application Application) *Workspace { return &Workspace{application: application} }

func NewWorkspaceWithPreview(application Application, preview WorkspaceArticlePreviewRenderer) *Workspace {
	return &Workspace{application: application, preview: preview}
}

func (workspace *Workspace) Runtime(ctx context.Context) (WorkspaceRuntime, error) {
	status, err := workspace.application.RuntimeStatus(ctx)
	if err != nil {
		return WorkspaceRuntime{}, workspaceError(err)
	}
	return WorkspaceRuntime{Version: status.Version, Profile: string(status.Profile), Portable: status.Portable,
		OfflineReady: status.OfflineReady, SecretBackend: status.SecretBackend, Storage: status.Storage, CheckedAt: status.CheckedAt}, nil
}

func (workspace *Workspace) Session(ctx context.Context) (WorkspaceSession, error) {
	session, err := workspace.application.SessionStatus(ctx)
	if err != nil {
		return WorkspaceSession{}, workspaceError(err)
	}
	return workspaceSession(session), nil
}

func (workspace *Workspace) Accounts(ctx context.Context, input WorkspaceAccountQuery) (WorkspacePage[domain.Account], error) {
	page, err := input.Page.normalize()
	if err != nil {
		return WorkspacePage[domain.Account]{}, err
	}
	result, err := workspace.application.QueryAccounts(ctx, domain.AccountQuery{Keyword: strings.TrimSpace(input.Keyword), Offset: page.Offset, Limit: page.Limit})
	return workspacePage(result), workspaceError(err)
}

func (workspace *Workspace) Articles(ctx context.Context, input WorkspaceArticleQuery) (WorkspacePage[domain.Article], error) {
	page, err := input.Page.normalize()
	if err != nil {
		return WorkspacePage[domain.Article]{}, err
	}
	result, err := workspace.application.QueryArticles(ctx, domain.ArticleQuery{
		AccountID: domain.AccountID(input.AccountID), AlbumID: domain.AlbumID(input.AlbumID), Keyword: strings.TrimSpace(input.Keyword),
		Author: strings.TrimSpace(input.Author), State: strings.TrimSpace(input.State), PublishedFrom: input.PublishedFrom, PublishedTo: input.PublishedTo,
		Deleted: input.Deleted, HasContent: input.HasContent, HasComments: input.HasComments, Original: input.Original, Paid: input.Paid,
		MessageTypes: append([]int(nil), input.MessageTypes...), ReadMin: input.ReadMin, ReadMax: input.ReadMax, OldLikeMin: input.OldLikeMin,
		OldLikeMax: input.OldLikeMax, ShareMin: input.ShareMin, ShareMax: input.ShareMax, LikeMin: input.LikeMin, LikeMax: input.LikeMax,
		CommentMin: input.CommentMin, CommentMax: input.CommentMax, WeCoinMin: input.WeCoinMin, WeCoinMax: input.WeCoinMax,
		MediaSecondsMin: input.MediaSecondsMin, MediaSecondsMax: input.MediaSecondsMax, Sort: input.Sort,
		Sorts: append([]domain.ArticleSort(nil), input.Sorts...), Offset: page.Offset, Limit: page.Limit,
	})
	return workspacePage(result), workspaceError(err)
}

func (workspace *Workspace) Albums(ctx context.Context, input WorkspaceAlbumQuery) (WorkspacePage[domain.Album], error) {
	page, err := input.Page.normalize()
	if err != nil {
		return WorkspacePage[domain.Album]{}, err
	}
	result, err := workspace.application.QueryAlbums(ctx, domain.AlbumQuery{AccountID: domain.AccountID(input.AccountID),
		Keyword: strings.TrimSpace(input.Keyword), Offset: page.Offset, Limit: page.Limit})
	return workspacePage(result), workspaceError(err)
}

func (workspace *Workspace) SavedArticleQueries(ctx context.Context, request WorkspacePageRequest) (WorkspacePage[domain.SavedArticleQuery], error) {
	page, err := request.normalize()
	if err != nil {
		return WorkspacePage[domain.SavedArticleQuery]{}, err
	}
	items, err := workspace.application.ListSavedArticleQueries(ctx)
	if err != nil {
		return WorkspacePage[domain.SavedArticleQuery]{}, workspaceError(err)
	}
	total := len(items)
	if page.Offset >= total {
		return WorkspacePage[domain.SavedArticleQuery]{Items: []domain.SavedArticleQuery{}, Total: total, Offset: page.Offset, Limit: page.Limit}, nil
	}
	end := min(page.Offset+page.Limit, total)
	return WorkspacePage[domain.SavedArticleQuery]{Items: append([]domain.SavedArticleQuery(nil), items[page.Offset:end]...),
		Total: total, Offset: page.Offset, Limit: page.Limit}, nil
}

func (workspace *Workspace) Jobs(ctx context.Context, input WorkspaceJobQuery) (WorkspacePage[WorkspaceJob], error) {
	page, err := input.Page.normalize()
	if err != nil {
		return WorkspacePage[WorkspaceJob]{}, err
	}
	result, err := workspace.application.QueryJobs(ctx, domain.JobQuery{Kind: strings.TrimSpace(input.Kind),
		States: append([]domain.JobState(nil), input.States...), Offset: page.Offset, Limit: page.Limit})
	if err != nil {
		return WorkspacePage[WorkspaceJob]{}, workspaceError(err)
	}
	items := make([]WorkspaceJob, 0, len(result.Items))
	for _, job := range result.Items {
		items = append(items, workspace.workspaceJob(job))
	}
	return WorkspacePage[WorkspaceJob]{Items: items, Total: result.Total, Offset: result.Offset, Limit: result.Limit}, nil
}

func (workspace *Workspace) BeginLogin(ctx context.Context, clientSessionID string) (wechat.LoginFlow, error) {
	flow, err := workspace.application.BeginLogin(ctx, clientSessionID)
	return flow, workspaceError(err)
}

func (workspace *Workspace) PollLogin(ctx context.Context) (wechat.PollResult, error) {
	result, err := workspace.application.PollLogin(ctx)
	return result, workspaceError(err)
}

func (workspace *Workspace) CompleteLogin(ctx context.Context) (WorkspaceSession, error) {
	session, err := workspace.application.CompleteLogin(ctx)
	if err != nil {
		return WorkspaceSession{}, workspaceError(err)
	}
	return workspaceSession(session), nil
}

func (workspace *Workspace) Logout(ctx context.Context) error {
	return workspaceError(workspace.application.Logout(ctx))
}

func (workspace *Workspace) SwitchableAccounts(ctx context.Context) (WorkspaceSwitchableAccounts, error) {
	accounts, err := workspace.application.ListSwitchableAccounts(ctx)
	if errors.Is(err, wechat.ErrAccountSwitching) || errors.Is(err, ErrUnavailable) {
		return WorkspaceSwitchableAccounts{Available: false, Accounts: []WorkspaceSwitchableAccount{}}, nil
	}
	if err != nil {
		return WorkspaceSwitchableAccounts{}, workspaceError(err)
	}
	items := make([]WorkspaceSwitchableAccount, 0, len(accounts))
	for _, account := range accounts {
		id := strings.TrimSpace(account.ID)
		if id == "" {
			continue
		}
		items = append(items, WorkspaceSwitchableAccount{ID: id, Name: strings.TrimSpace(account.Name), Alias: strings.TrimSpace(account.Alias)})
	}
	return WorkspaceSwitchableAccounts{Available: true, Accounts: items}, nil
}

func (workspace *Workspace) SwitchAccount(ctx context.Context, accountID string) (WorkspaceSession, error) {
	accountID = strings.TrimSpace(accountID)
	if !validWorkspaceSwitchableAccountID(accountID) {
		return WorkspaceSession{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "account identifier is invalid"}
	}
	session, err := workspace.application.SwitchAccount(ctx, accountID)
	if err != nil {
		return WorkspaceSession{}, workspaceError(err)
	}
	return workspaceSession(session), nil
}

func validWorkspaceSwitchableAccountID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (workspace *Workspace) SynchronizeAccount(ctx context.Context, request domain.SynchronizeAccountRequest) (domain.Job, error) {
	job, err := workspace.application.SynchronizeAccount(ctx, request)
	return job, workspaceError(err)
}

func (workspace *Workspace) SynchronizeAlbum(ctx context.Context, request WorkspaceAlbumTraversalRequest) (domain.Job, error) {
	if len(request.AlbumIDs) > 0 {
		if request.AlbumID != "" || request.AccountID != "" {
			return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "multi-album traversal does not accept an account or single album identifier"}
		}
		if request.Order == "" {
			request.Order = wechat.AlbumForward
		}
		if request.Order != wechat.AlbumForward && request.Order != wechat.AlbumReverse {
			return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "album traversal order is not supported"}
		}
		if len(request.AlbumIDs) > 50 {
			return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "album traversal accepts no more than 50 unique album identifiers"}
		}
		seen := make(map[domain.AlbumID]struct{}, len(request.AlbumIDs))
		for _, albumID := range request.AlbumIDs {
			albumID = domain.AlbumID(strings.TrimSpace(string(albumID)))
			if albumID == "" {
				return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "album traversal identifiers are required"}
			}
			if _, duplicate := seen[albumID]; duplicate {
				return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "album traversal identifiers must be unique"}
			}
			seen[albumID] = struct{}{}
		}
		controls, ok := workspace.application.(WorkspaceMultiAlbumTraversalController)
		if !ok {
			return domain.Job{}, workspaceError(fmt.Errorf("multi-album traversal: %w", ErrUnavailable))
		}
		job, err := controls.SynchronizeAlbumsWithOrder(ctx, request.AlbumIDs, request.Order, request.Download)
		return job, workspaceError(err)
	}
	if strings.TrimSpace(string(request.AccountID)) == "" || strings.TrimSpace(string(request.AlbumID)) == "" {
		return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "album account and identifier are required"}
	}
	if request.Order == "" {
		request.Order = wechat.AlbumForward
	}
	if request.Order != wechat.AlbumForward && request.Order != wechat.AlbumReverse {
		return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "album traversal order is not supported"}
	}
	controls, ok := workspace.application.(WorkspaceAlbumTraversalController)
	if !ok {
		return domain.Job{}, workspaceError(fmt.Errorf("album traversal order: %w", ErrUnavailable))
	}
	var (
		job domain.Job
		err error
	)
	if request.Download {
		job, err = controls.SynchronizeAlbumWithOrderAndDownload(ctx, request.AccountID, request.AlbumID, request.Order)
	} else {
		job, err = controls.SynchronizeAlbumWithOrder(ctx, request.AccountID, request.AlbumID, request.Order)
	}
	return job, workspaceError(err)
}

func (workspace *Workspace) StartDownload(ctx context.Context, request domain.DownloadRequest) (domain.Job, error) {
	request.Kind = strings.TrimSpace(request.Kind)
	if len(request.ArticleIDs) == 0 && len(request.URLs) == 0 {
		return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "at least one article identifier or URL is required"}
	}
	switch request.Kind {
	case "", "article", "resources", "metadata", "comments", "paid":
	default:
		return domain.Job{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "download kind is not supported"}
	}
	job, err := workspace.application.StartDownload(ctx, request)
	return job, workspaceError(err)
}

func (workspace *Workspace) ArticlePreview(ctx context.Context, id domain.ArticleID) (WorkspaceArticlePreview, error) {
	if strings.TrimSpace(string(id)) == "" {
		return WorkspaceArticlePreview{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "article identifier is required"}
	}
	articles, ok := workspace.application.(WorkspaceArticleLookup)
	if !ok {
		return WorkspaceArticlePreview{}, workspaceError(fmt.Errorf("article preview: %w", ErrUnavailable))
	}
	article, err := articles.GetArticle(ctx, id)
	if err != nil {
		return WorkspaceArticlePreview{}, workspaceError(err)
	}
	return WorkspaceArticlePreview{ArticleID: article.ID, Title: article.Title, Available: article.HasContent}, nil
}

func (workspace *Workspace) ArticleResources(ctx context.Context, id domain.ArticleID) (WorkspaceArticleResources, error) {
	id = domain.ArticleID(strings.TrimSpace(string(id)))
	if id == "" {
		return WorkspaceArticleResources{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "article identifier is required"}
	}
	resources, ok := workspace.application.(WorkspaceArticleResourceLookup)
	if !ok {
		return WorkspaceArticleResources{}, workspaceError(fmt.Errorf("article resources: %w", ErrUnavailable))
	}
	availability, err := resources.ArticleResourceAvailability(ctx, id)
	if err != nil {
		return WorkspaceArticleResources{}, workspaceError(err)
	}
	missing := availability.Total - availability.Available
	if missing < 0 {
		return WorkspaceArticleResources{}, workspaceError(fmt.Errorf("article resources: invalid availability aggregate"))
	}
	return WorkspaceArticleResources{ArticleID: id, Total: availability.Total, Available: availability.Available,
		Missing: missing, Complete: availability.Total > 0 && availability.Available == availability.Total}, nil
}

func (workspace *Workspace) ArticleDetail(ctx context.Context, id domain.ArticleID, page WorkspacePageRequest) (WorkspaceArticleDetail, error) {
	id = domain.ArticleID(strings.TrimSpace(string(id)))
	if id == "" {
		return WorkspaceArticleDetail{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "article identifier is required"}
	}
	page, err := page.normalize()
	if err != nil {
		return WorkspaceArticleDetail{}, err
	}
	details, ok := workspace.application.(interface {
		LatestArticleMetrics(context.Context, domain.ArticleID) (library.ArticleMetrics, error)
		ListArticleResourceDetails(context.Context, domain.ArticleID, int, int) (domain.Page[library.ArticleResourceDetail], error)
	})
	if !ok {
		return WorkspaceArticleDetail{}, workspaceError(fmt.Errorf("article detail: %w", ErrUnavailable))
	}
	resources, err := details.ListArticleResourceDetails(ctx, id, page.Offset, page.Limit)
	if err != nil {
		return WorkspaceArticleDetail{}, workspaceError(err)
	}
	result := WorkspaceArticleDetail{ArticleID: id, Resources: WorkspacePage[WorkspaceArticleResourceDetail]{Items: make([]WorkspaceArticleResourceDetail, 0, len(resources.Items)), Total: resources.Total, Offset: resources.Offset, Limit: resources.Limit}}
	for _, resource := range resources.Items {
		result.Resources.Items = append(result.Resources.Items, WorkspaceArticleResourceDetail{Role: resource.Role, Ordinal: resource.Ordinal, Available: resource.Available})
	}
	metrics, err := details.LatestArticleMetrics(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return WorkspaceArticleDetail{}, workspaceError(err)
	}
	result.Metrics = WorkspaceArticleMetrics{Available: true, ReadCount: metrics.ReadCount, OldLikeCount: metrics.OldLikeCount,
		LikeCount: metrics.LikeCount, ShareCount: metrics.ShareCount, CommentCount: metrics.CommentCount, CapturedAt: metrics.CapturedAt}
	return result, nil
}

func (workspace *Workspace) ArticleComments(ctx context.Context, id domain.ArticleID, page WorkspacePageRequest) (WorkspaceArticleComments, error) {
	id, page, err := workspaceCommentPageInput(id, page)
	if err != nil {
		return WorkspaceArticleComments{}, err
	}
	comments, ok := workspace.application.(interface {
		ListArticleComments(context.Context, domain.ArticleID, int, int) (domain.Page[library.CommentRecord], error)
		PendingArticleReplyThreads(context.Context, domain.ArticleID) ([]library.ReplyThread, error)
	})
	if !ok {
		return WorkspaceArticleComments{}, workspaceError(fmt.Errorf("article comments: %w", ErrUnavailable))
	}
	pageResult, err := comments.ListArticleComments(ctx, id, page.Offset, page.Limit)
	if err != nil {
		return WorkspaceArticleComments{}, workspaceError(err)
	}
	pending, err := comments.PendingArticleReplyThreads(ctx, id)
	if err != nil {
		return WorkspaceArticleComments{}, workspaceError(err)
	}
	result := WorkspaceArticleComments{ArticleID: id, PendingReplies: len(pending), Comments: WorkspacePage[WorkspaceArticleComment]{Items: make([]WorkspaceArticleComment, 0, len(pageResult.Items)), Total: pageResult.Total, Offset: pageResult.Offset, Limit: pageResult.Limit}}
	for _, comment := range pageResult.Items {
		status := "complete"
		if comment.ReplyTotal > 0 && containsPendingReplyThread(pending, comment.UpstreamID) {
			status = "pending"
		}
		result.Comments.Items = append(result.Comments.Items, WorkspaceArticleComment{ID: comment.UpstreamID, AuthorName: comment.AuthorName, Content: comment.Content, CreatedAt: comment.CreatedAt, LikeCount: comment.LikeCount, ReplyCount: comment.ReplyTotal, ReplyStatus: status})
	}
	return result, nil
}

func (workspace *Workspace) ArticleCommentReplies(ctx context.Context, id domain.ArticleID, commentID string, page WorkspacePageRequest) (WorkspacePage[WorkspaceArticleReply], error) {
	id, page, err := workspaceCommentPageInput(id, page)
	if err != nil {
		return WorkspacePage[WorkspaceArticleReply]{}, err
	}
	commentID = strings.TrimSpace(commentID)
	if !validWorkspaceOpaqueID(commentID) {
		return WorkspacePage[WorkspaceArticleReply]{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "comment identifier is invalid"}
	}
	replies, ok := workspace.application.(interface {
		ListArticleCommentReplies(context.Context, domain.ArticleID, string, int, int) (domain.Page[library.ReplyRecord], error)
	})
	if !ok {
		return WorkspacePage[WorkspaceArticleReply]{}, workspaceError(fmt.Errorf("article comment replies: %w", ErrUnavailable))
	}
	pageResult, err := replies.ListArticleCommentReplies(ctx, id, commentID, page.Offset, page.Limit)
	if err != nil {
		return WorkspacePage[WorkspaceArticleReply]{}, workspaceError(err)
	}
	result := WorkspacePage[WorkspaceArticleReply]{Items: make([]WorkspaceArticleReply, 0, len(pageResult.Items)), Total: pageResult.Total, Offset: pageResult.Offset, Limit: pageResult.Limit}
	for _, reply := range pageResult.Items {
		result.Items = append(result.Items, WorkspaceArticleReply{ID: reply.UpstreamID, AuthorName: reply.AuthorName, Content: reply.Content, CreatedAt: reply.CreatedAt, LikeCount: reply.LikeCount})
	}
	return result, nil
}

func workspaceCommentPageInput(id domain.ArticleID, page WorkspacePageRequest) (domain.ArticleID, WorkspacePageRequest, error) {
	rawID := string(id)
	trimmedID := strings.TrimSpace(rawID)
	if rawID != trimmedID || !validWorkspaceArticleID(trimmedID) {
		return "", WorkspacePageRequest{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "article identifier is invalid"}
	}
	id = domain.ArticleID(trimmedID)
	page, err := page.normalize()
	if err != nil {
		return "", WorkspacePageRequest{}, err
	}
	return id, page, nil
}

func validWorkspaceArticleID(value string) bool {
	if strings.HasPrefix(value, "article:") {
		return validWorkspaceHexID(value[len("article:"):])
	}
	return validWorkspaceOpaqueID(value)
}

func validWorkspaceOpaqueID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

func validWorkspaceHexID(value string) bool {
	if value == "" || len(value)+len("article:") > 256 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F' || character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
}

func containsPendingReplyThread(threads []library.ReplyThread, commentID string) bool {
	for _, thread := range threads {
		if thread.ContentID == commentID {
			return true
		}
	}
	return false
}

func (workspace *Workspace) RenderArticlePreview(ctx context.Context, id domain.ArticleID) (WorkspaceRenderedArticlePreview, error) {
	if strings.TrimSpace(string(id)) == "" {
		return WorkspaceRenderedArticlePreview{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "article identifier is required"}
	}
	if workspace.preview == nil {
		return WorkspaceRenderedArticlePreview{}, workspaceError(fmt.Errorf("render article preview: %w", ErrUnavailable))
	}
	preview, err := workspace.preview.RenderArticlePreview(ctx, id)
	if err != nil {
		return WorkspaceRenderedArticlePreview{}, workspaceError(err)
	}
	if preview.ArticleID != id || len(preview.HTML) == 0 {
		return WorkspaceRenderedArticlePreview{}, &WorkspaceError{Code: WorkspaceErrorUnavailable, Message: "article preview is not available"}
	}
	return WorkspaceRenderedArticlePreview{ArticleID: preview.ArticleID, HTML: append([]byte(nil), preview.HTML...)}, nil
}

func (workspace *Workspace) SaveArticleQuery(ctx context.Context, name string, query domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.SavedArticleQuery{}, &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "saved query name is required"}
	}
	item, err := workspace.application.SaveArticleQuery(ctx, name, workspaceArticleQuery(query))
	return item, workspaceError(err)
}

func (workspace *Workspace) DeleteSavedArticleQuery(ctx context.Context, name, confirmation string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "saved query name is required"}
	}
	if confirmation != "delete-saved-query:"+name {
		return &WorkspaceError{Code: WorkspaceErrorInvalidArgument, Message: "saved query removal requires exact confirmation"}
	}
	deleted, err := workspace.application.DeleteSavedArticleQuery(ctx, name)
	if err != nil {
		return workspaceError(err)
	}
	if !deleted {
		return &WorkspaceError{Code: WorkspaceErrorNotFound, Message: "saved query was not found"}
	}
	return nil
}

func workspaceArticleQuery(query domain.ArticleQuery) domain.ArticleQuery {
	query.AccountID = domain.AccountID(strings.TrimSpace(string(query.AccountID)))
	query.AlbumID = domain.AlbumID(strings.TrimSpace(string(query.AlbumID)))
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.Author = strings.TrimSpace(query.Author)
	query.State = strings.TrimSpace(query.State)
	query.MessageTypes = append([]int(nil), query.MessageTypes...)
	query.Sorts = append([]domain.ArticleSort(nil), query.Sorts...)
	// Pagination controls the listing request, never the persisted predicate.
	query.Offset, query.Limit = 0, 0
	return query
}

func (workspace *Workspace) CancelJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	job, err := workspace.application.CancelJob(ctx, id)
	return job, workspaceError(err)
}

func workspaceSession(session wechat.Session) WorkspaceSession {
	return WorkspaceSession{State: session.State, AccountID: string(session.AccountID), AccountName: session.AccountName,
		AvatarURL: session.AvatarURL, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt,
		LastValidatedAt: session.LastValidatedAt, Validation: session.Validation}
}

func workspacePage[T any](page domain.Page[T]) WorkspacePage[T] {
	return WorkspacePage[T]{Items: append([]T(nil), page.Items...), Total: page.Total, Offset: page.Offset, Limit: page.Limit}
}

var _ WorkspaceReader = (*Workspace)(nil)
var _ WorkspaceController = (*Workspace)(nil)
var _ WorkspaceSavedQueryController = (*Workspace)(nil)
