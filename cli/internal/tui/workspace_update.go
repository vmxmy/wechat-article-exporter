package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tea.WindowSizeMsg:
		model.width = typed.Width
		model.height = typed.Height
		model.updateLayout()
		return model, nil
	case workspaceLoadedMsg:
		model.loading = false
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.runtime, model.session, model.storage = typed.runtime, typed.session, typed.storage
		model.accounts, model.articles, model.albums, model.jobs, model.exports = typed.accounts, typed.articles, typed.albums, typed.jobs, typed.exports
		if typed.warning != "" {
			model.notice = typed.warning
		}
		for area, panel := range typed.panels {
			model.panels[area] = panel
		}
		model.clampCursor()
		return model, model.scheduleRefreshCmd()
	case areaLoadedMsg:
		if !model.areaLoadMatches(typed) {
			return model, nil
		}
		model.loading = false
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		switch typed.area {
		case AreaAccounts:
			model.accounts = typed.accounts
		case AreaArticles:
			model.articles = typed.articles
		case AreaAlbums:
			model.albums = typed.albums
		case AreaJobs:
			model.jobs = typed.jobs
		case AreaExports:
			model.exports = typed.exports
		default:
			model.panels[typed.area] = typed.panel
		}
		model.clampCursor()
		return model, nil
	case workspaceRefreshTickMsg:
		next := model.scheduleRefreshCmd()
		if model.loading || model.busy || model.refreshInFlight || model.modal == modalConfirm ||
			model.state.Area != AreaJobs && model.state.Area != AreaExports {
			return model, next
		}
		model.refreshGeneration++
		model.refreshInFlight = true
		return model, tea.Batch(next, model.refreshActiveWorkCmd(model.refreshGeneration))
	case workspaceRefreshMsg:
		if typed.generation != model.refreshGeneration {
			return model, nil
		}
		model.refreshInFlight = false
		if typed.err != nil {
			model.notice = "automatic refresh failed: " + typed.err.Error()
			return model, nil
		}
		if typed.area == AreaJobs && model.state.Area == AreaJobs && model.modal != modalConfirm &&
			reflect.DeepEqual(typed.jobsQuery, model.state.Queries.Jobs) {
			model.jobs = typed.jobs
		}
		if typed.area == AreaExports && model.state.Area == AreaExports && model.modal != modalConfirm &&
			typed.exportsQuery == model.state.Queries.Exports {
			model.exports = typed.exports
		}
		model.clampCursor()
		return model, nil
	case actionResultMsg:
		if !model.finishCommand(typed.generation) {
			return model, nil
		}
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.err = ""
		model.notice = typed.notice
		if typed.job.ID != "" {
			model.notice = fmt.Sprintf("job %s queued (%s)", typed.job.ID, typed.job.Kind)
		}
		if typed.operation.Title != "" || typed.operation.Message != "" || len(typed.operation.Lines) > 0 {
			model.operation = typed.operation
			model.modal = modalOperation
		}
		return model, model.loadAreaCmd(model.state.Area)
	case loginStartedMsg:
		if !model.finishCommand(typed.generation) {
			return model, nil
		}
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.loginFlow = typed.flow
		model.modal = modalLogin
		model.notice = "scan the QR code in WeChat, then press r to check status"
		return model, nil
	case loginPolledMsg:
		if !model.finishCommand(typed.generation) {
			return model, nil
		}
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.loginPoll = typed.result
		if typed.result.State == wechat.QRConfirmed || typed.result.State == wechat.QRScanned {
			return model, model.beginCommand(func(ctx context.Context) tea.Msg {
				session, err := model.options.Application.CompleteLogin(ctx)
				return loginCompletedMsg{session: session, err: err}
			})
		}
		if typed.result.State == wechat.QRExpired {
			model.notice = "QR code expired; press l to request a new one"
		} else {
			model.notice = "login status: " + string(typed.result.State)
		}
		return model, nil
	case loginCompletedMsg:
		if !model.finishCommand(typed.generation) {
			return model, nil
		}
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.session = typed.session
		model.modal = modalNone
		model.notice = "WeChat session authenticated"
		return model, nil
	case previewLoadedMsg:
		if !model.finishCommand(typed.generation) {
			return model, nil
		}
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.preview = typed.preview
		model.modal = modalPreview
		return model, nil
	case articleQueryLoadedMsg:
		if !model.finishCommand(typed.generation) {
			return model, nil
		}
		model.state.Queries.Articles = typed.query
		model.state.Queries.Articles.Offset = 0
		model.notice = "loaded saved article query " + typed.name
		model.loading = true
		return model, model.loadAreaCmd(AreaArticles)
	case tea.KeyMsg:
		return model.updateKey(typed)
	}
	return model, nil
}

