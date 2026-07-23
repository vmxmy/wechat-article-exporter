package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type modalKind string

const (
	modalNone      modalKind = ""
	modalHelp      modalKind = "help"
	modalInput     modalKind = "input"
	modalConfirm   modalKind = "confirm"
	modalColumns   modalKind = "columns"
	modalActions   modalKind = "actions"
	modalDetail    modalKind = "detail"
	modalPreview   modalKind = "preview"
	modalOperation modalKind = "operation"
	modalLogin     modalKind = "login"
)

type inputPurpose string

const (
	inputSearch           inputPurpose = "search"
	inputDiscoverAccount  inputPurpose = "discover_account"
	inputSingleArticle    inputPurpose = "single_article"
	inputAccountImport    inputPurpose = "account_import"
	inputAccountExport    inputPurpose = "account_export"
	inputRestoreArchive   inputPurpose = "restore_archive"
	inputExportFormat     inputPurpose = "export_format"
	inputExportOutput     inputPurpose = "export_output"
	inputExportPolicy     inputPurpose = "export_policy"
	inputExportArchive    inputPurpose = "export_archive"
	inputArticleQuery     inputPurpose = "article_query"
	inputSavedQueryName   inputPurpose = "saved_query_name"
	inputLoadQueryName    inputPurpose = "load_query_name"
	inputDeleteQueryName  inputPurpose = "delete_query_name"
	inputGCPreview        inputPurpose = "gc_preview"
	inputGCApply          inputPurpose = "gc_apply"
	inputCredentialFile   inputPurpose = "credential_file"
	inputCredentialID     inputPurpose = "credential_id"
	inputProxyName        inputPurpose = "proxy_name"
	inputProxyEndpoint    inputPurpose = "proxy_endpoint"
	inputProxyAuth        inputPurpose = "proxy_authorization"
	inputProxyTarget      inputPurpose = "proxy_target"
	inputPreferenceKey    inputPurpose = "preference_key"
	inputPreferenceValue  inputPurpose = "preference_value"
	inputDiagnosticBundle inputPurpose = "diagnostic_bundle"
)

type actionItem struct {
	Label       string
	Description string
	Kind        string
	Destructive bool
}

type confirmation struct {
	Title          string
	Scope          string
	Recoverability string
	Phrase         string
	Action         string
	Input          string
	Error          string
	IDs            []string
}

type workspaceLoadedMsg struct {
	runtime  domain.RuntimeStatus
	session  wechat.Session
	accounts domain.Page[domain.Account]
	articles domain.Page[domain.Article]
	albums   domain.Page[domain.Album]
	jobs     domain.Page[domain.Job]
	exports  domain.Page[ExportSummary]
	storage  domain.StorageStatus
	panels   map[Area]OperationResult
	warning  string
	err      error
}

type areaLoadSnapshot struct {
	accounts domain.AccountQuery
	articles domain.ArticleQuery
	albums   domain.AlbumQuery
	jobs     domain.JobQuery
	exports  PageQuery
}

type areaLoadedMsg struct {
	generation uint64
	area       Area
	query      areaLoadSnapshot
	accounts   domain.Page[domain.Account]
	articles   domain.Page[domain.Article]
	albums     domain.Page[domain.Album]
	jobs       domain.Page[domain.Job]
	exports    domain.Page[ExportSummary]
	panel      OperationResult
	err        error
}

type workspaceRefreshMsg struct {
	generation   uint64
	area         Area
	jobsQuery    domain.JobQuery
	exportsQuery PageQuery
	jobs         domain.Page[domain.Job]
	exports      domain.Page[ExportSummary]
	err          error
}

type workspaceRefreshTickMsg time.Time

type actionResultMsg struct {
	generation uint64
	job        domain.Job
	operation  OperationResult
	notice     string
	err        error
}

type loginStartedMsg struct {
	generation uint64
	flow       wechat.LoginFlow
	err        error
}

type loginPolledMsg struct {
	generation uint64
	result     wechat.PollResult
	err        error
}

type loginCompletedMsg struct {
	generation uint64
	session    wechat.Session
	err        error
}

