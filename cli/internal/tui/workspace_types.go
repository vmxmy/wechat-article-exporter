package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type Area string

const (
	AreaHome        Area = "home"
	AreaAccounts    Area = "accounts"
	AreaArticles    Area = "articles"
	AreaAlbums      Area = "albums"
	AreaJobs        Area = "jobs"
	AreaExports     Area = "exports"
	AreaSettings    Area = "settings"
	AreaStorage     Area = "storage"
	AreaDiagnostics Area = "diagnostics"
)

var workspaceAreas = []Area{
	AreaHome, AreaAccounts, AreaArticles, AreaAlbums, AreaJobs,
	AreaExports, AreaSettings, AreaStorage, AreaDiagnostics,
}

type LayoutMode string

const (
	LayoutWide    LayoutMode = "wide"
	LayoutCompact LayoutMode = "compact"
	LayoutPlain   LayoutMode = "plain"
)

type OperationKind string

const (
	OperationAccountImport    OperationKind = "account_import"
	OperationAccountExport    OperationKind = "account_export"
	OperationAlbumTraverse    OperationKind = "album_traverse"
	OperationJobLogs          OperationKind = "job_logs"
	OperationJobPause         OperationKind = "job_pause"
	OperationJobResume        OperationKind = "job_resume"
	OperationJobRetry         OperationKind = "job_retry"
	OperationRouteHealth      OperationKind = "route_health"
	OperationExportManifest   OperationKind = "export_manifest"
	OperationExportVerify     OperationKind = "export_verify"
	OperationOpenExport       OperationKind = "open_export"
	OperationExportConfig     OperationKind = "export_config"
	OperationExportStart      OperationKind = "export_start"
	OperationCredentials      OperationKind = "credentials"
	OperationCredentialImport OperationKind = "credential_import"
	OperationCredentialCheck  OperationKind = "credential_validate"
	OperationCredentialRemove OperationKind = "credential_remove"
	OperationProxies          OperationKind = "proxies"
	OperationProxyAdd         OperationKind = "proxy_add"
	OperationProxyEnable      OperationKind = "proxy_enable"
	OperationProxyDisable     OperationKind = "proxy_disable"
	OperationProxyTest        OperationKind = "proxy_test"
	OperationProxyRemove      OperationKind = "proxy_remove"
	OperationPreferences      OperationKind = "preferences"
	OperationPreferenceSet    OperationKind = "preference_set"
	OperationBackup           OperationKind = "backup"
	OperationRestore          OperationKind = "restore"
	OperationIntegrity        OperationKind = "integrity"
	OperationGarbageCollect   OperationKind = "garbage_collect"
	OperationDiagnostics      OperationKind = "diagnostics"
	OperationDiagnosticBundle OperationKind = "diagnostic_bundle"
	OperationArticleComments  OperationKind = "article_comments"
	OperationArticleMetrics   OperationKind = "article_metrics"
	OperationArticleResources OperationKind = "article_resources"
)

type OperationRequest struct {
	Kind       OperationKind     `json:"kind"`
	Area       Area              `json:"area"`
	IDs        []string          `json:"ids,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

type OperationResult struct {
	Title   string            `json:"title"`
	Message string            `json:"message,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	Lines   []string          `json:"lines,omitempty"`
}

type ExportSummary struct {
	ID                   domain.ExportID `json:"id"`
	Format               string          `json:"format"`
	State                string          `json:"state"`
	OutputRoot           string          `json:"outputRoot"`
	ProvenanceState      string          `json:"provenanceState"`
	ProvenancePath       string          `json:"provenancePath,omitempty"`
	ProvenanceGeneration int64           `json:"provenanceGeneration"`
	CreatedAt            time.Time       `json:"createdAt"`
	CompletedAt          *time.Time      `json:"completedAt,omitempty"`
}

type PreviewDocument struct {
	Title    string `json:"title"`
	Format   string `json:"format"`
	Text     string `json:"text"`
	LocalURL string `json:"localUrl,omitempty"`
}

// WorkspaceExtensions contains presentation-facing operations not yet present
// on application.Application. Implementations must delegate to the same domain
// services used by other adapters rather than place business rules in the TUI.
type WorkspaceExtensions interface {
	Panel(context.Context, Area) (OperationResult, error)
	QueryExports(context.Context, int, int) (domain.Page[ExportSummary], error)
	PreviewArticle(context.Context, domain.ArticleID) (PreviewDocument, error)
	OpenHTMLPreview(context.Context, domain.ArticleID) error
	Operate(context.Context, OperationRequest) (OperationResult, error)
}