func (model Model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	value := key.String()
	if keyMatches(value, model.keys.Cancel) {
		if model.busy && model.cancel != nil {
			model.cancelCommand()
			model.notice = "operation cancelled"
			return model, nil
		}
		if model.modal != modalNone {
			model.closeModal()
			return model, nil
		}
		model.quitting = true
		return model, tea.Quit
	}
	if model.modal != modalNone {
		return model.updateModalKey(key)
	}
	if model.busy {
		return model, nil
	}
	if keyMatches(value, model.keys.Quit) {
		model.quitting = true
		return model, tea.Quit
	}
	if keyMatches(value, model.keys.Help) {
		model.modal = modalHelp
		return model, nil
	}
	if model.state.Area == AreaHome && value == "l" {
		return model.startLogin()
	}
	if keyMatches(value, model.keys.NextArea) {
		model.navigate(1)
		return model, nil
	}
	if keyMatches(value, model.keys.PreviousArea) {
		model.navigate(-1)
		return model, nil
	}
	if keyMatches(value, model.keys.MoveUp) {
		model.state.Cursors[model.state.Area]--
		model.clampCursor()
		return model, nil
	}
	if keyMatches(value, model.keys.MoveDown) {
		model.state.Cursors[model.state.Area]++
		model.clampCursor()
		return model, nil
	}
	if keyMatches(value, model.keys.Select) {
		if id := model.currentID(); id != "" {
			model.state.Selection.Toggle(model.state.Area, id)
		}
		return model, nil
	}
	if keyMatches(value, model.keys.Search) {
		if model.state.Area == AreaArticles {
			model.inputMode, model.inputLabel = inputArticleQuery, articleQueryPrompt()
			model.input = formatArticleQueryInput(model.state.Queries.Articles)
			model.modal = modalInput
			return model, nil
		}
		model.inputMode = inputSearch
		model.inputLabel = "Local filter"
		model.input = model.currentKeyword()
		model.modal = modalInput
		return model, nil
	}
	if keyMatches(value, model.keys.Columns) && (model.state.Area == AreaAccounts || model.state.Area == AreaArticles) {
		model.columnArea = model.state.Area
		model.modalCursor = 0
		model.modal = modalColumns
		return model, nil
	}
	if keyMatches(value, model.keys.Refresh) {
		model.loading = true
		return model, model.loadAreaCmd(model.state.Area)
	}
	if keyMatches(value, model.keys.NextPage) {
		if model.movePage(1) {
			model.loading = true
			return model, model.loadAreaCmd(model.state.Area)
		}
		return model, nil
	}
	if keyMatches(value, model.keys.PreviousPage) {
		if model.movePage(-1) {
			model.loading = true
			return model, model.loadAreaCmd(model.state.Area)
		}
		return model, nil
	}
	if keyMatches(value, model.keys.Actions) {
		model.actions = model.actionsForArea()
		model.modalCursor = 0
		model.modal = modalActions
		return model, nil
	}
	if keyMatches(value, model.keys.Open) {
		return model.openCurrent()
	}
	if keyMatches(value, model.keys.Preview) && model.state.Area == AreaArticles {
		return model.startArticlePreview()
	}
	if keyMatches(value, model.keys.HTMLPreview) && model.state.Area == AreaArticles {
		return model.startHTMLPreview()
	}
	if model.state.Area == AreaAccounts && value == "d" {
		model.inputMode = inputDiscoverAccount
		model.inputLabel = "Search WeChat accounts"
		model.input = ""
		model.modal = modalInput
		return model, nil
	}
	if model.state.Area == AreaArticles && value == "n" {
		model.inputMode = inputSingleArticle
		model.inputLabel = "Single article URL"
		model.input = ""
		model.modal = modalInput
		return model, nil
	}
	return model, nil
}

func (model Model) updateModalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	value := key.String()
	if keyMatches(value, model.keys.Back) || value == "q" {
		model.closeModal()
		return model, nil
	}
	switch model.modal {
	case modalInput:
		if value == "enter" {
			return model.submitInput()
		}
		if key.Type == tea.KeyBackspace || key.Type == tea.KeyDelete {
			model.input = trimLastRune(model.input)
			return model, nil
		}
		if key.Type == tea.KeyRunes {
			model.input += string(key.Runes)
		}
	case modalConfirm:
		if value == "enter" {
			if model.confirm.Input != model.confirm.Phrase {
				model.confirm.Error = "confirmation text does not match"
				return model, nil
			}
			return model.executeConfirmedAction()
		}
		if key.Type == tea.KeyBackspace || key.Type == tea.KeyDelete {
			model.confirm.Input = trimLastRune(model.confirm.Input)
			return model, nil
		}
		if key.Type == tea.KeyRunes {
			model.confirm.Input += string(key.Runes)
		}
	case modalActions:
		if keyMatches(value, model.keys.MoveUp) && model.modalCursor > 0 {
			model.modalCursor--
		}
		if keyMatches(value, model.keys.MoveDown) && model.modalCursor < len(model.actions)-1 {
			model.modalCursor++
		}
		if value == "enter" && len(model.actions) > 0 {
			return model.chooseAction(model.actions[model.modalCursor])
		}
	case modalColumns:
		columns := availableColumns(model.columnArea)
		if keyMatches(value, model.keys.MoveUp) && model.modalCursor > 0 {
			model.modalCursor--
		}
		if keyMatches(value, model.keys.MoveDown) && model.modalCursor < len(columns)-1 {
			model.modalCursor++
		}
		if keyMatches(value, model.keys.Select) && len(columns) > 0 {
			model.toggleColumn(columns[model.modalCursor])
		}
	case modalLogin:
		if value == "r" {
			return model, model.beginCommand(func(ctx context.Context) tea.Msg {
				result, err := model.options.Application.PollLogin(ctx)
				return loginPolledMsg{result: result, err: err}
			})
		}
	}
	return model, nil
}

func (model *Model) closeModal() {
	model.modal = modalNone
	model.input = ""
	model.inputParams = nil
	model.exportIDs = nil
	model.exportArea = ""
	model.inputSecret = false
	model.confirm = confirmation{}
	model.actions = nil
	model.operation = OperationResult{}
	model.preview = PreviewDocument{}
	model.modalCursor = 0
}

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return ""
	}
	return string(runes[:len(runes)-1])
}