type Model struct {
	options WorkspaceOptions
	state   WorkspaceState
	keys    KeyMap

	width  int
	height int
	layout LayoutMode

	loading  bool
	busy     bool
	quitting bool
	err      string
	notice   string

	runtime  domain.RuntimeStatus
	session  wechat.Session
	storage  domain.StorageStatus
	accounts domain.Page[domain.Account]
	articles domain.Page[domain.Article]
	albums   domain.Page[domain.Album]
	jobs     domain.Page[domain.Job]
	exports  domain.Page[ExportSummary]
	panels   map[Area]OperationResult

	modal       modalKind
	modalCursor int
	input       string
	inputLabel  string
	inputMode   inputPurpose
	inputParams map[string]string
	exportIDs   []string
	exportArea  Area
	inputSecret bool
	confirm     confirmation
	actions     []actionItem
	columnArea  Area
	operation   OperationResult
	preview     PreviewDocument
	discovered  domain.Page[domain.Account]

	loginFlow wechat.LoginFlow
	loginPoll wechat.PollResult

	cancel context.CancelFunc

	commandGeneration  uint64
	areaLoadGeneration uint64
	refreshGeneration  uint64
	refreshInFlight    bool
}

func NewWorkspace(options WorkspaceOptions) Model {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Width <= 0 {
		options.Width = 100
	}
	if options.Height <= 0 {
		options.Height = 30
	}
	if _, present := os.LookupEnv("NO_COLOR"); present {
		options.NoColor = true
	}
	if !options.ASCII {
		options.ASCII = !UnicodeSupported(os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG"))
	}
	state := defaultWorkspaceState(options.PageSize)
	model := Model{
		options: options, state: state, keys: DefaultKeyMap(), width: options.Width, height: options.Height,
		loading: true, panels: map[Area]OperationResult{},
	}
	model.updateLayout()
	return model
}

func (model Model) Init() tea.Cmd {
	if model.options.Application == nil {
		return func() tea.Msg { return workspaceLoadedMsg{err: errors.New("workspace application is required")} }
	}
	return model.loadWorkspaceCmd()
}

func (model Model) State() WorkspaceState {
	result := model.state
	result.Selection = SelectionState{}
	for area, values := range model.state.Selection {
		result.Selection[area] = append([]string(nil), values...)
	}
	result.Columns = map[Area][]string{}
	for area, values := range model.state.Columns {
		result.Columns[area] = append([]string(nil), values...)
	}
	result.Cursors = map[Area]int{}
	for area, value := range model.state.Cursors {
		result.Cursors[area] = value
	}
	return result
}

func (model Model) Layout() LayoutMode { return model.layout }
func (model Model) CurrentArea() Area  { return model.state.Area }
func (model Model) Quitting() bool     { return model.quitting }
func (model Model) Error() string      { return model.err }

func (model *Model) updateLayout() {
	switch {
	case model.options.Plain:
		model.layout = LayoutPlain
	case model.width < 72 || model.height < 20:
		model.layout = LayoutCompact
	default:
		model.layout = LayoutWide
	}
}

func UnicodeSupported(values ...string) bool {
	for _, value := range values {
		upper := strings.ToUpper(value)
		if strings.Contains(upper, "UTF-8") || strings.Contains(upper, "UTF8") {
			return true
		}
	}
	return false
}

func (model Model) loadWorkspaceCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := model.options.Context
		app := model.options.Application
		runtimeStatus, err := app.RuntimeStatus(ctx)
		if err != nil {
			return workspaceLoadedMsg{err: err}
		}
		session, err := app.SessionStatus(ctx)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, err: err}
		}
		accounts, err := app.QueryAccounts(ctx, model.state.Queries.Accounts)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, session: session, err: err}
		}
		articles, err := app.QueryArticles(ctx, model.state.Queries.Articles)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, session: session, accounts: accounts, err: err}
		}
		albums, err := app.QueryAlbums(ctx, model.state.Queries.Albums)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, session: session, accounts: accounts, articles: articles, err: err}
		}
		jobsPage, err := app.QueryJobs(ctx, model.state.Queries.Jobs)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, session: session, accounts: accounts, articles: articles,
				albums: albums, err: err}
		}
		exportsPage := domain.Page[ExportSummary]{Offset: model.state.Queries.Exports.Offset, Limit: model.state.Queries.Exports.Limit}
		warning := ""
		if model.options.Extensions != nil {
			exportsPage, err = model.options.Extensions.QueryExports(ctx, model.state.Queries.Exports.Offset, model.state.Queries.Exports.Limit)
			if err != nil {
				warning = "exports are temporarily unavailable: " + err.Error()
				exportsPage = domain.Page[ExportSummary]{Offset: model.state.Queries.Exports.Offset, Limit: model.state.Queries.Exports.Limit}
			}
		}
		storage, err := app.StorageStatus(ctx)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, session: session, accounts: accounts, articles: articles,
				albums: albums, jobs: jobsPage, exports: exportsPage, err: err}
		}
		panels := make(map[Area]OperationResult)
		if model.options.Extensions != nil {
			for _, area := range []Area{AreaExports, AreaSettings, AreaStorage, AreaDiagnostics} {
				panel, panelErr := model.options.Extensions.Panel(ctx, area)
				if panelErr == nil {
					panels[area] = panel
				}
			}
		}
		return workspaceLoadedMsg{runtime: runtimeStatus, session: session, accounts: accounts, articles: articles,
			albums: albums, jobs: jobsPage, exports: exportsPage, storage: storage, panels: panels, warning: warning}
	}
}