// ExportRootProvider optionally supplies the preferred local output directory
// for new export jobs. Workspace implementations fall back to a readable
// Downloads path when this capability is unavailable.
type ExportRootProvider interface {
	DefaultExportRoot(context.Context) (string, error)
}

type WorkspaceOptions struct {
	Context     context.Context
	Application WorkspaceApplication
	Extensions  WorkspaceExtensions
	Input       io.Reader
	Output      io.Writer
	Force       bool
	NoColor     bool
	ASCII       bool
	Plain       bool
	Language    string
	Width       int
	Height      int
	PageSize    int
	Now         func() time.Time
}

// WorkspaceApplication is the exact shared application surface consumed by
// the terminal workspace. application.Application satisfies it, while tests
// can provide a narrow adapter without duplicating the full command/MCP seam.
type WorkspaceApplication interface {
	RuntimeStatus(context.Context) (domain.RuntimeStatus, error)
	BeginLogin(context.Context, string) (wechat.LoginFlow, error)
	PollLogin(context.Context) (wechat.PollResult, error)
	CompleteLogin(context.Context) (wechat.Session, error)
	SessionStatus(context.Context) (wechat.Session, error)
	Logout(context.Context) error
	SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
	SaveAccount(context.Context, domain.Account) (domain.Account, error)
	QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
	DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error)
	QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error)
	SaveArticleQuery(context.Context, string, domain.ArticleQuery) (domain.SavedArticleQuery, error)
	ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error)
	DeleteSavedArticleQuery(context.Context, string) (bool, error)
	QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error)
	SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error)
	StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error)
	StartExport(context.Context, domain.ExportRequest) (domain.Job, error)
	QueryJobs(context.Context, domain.JobQuery) (domain.Page[domain.Job], error)
	CancelJob(context.Context, domain.JobID) (domain.Job, error)
	StorageStatus(context.Context) (domain.StorageStatus, error)
}

type QueryState struct {
	Accounts domain.AccountQuery `json:"accounts"`
	Articles domain.ArticleQuery `json:"articles"`
	Albums   domain.AlbumQuery   `json:"albums"`
	Jobs     domain.JobQuery     `json:"jobs"`
	Exports  PageQuery           `json:"exports"`
}

