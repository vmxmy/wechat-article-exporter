package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type workspaceTheme struct {
	title    lipgloss.Style
	active   lipgloss.Style
	muted    lipgloss.Style
	error    lipgloss.Style
	warning  lipgloss.Style
	selected lipgloss.Style
	border   lipgloss.Style
}

func theme(noColor bool) workspaceTheme {
	style := workspaceTheme{
		border: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
	}
	if noColor {
		return style
	}
	return workspaceTheme{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		active:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("24")),
		muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		error:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		warning:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
		selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		border:   style.border.BorderForeground(lipgloss.Color("240")),
	}
}

func (model Model) View() string {
	if model.quitting {
		return ""
	}
	view := model.renderWorkspace()
	if model.modal != modalNone {
		view += "\n" + model.renderModal()
	}
	return view
}

func (model Model) renderWorkspace() string {
	var builder strings.Builder
	style := theme(model.options.NoColor || model.layout == LayoutPlain)
	title := model.text("WeChat Article Workspace", "微信公众号文章工作台")
	builder.WriteString(model.renderComponent(style, style.title.Render(title)))
	builder.WriteString("\n")
	builder.WriteString(model.renderComponent(style, model.renderNavigation(style)))
	builder.WriteString("\n")
	if model.loading {
		builder.WriteString(model.renderComponent(style, model.symbol("…", "...")+model.text(" Loading local workspace through Application…", " 正在加载本地工作区…")))
	} else {
		content := style.title.Render(model.areaLabel(model.state.Area)) + "\n" + model.renderArea(style)
		builder.WriteString(model.renderComponent(style, content))
	}
	status := ""
	if model.err != "" {
		status += style.error.Render(model.symbol("!", "!") + " " + model.err)
	}
	if model.notice != "" {
		if status != "" {
			status += "\n"
		}
		status += style.muted.Render(model.notice)
	}
	if status != "" {
		builder.WriteString("\n" + model.renderComponent(style, status))
	}
	builder.WriteString("\n" + model.renderComponent(style, style.muted.Render(model.footerHelp())))
	return builder.String()
}

func (model Model) renderComponent(style workspaceTheme, content string) string {
	if model.layout == LayoutPlain {
		return content
	}
	width := max(20, min(model.width-4, 120))
	contentWidth := max(1, width-4)
	border := lipgloss.RoundedBorder()
	if model.options.ASCII {
		border = lipgloss.NormalBorder()
	}
	return style.border.Border(border).Width(contentWidth).Render(content)
}

func (model Model) renderNavigation(style workspaceTheme) string {
	labels := make([]string, 0, len(workspaceAreas))
	for index, area := range workspaceAreas {
		label := fmt.Sprintf("%d %s", index+1, model.areaLabel(area))
		if area == model.state.Area {
			label = style.active.Render(" " + label + " ")
		}
		labels = append(labels, label)
	}
	separator := "  "
	if model.layout == LayoutCompact {
		separator = "\n"
	}
	return strings.Join(labels, separator)
}

func (model Model) renderArea(style workspaceTheme) string {
	switch model.state.Area {
	case AreaHome:
		return model.renderHome(style)
	case AreaAccounts:
		return model.renderAccounts(style)
	case AreaArticles:
		return model.renderArticles(style)
	case AreaAlbums:
		return model.renderAlbums(style)
	case AreaJobs:
		return model.renderJobs(style)
	case AreaExports:
		return model.renderExports(style)
	case AreaSettings, AreaStorage, AreaDiagnostics:
		return model.renderPanel(style, model.state.Area)
	default:
		return ""
	}
}

func (model Model) renderHome(style workspaceTheme) string {
	sessionLabel := string(model.session.State)
	if sessionLabel == "" {
		sessionLabel = string(wechat.SessionMissing)
	}
	lines := []string{
		style.title.Render(model.text("Profile and session", "配置档案和会话")),
		model.text("Profile: ", "配置档案：") + fallback(string(model.runtime.Profile), "default"),
		model.text("Session: ", "会话：") + sessionLabel,
		model.text("Offline library: ", "离线资料库：") + yesNoLocalized(model, model.runtime.OfflineReady),
		fmt.Sprintf(model.text("Local records: %d accounts · %d articles · %d albums · %d jobs", "本地记录：%d 个公众号 · %d 篇文章 · %d 个专辑 · %d 个任务"),
			model.storage.Accounts, model.storage.Articles, model.storage.Albums, model.storage.Jobs),
	}
	if model.session.State != wechat.SessionAuthenticated {
		lines = append(lines, "", style.warning.Render(model.text("Online discovery and sync require QR login.", "在线搜索和同步需要扫码登录。")),
			model.text("Press l to log in. Accounts, articles, albums, jobs, exports, storage, and diagnostics remain available offline.", "按 l 登录。公众号、文章、专辑、任务、导出、存储和诊断仍可离线使用。"))
	} else {
		lines = append(lines, model.text("Account: ", "账号：")+fallback(model.session.AccountName, string(model.session.AccountID)),
			model.text("Session expiry: ", "会话过期时间：")+formatTime(model.session.ExpiresAt))
	}
	return strings.Join(lines, "\n")
}

