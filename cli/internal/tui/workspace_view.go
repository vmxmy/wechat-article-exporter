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
	if noColor {
		return workspaceTheme{}
	}
	return workspaceTheme{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		active:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Background(lipgloss.Color("24")),
		muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		error:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		warning:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
		selected: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")),
		border:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
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
	title := "WeChat Article Workspace"
	if model.options.ASCII {
		title = "WeChat Article Workspace"
	}
	builder.WriteString(style.title.Render(title))
	builder.WriteString("\n")
	builder.WriteString(model.renderNavigation(style))
	builder.WriteString("\n")
	if model.loading {
		builder.WriteString(model.symbol("…", "...") + " Loading local workspace through Application…\n")
	} else {
		builder.WriteString(model.renderArea(style))
	}
	if model.err != "" {
		builder.WriteString("\n" + style.error.Render(model.symbol("!", "!")+" "+model.err) + "\n")
	}
	if model.notice != "" {
		builder.WriteString("\n" + style.muted.Render(model.notice) + "\n")
	}
	builder.WriteString("\n" + style.muted.Render(model.footerHelp()))
	return builder.String()
}

func (model Model) renderNavigation(style workspaceTheme) string {
	labels := make([]string, 0, len(workspaceAreas))
	for index, area := range workspaceAreas {
		label := fmt.Sprintf("%d %s", index+1, areaLabel(area))
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

func areaLabel(area Area) string {
	switch area {
	case AreaHome:
		return "Home"
	case AreaAccounts:
		return "Accounts"
	case AreaArticles:
		return "Articles"
	case AreaAlbums:
		return "Albums"
	case AreaJobs:
		return "Jobs"
	case AreaExports:
		return "Exports"
	case AreaSettings:
		return "Settings"
	case AreaStorage:
		return "Storage"
	case AreaDiagnostics:
		return "Diagnostics"
	default:
		return string(area)
	}
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
		style.title.Render("Profile and session"),
		"Profile: " + fallback(string(model.runtime.Profile), "default"),
		"Session: " + sessionLabel,
		"Offline library: " + yesNo(model.runtime.OfflineReady),
		fmt.Sprintf("Local records: %d accounts · %d articles · %d albums · %d jobs",
			model.storage.Accounts, model.storage.Articles, model.storage.Albums, model.storage.Jobs),
	}
	if model.session.State != wechat.SessionAuthenticated {
		lines = append(lines, "", style.warning.Render("Online discovery and sync require QR login."),
			"Press l to log in. Accounts, articles, albums, jobs, exports, storage, and diagnostics remain available offline.")
	} else {
		lines = append(lines, "Account: "+fallback(model.session.AccountName, string(model.session.AccountID)),
			"Session expiry: "+formatTime(model.session.ExpiresAt))
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
	return model.renderTable(style, columns, rows, model.accounts.Offset, model.accounts.Limit, model.accounts.Total,
		"d discover · / local filter · space select · a actions")
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
			"content": yesNo(article.HasContent), "comments": yesNo(article.HasComments),
			"original": yesNo(article.Original), "paid": yesNo(article.Paid), "albums": strings.Join(albums, ", "),
			"metrics": fmt.Sprintf("R%d L%d C%d", article.ReadCount, article.LikeCount, article.CommentCount),
		}
		rows = append(rows, rowValues(columns, values))
	}
	return model.renderTable(style, columns, rows, model.articles.Offset, model.articles.Limit, model.articles.Total,
		"n single URL · / compound filter · c columns · p safe preview · H local HTML · space multi-select · a bulk actions")
}