func (model Model) currentKeyword() string {
	switch model.state.Area {
	case AreaAccounts:
		return model.state.Queries.Accounts.Keyword
	case AreaArticles:
		return model.state.Queries.Articles.Keyword
	case AreaAlbums:
		return model.state.Queries.Albums.Keyword
	}
	return ""
}

func (model *Model) setKeyword(value string) {
	value = strings.TrimSpace(value)
	switch model.state.Area {
	case AreaAccounts:
		model.state.Queries.Accounts.Keyword, model.state.Queries.Accounts.Offset = value, 0
	case AreaArticles:
		model.state.Queries.Articles.Keyword, model.state.Queries.Articles.Offset = value, 0
	case AreaAlbums:
		model.state.Queries.Albums.Keyword, model.state.Queries.Albums.Offset = value, 0
	}
}

func (model Model) submitInput() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(model.input)
	switch model.inputMode {
	case inputSearch:
		model.setKeyword(value)
		model.closeModal()
		model.loading = true
		return model, model.loadAreaCmd(model.state.Area)
	case inputDiscoverAccount:
		model.closeModal()
		if value == "" {
			model.err = "account search keyword is required"
			return model, nil
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			page, err := model.options.Application.SearchAccounts(ctx, domain.AccountQuery{Keyword: value, Limit: 20})
			if err != nil {
				return actionResultMsg{err: err}
			}
			added := 0
			operation := OperationResult{Title: "Account search", Message: fmt.Sprintf("%d upstream matches; saved returned accounts locally", page.Total)}
			for _, account := range page.Items {
				saved, saveErr := model.options.Application.SaveAccount(ctx, account)
				if saveErr != nil {
					operation.Lines = append(operation.Lines, "FAILED · "+account.Name+" · "+saveErr.Error())
					continue
				}
				added++
				operation.Lines = append(operation.Lines, saved.Name+" · "+saved.FakeID+" · "+string(saved.ID))
			}
			failed := len(page.Items) - added
			operation.Fields = map[string]string{"saved": fmt.Sprint(added), "failed": fmt.Sprint(failed)}
			if failed > 0 {
				operation.Message = fmt.Sprintf("saved %d of %d returned accounts; failed rows are listed explicitly", added, len(page.Items))
			}
			return actionResultMsg{operation: operation}
		})
	case inputSingleArticle:
		model.closeModal()
		if value == "" {
			model.err = "article URL is required"
			return model, nil
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			job, err := model.options.Application.StartDownload(ctx, domain.DownloadRequest{URLs: []string{value}})
			return actionResultMsg{job: job, err: err}
		})
	case inputAccountImport:
		model.closeModal()
		if value == "" {
			model.err = "account manifest path is required"
			return model, nil
		}
		return model.executeExtension(OperationAccountImport, nil, map[string]string{"path": value})
	case inputAccountExport:
		model.closeModal()
		if value == "" {
			model.err = "account manifest output path is required"
			return model, nil
		}
		return model.executeExtension(OperationAccountExport, nil, map[string]string{"path": value})
	case inputRestoreArchive:
		model.input = value
		if value == "" {
			model.err = "backup archive path is required"
			return model, nil
		}
		model.confirm = confirmation{Title: "Restore library backup", Scope: "Replace the active local profile library from " + value,
			Recoverability: "The archive is staged and verified; create a backup first.", Phrase: "restore-library", Action: string(OperationRestore)}
		model.inputParams = map[string]string{"path": value, "conflict": "refuse"}
		model.modal = modalConfirm
		return model, nil
	case inputExportFormat:
		format := strings.ToLower(value)
		allowed := map[string]struct{}{"html": {}, "markdown": {}, "text": {}, "json": {}, "xlsx": {}, "docx": {}, "pdf": {}}
		if _, ok := allowed[format]; !ok {
			model.err = "export format must be html, markdown, text, json, xlsx, docx, or pdf"
			return model, nil
		}
		model.inputParams = map[string]string{"format": format}
		model.exportIDs = model.exportSelectionIDs()
		model.exportArea = model.state.Area
		if format == "html" {
			model.inputMode = inputExportPolicy
			model.inputLabel = "HTML resource policy (best-effort or strict)"
			model.input = "best-effort"
			model.modal = modalInput
			return model, nil
		}
		model.inputMode = inputExportOutput
		model.inputLabel = "Export output directory"
		model.input = ""
		model.modal = modalInput
		return model, nil
	case inputExportPolicy:
		policy := strings.ToLower(value)
		if policy != "best-effort" && policy != "strict" {
			model.err = "HTML resource policy must be best-effort or strict"
			return model, nil
		}
		model.inputParams["htmlResourcePolicy"] = policy
		model.inputMode = inputExportArchive
		model.inputLabel = "HTML batch archive file (optional .zip; blank for per-article directories)"
		model.input = ""
		return model, nil
	case inputExportArchive:
		if value != "" && !strings.HasSuffix(strings.ToLower(value), ".zip") {
			model.err = "HTML batch archive must end with .zip"
			return model, nil
		}
		model.inputParams["htmlBatchArchive"] = value
		model.inputMode = inputExportOutput
		model.inputLabel = "Export output directory"
		model.input = ""
		return model, nil
	case inputExportOutput:
		if value == "" {
			model.err = "export output directory is required"
			return model, nil
		}
		parameters := model.inputParams
		if parameters == nil {
			parameters = map[string]string{}
		}
		parameters["outputRoot"] = value
		ids := append([]string(nil), model.exportIDs...)
		area := model.exportArea
		model.closeModal()
		return model.executeExtensionForArea(OperationExportStart, area, ids, parameters)
	case inputArticleQuery:
		query, err := parseArticleQueryInput(value, model.state.Queries.Articles.Limit)
		if err != nil {
			model.err = err.Error()
			return model, nil
		}
		model.state.Queries.Articles = query
		model.closeModal()
		model.loading = true
		return model, model.loadAreaCmd(AreaArticles)
	case inputSavedQueryName:
		if value == "" {
			model.err = "saved query name is required"
			return model, nil
		}
		model.closeModal()
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			saved, err := model.options.Application.SaveArticleQuery(ctx, value, model.state.Queries.Articles)
			return actionResultMsg{notice: "saved article query " + saved.Name, err: err}
		})
	case inputLoadQueryName:
		if value == "" {
			model.err = "saved query name is required"
			return model, nil
		}
		model.closeModal()
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			items, err := model.options.Application.ListSavedArticleQueries(ctx)
			if err != nil {
				return actionResultMsg{err: err}
			}
			for _, item := range items {
				if item.Name == value {
					return articleQueryLoadedMsg{name: item.Name, query: item.Query}
				}
			}
			return actionResultMsg{err: fmt.Errorf("saved article query %q was not found", value)}
		})
	case inputDeleteQueryName:
		if value == "" {
			model.err = "saved query name is required"
			return model, nil
		}
		model.closeModal()
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			removed, err := model.options.Application.DeleteSavedArticleQuery(ctx, value)
			if err != nil {
				return actionResultMsg{err: err}
			}
			if !removed {
				return actionResultMsg{err: fmt.Errorf("saved article query %q was not found", value)}
			}
			return actionResultMsg{notice: "deleted saved article query " + value}
		})
	case inputGCPreview:
		model.closeModal()
		return model.executeExtension(OperationGarbageCollect, nil, map[string]string{"mode": "plan"})
	case inputGCApply:
		model.closeModal()
		if value == "" {
			model.err = "garbage collection confirmation is required"
			return model, nil
		}
		return model.executeExtension(OperationGarbageCollect, nil, map[string]string{"mode": "apply", "confirm": value})
	case inputCredentialFile:
		model.closeModal()
		if value == "" {
			model.err = "credential JSON path is required"
			return model, nil
		}
		return model.executeExtension(OperationCredentialImport, nil, map[string]string{"path": value})
	case inputCredentialID:
		operation := OperationKind(model.inputParams["operation"])
		model.closeModal()
		if value == "" {
			model.err = "credential ID is required"
			return model, nil
		}
		if operation == OperationCredentialRemove {
			model.inputParams = map[string]string{"id": value}
			model.confirm = confirmation{Title: "Remove credential", Scope: "Credential metadata and secret bytes for " + value,
				Recoverability: "Import the credential again to restore access.", Phrase: "remove-credential", Action: string(operation)}
			model.modal = modalConfirm
			return model, nil
		}
		return model.executeExtension(operation, nil, map[string]string{"id": value})
	case inputProxyName:
		if value == "" {
			model.err = "proxy name is required"
			return model, nil
		}
		model.inputParams = map[string]string{"name": value}
		model.inputMode, model.inputLabel, model.input = inputProxyEndpoint, "Proxy endpoint URL", ""
		return model, nil
	case inputProxyEndpoint:
		if value == "" {
			model.err = "proxy endpoint is required"
			return model, nil
		}
		model.inputParams["endpoint"] = value
		model.inputMode, model.inputLabel, model.input, model.inputSecret = inputProxyAuth, "Proxy authorization (optional, hidden)", "", true
		return model, nil
	case inputProxyAuth:
		parameters := model.inputParams
		parameters["authorization"] = value
		model.closeModal()
		return model.executeExtension(OperationProxyAdd, nil, parameters)
	case inputProxyTarget:
		operation := OperationKind(model.inputParams["operation"])
		model.closeModal()
		if value == "" {
			model.err = "proxy name or ID is required"
			return model, nil
		}
		if operation == OperationProxyRemove {
			model.inputParams = map[string]string{"id": value}
			model.confirm = confirmation{Title: "Remove proxy route", Scope: "Route metadata and authorization for " + value,
				Recoverability: "Add the proxy route again to restore it.", Phrase: "remove-proxy", Action: string(operation)}
			model.modal = modalConfirm
			return model, nil
		}
		return model.executeExtension(operation, nil, map[string]string{"id": value})
	case inputPreferenceKey:
		if value == "" {
			model.err = "preference key is required"
			return model, nil
		}
		model.inputParams = map[string]string{"key": value}
		model.inputMode, model.inputLabel, model.input = inputPreferenceValue, "Preference value", ""
		return model, nil
	case inputPreferenceValue:
		parameters := model.inputParams
		parameters["value"] = value
		model.closeModal()
		return model.executeExtension(OperationPreferenceSet, nil, parameters)
	case inputDiagnosticBundle:
		model.closeModal()
		if value == "" {
			model.err = "diagnostic bundle output path is required"
			return model, nil
		}
		return model.executeExtension(OperationDiagnosticBundle, nil, map[string]string{"path": value})
	}
	return model, nil
}

