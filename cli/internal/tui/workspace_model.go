package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	inputSearch          inputPurpose = "search"
	inputDiscoverAccount inputPurpose = "discover_account"
	inputSingleArticle   inputPurpose = "single_article"
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
}

type workspaceLoadedMsg struct {
	runtime  domain.RuntimeStatus
	session  wechat.Session
	accounts domain.Page[domain.Account]
	articles domain.Page[domain.Article]
	albums   domain.Page[domain.Album]
	jobs     domain.Page[domain.Job]
	storage  domain.StorageStatus
	panels   map[Area]OperationResult
	err      error
}

type areaLoadedMsg struct {
	area     Area
	accounts domain.Page[domain.Account]
	articles domain.Page[domain.Article]
	albums   domain.Page[domain.Album]
	jobs     domain.Page[domain.Job]
	panel    OperationResult
	err      error
}

type actionResultMsg struct {
	job       domain.Job
	operation OperationResult
	notice    string
	err       error
}

type loginStartedMsg struct {
	flow wechat.LoginFlow
	err  error
}

type loginPolledMsg struct {
	result wechat.PollResult
	err    error
}

type loginCompletedMsg struct {
	session wechat.Session
	err     error
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
	panels   map[Area]OperationResult

	modal       modalKind
	modalCursor int
	input       string
	inputLabel  string
	inputMode   inputPurpose
	confirm     confirmation
	actions     []actionItem
	columnArea  Area
	operation   OperationResult
	preview     PreviewDocument
	discovered  domain.Page[domain.Account]

	loginFlow wechat.LoginFlow
	loginPoll wechat.PollResult

	cancel context.CancelFunc
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
		storage, err := app.StorageStatus(ctx)
		if err != nil {
			return workspaceLoadedMsg{runtime: runtimeStatus, session: session, accounts: accounts, articles: articles,
				albums: albums, jobs: jobsPage, err: err}
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
			albums: albums, jobs: jobsPage, storage: storage, panels: panels}
	}
}

func (model Model) loadAreaCmd(area Area) tea.Cmd {
	return func() tea.Msg {
		ctx := model.options.Context
		app := model.options.Application
		result := areaLoadedMsg{area: area}
		var err error
		switch area {
		case AreaAccounts:
			result.accounts, err = app.QueryAccounts(ctx, model.state.Queries.Accounts)
		case AreaArticles:
			result.articles, err = app.QueryArticles(ctx, model.state.Queries.Articles)
		case AreaAlbums:
			result.albums, err = app.QueryAlbums(ctx, model.state.Queries.Albums)
		case AreaJobs:
			result.jobs, err = app.QueryJobs(ctx, model.state.Queries.Jobs)
		default:
			if model.options.Extensions != nil {
				result.panel, err = model.options.Extensions.Panel(ctx, area)
			}
		}
		result.err = err
		return result
	}
}

func (model *Model) beginCommand(command func(context.Context) tea.Msg) tea.Cmd {
	ctx, cancel := context.WithCancel(model.options.Context)
	model.cancel = cancel
	model.busy = true
	model.err = ""
	return func() tea.Msg { return command(ctx) }
}

func (model *Model) finishCommand() {
	if model.cancel != nil {
		model.cancel()
	}
	model.cancel = nil
	model.busy = false
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