func (model Model) renderAccounts(style workspaceTheme) string {
	columns := model.visibleColumns(AreaAccounts)
	rows := make([][]string, 0, len(model.accounts.Items))
	for _, account := range model.accounts.Items {
		values := map[string]string{
			"name": account.Name, "alias": account.Alias, "fakeid": account.FakeID,
			"articles": fmt.Sprint(account.ArticleCount), "messages": fmt.Sprint(account.MessageCount),
			"last_sync": formatTime(account.LastSyncAt), "completed": yesNo(account.SyncCompleted),
		}
		rows = append(rows, rowValues(columns, values))
	}
	return model.renderTable(style, model.formatColumns(columns), rows, model.accounts.Offset, model.accounts.Limit, model.accounts.Total,
		model.text("d discover · / local filter · space select · a actions", "d 搜索公众号 · / 本地筛选 · 空格选择 · a 操作"))
}

func (model Model) renderArticles(style workspaceTheme) string {
	columns := model.visibleColumns(AreaArticles)
	rows := make([][]string, 0, len(model.articles.Items))
	for _, article := range model.articles.Items {
		albums := make([]string, 0, len(article.Albums))
		for _, album := range article.Albums {
			albums = append(albums, album.Name)
		}
		values := map[string]string{
			"title": article.Title, "author": article.Author, "published": formatTime(article.PublishedAt),
			"account": string(article.AccountID), "type": fmt.Sprint(article.MessageType),
			"content": yesNoLocalized(model, article.HasContent), "comments": yesNoLocalized(model, article.HasComments),
			"original": yesNoLocalized(model, article.Original), "paid": yesNoLocalized(model, article.Paid), "albums": strings.Join(albums, ", "),
			"metrics": fmt.Sprintf("R%d L%d C%d", article.ReadCount, article.LikeCount, article.CommentCount),
		}
		rows = append(rows, rowValues(columns, values))
	}
	return model.renderTable(style, model.formatColumns(columns), rows, model.articles.Offset, model.articles.Limit, model.articles.Total,
		model.text("n single URL · / compound filter · c columns · p safe preview · H local HTML · space multi-select · a bulk actions", "n 单篇 URL · / 组合筛选 · c 列 · p 安全预览 · H 本地 HTML · 空格多选 · a 批量操作"))
}

func (model Model) renderAlbums(style workspaceTheme) string {
	columns := []string{"name", "articles", "paid"}
	rows := make([][]string, 0, len(model.albums.Items))
	for _, album := range model.albums.Items {
		rows = append(rows, []string{album.Name, fmt.Sprint(album.ArticleCount), yesNoLocalized(model, album.Paid)})
	}
	return model.renderTable(style, model.formatColumns(columns), rows, model.albums.Offset, model.albums.Limit, model.albums.Total,
		model.text("/ filter · space select · a order/traverse/download/export", "/ 筛选 · 空格选择 · a 排序/遍历/下载/导出"))
}

func (model Model) renderJobs(style workspaceTheme) string {
	columns := []string{"kind", "state", "progress", "throughput", "updated"}
	rows := make([][]string, 0, len(model.jobs.Items))
	for _, job := range model.jobs.Items {
		completed := job.Counts[string(domain.JobCompleted)]
		end := model.options.Now()
		switch job.State {
		case domain.JobCompleted, domain.JobPartial, domain.JobFailed, domain.JobCancelled:
			end = job.UpdatedAt
		}
		elapsed := end.Sub(job.CreatedAt)
		throughput := "-"
		if completed > 0 && elapsed > 0 {
			throughput = fmt.Sprintf("%.1f/min", float64(completed)/elapsed.Minutes())
		}
		rows = append(rows, []string{job.Kind, string(job.State), formatCounts(job.Counts), throughput, formatTime(job.UpdatedAt)})
	}
	return model.renderTable(style, model.formatColumns(columns), rows, model.jobs.Offset, model.jobs.Limit, model.jobs.Total,
		model.text("enter detail/lease · a logs/route health/pause/resume/cancel/retry · r refresh", "Enter 详情/租约 · a 日志/路由/暂停/继续/取消/重试 · r 刷新"))
}