func (model *Model) loadAreaCmd(area Area) tea.Cmd {
	model.areaLoadGeneration++
	if area == AreaJobs || area == AreaExports {
		model.refreshGeneration++
		model.refreshInFlight = false
	}
	generation := model.areaLoadGeneration
	query := model.areaLoadSnapshot()
	return func() tea.Msg {
		ctx := model.options.Context
		app := model.options.Application
		result := areaLoadedMsg{generation: generation, area: area, query: query}
		var err error
		switch area {
		case AreaAccounts:
			result.accounts, err = app.QueryAccounts(ctx, query.accounts)
		case AreaArticles:
			result.articles, err = app.QueryArticles(ctx, query.articles)
		case AreaAlbums:
			result.albums, err = app.QueryAlbums(ctx, query.albums)
		case AreaJobs:
			result.jobs, err = app.QueryJobs(ctx, query.jobs)
		case AreaExports:
			if model.options.Extensions != nil {
				result.exports, err = model.options.Extensions.QueryExports(ctx, query.exports.Offset, query.exports.Limit)
			}
		default:
			if model.options.Extensions != nil {
				result.panel, err = model.options.Extensions.Panel(ctx, area)
			}
		}
		result.err = err
		return result
	}
}

func (model Model) areaLoadSnapshot() areaLoadSnapshot {
	query := areaLoadSnapshot{
		accounts: model.state.Queries.Accounts,
		articles: model.state.Queries.Articles,
		albums:   model.state.Queries.Albums,
		jobs:     model.state.Queries.Jobs,
		exports:  model.state.Queries.Exports,
	}
	query.articles.MessageTypes = append([]int(nil), query.articles.MessageTypes...)
	query.articles.Sorts = append([]domain.ArticleSort(nil), query.articles.Sorts...)
	query.jobs.States = append([]domain.JobState(nil), query.jobs.States...)
	return query
}

func (model Model) areaLoadMatches(message areaLoadedMsg) bool {
	if message.generation != model.areaLoadGeneration || message.area != model.state.Area {
		return false
	}
	current := model.areaLoadSnapshot()
	switch message.area {
	case AreaAccounts:
		return message.query.accounts == current.accounts
	case AreaArticles:
		return reflect.DeepEqual(message.query.articles, current.articles)
	case AreaAlbums:
		return message.query.albums == current.albums
	case AreaJobs:
		return reflect.DeepEqual(message.query.jobs, current.jobs)
	case AreaExports:
		return message.query.exports == current.exports
	default:
		return true
	}
}

func (model Model) scheduleRefreshCmd() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return workspaceRefreshTickMsg(now) })
}

func (model Model) refreshActiveWorkCmd(generation uint64) tea.Cmd {
	area := model.state.Area
	jobsQuery := model.state.Queries.Jobs
	jobsQuery.States = append([]domain.JobState(nil), jobsQuery.States...)
	exportsQuery := model.state.Queries.Exports
	return func() tea.Msg {
		ctx := model.options.Context
		result := workspaceRefreshMsg{generation: generation, area: area, jobsQuery: jobsQuery, exportsQuery: exportsQuery}
		switch area {
		case AreaJobs:
			result.jobs, result.err = model.options.Application.QueryJobs(ctx, jobsQuery)
		case AreaExports:
			if model.options.Extensions != nil {
				result.exports, result.err = model.options.Extensions.QueryExports(ctx, exportsQuery.Offset, exportsQuery.Limit)
			}
		}
		return result
	}
}