func (model Model) movePage(direction int) bool {
	switch model.state.Area {
	case AreaAccounts:
		return moveOffset(&model.state.Queries.Accounts.Offset, model.state.Queries.Accounts.Limit, model.accounts.Total, direction)
	case AreaArticles:
		return moveOffset(&model.state.Queries.Articles.Offset, model.state.Queries.Articles.Limit, model.articles.Total, direction)
	case AreaAlbums:
		return moveOffset(&model.state.Queries.Albums.Offset, model.state.Queries.Albums.Limit, model.albums.Total, direction)
	case AreaJobs:
		return moveOffset(&model.state.Queries.Jobs.Offset, model.state.Queries.Jobs.Limit, model.jobs.Total, direction)
	case AreaExports:
		return moveOffset(&model.state.Queries.Exports.Offset, model.state.Queries.Exports.Limit, model.exports.Total, direction)
	}
	return false
}

func moveOffset(offset *int, limit, total, direction int) bool {
	if limit <= 0 {
		limit = 20
	}
	next := *offset + direction*limit
	if next < 0 {
		next = 0
	}
	if direction > 0 && next >= total {
		return false
	}
	if next == *offset {
		return false
	}
	*offset = next
	return true
}

func (model Model) openCurrent() (tea.Model, tea.Cmd) {
	if model.currentID() == "" {
		return model, nil
	}
	model.operation = model.currentDetail()
	model.modal = modalDetail
	return model, nil
}

