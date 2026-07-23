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

// WorkspaceReader is the P0 read contract for local browser, TUI, and MCP
// presentation adapters. It exposes only stable, profile-scoped DTOs.
type WorkspaceReader interface {
	Runtime(context.Context) (WorkspaceRuntime, error)
	Session(context.Context) (WorkspaceSession, error)
	Accounts(context.Context, WorkspaceAccountQuery) (WorkspacePage[domain.Account], error)
	Articles(context.Context, WorkspaceArticleQuery) (WorkspacePage[domain.Article], error)
	Albums(context.Context, WorkspaceAlbumQuery) (WorkspacePage[domain.Album], error)
	SavedArticleQueries(context.Context, WorkspacePageRequest) (WorkspacePage[domain.SavedArticleQuery], error)
	Jobs(context.Context, WorkspaceJobQuery) (WorkspacePage[domain.Job], error)
}

// WorkspaceController keeps supported controls tied to the same durable jobs
// and session semantics used by the existing adapters. It intentionally omits
// filesystem-bearing export inputs; those belong to WorkspaceFileService.
type WorkspaceController interface {
	BeginLogin(context.Context, string) (wechat.LoginFlow, error)
	PollLogin(context.Context) (wechat.PollResult, error)
	CompleteLogin(context.Context) (WorkspaceSession, error)
	Logout(context.Context) error
	SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error)
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
type Workspace struct{ application Application }

func NewWorkspace(application Application) *Workspace { return &Workspace{application: application} }

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

func (workspace *Workspace) Jobs(ctx context.Context, input WorkspaceJobQuery) (WorkspacePage[domain.Job], error) {
	page, err := input.Page.normalize()
	if err != nil {
		return WorkspacePage[domain.Job]{}, err
	}
	result, err := workspace.application.QueryJobs(ctx, domain.JobQuery{Kind: strings.TrimSpace(input.Kind),
		States: append([]domain.JobState(nil), input.States...), Offset: page.Offset, Limit: page.Limit})
	return workspacePage(result), workspaceError(err)
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

func (workspace *Workspace) SynchronizeAccount(ctx context.Context, request domain.SynchronizeAccountRequest) (domain.Job, error) {
	job, err := workspace.application.SynchronizeAccount(ctx, request)
	return job, workspaceError(err)
}

func (workspace *Workspace) StartDownload(ctx context.Context, request domain.DownloadRequest) (domain.Job, error) {
	job, err := workspace.application.StartDownload(ctx, request)
	return job, workspaceError(err)
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