type PageQuery struct {
	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`
}

type SelectionState map[Area][]string

func (selection SelectionState) Has(area Area, id string) bool {
	values := selection[area]
	index := sort.SearchStrings(values, id)
	return index < len(values) && values[index] == id
}

func (selection SelectionState) Toggle(area Area, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	values := append([]string(nil), selection[area]...)
	index := sort.SearchStrings(values, id)
	if index < len(values) && values[index] == id {
		values = append(values[:index], values[index+1:]...)
	} else {
		values = append(values, "")
		copy(values[index+1:], values[index:])
		values[index] = id
	}
	selection[area] = values
}

func (selection SelectionState) Clear(area Area) { delete(selection, area) }

func (selection SelectionState) IDs(area Area) []string {
	return append([]string(nil), selection[area]...)
}

type WorkspaceState struct {
	Area      Area              `json:"area"`
	Queries   QueryState        `json:"queries"`
	Selection SelectionState    `json:"selection"`
	Columns   map[Area][]string `json:"columns"`
	Cursors   map[Area]int      `json:"cursors"`
}

func (state WorkspaceState) Marshal() ([]byte, error) { return json.Marshal(state) }

func ParseWorkspaceState(data []byte) (WorkspaceState, error) {
	return ParseWorkspaceStateWithPageSize(data, 20)
}

func ParseWorkspaceStateWithPageSize(data []byte, pageSize int) (WorkspaceState, error) {
	var state WorkspaceState
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return WorkspaceState{}, err
	}
	if !validArea(state.Area) {
		return WorkspaceState{}, errors.New("workspace state contains an invalid area")
	}
	state.normalize(pageSize)
	return state, nil
}

func defaultWorkspaceState(pageSize int) WorkspaceState {
	if pageSize <= 0 {
		pageSize = 20
	}
	return WorkspaceState{
		Area: AreaHome,
		Queries: QueryState{
			Accounts: domain.AccountQuery{Limit: pageSize},
			Articles: domain.ArticleQuery{Limit: pageSize},
			Albums:   domain.AlbumQuery{Limit: pageSize},
			Jobs:     domain.JobQuery{Limit: pageSize},
			Exports:  PageQuery{Limit: pageSize},
		},
		Selection: SelectionState{},
		Columns: map[Area][]string{
			AreaAccounts: {"name", "alias", "articles", "last_sync"},
			AreaArticles: {"title", "author", "published", "content", "metrics"},
			AreaAlbums:   {"name", "articles", "paid"},
			AreaJobs:     {"kind", "state", "updated"},
		},
		Cursors: map[Area]int{},
	}
}

func (state *WorkspaceState) normalize(defaultPageSize int) {
	if defaultPageSize <= 0 {
		defaultPageSize = 20
	}
	if !validArea(state.Area) {
		state.Area = AreaHome
	}
	if state.Selection == nil {
		state.Selection = SelectionState{}
	}
	if state.Columns == nil {
		state.Columns = map[Area][]string{}
	}
	if state.Cursors == nil {
		state.Cursors = map[Area]int{}
	}
	if state.Queries.Accounts.Limit <= 0 {
		state.Queries.Accounts.Limit = defaultPageSize
	}
	if state.Queries.Accounts.Limit > 500 {
		state.Queries.Accounts.Limit = 500
	}
	if state.Queries.Accounts.Offset < 0 {
		state.Queries.Accounts.Offset = 0
	}
	if state.Queries.Articles.Limit <= 0 {
		state.Queries.Articles.Limit = defaultPageSize
	}
	if state.Queries.Articles.Limit > 500 {
		state.Queries.Articles.Limit = 500
	}
	if state.Queries.Articles.Offset < 0 {
		state.Queries.Articles.Offset = 0
	}
	if state.Queries.Albums.Limit <= 0 {
		state.Queries.Albums.Limit = defaultPageSize
	}
	if state.Queries.Albums.Limit > 500 {
		state.Queries.Albums.Limit = 500
	}
	if state.Queries.Albums.Offset < 0 {
		state.Queries.Albums.Offset = 0
	}
	if state.Queries.Jobs.Limit <= 0 {
		state.Queries.Jobs.Limit = defaultPageSize
	}
	if state.Queries.Jobs.Limit > 500 {
		state.Queries.Jobs.Limit = 500
	}
	if state.Queries.Jobs.Offset < 0 {
		state.Queries.Jobs.Offset = 0
	}
	if state.Queries.Exports.Limit <= 0 {
		state.Queries.Exports.Limit = defaultPageSize
	}
	if state.Queries.Exports.Limit > 500 {
		state.Queries.Exports.Limit = 500
	}
	if state.Queries.Exports.Offset < 0 {
		state.Queries.Exports.Offset = 0
	}
	for area, values := range state.Selection {
		sort.Strings(values)
		state.Selection[area] = compactStrings(values)
	}
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || len(result) > 0 && result[len(result)-1] == value {
			continue
		}
		result = append(result, value)
	}
	return result
}

func validArea(area Area) bool {
	for _, candidate := range workspaceAreas {
		if area == candidate {
			return true
		}
	}
	return false
}

type KeyMap struct {
	NextArea     []string
	PreviousArea []string
	MoveUp       []string
	MoveDown     []string
	Select       []string
	Open         []string
	Back         []string
	Search       []string
	Columns      []string
	Refresh      []string
	Actions      []string
	Help         []string
	Quit         []string
	Cancel       []string
	NextPage     []string
	PreviousPage []string
	Preview      []string
	HTMLPreview  []string
	Confirm      []string
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		NextArea: []string{"tab", "right"}, PreviousArea: []string{"shift+tab", "left"},
		MoveUp: []string{"up", "k"}, MoveDown: []string{"down", "j"}, Select: []string{" "}, Open: []string{"enter"},
		Back: []string{"esc", "backspace"}, Search: []string{"/"}, Columns: []string{"c"}, Refresh: []string{"r"}, Actions: []string{"a"},
		Help: []string{"?"}, Quit: []string{"q"}, Cancel: []string{"ctrl+c"}, NextPage: []string{"]", "pgdown"}, PreviousPage: []string{"[", "pgup"},
		Preview: []string{"p"}, HTMLPreview: []string{"H"}, Confirm: []string{"enter"},
	}
}

func keyMatches(value string, bindings []string) bool {
	for _, binding := range bindings {
		if value == binding {
			return true
		}
	}
	return false
}