func (model Model) startArticlePreview() (tea.Model, tea.Cmd) {
	id := domain.ArticleID(model.currentID())
	if id == "" {
		return model, nil
	}
	if model.options.Extensions == nil {
		model.err = "text/Markdown preview is unavailable in this runtime"
		return model, nil
	}
	return model, model.beginCommand(func(ctx context.Context) tea.Msg {
		preview, err := model.options.Extensions.PreviewArticle(ctx, id)
		if err != nil {
			return actionResultMsg{err: err}
		}
		return previewLoadedMsg{preview: preview}
	})
}

type previewLoadedMsg struct {
	generation uint64
	preview    PreviewDocument
	err        error
}

func (model Model) startHTMLPreview() (tea.Model, tea.Cmd) {
	id := domain.ArticleID(model.currentID())
	if id == "" {
		return model, nil
	}
	if model.options.Extensions == nil {
		model.err = "local HTML preview handoff is unavailable in this runtime"
		return model, nil
	}
	model.confirm = confirmation{
		Title: "Open local HTML preview", Scope: "1 cached article in the local browser",
		Recoverability: "No remote renderer is used. The browser opens only after confirmation.",
		Phrase:         "open-html", Action: "open_html",
	}
	model.modal = modalConfirm
	return model, nil
}

func (model Model) startLogin() (tea.Model, tea.Cmd) {
	return model, model.beginCommand(func(ctx context.Context) tea.Msg {
		flow, err := model.options.Application.BeginLogin(ctx, "")
		return loginStartedMsg{flow: flow, err: err}
	})
}

func (model Model) chooseAction(action actionItem) (tea.Model, tea.Cmd) {
	switch action.Kind {
	case string(OperationAccountImport):
		model.inputMode, model.inputLabel, model.input = inputAccountImport, "Account manifest path", ""
		model.modal = modalInput
		return model, nil
	case string(OperationAccountExport):
		model.inputMode, model.inputLabel, model.input = inputAccountExport, "Account manifest output path", ""
		model.modal = modalInput
		return model, nil
	case string(OperationRestore):
		model.inputMode, model.inputLabel, model.input = inputRestoreArchive, "Backup archive path", ""
		model.modal = modalInput
		return model, nil
	case "article_export", "album_export":
		model.inputMode, model.inputLabel, model.input = inputExportFormat, "Export format", "markdown"
		model.modal = modalInput
		return model, nil
	case "article_filter":
		model.inputMode, model.inputLabel = inputArticleQuery, articleQueryPrompt()
		model.input = formatArticleQueryInput(model.state.Queries.Articles)
		model.modal = modalInput
		return model, nil
	case "article_query_save":
		model.inputMode, model.inputLabel, model.input = inputSavedQueryName, "Saved article query name", ""
		model.modal = modalInput
		return model, nil
	case "article_query_load":
		model.inputMode, model.inputLabel, model.input = inputLoadQueryName, "Saved article query name to load", ""
		model.modal = modalInput
		return model, nil
	case "article_query_list":
		model.closeModal()
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			items, err := model.options.Application.ListSavedArticleQueries(ctx)
			operation := OperationResult{Title: "Saved article queries", Message: fmt.Sprintf("%d saved queries", len(items))}
			for _, item := range items {
				operation.Lines = append(operation.Lines, item.Name+" · "+formatArticleQueryInput(item.Query))
			}
			return actionResultMsg{operation: operation, err: err}
		})
	case "article_query_delete":
		model.inputMode, model.inputLabel, model.input = inputDeleteQueryName, "Saved article query name to delete", ""
		model.modal = modalInput
		return model, nil
	case "garbage_plan":
		model.inputMode, model.inputLabel, model.input = inputGCPreview, "Press Enter to generate a fresh garbage-collection plan", ""
		model.modal = modalInput
		return model, nil
	case "garbage_apply":
		model.inputMode, model.inputLabel, model.input = inputGCApply, "Paste exact confirmation from the latest plan", ""
		model.modal = modalInput
		return model, nil
	case string(OperationCredentialImport):
		model.inputMode, model.inputLabel, model.input = inputCredentialFile, "Credential JSON path", ""
		model.modal = modalInput
		return model, nil
	case string(OperationCredentialCheck), string(OperationCredentialRemove):
		model.inputParams = map[string]string{"operation": action.Kind}
		model.inputMode, model.inputLabel, model.input = inputCredentialID, "Credential ID", ""
		model.modal = modalInput
		return model, nil
	case string(OperationProxyAdd):
		model.inputMode, model.inputLabel, model.input = inputProxyName, "Proxy name", ""
		model.modal = modalInput
		return model, nil
	case string(OperationProxyEnable), string(OperationProxyDisable), string(OperationProxyTest), string(OperationProxyRemove):
		model.inputParams = map[string]string{"operation": action.Kind}
		model.inputMode, model.inputLabel, model.input = inputProxyTarget, "Proxy name or ID", ""
		model.modal = modalInput
		return model, nil
	case string(OperationPreferenceSet):
		model.inputMode, model.inputLabel, model.input = inputPreferenceKey, "Preference key", ""
		model.modal = modalInput
		return model, nil
	case string(OperationDiagnosticBundle):
		model.inputMode, model.inputLabel, model.input = inputDiagnosticBundle, "Diagnostic bundle output path", ""
		model.modal = modalInput
		return model, nil
	}
	if action.Destructive {
		model.confirm = model.confirmationFor(action.Kind)
		model.modal = modalConfirm
		return model, nil
	}
	model.closeModal()
	return model.executeAction(action.Kind)
}