func (model Model) renderExports(style workspaceTheme) string {
	columns := []string{"id", "format", "state", "provenance", "generation", "output"}
	rows := make([][]string, 0, len(model.exports.Items))
	for _, item := range model.exports.Items {
		rows = append(rows, []string{
			sanitizeTableCell(string(item.ID)), sanitizeTableCell(item.Format), sanitizeTableCell(item.State),
			sanitizeTableCell(fallback(item.ProvenanceState, "pending")),
			fmt.Sprint(item.ProvenanceGeneration), sanitizeTableCell(item.OutputRoot),
		})
	}
	return model.renderTable(style, model.formatColumns(columns), rows, model.exports.Offset, model.exports.Limit, model.exports.Total,
		model.text("space select exact export ID · enter detail · a start/manifest/verify/open · r refresh", "空格选择导出 ID · Enter 详情 · a 配置/清单/验证/打开 · r 刷新"))
}

func (model Model) renderPanel(style workspaceTheme, area Area) string {
	panel, ok := model.panels[area]
	if !ok || panel.Title == "" && panel.Message == "" && len(panel.Lines) == 0 {
		panel = fallbackPanel(area)
	}
	return renderOperation(style, panel) + "\n\n" + style.muted.Render(model.text("Press a for available workflows.", "按 a 查看可用操作。"))
}

func fallbackPanel(area Area) OperationResult {
	switch area {
	case AreaExports:
		return OperationResult{Title: "Exports", Message: "Configure export format and selection, then follow persistent progress and result manifests.",
			Lines: []string{"Selected IDs, albums, saved queries, and all-matching queries use the shared Application export job seam."}}
	case AreaSettings:
		return OperationResult{Title: "Settings", Message: "Credentials, proxies, and preferences are managed through secure shared application operations.",
			Lines: []string{"Secret bytes are never rendered here.", "Saved and effective values should be shown separately by the runtime adapter."}}
	case AreaStorage:
		return OperationResult{Title: "Storage", Message: "Local database and content-addressed object status.", Lines: []string{
			"Database available: yes", "Object store ready: yes", "Backup, restore, integrity, and garbage collection are available from Actions.",
		}}
	case AreaDiagnostics:
		return OperationResult{Title: "Diagnostics", Message: "Runtime, session, route, migration, browser, and recent-job diagnostics without secrets.",
			Lines: []string{"Version: shared Application runtime", "Use Actions to refresh route health and full diagnostics."}}
	}
	return OperationResult{}
}

func (model Model) renderTable(style workspaceTheme, columns []string, rows [][]string, offset, limit, total int, help string) string {
	if len(rows) == 0 {
		return style.muted.Render(model.text("No local results. ", "没有本地结果。") + help)
	}
	if model.layout == LayoutCompact {
		var builder strings.Builder
		for index, row := range rows {
			cursor := "  "
			if index == model.state.Cursors[model.state.Area] {
				cursor = model.symbol("› ", "> ")
			}
			selected := ""
			if model.state.Selection.Has(model.state.Area, model.idAt(index)) {
				selected = model.symbol("✓ ", "[x] ")
			}
			builder.WriteString(cursor + selected + compactRow(columns, row, max(20, model.width-6)) + "\n")
		}
		builder.WriteString(style.muted.Render(model.pageDescription(offset, limit, total) + " · " + help))
		return builder.String()
	}
	widths := tableWidths(columns, rows, max(40, model.width-8))
	var builder strings.Builder
	builder.WriteString("   " + joinCells(columns, widths) + "\n")
	for index, row := range rows {
		cursor := "  "
		if index == model.state.Cursors[model.state.Area] {
			cursor = model.symbol("› ", "> ")
		}
		selected := " "
		if model.state.Selection.Has(model.state.Area, model.idAt(index)) {
			selected = model.symbol("✓", "x")
		}
		line := cursor + selected + " " + joinCells(row, widths)
		if index == model.state.Cursors[model.state.Area] {
			line = style.selected.Render(line)
		}
		builder.WriteString(line + "\n")
	}
	builder.WriteString(style.muted.Render(model.pageDescription(offset, limit, total) + " · " + help))
	return builder.String()
}

func (model Model) idAt(index int) string {
	switch model.state.Area {
	case AreaAccounts:
		if index < len(model.accounts.Items) {
			return string(model.accounts.Items[index].ID)
		}
	case AreaArticles:
		if index < len(model.articles.Items) {
			return string(model.articles.Items[index].ID)
		}
	case AreaAlbums:
		if index < len(model.albums.Items) {
			return string(model.albums.Items[index].ID)
		}
	case AreaJobs:
		if index < len(model.jobs.Items) {
			return string(model.jobs.Items[index].ID)
		}
	case AreaExports:
		if index < len(model.exports.Items) {
			return string(model.exports.Items[index].ID)
		}
	}
	return ""
}

