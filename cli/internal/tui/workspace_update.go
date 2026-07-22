package tui

import (
	"context"
	"fmt"
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
		model.finishCommand()
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.runtime, model.session, model.storage = typed.runtime, typed.session, typed.storage
		model.accounts, model.articles, model.albums, model.jobs = typed.accounts, typed.articles, typed.albums, typed.jobs
		for area, panel := range typed.panels {
			model.panels[area] = panel
		}
		model.clampCursor()
		return model, nil
	case areaLoadedMsg:
		model.loading = false
		model.finishCommand()
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
		default:
			model.panels[typed.area] = typed.panel
		}
		model.clampCursor()
		return model, nil
	case actionResultMsg:
		model.finishCommand()
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
		model.finishCommand()
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.loginFlow = typed.flow
		model.modal = modalLogin
		model.notice = "scan the QR code in WeChat, then press r to check status"
		return model, nil
	case loginPolledMsg:
		model.finishCommand()
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.loginPoll = typed.result
		if typed.result.State == wechat.QRConfirmed {
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
		model.finishCommand()
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.session = typed.session
		model.modal = modalNone
		model.notice = "WeChat session authenticated"
		return model, nil
	case previewLoadedMsg:
		model.finishCommand()
		if typed.err != nil {
			model.err = typed.err.Error()
			return model, nil
		}
		model.preview = typed.preview
		model.modal = modalPreview
		return model, nil
	case tea.KeyMsg:
		return model.updateKey(typed)
	}
	return model, nil
}

func (model Model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	value := key.String()
	if keyMatches(value, model.keys.Cancel) {
		if model.busy && model.cancel != nil {
			model.cancel()
			model.finishCommand()
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
			operation := OperationResult{Title: "Account search", Message: fmt.Sprintf("%d upstream matches", page.Total)}
			if err == nil {
				for _, account := range page.Items {
					operation.Lines = append(operation.Lines, account.Name+" · "+account.FakeID)
				}
			}
			return actionResultMsg{operation: operation, err: err}
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
	preview PreviewDocument
	err     error
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
	model.closeModal()
	return model.executeAction(action)
}

func (model Model) executeAction(action string) (tea.Model, tea.Cmd) {
	ids := model.selectedIDs()
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
		return model, model.beginCommand(func(ctx context.Context) tea.Msg {
			job, err := model.options.Application.StartDownload(ctx, domain.DownloadRequest{ArticleIDs: articleIDs})
			return actionResultMsg{job: job, err: err}
		})
	case "article_export", "album_export", "export_start":
		if action == "export_start" && len(ids) == 0 {
			return model.executeExtension(OperationExportConfig, nil, nil)
		}
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
	if model.options.Extensions == nil {
		model.err = "this operation is unavailable in the current application seam"
		return model, nil
	}
	request := OperationRequest{Kind: kind, Area: model.state.Area, IDs: ids, Parameters: parameters}
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
			Phrase:         phrase, Action: action}
	case "job_cancel":
		return confirmation{Title: "Cancel persistent job", Scope: fmt.Sprintf("%d selected jobs", len(ids)),
			Recoverability: "Committed work is retained and safe checkpoints remain available.", Phrase: "cancel-job", Action: action}
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
			{Label: "Configure export", Kind: string(OperationExportConfig)}, {Label: "Result manifest", Kind: string(OperationExportManifest)},
			{Label: "Open output", Kind: string(OperationOpenExport)},
		}
	case AreaSettings:
		return []actionItem{
			{Label: "Credentials", Kind: string(OperationCredentials)}, {Label: "Proxies", Kind: string(OperationProxies)},
			{Label: "Preferences", Kind: string(OperationPreferences)},
		}
	case AreaStorage:
		return []actionItem{
			{Label: "Backup", Kind: string(OperationBackup)},
			{Label: "Restore", Kind: string(OperationRestore), Destructive: true},
			{Label: "Integrity check", Kind: string(OperationIntegrity)},
			{Label: "Garbage collection", Kind: string(OperationGarbageCollect), Destructive: true},
		}
	case AreaDiagnostics:
		return []actionItem{{Label: "Refresh diagnostics", Kind: string(OperationDiagnostics)}, {Label: "Route health", Kind: string(OperationRouteHealth)}}
	}
	return nil
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
	}
	return OperationResult{}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("2006-01-02 15:04")
}