func (model Model) renderAlbums(style workspaceTheme) string {
	columns := []string{"name", "articles", "paid"}
	rows := make([][]string, 0, len(model.albums.Items))
	for _, album := range model.albums.Items {
		rows = append(rows, []string{album.Name, fmt.Sprint(album.ArticleCount), yesNo(album.Paid)})
	}
	return model.renderTable(style, columns, rows, model.albums.Offset, model.albums.Limit, model.albums.Total,
		"/ filter · space select · a order/traverse/download/export")
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
	return model.renderTable(style, columns, rows, model.jobs.Offset, model.jobs.Limit, model.jobs.Total,
		"enter detail/lease · a logs/route health/pause/resume/cancel/retry · r refresh")
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
	return model.renderTable(style, columns, rows, model.exports.Offset, model.exports.Limit, model.exports.Total,
		"space select exact export ID · enter detail · a start/manifest/verify/open · r refresh")
}

func (model Model) renderPanel(style workspaceTheme, area Area) string {
	panel, ok := model.panels[area]
	if !ok || panel.Title == "" && panel.Message == "" && len(panel.Lines) == 0 {
		panel = fallbackPanel(area)
	}
	return renderOperation(style, panel) + "\n\n" + style.muted.Render("Press a for available workflows.")
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
		return style.muted.Render("No local results. " + help)
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
		widths[index] = max(4, utf8.RuneCountInString(column))
	}
	for _, row := range rows {
		for index, value := range row {
			if index < len(widths) {
				widths[index] = max(widths[index], min(32, utf8.RuneCountInString(value)))
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
		value = truncateRunes(value, width)
		cells[index] = value + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(value)))
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

func (model Model) renderModal() string {
	style := theme(model.options.NoColor || model.layout == LayoutPlain)
	content := ""
	switch model.modal {
	case modalHelp:
		content = strings.Join([]string{
			"Keyboard help", "Tab/Shift-Tab or left/right: navigate", "j/k or arrows: move", "Space: select",
			"/: filter", "c: columns", "a: actions", "Enter: details", "p: safe preview", "H: local HTML handoff",
			"[/]: page", "r: refresh", "Esc: close", "Ctrl-C: cancel operation", "q: quit",
		}, "\n")
	case modalInput:
		value := model.input
		if model.inputSecret {
			value = strings.Repeat("•", utf8.RuneCountInString(value))
			if model.options.ASCII || model.layout == LayoutPlain {
				value = strings.Repeat("*", utf8.RuneCountInString(model.input))
			}
		}
		content = model.inputLabel + "\n\n> " + value + model.symbol("▌", "_") + "\n\nEnter submit · Esc cancel"
	case modalConfirm:
		content = strings.Join([]string{
			style.warning.Render(model.confirm.Title), "Scope: " + model.confirm.Scope,
			"Recoverability: " + model.confirm.Recoverability,
			"", "Type exactly: " + model.confirm.Phrase, "> " + model.confirm.Input + model.symbol("▌", "_"),
		}, "\n")
		if model.confirm.Error != "" {
			content += "\n" + style.error.Render(model.confirm.Error)
		}
	case modalActions:
		var builder strings.Builder
		builder.WriteString("Actions\n\n")
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
		builder.WriteString("Visible columns\n\n")
		for index, column := range availableColumns(model.columnArea) {
			cursor := "  "
			if index == model.modalCursor {
				cursor = model.symbol("› ", "> ")
			}
			checked := "[ ]"
			if contains(model.state.Columns[model.columnArea], column) {
				checked = "[x]"
			}
			builder.WriteString(fmt.Sprintf("%s%s %s\n", cursor, checked, column))
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
		if text, err := wechat.RenderQRImageText(model.loginFlow.QRBytes); err == nil && !model.options.ASCII {
			content += text + "\n"
		} else {
			content += "QR image loaded in memory. Use an UTF-8 terminal or the non-interactive --qr-output flow if it cannot render.\n"
		}
		content += "Expires: " + formatTime(model.loginFlow.ExpiresAt) + "\nPress r to check status · Esc cancel"
	}
	if model.layout == LayoutPlain || model.options.NoColor {
		return "---\n" + content + "\n---"
	}
	return style.border.Width(max(20, min(model.width-4, 90))).Render(content)
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
	return fmt.Sprintf("Tab navigate · j/k move · / filter · space select · a actions · ? help · q quit  [%s · %s]", mode, appearance)
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