func (model Model) visibleColumns(area Area) []string {
	columns := append([]string(nil), model.state.Columns[area]...)
	if model.layout == LayoutCompact {
		limit := 2
		if area == AreaArticles {
			limit = 3
		}
		if len(columns) > limit {
			columns = columns[:limit]
		}
	}
	return columns
}

func rowValues(columns []string, values map[string]string) []string {
	result := make([]string, len(columns))
	for index, column := range columns {
		result[index] = values[column]
	}
	return result
}

func tableWidths(columns []string, rows [][]string, available int) []int {
	widths := make([]int, len(columns))
	for index, column := range columns {
		widths[index] = max(4, lipgloss.Width(column))
	}
	for _, row := range rows {
		for index, value := range row {
			if index < len(widths) {
				widths[index] = max(widths[index], min(32, lipgloss.Width(value)))
			}
		}
	}
	for sumWidths(widths)+max(0, len(widths)-1)*3 > available {
		largest := -1
		for index, width := range widths {
			if width > 8 && (largest < 0 || width > widths[largest]) {
				largest = index
			}
		}
		if largest < 0 {
			break
		}
		widths[largest]--
	}
	return widths
}

func sumWidths(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func joinCells(values []string, widths []int) string {
	cells := make([]string, len(widths))
	for index, width := range widths {
		value := ""
		if index < len(values) {
			value = values[index]
		}
		value = truncateDisplayWidth(value, width)
		cells[index] = value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
	}
	return strings.Join(cells, " │ ")
}

func compactRow(columns, row []string, width int) string {
	parts := make([]string, 0, len(columns))
	for index, column := range columns {
		if index < len(row) && strings.TrimSpace(row[index]) != "" {
			parts = append(parts, column+": "+row[index])
		}
	}
	return truncateRunes(strings.Join(parts, " · "), width)
}

func truncateRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func truncateDisplayWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var builder strings.Builder
	for _, character := range value {
		if lipgloss.Width(builder.String()+string(character)+"…") > width {
			break
		}
		builder.WriteRune(character)
	}
	return builder.String() + "…"
}