func (model *Model) beginCommand(command func(context.Context) tea.Msg) tea.Cmd {
	if model.cancel != nil {
		model.cancel()
	}
	model.commandGeneration++
	generation := model.commandGeneration
	ctx, cancel := context.WithCancel(model.options.Context)
	model.cancel = cancel
	model.busy = true
	model.err = ""
	return func() tea.Msg { return stampCommandMessage(command(ctx), generation) }
}

func stampCommandMessage(message tea.Msg, generation uint64) tea.Msg {
	switch typed := message.(type) {
	case actionResultMsg:
		typed.generation = generation
		return typed
	case loginStartedMsg:
		typed.generation = generation
		return typed
	case loginPolledMsg:
		typed.generation = generation
		return typed
	case loginCompletedMsg:
		typed.generation = generation
		return typed
	case previewLoadedMsg:
		typed.generation = generation
		return typed
	case articleQueryLoadedMsg:
		typed.generation = generation
		return typed
	default:
		return actionResultMsg{generation: generation, err: fmt.Errorf("unsupported command result %T", message)}
	}
}

func (model *Model) finishCommand(generation uint64) bool {
	if generation != model.commandGeneration {
		return false
	}
	if model.cancel != nil {
		model.cancel()
	}
	model.cancel = nil
	model.busy = false
	return true
}

func (model *Model) cancelCommand() {
	if model.cancel != nil {
		model.cancel()
	}
	model.cancel = nil
	model.busy = false
	model.commandGeneration++
}

func (model Model) currentID() string {
	index := model.state.Cursors[model.state.Area]
	switch model.state.Area {
	case AreaAccounts:
		if index >= 0 && index < len(model.accounts.Items) {
			return string(model.accounts.Items[index].ID)
		}
	case AreaArticles:
		if index >= 0 && index < len(model.articles.Items) {
			return string(model.articles.Items[index].ID)
		}
	case AreaAlbums:
		if index >= 0 && index < len(model.albums.Items) {
			return string(model.albums.Items[index].ID)
		}
	case AreaJobs:
		if index >= 0 && index < len(model.jobs.Items) {
			return string(model.jobs.Items[index].ID)
		}
	case AreaExports:
		if index >= 0 && index < len(model.exports.Items) {
			return string(model.exports.Items[index].ID)
		}
	}
	return ""
}

func (model Model) selectedIDs() []string {
	values := model.state.Selection.IDs(model.state.Area)
	if len(values) == 0 {
		if current := model.currentID(); current != "" {
			values = []string{current}
		}
	}
	return values
}

// exportSelectionIDs intentionally excludes IDs from the Exports area. Those
// rows identify prior export records, not articles, and must never be reused as
// an article selection for a new export.
func (model Model) exportSelectionIDs() []string {
	switch model.state.Area {
	case AreaArticles, AreaAlbums:
		return model.selectedIDs()
	default:
		return nil
	}
}

func (model *Model) clampCursor() {
	maximum := model.itemCount() - 1
	if maximum < 0 {
		maximum = 0
	}
	if model.state.Cursors[model.state.Area] > maximum {
		model.state.Cursors[model.state.Area] = maximum
	}
	if model.state.Cursors[model.state.Area] < 0 {
		model.state.Cursors[model.state.Area] = 0
	}
}

func (model Model) itemCount() int {
	switch model.state.Area {
	case AreaAccounts:
		return len(model.accounts.Items)
	case AreaArticles:
		return len(model.articles.Items)
	case AreaAlbums:
		return len(model.albums.Items)
	case AreaJobs:
		return len(model.jobs.Items)
	case AreaExports:
		return len(model.exports.Items)
	default:
		return 0
	}
}

func (model *Model) navigate(delta int) {
	index := 0
	for cursor, area := range workspaceAreas {
		if area == model.state.Area {
			index = cursor
			break
		}
	}
	index = (index + delta + len(workspaceAreas)) % len(workspaceAreas)
	model.state.Area = workspaceAreas[index]
	model.notice = ""
	model.err = ""
	model.clampCursor()
}

func (model Model) pageDescription(offset, limit, total int) string {
	if total == 0 {
		return "0 items"
	}
	start := offset + 1
	end := offset + limit
	if end > total {
		end = total
	}
	return fmt.Sprintf("%d-%d of %d", start, end, total)
}