func (model Model) executeConfirmedAction() (tea.Model, tea.Cmd) {
	action := model.confirm.Action
	confirmedIDs := append([]string(nil), model.confirm.IDs...)
	parameters := model.inputParams
	model.closeModal()
	if action == string(OperationRestore) {
		return model.executeExtension(OperationRestore, nil, parameters)
	}
	if action == string(OperationCredentialRemove) || action == string(OperationProxyRemove) {
		return model.executeExtension(OperationKind(action), nil, parameters)
	}
	return model.executeActionWithIDs(action, confirmedIDs)
}

func (model Model) executeAction(action string) (tea.Model, tea.Cmd) {
	return model.executeActionWithIDs(action, nil)
}

func (model Model) executeActionWithIDs(action string, confirmedIDs []string) (tea.Model, tea.Cmd) {
	ids := confirmedIDs
	if ids == nil {
		ids = model.selectedIDs()
	}
	switch action {
	case "login":
		return model.startLogin()
	case "logout":
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			err := model.options.Application.Logout(ctx)
			return actionResultMsg{notice: "local WeChat session removed", err: err}
		})
	case "account_sync":
		if len(ids) == 0 {
			model.err = "select an account to synchronize"
			return model, nil
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			job, err := model.options.Application.SynchronizeAccount(ctx, domain.SynchronizeAccountRequest{AccountID: domain.AccountID(ids[0]), Incremental: true})
			return actionResultMsg{job: job, err: err}
		})
	case "account_delete":
		accountIDs := make([]domain.AccountID, len(ids))
		for index, id := range ids {
			accountIDs[index] = domain.AccountID(id)
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			report, err := model.options.Application.DeleteAccounts(ctx, accountIDs)
			return actionResultMsg{notice: fmt.Sprintf("deleted %d accounts and %d articles", report.AccountsDeleted, report.ArticlesDeleted), err: err}
		})
	case "article_download":
		articleIDs := make([]domain.ArticleID, len(ids))
		for index, id := range ids {
			articleIDs[index] = domain.ArticleID(id)
		}
		if len(articleIDs) == 0 {
			model.err = "select one or more articles before starting a download"
			return model, nil
		}
		model.confirm = confirmation{
			Title: "Start article download", Scope: fmt.Sprintf("%d resolved stable article IDs", len(articleIDs)),
			Recoverability: "The persistent job can be paused, resumed, cancelled, and retried.",
			Phrase:         fmt.Sprintf("download-%d-articles", len(articleIDs)), Action: "article_download_confirmed",
		}
		model.modal = modalConfirm
		return model, nil
	case "article_download_confirmed":
		articleIDs := make([]domain.ArticleID, len(ids))
		for index, id := range ids {
			articleIDs[index] = domain.ArticleID(id)
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			job, err := model.options.Application.StartDownload(ctx, domain.DownloadRequest{ArticleIDs: articleIDs})
			return actionResultMsg{job: job, err: err}
		})
	case "article_export", "album_export":
		selection := domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs}
		for _, id := range ids {
			selection.ArticleIDs = append(selection.ArticleIDs, domain.ArticleID(id))
		}
		if action == "album_export" && len(ids) > 0 {
			selection = domain.ExportSelection{Kind: domain.ExportSelectionAlbum, AlbumID: domain.AlbumID(ids[0])}
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			job, err := model.options.Application.StartExport(ctx, domain.ExportRequest{Selection: selection, Format: "markdown"})
			return actionResultMsg{job: job, err: err}
		})
	case "export_start":
		model.err = "start exports from Articles or Albums so the selection uses stable article or album IDs"
		return model, nil
	case "album_download":
		return model.executeExtension(OperationAlbumTraverse, ids, map[string]string{"mode": "download", "order": "forward"})
	case "album_reverse":
		return model.executeExtension(OperationAlbumTraverse, ids, map[string]string{"mode": "all", "order": "reverse"})
	case "job_cancel":
		if len(ids) == 0 {
			return model, nil
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			job, err := model.options.Application.CancelJob(ctx, domain.JobID(ids[0]))
			return actionResultMsg{job: job, err: err}
		})
	case "open_html":
		if model.options.Extensions == nil || len(ids) == 0 {
			model.err = "local HTML preview handoff is unavailable"
			return model, nil
		}
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			err := model.options.Extensions.OpenHTMLPreview(ctx, domain.ArticleID(ids[0]))
			return actionResultMsg{notice: "local HTML preview opened", err: err}
		})
	default:
		return model.executeExtension(OperationKind(action), ids, nil)
	}
}