func (model Model) renderModal() string {
	style := theme(model.options.NoColor || model.layout == LayoutPlain)
	content := ""
	switch model.modal {
	case modalHelp:
		content = strings.Join([]string{
			model.text("Keyboard help", "快捷键帮助"), model.text("Tab/Shift-Tab or left/right: navigate", "Tab/Shift-Tab 或左右键：切换区域"), model.text("j/k or arrows: move", "j/k 或上下键：移动"), model.text("Space: select", "空格：选择"),
			model.text("/: filter", "/：筛选"), model.text("c: columns", "c：列"), model.text("a: actions", "a：操作"), model.text("Enter: details", "Enter：详情"), model.text("p: safe preview", "p：安全预览"), model.text("H: local HTML handoff", "H：本地 HTML 预览"),
			model.text("[/]: page", "[/]：翻页"), model.text("r: refresh", "r：刷新"), model.text("Esc: close", "Esc：关闭"), model.text("Ctrl-C: cancel operation", "Ctrl-C：取消操作"), model.text("q: quit", "q：退出"),
		}, "\n")
	case modalInput:
		value := model.input
		if model.inputSecret {
			value = strings.Repeat("•", utf8.RuneCountInString(value))
			if model.options.ASCII || model.layout == LayoutPlain {
				value = strings.Repeat("*", utf8.RuneCountInString(model.input))
			}
		}
		content = model.localizedInputLabel(model.inputMode, model.inputLabel) + "\n\n> " + value + model.symbol("▌", "_") + "\n\n" + model.text("Enter submit · Esc cancel", "Enter 提交 · Esc 取消")
	case modalConfirm:
		content = strings.Join([]string{
			style.warning.Render(model.localizedConfirmationTitle()), model.text("Scope: ", "范围：") + model.localizedConfirmationScope(),
			model.text("Recoverability: ", "可恢复性：") + model.localizedConfirmationRecoverability(),
			"", model.text("Type exactly: ", "请输入：") + model.confirm.Phrase, "> " + model.confirm.Input + model.symbol("▌", "_"),
		}, "\n")
		if model.confirm.Error != "" {
			content += "\n" + style.error.Render(model.confirm.Error)
		}
	case modalActions:
		var builder strings.Builder
		builder.WriteString(model.text("Actions", "操作") + "\n\n")
		for index, action := range model.actions {
			cursor := "  "
			if index == model.modalCursor {
				cursor = model.symbol("› ", "> ")
			}
			label := action.Label
			if action.Destructive {
				label += " " + model.symbol("⚠", "!")
			}
			builder.WriteString(cursor + label + "\n")
			if action.Description != "" {
				builder.WriteString("    " + action.Description + "\n")
			}
		}
		content = builder.String()
	case modalColumns:
		var builder strings.Builder
		builder.WriteString(model.text("Visible columns", "显示列") + "\n\n")
		for index, column := range availableColumns(model.columnArea) {
			cursor := "  "
			if index == model.modalCursor {
				cursor = model.symbol("› ", "> ")
			}
			checked := "[ ]"
			if contains(model.state.Columns[model.columnArea], column) {
				checked = "[x]"
			}
			builder.WriteString(fmt.Sprintf("%s%s %s\n", cursor, checked, model.formatColumns([]string{column})[0]))
		}
		content = builder.String()
	case modalDetail, modalOperation:
		content = renderOperation(style, model.operation)
	case modalPreview:
		content = model.preview.Title + " · " + fallback(model.preview.Format, "text") + "\n\n" +
			truncateLines(sanitizePreview(model.preview.Text), max(8, model.height-10)) +
			"\n\nSafe text/Markdown preview; article scripts are never executed in the terminal."
	case modalLogin:
		content = "WeChat QR login\n\n"
		maximumQRWidth := max(12, min(model.width-8, 86))
		if text, err := wechat.RenderQRImageText(model.loginFlow.QRBytes); err == nil && !model.options.ASCII &&
			maxLineWidth(text) <= maximumQRWidth {
			content += text + "\n"
		} else {
			content += "QR image loaded in memory. Use an UTF-8 terminal with more width or the non-interactive --qr-output flow.\n"
		}
		content += "Expires: " + formatTime(model.loginFlow.ExpiresAt) + "\nPress r to check status · Esc cancel"
	}
	if model.layout == LayoutPlain || model.options.NoColor {
		return "---\n" + content + "\n---"
	}
	return style.border.Width(max(20, min(model.width-4, 90))).Render(content)
}

func maxLineWidth(value string) int {
	maximum := 0
	for _, line := range strings.Split(value, "\n") {
		maximum = max(maximum, utf8.RuneCountInString(line))
	}
	return maximum
}

func renderOperation(style workspaceTheme, operation OperationResult) string {
	var builder strings.Builder
	if operation.Title != "" {
		builder.WriteString(style.title.Render(operation.Title) + "\n")
	}
	if operation.Message != "" {
		builder.WriteString(operation.Message + "\n")
	}
	if len(operation.Fields) > 0 {
		keys := make([]string, 0, len(operation.Fields))
		for key := range operation.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteString(key + ": " + operation.Fields[key] + "\n")
		}
	}
	for _, line := range operation.Lines {
		builder.WriteString("- " + line + "\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func sanitizePreview(value string) string {
	value = strings.ReplaceAll(value, "\x1b", "")
	var builder strings.Builder
	for _, character := range value {
		if character == '\n' || character == '\t' || character >= 0x20 {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func sanitizeTableCell(value string) string {
	value = strings.ReplaceAll(value, "\x1b", "")
	var builder strings.Builder
	for _, character := range value {
		if !unicode.IsControl(character) && !unicode.Is(unicode.Cf, character) {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func truncateLines(value string, maximum int) string {
	lines := strings.Split(value, "\n")
	if len(lines) <= maximum {
		return value
	}
	return strings.Join(lines[:maximum], "\n") + "\n…"
}

func (model Model) footerHelp() string {
	mode := string(model.layout)
	appearance := "color/unicode"
	if model.options.NoColor {
		appearance = "no-color"
	}
	if model.options.ASCII {
		appearance += "/ASCII"
	}
	return fmt.Sprintf(model.text("Tab navigate · j/k move · / filter · space select · a actions · ? help · q quit  [%s · %s]", "Tab 切换 · j/k 移动 · / 筛选 · 空格选择 · a 操作 · ? 帮助 · q 退出  [%s · %s]"), mode, appearance)
}

func (model Model) symbol(unicodeValue, asciiValue string) string {
	if model.options.ASCII {
		return asciiValue
	}
	return unicodeValue
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func yesNoLocalized(model Model, value bool) string {
	if model.zh() {
		if value {
			return "是"
		}
		return "否"
	}
	return yesNo(value)
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(values, " ")
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