func (model Model) executeExtension(kind OperationKind, ids []string, parameters map[string]string) (tea.Model, tea.Cmd) {
	return model.executeExtensionForArea(kind, model.state.Area, ids, parameters)
}

func (model Model) executeExtensionForArea(kind OperationKind, area Area, ids []string, parameters map[string]string) (tea.Model, tea.Cmd) {
	if model.options.Extensions == nil {
		model.err = "this operation is unavailable in the current application seam"
		return model, nil
	}
	request := OperationRequest{Kind: kind, Area: area, IDs: append([]string(nil), ids...), Parameters: parameters}
	return model, model.beginCommand(func(ctx context.Context) tea.Msg {
		result, err := model.options.Extensions.Operate(ctx, request)
		return actionResultMsg{operation: result, err: err}
	})
}

func (model Model) confirmationFor(action string) confirmation {
	ids := model.selectedIDs()
	switch action {
	case "account_delete":
		articleCount := 0
		for _, account := range model.accounts.Items {
			for _, id := range ids {
				if string(account.ID) == id {
					articleCount += account.ArticleCount
				}
			}
		}
		phrase := fmt.Sprintf("delete-%d-accounts", len(ids))
		return confirmation{Title: "Delete local account data",
			Scope:          fmt.Sprintf("%d accounts, approximately %d articles, and eligible unshared objects", len(ids), articleCount),
			Recoverability: "Create a backup first. Shared objects remain; unreferenced objects become garbage-collection eligible.",
			Phrase:         phrase, Action: action, IDs: append([]string(nil), ids...)}
	case "job_cancel":
		return confirmation{Title: "Cancel persistent job", Scope: fmt.Sprintf("%d selected jobs", len(ids)),
			Recoverability: "Committed work is retained and safe checkpoints remain available.", Phrase: "cancel-job", Action: action,
			IDs: append([]string(nil), ids...)}
	case string(OperationRestore):
		return confirmation{Title: "Restore library backup", Scope: "Replace the active local profile library",
			Recoverability: "A pre-restore backup is strongly recommended.", Phrase: "restore-library", Action: string(OperationRestore)}
	case string(OperationGarbageCollect):
		return confirmation{Title: "Garbage collect objects", Scope: "Delete verified unreferenced local objects",
			Recoverability: "Deleted objects may need to be downloaded again.", Phrase: "collect-garbage", Action: string(OperationGarbageCollect)}
	default:
		return confirmation{Title: "Confirm action", Scope: action, Recoverability: "Review the scope before proceeding.",
			Phrase: "confirm", Action: action}
	}
}

func (model Model) actionsForArea() []actionItem {
	switch model.state.Area {
	case AreaHome:
		if model.session.State == wechat.SessionAuthenticated {
			return []actionItem{{Label: "Log out", Description: "Remove the local WeChat session", Kind: "logout", Destructive: true}}
		}
		return []actionItem{{Label: "QR login", Description: "Authenticate directly with WeChat", Kind: "login"}}
	case AreaAccounts:
		return []actionItem{
			{Label: "Synchronize", Description: "Start incremental account synchronization", Kind: "account_sync"},
			{Label: "Import manifest", Kind: string(OperationAccountImport)},
			{Label: "Export manifest", Kind: string(OperationAccountExport)},
			{Label: "Delete local data", Description: "Requires exact typed confirmation", Kind: "account_delete", Destructive: true},
		}
	case AreaArticles:
		return []actionItem{
			{Label: "Edit compound filter", Description: "key=value pairs for all article query fields", Kind: "article_filter"},
			{Label: "Save current query", Kind: "article_query_save"}, {Label: "Load saved query", Kind: "article_query_load"},
			{Label: "List saved queries", Kind: "article_query_list"}, {Label: "Delete saved query", Kind: "article_query_delete"},
			{Label: "Download selected", Description: "Creates one persistent job for stable article IDs", Kind: "article_download"},
			{Label: "Export selected", Kind: "article_export"},
			{Label: "Comments", Kind: string(OperationArticleComments)},
			{Label: "Metrics", Kind: string(OperationArticleMetrics)},
			{Label: "Resource completeness", Kind: string(OperationArticleResources)},
		}
	case AreaAlbums:
		return []actionItem{
			{Label: "Traverse all forward", Kind: string(OperationAlbumTraverse)},
			{Label: "Traverse all reverse", Kind: "album_reverse"},
			{Label: "Batch download", Kind: "album_download"},
			{Label: "Export album", Kind: "album_export"},
		}
	case AreaJobs:
		return []actionItem{
			{Label: "Show logs and lease", Kind: string(OperationJobLogs)},
			{Label: "Pause", Kind: string(OperationJobPause)}, {Label: "Resume", Kind: string(OperationJobResume)},
			{Label: "Retry", Kind: string(OperationJobRetry)}, {Label: "Route health", Kind: string(OperationRouteHealth)},
			{Label: "Cancel", Kind: "job_cancel", Destructive: true},
		}
	case AreaExports:
		return []actionItem{
			{Label: "Configure export", Kind: string(OperationExportConfig)},
			{Label: "Result manifest", Kind: string(OperationExportManifest)},
			{Label: "Verify result", Kind: string(OperationExportVerify)},
			{Label: "Open output", Kind: string(OperationOpenExport)},
		}
	case AreaSettings:
		return []actionItem{
			{Label: "List credentials", Kind: string(OperationCredentials)}, {Label: "Import credential", Kind: string(OperationCredentialImport)},
			{Label: "Validate credential", Kind: string(OperationCredentialCheck)}, {Label: "Remove credential", Kind: string(OperationCredentialRemove)},
			{Label: "List proxies", Kind: string(OperationProxies)}, {Label: "Add proxy", Kind: string(OperationProxyAdd)},
			{Label: "Enable proxy", Kind: string(OperationProxyEnable)}, {Label: "Disable proxy", Kind: string(OperationProxyDisable)},
			{Label: "Test proxy", Kind: string(OperationProxyTest)}, {Label: "Remove proxy", Kind: string(OperationProxyRemove)},
			{Label: "Show preferences", Kind: string(OperationPreferences)}, {Label: "Set preference", Kind: string(OperationPreferenceSet)},
		}
	case AreaStorage:
		return []actionItem{
			{Label: "Backup", Kind: string(OperationBackup)},
			{Label: "Restore", Kind: string(OperationRestore), Destructive: true},
			{Label: "Integrity check", Kind: string(OperationIntegrity)},
			{Label: "Garbage collection plan", Kind: "garbage_plan"},
			{Label: "Apply garbage collection", Kind: "garbage_apply"},
		}
	case AreaDiagnostics:
		return []actionItem{
			{Label: "Refresh diagnostics", Kind: string(OperationDiagnostics)},
			{Label: "Create diagnostic bundle", Kind: string(OperationDiagnosticBundle)},
			{Label: "Route health", Kind: string(OperationRouteHealth)},
		}
	}
	return nil
}

type articleQueryLoadedMsg struct {
	generation uint64
	name       string
	query      domain.ArticleQuery
}

func availableColumns(area Area) []string {
	if area == AreaAccounts {
		return []string{"name", "alias", "fakeid", "articles", "messages", "last_sync", "completed"}
	}
	return []string{"title", "author", "published", "account", "type", "content", "comments", "original", "paid", "albums", "metrics"}
}

func (model *Model) toggleColumn(column string) {
	values := model.state.Columns[model.columnArea]
	for index, value := range values {
		if value == column {
			if len(values) == 1 {
				model.err = "at least one column must remain visible"
				return
			}
			model.state.Columns[model.columnArea] = append(values[:index], values[index+1:]...)
			return
		}
	}
	model.state.Columns[model.columnArea] = append(values, column)
}

func (model Model) currentDetail() OperationResult {
	index := model.state.Cursors[model.state.Area]
	switch model.state.Area {
	case AreaAccounts:
		if index < len(model.accounts.Items) {
			account := model.accounts.Items[index]
			return OperationResult{Title: account.Name, Fields: map[string]string{
				"fakeid": account.FakeID, "alias": account.Alias, "description": account.Description,
				"articles": fmt.Sprint(account.ArticleCount), "messages": fmt.Sprint(account.MessageCount),
				"last sync": formatTime(account.LastSyncAt),
			}}
		}
	case AreaArticles:
		if index < len(model.articles.Items) {
			article := model.articles.Items[index]
			albums := make([]string, 0, len(article.Albums))
			for _, album := range article.Albums {
				albums = append(albums, album.Name)
			}
			return OperationResult{Title: article.Title, Fields: map[string]string{
				"author": article.Author, "published": formatTime(article.PublishedAt), "URL": article.CanonicalURL,
				"downloaded": fmt.Sprint(article.HasContent), "comments": fmt.Sprint(article.HasComments),
				"read": fmt.Sprint(article.ReadCount), "like": fmt.Sprint(article.LikeCount),
				"albums": strings.Join(albums, ", "),
			}}
		}
	case AreaAlbums:
		if index < len(model.albums.Items) {
			album := model.albums.Items[index]
			return OperationResult{Title: album.Name, Fields: map[string]string{"upstream ID": album.UpstreamID,
				"description": album.Description, "articles": fmt.Sprint(album.ArticleCount), "paid": fmt.Sprint(album.Paid)}}
		}
	case AreaJobs:
		if index < len(model.jobs.Items) {
			job := model.jobs.Items[index]
			return OperationResult{Title: string(job.ID), Fields: map[string]string{"kind": job.Kind,
				"state": string(job.State), "profile": string(job.Profile), "updated": formatTime(job.UpdatedAt)},
				Message: "Persisted progress remains observable even when another process holds the execution lease."}
		}
	case AreaExports:
		if index < len(model.exports.Items) {
			export := model.exports.Items[index]
			return OperationResult{Title: string(export.ID), Fields: map[string]string{
				"format": export.Format, "state": export.State, "output root": export.OutputRoot,
				"provenance state":      fallback(export.ProvenanceState, "pending"),
				"provenance path":       fallback(export.ProvenancePath, "not written"),
				"provenance generation": fmt.Sprint(export.ProvenanceGeneration),
				"created":               formatTime(export.CreatedAt), "completed": formatOptionalTimePointer(export.CompletedAt),
			}, Message: "Manifest, verification, and output-opening actions target this stable export ID."}
		}
	}
	return OperationResult{}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func formatOptionalTimePointer(value *time.Time) string {
	if value == nil {
		return "—"
	}
	return formatTime(*value)
}
