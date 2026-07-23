package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestWorkspaceInitialLoadAndMajorNavigationUseApplicationSeam(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := NewWorkspace(WorkspaceOptions{Context: context.Background(), Application: app, Extensions: extensions,
		NoColor: true, ASCII: true, Width: 100, Height: 30})
	loaded := runCommand(t, model.Init()).(workspaceLoadedMsg)
	updated := updateWorkspace(t, model, loaded)
	if updated.runtime.Profile != "profile-a" || updated.accounts.Total != 2 || updated.articles.Total != 2 ||
		updated.albums.Total != 1 || updated.jobs.Total != 1 {
		t.Fatalf("loaded model = %#v", updated)
	}
	if !reflect.DeepEqual(app.calls[:7], []string{
		"RuntimeStatus", "SessionStatus", "QueryAccounts", "QueryArticles", "QueryAlbums", "QueryJobs", "StorageStatus",
	}) {
		t.Fatalf("application calls = %#v", app.calls)
	}
	updated = updateWorkspace(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	if updated.CurrentArea() != AreaAccounts {
		t.Fatalf("area = %s", updated.CurrentArea())
	}
	updated = updateWorkspace(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	if updated.CurrentArea() != AreaArticles {
		t.Fatalf("area = %s", updated.CurrentArea())
	}
	view := updated.View()
	for _, text := range []string{"Articles", "First article", "single URL", "bulk actions"} {
		if !strings.Contains(view, text) {
			t.Fatalf("view does not contain %q:\n%s", text, view)
		}
	}
}

func TestWorkspaceRendersBorderedComponentsAndKeepsPlainLayoutUnboxed(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.options.NoColor = true
	model.options.ASCII = true
	model.width = 100
	model.updateLayout()
	view := model.View()
	for _, fragment := range []string{"┌", "│ WeChat Article Workspace", "│ Home"} {
		if !strings.Contains(view, fragment) {
			t.Fatalf("bordered view does not contain %q:\n%s", fragment, view)
		}
	}
	model.layout = LayoutPlain
	plain := model.View()
	if strings.Contains(plain, "+") || strings.Contains(plain, "| WeChat Article Workspace") {
		t.Fatalf("plain layout unexpectedly contains component borders:\n%s", plain)
	}
}

func TestWorkspaceArticleSelectionStartsOneDownloadJobForStableIDs(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaArticles
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeySpace})
	model = updateWorkspace(t, model, keyRune("j"))
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeySpace})
	model = updateWorkspace(t, model, keyRune("a"))
	if model.modal != modalActions {
		t.Fatalf("modal = %q", model.modal)
	}
	for model.actions[model.modalCursor].Kind != "article_download" {
		model = updateWorkspace(t, model, keyRune("j"))
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if command != nil || model.modal != modalConfirm || model.confirm.Phrase != "download-2-articles" ||
		!strings.Contains(model.confirm.Scope, "2 resolved") {
		t.Fatalf("download confirmation=%#v command=%v", model.confirm, command)
	}
	model.confirm.Input = model.confirm.Phrase
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	result := runCommand(t, command).(actionResultMsg)
	model = updateWorkspace(t, model, result)
	if len(app.downloads) != 1 || !reflect.DeepEqual(app.downloads[0].ArticleIDs,
		[]domain.ArticleID{"article-a", "article-b"}) {
		t.Fatalf("downloads = %#v", app.downloads)
	}
	if !strings.Contains(model.notice, "job-download") {
		t.Fatalf("notice = %q", model.notice)
	}
}

func TestWorkspaceDestructiveConfirmationRequiresExactPhraseAndCanCancel(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaAccounts
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeySpace})
	model = updateWorkspace(t, model, keyRune("a"))
	for model.modalCursor < len(model.actions)-1 {
		model = updateWorkspace(t, model, keyRune("j"))
	}
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.modal != modalConfirm || model.confirm.Phrase != "delete-1-accounts" ||
		!strings.Contains(model.confirm.Scope, "12 articles") || !strings.Contains(model.confirm.Recoverability, "backup") {
		t.Fatalf("confirmation = %#v", model.confirm)
	}
	model.confirm.Input = "y"
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.confirm.Error == "" || len(app.deleted) != 0 {
		t.Fatalf("confirmation=%#v deleted=%#v", model.confirm, app.deleted)
	}
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.modal != modalNone || len(app.deleted) != 0 {
		t.Fatalf("cancelled modal=%q deleted=%#v", model.modal, app.deleted)
	}
	model = updateWorkspace(t, model, keyRune("a"))
	for model.modalCursor < len(model.actions)-1 {
		model = updateWorkspace(t, model, keyRune("j"))
	}
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model.confirm.Input = model.confirm.Phrase
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if !reflect.DeepEqual(app.deleted, [][]domain.AccountID{{"account-a"}}) {
		t.Fatalf("deleted = %#v", app.deleted)
	}
}

func TestWorkspaceResizeSwitchesCompactWithoutHidingNavigationOrConfirmation(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaAccounts
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeySpace})
	model = updateWorkspace(t, model, keyRune("a"))
	for model.modalCursor < len(model.actions)-1 {
		model = updateWorkspace(t, model, keyRune("j"))
	}
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateWorkspace(t, model, tea.WindowSizeMsg{Width: 48, Height: 16})
	if model.Layout() != LayoutCompact {
		t.Fatalf("layout = %s", model.Layout())
	}
	view := model.View()
	for _, text := range []string{"Home", "Accounts", "Diagnostics", "delete-1-accounts", "Scope:"} {
		if !strings.Contains(view, text) {
			t.Fatalf("narrow view does not contain %q:\n%s", text, view)
		}
	}
	model = updateWorkspace(t, model, tea.WindowSizeMsg{Width: 120, Height: 36})
	if model.Layout() != LayoutWide {
		t.Fatalf("resized layout = %s", model.Layout())
	}
}

func TestWorkspaceCancellationCancelsInFlightCommandWithoutQuitting(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	ctx, cancel := context.WithCancel(context.Background())
	model.busy = true
	model.cancel = cancel
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if ctx.Err() != context.Canceled || model.busy || model.Quitting() || model.notice != "operation cancelled" {
		t.Fatalf("ctx=%v busy=%v quitting=%v notice=%q", ctx.Err(), model.busy, model.Quitting(), model.notice)
	}
}

func TestWorkspaceLateCancelledCommandResultDoesNotAffectReplacementCommand(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.notice = "ready"

	commandA := model.beginCommand(func(context.Context) tea.Msg {
		return actionResultMsg{notice: "late command A"}
	})
	lateA := runCommand(t, commandA)
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})

	commandB := model.beginCommand(func(context.Context) tea.Msg {
		return actionResultMsg{notice: "command B"}
	})
	if commandB == nil || !model.busy {
		t.Fatalf("replacement command not active: command=%v busy=%v", commandB, model.busy)
	}
	replacementGeneration := model.commandGeneration

	model = updateWorkspace(t, model, lateA)
	if !model.busy || model.commandGeneration != replacementGeneration || model.notice != "operation cancelled" {
		t.Fatalf("late A affected B: busy=%v generation=%d notice=%q", model.busy, model.commandGeneration, model.notice)
	}
}

func TestWorkspaceReplacementCommandCancelsPreviousContext(t *testing.T) {
	model := loadedWorkspace(t, newFakeWorkspaceApplication(), nil)
	previous := make(chan context.Context, 1)
	first := model.beginCommand(func(ctx context.Context) tea.Msg {
		previous <- ctx
		return actionResultMsg{}
	})
	_ = runCommand(t, first)
	previousContext := <-previous
	_ = model.beginCommand(func(context.Context) tea.Msg { return actionResultMsg{} })
	if previousContext.Err() != context.Canceled {
		t.Fatalf("previous command context error=%v", previousContext.Err())
	}
}

func TestWorkspaceSafePreviewAndExplicitHTMLHandoff(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{preview: PreviewDocument{
		Title: "Cached article", Format: "markdown", Text: "hello\x1b[31m<script>alert(1)</script>",
	}}
	model := loadedWorkspace(t, app, extensions)
	model.state.Area = AreaArticles
	next, command := model.Update(keyRune("p"))
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if model.modal != modalPreview || strings.Contains(model.View(), "\x1b") || !strings.Contains(model.View(), "scripts are never executed") {
		t.Fatalf("preview view:\n%s", model.View())
	}
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	model = updateWorkspace(t, model, keyRune("H"))
	if model.modal != modalConfirm || model.confirm.Phrase != "open-html" || extensions.opened != 0 {
		t.Fatalf("confirm=%#v opened=%d", model.confirm, extensions.opened)
	}
	model.confirm.Input = "open-html"
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if extensions.opened != 1 {
		t.Fatalf("opened = %d", extensions.opened)
	}
}

func TestWorkspaceOnboardingQRAndOfflineEntryPoints(t *testing.T) {
	app := newFakeWorkspaceApplication()
	app.session = wechat.Session{State: wechat.SessionMissing}
	model := loadedWorkspace(t, app, nil)
	view := model.View()
	for _, text := range []string{"Online discovery and sync require QR login", "remain available offline", "Press l to log in"} {
		if !strings.Contains(view, text) {
			t.Fatalf("onboarding view does not contain %q:\n%s", text, view)
		}
	}
	next, command := model.Update(keyRune("l"))
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if model.modal != modalLogin || len(app.loginSessions) != 1 {
		t.Fatalf("modal=%q sessions=%#v", model.modal, app.loginSessions)
	}
	next, command = model.Update(keyRune("r"))
	model = next.(Model)
	next, command = model.Update(runCommand(t, command))
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if model.session.State != wechat.SessionAuthenticated || model.modal != modalNone {
		t.Fatalf("session=%#v modal=%q", model.session, model.modal)
	}
}

func TestWorkspaceLoginCompletesAfterScannedStatus(t *testing.T) {
	app := newFakeWorkspaceApplication()
	app.session = wechat.Session{State: wechat.SessionMissing}
	app.loginPoll = wechat.PollResult{State: wechat.QRScanned, AccountCount: 1}
	model := loadedWorkspace(t, app, nil)

	next, command := model.Update(keyRune("l"))
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	next, command = model.Update(keyRune("r"))
	model = next.(Model)
	next, command = model.Update(runCommand(t, command))
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))

	if model.session.State != wechat.SessionAuthenticated || model.modal != modalNone {
		t.Fatalf("session=%#v modal=%q", model.session, model.modal)
	}
}

func TestWorkspaceLoginPollsAutomaticallyAndReloadsWorkspace(t *testing.T) {
	app := newFakeWorkspaceApplication()
	app.session = wechat.Session{State: wechat.SessionMissing}
	app.loginPoll = wechat.PollResult{State: wechat.QRScanned, AccountCount: 1}
	model := loadedWorkspace(t, app, nil)
	model.modal = modalLogin

	next, command := model.Update(loginPollTickMsg(time.Now()))
	model = next.(Model)
	if command == nil {
		t.Fatal("automatic login poll command is nil")
	}
	next, command = model.Update(runCommand(t, command))
	model = next.(Model)
	if command == nil {
		t.Fatal("login completion command is nil")
	}
	next, command = model.Update(runCommand(t, command))
	model = next.(Model)
	if command == nil || !model.loading {
		t.Fatalf("workspace reload command=%v loading=%t", command, model.loading)
	}
	model = updateWorkspace(t, model, runCommand(t, command))

	if model.session.State != wechat.SessionAuthenticated || model.modal != modalNone || model.loading {
		t.Fatalf("session=%#v modal=%q loading=%t", model.session, model.modal, model.loading)
	}
	if !slices.Contains(app.calls, "PollLogin") || !slices.Contains(app.calls, "RuntimeStatus") {
		t.Fatalf("application calls = %#v", app.calls)
	}
}

func TestWorkspaceLoginModalRendersRasterQRAtModuleWidth(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.options.NoColor = true
	model.options.ASCII = false
	model.width = 100
	model.modal = modalLogin
	model.loginFlow = wechat.LoginFlow{QRBytes: scaledQRPNG(t, 29, 4, 3), ExpiresAt: time.Now().UTC().Add(time.Minute)}

	view := model.renderModal()
	for _, line := range strings.Split(view, "\n") {
		if width := len([]rune(line)); width > 92 {
			t.Fatalf("login modal wrapped a QR raster line to %d columns:\n%s", width, view)
		}
	}
}

func TestWorkspaceLoginModalFallsBackWhenQRExceedsTerminalWidth(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.options.NoColor = true
	model.options.ASCII = false
	model.width = 30
	model.modal = modalLogin
	model.loginFlow = wechat.LoginFlow{QRBytes: scaledQRPNG(t, 29, 4, 3), ExpiresAt: time.Now().UTC().Add(time.Minute)}

	view := model.renderModal()
	if !strings.Contains(view, "QR image loaded in memory") {
		t.Fatalf("narrow login modal must show QR output fallback:\n%s", view)
	}
}

func TestWorkspaceNoColorUnicodeFallbackStateRoundTripAndNonTTYGuard(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.options.NoColor = true
	model.options.ASCII = true
	model.state.Area = AreaArticles
	model.state.Selection.Toggle(AreaArticles, "article-b")
	model.state.Selection.Toggle(AreaArticles, "article-a")
	encoded, err := model.State().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseWorkspaceState(encoded)
	if err != nil || decoded.Area != AreaArticles || !reflect.DeepEqual(decoded.Selection[AreaArticles], []string{"article-a", "article-b"}) {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
	view := model.View()
	if strings.Contains(view, "\x1b[") || strings.Contains(view, "✓") || strings.Contains(view, "›") {
		t.Fatalf("fallback view contains color/unicode control:\n%s", view)
	}
	if UnicodeSupported("C", "POSIX") || !UnicodeSupported("en_US.UTF-8") {
		t.Fatal("UnicodeSupported locale detection is incorrect")
	}
	if ShouldStartWorkspace(bytes.NewBuffer(nil), io.Discard, false) {
		t.Fatal("non-TTY workspace should not start")
	}
	if !ShouldStartWorkspace(bytes.NewBuffer(nil), io.Discard, true) {
		t.Fatal("forced workspace should start")
	}
	err = RunWorkspace(context.Background(), WorkspaceOptions{Application: app, Input: bytes.NewBuffer(nil), Output: io.Discard})
	if !errors.Is(err, ErrNonInteractive) {
		t.Fatalf("RunWorkspace() error = %v", err)
	}
}

func TestWorkspaceStateRestoreUsesConfiguredPageSizeAndClampsExportOffset(t *testing.T) {
	state, err := ParseWorkspaceStateWithPageSize([]byte(`{"area":"exports","queries":{"accounts":{},"articles":{},"albums":{},"jobs":{},"exports":{"offset":-20}},"selection":{},"columns":{},"cursors":{}}`), 37)
	if err != nil {
		t.Fatal(err)
	}
	if state.Queries.Exports.Offset != 0 || state.Queries.Exports.Limit != 37 || state.Queries.Jobs.Limit != 37 {
		t.Fatalf("restored state=%#v", state.Queries)
	}
}

func TestExportSummaryOmitsUnsetCompletionTime(t *testing.T) {
	encoded, err := json.Marshal(ExportSummary{ID: "export-a", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "completedAt") {
		t.Fatalf("unfinished export encoded completion time: %s", encoded)
	}
}

func TestWorkspaceAreasExposeAllOpenSpecWorkflows(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, &fakeWorkspaceExtensions{})
	want := map[Area][]string{
		AreaAccounts: {"Synchronize", "Import manifest", "Export manifest", "Delete local data"},
		AreaArticles: {"Edit compound filter", "Save current query", "Load saved query", "List saved queries", "Delete saved query",
			"Download selected", "Export selected", "Comments", "Metrics", "Resource completeness"},
		AreaAlbums:  {"Traverse all forward", "Traverse all reverse", "Batch download", "Export album"},
		AreaJobs:    {"Show logs and lease", "Pause", "Resume", "Retry", "Route health", "Cancel"},
		AreaExports: {"Configure export", "Result manifest", "Verify result", "Open output"},
		AreaSettings: {"List credentials", "Import credential", "Validate credential", "Remove credential", "List proxies", "Add proxy",
			"Enable proxy", "Disable proxy", "Test proxy", "Remove proxy", "Show preferences", "Set preference"},
		AreaStorage:     {"Backup", "Restore", "Integrity check", "Garbage collection plan", "Apply garbage collection"},
		AreaDiagnostics: {"Refresh diagnostics", "Create diagnostic bundle", "Route health"},
	}
	for area, labels := range want {
		model.state.Area = area
		actions := model.actionsForArea()
		got := make([]string, len(actions))
		for index, action := range actions {
			got[index] = action.Label
		}
		if !reflect.DeepEqual(got, labels) {
			t.Fatalf("%s actions = %#v, want %#v", area, got, labels)
		}
	}
}

func TestWorkspacePromptsForAccountImportRestoreAndExportParameters(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)

	model.state.Area = AreaAccounts
	next, _ := model.chooseAction(actionItem{Kind: string(OperationAccountImport)})
	model = next.(Model)
	if model.modal != modalInput || model.inputMode != inputAccountImport {
		t.Fatalf("account import prompt = modal %q mode %q", model.modal, model.inputMode)
	}
	model.input = "/tmp/accounts.json"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	if message.err != nil || len(extensions.requests) != 1 || extensions.requests[0].Kind != OperationAccountImport ||
		extensions.requests[0].Parameters["path"] != "/tmp/accounts.json" {
		t.Fatalf("account import request=%#v err=%v", extensions.requests, message.err)
	}

	model.state.Area = AreaStorage
	next, _ = model.chooseAction(actionItem{Kind: string(OperationRestore), Destructive: true})
	model = next.(Model)
	model.input = "/tmp/backup.zip"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.modal != modalConfirm || model.confirm.Phrase != "restore-library" ||
		model.inputParams["path"] != "/tmp/backup.zip" {
		t.Fatalf("restore confirmation=%#v params=%#v", model.confirm, model.inputParams)
	}
	model.confirm.Input = model.confirm.Phrase
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message = runCommand(t, command).(actionResultMsg)
	last := extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationRestore || last.Parameters["path"] != "/tmp/backup.zip" {
		t.Fatalf("restore request=%#v err=%v", last, message.err)
	}

	model.state.Area = AreaArticles
	model.state.Selection.Toggle(AreaArticles, "article-a")
	next, _ = model.chooseAction(actionItem{Kind: "article_export"})
	model = next.(Model)
	if model.inputMode != inputExportFormat || model.input != "markdown" {
		t.Fatalf("export format prompt mode=%q input=%q", model.inputMode, model.input)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.inputMode != inputExportOutput || model.input != "~/Downloads/wechat-article-exports" ||
		model.inputLabel != "Export output directory (Enter accepts default)" {
		t.Fatalf("export output prompt mode=%q label=%q input=%q", model.inputMode, model.inputLabel, model.input)
	}
	model.input = "/tmp/exports"
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message = runCommand(t, command).(actionResultMsg)
	last = extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationExportStart || last.Parameters["format"] != "markdown" ||
		last.Parameters["outputRoot"] != "/tmp/exports" || !reflect.DeepEqual(last.IDs, []string{"article-a"}) {
		t.Fatalf("export request=%#v err=%v", last, message.err)
	}
}

func TestWorkspacePromptsForDiagnosticBundleDestination(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)
	model.state.Area = AreaDiagnostics

	next, _ := model.chooseAction(actionItem{Kind: string(OperationDiagnosticBundle)})
	model = next.(Model)
	if model.modal != modalInput || model.inputMode != inputDiagnosticBundle {
		t.Fatalf("diagnostic bundle prompt = modal %q mode %q", model.modal, model.inputMode)
	}
	model.input = "/tmp/diagnostics.zip"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	last := extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationDiagnosticBundle || last.Parameters["path"] != "/tmp/diagnostics.zip" {
		t.Fatalf("diagnostic bundle request=%#v err=%v", last, message.err)
	}
}

func TestWorkspacePromptsForHTMLPolicyAndOptionalBatchArchive(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)
	model.state.Area = AreaArticles
	model.state.Selection.Toggle(AreaArticles, "article-a")

	next, _ := model.chooseAction(actionItem{Kind: "article_export"})
	model = next.(Model)
	model.input = "html"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.inputMode != inputExportPolicy || model.input != "best-effort" {
		t.Fatalf("HTML policy prompt mode=%q input=%q", model.inputMode, model.input)
	}
	model.input = "strict"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.inputMode != inputExportArchive {
		t.Fatalf("HTML archive prompt mode=%q", model.inputMode)
	}
	model.input = "articles.zip"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.inputMode != inputExportOutput || model.input != "~/Downloads/wechat-article-exports" {
		t.Fatalf("HTML export output prompt mode=%q input=%q", model.inputMode, model.input)
	}
	model.input = "/tmp/html-exports"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	last := extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Parameters["format"] != "html" || last.Parameters["htmlResourcePolicy"] != "strict" ||
		last.Parameters["htmlBatchArchive"] != "articles.zip" || last.Parameters["outputRoot"] != "/tmp/html-exports" {
		t.Fatalf("HTML export request=%#v err=%v", last, message.err)
	}
}

func TestWorkspaceUsesConfiguredExportRootAsDefault(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{defaultExportRoot: "/tmp/preferred-exports"}
	model := loadedWorkspace(t, app, extensions)
	model.state.Area = AreaArticles
	model.state.Selection.Toggle(AreaArticles, "article-a")

	next, _ := model.chooseAction(actionItem{Kind: "article_export"})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if model.inputMode != inputExportOutput || model.input != "/tmp/preferred-exports" {
		t.Fatalf("configured export output prompt mode=%q input=%q", model.inputMode, model.input)
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	last := extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Parameters["outputRoot"] != "/tmp/preferred-exports" {
		t.Fatalf("default export request=%#v err=%v", last, message.err)
	}
}

func TestWorkspaceSettingsAndStorageActionsCollectMutationParameters(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)

	model.state.Area = AreaSettings
	next, _ := model.chooseAction(actionItem{Kind: string(OperationCredentialImport)})
	model = next.(Model)
	model.input = "/tmp/credential.json"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	last := extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationCredentialImport || last.Parameters["path"] != "/tmp/credential.json" {
		t.Fatalf("credential import request=%#v err=%v", last, message.err)
	}

	next, _ = model.chooseAction(actionItem{Kind: string(OperationProxyAdd)})
	model = next.(Model)
	model.input = "public"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model.input = "https://proxy.example/wrap"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if !model.inputSecret {
		t.Fatal("proxy authorization prompt is not marked secret")
	}
	model.input = "proxy-secret"
	view := model.View()
	if strings.Contains(view, "proxy-secret") {
		t.Fatalf("secret prompt leaked authorization in view:\n%s", view)
	}
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message = runCommand(t, command).(actionResultMsg)
	last = extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationProxyAdd || last.Parameters["name"] != "public" ||
		last.Parameters["endpoint"] != "https://proxy.example/wrap" || last.Parameters["authorization"] != "proxy-secret" {
		t.Fatalf("proxy add request=%#v err=%v", last, message.err)
	}

	next, _ = model.chooseAction(actionItem{Kind: string(OperationPreferenceSet)})
	model = next.(Model)
	model.input = "download.concurrency"
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model.input = "8"
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message = runCommand(t, command).(actionResultMsg)
	last = extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationPreferenceSet || last.Parameters["key"] != "download.concurrency" ||
		last.Parameters["value"] != "8" {
		t.Fatalf("preference request=%#v err=%v", last, message.err)
	}

	model.state.Area = AreaStorage
	next, _ = model.chooseAction(actionItem{Kind: "garbage_apply"})
	model = next.(Model)
	model.input = "garbage-collect:1:0:0:0"
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message = runCommand(t, command).(actionResultMsg)
	last = extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationGarbageCollect || last.Parameters["mode"] != "apply" ||
		last.Parameters["confirm"] != "garbage-collect:1:0:0:0" {
		t.Fatalf("garbage apply request=%#v err=%v", last, message.err)
	}
}

func TestWorkspaceQuerySearchPagesAndColumnSelection(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaArticles
	model = updateWorkspace(t, model, keyRune("/"))
	model.input = "keyword=agent;author=Alice;content=true;read=10..100;sort=published:desc,title:asc"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	query := app.articleQueries[len(app.articleQueries)-1]
	if query.Keyword != "agent" || query.Author != "Alice" || query.HasContent == nil || !*query.HasContent ||
		query.ReadMin == nil || *query.ReadMin != 10 || query.ReadMax == nil || *query.ReadMax != 100 || len(query.Sorts) != 2 {
		t.Fatalf("compound query=%#v calls=%#v", model.state.Queries.Articles, app.articleQueries)
	}
	model = updateWorkspace(t, model, keyRune("c"))
	before := append([]string(nil), model.state.Columns[AreaArticles]...)
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeySpace})
	if reflect.DeepEqual(before, model.state.Columns[AreaArticles]) {
		t.Fatalf("columns did not change: %#v", before)
	}
}

func TestWorkspaceSavedArticleQueryAndExportSelectionWorkflows(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)
	model.state.Area = AreaArticles
	model.state.Queries.Articles = domain.ArticleQuery{Keyword: "agent", Limit: 20}

	next, _ := model.chooseAction(actionItem{Kind: "article_query_save"})
	model = next.(Model)
	model.input = "agents"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if len(app.savedQueries) != 1 || app.savedQueries[0].Query.Keyword != "agent" {
		t.Fatalf("saved queries=%#v", app.savedQueries)
	}

	model.state.Queries.Articles = domain.ArticleQuery{Limit: 20}
	next, _ = model.chooseAction(actionItem{Kind: "article_query_load"})
	model = next.(Model)
	model.input = "agents"
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	next, command = model.Update(runCommand(t, command))
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if model.state.Queries.Articles.Keyword != "agent" {
		t.Fatalf("loaded query=%#v", model.state.Queries.Articles)
	}

	model.state.Area = AreaExports
	if model.currentID() != "export-a" {
		t.Fatalf("current export ID=%q", model.currentID())
	}
	model = updateWorkspace(t, model, keyRune("a"))
	for model.actions[model.modalCursor].Kind != string(OperationExportManifest) {
		model = updateWorkspace(t, model, keyRune("j"))
	}
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	last := extensions.requests[len(extensions.requests)-1]
	if message.err != nil || last.Kind != OperationExportManifest || !reflect.DeepEqual(last.IDs, []string{"export-a"}) {
		t.Fatalf("export request=%#v err=%v", last, message.err)
	}
}

func TestWorkspaceAutomaticRefreshPreservesModalInputSelectionAndCursor(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, &fakeWorkspaceExtensions{})
	model.state.Area = AreaJobs
	model.state.Cursors[AreaJobs] = 0
	model.state.Selection.Toggle(AreaJobs, "job-a")
	model.modal, model.inputMode, model.input = modalInput, inputSearch, "unchanged"

	model.refreshGeneration = 1
	message := runCommand(t, model.refreshActiveWorkCmd(model.refreshGeneration)).(workspaceRefreshMsg)
	model = updateWorkspace(t, model, message)
	if model.modal != modalInput || model.input != "unchanged" || !model.state.Selection.Has(AreaJobs, "job-a") ||
		model.state.Cursors[AreaJobs] != 0 {
		t.Fatalf("refresh mutated interaction state: modal=%q input=%q state=%#v", model.modal, model.input, model.state)
	}
}

func TestWorkspaceAutomaticRefreshQueriesOnlyCapturedActiveArea(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)
	app.calls = nil
	model.state.Area = AreaExports
	message := runCommand(t, model.refreshActiveWorkCmd(1)).(workspaceRefreshMsg)
	if message.err != nil || slices.Contains(app.calls, "QueryJobs") {
		t.Fatalf("exports refresh calls=%#v err=%v", app.calls, message.err)
	}
	model.state.Area = AreaJobs
	app.calls = nil
	message = runCommand(t, model.refreshActiveWorkCmd(2)).(workspaceRefreshMsg)
	if message.err != nil || !slices.Contains(app.calls, "QueryJobs") {
		t.Fatalf("jobs refresh calls=%#v err=%v", app.calls, message.err)
	}
}

func TestTerminalJobThroughputUsesStableUpdatedTime(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaJobs
	created := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	model.jobs.Items = []domain.Job{{ID: "job-a", Kind: "export", State: domain.JobCompleted,
		CreatedAt: created, UpdatedAt: created.Add(2 * time.Minute), Counts: map[string]int{string(domain.JobCompleted): 2, "total": 2}}}
	model.options.Now = func() time.Time { return created.Add(24 * time.Hour) }
	if view := model.renderJobs(theme(true)); !strings.Contains(view, "1.0/min") {
		t.Fatalf("terminal throughput view=%s", view)
	}
}

func TestWorkspaceAutomaticRefreshRejectsStaleOrChangedQueries(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, &fakeWorkspaceExtensions{})
	model.state.Area = AreaJobs
	model.refreshGeneration = 2
	model.refreshInFlight = true
	original := model.jobs

	stale := workspaceRefreshMsg{generation: 1, area: AreaJobs, jobsQuery: model.state.Queries.Jobs,
		jobs: domain.Page[domain.Job]{Items: []domain.Job{{ID: "stale"}}, Total: 1}}
	model = updateWorkspace(t, model, stale)
	if !reflect.DeepEqual(model.jobs, original) || !model.refreshInFlight {
		t.Fatalf("stale refresh mutated model: jobs=%#v inFlight=%v", model.jobs, model.refreshInFlight)
	}

	oldQuery := model.state.Queries.Jobs
	model.state.Queries.Jobs.Offset = 20
	changed := workspaceRefreshMsg{generation: 2, area: AreaJobs, jobsQuery: oldQuery,
		jobs: domain.Page[domain.Job]{Items: []domain.Job{{ID: "old-page"}}, Total: 1}}
	model = updateWorkspace(t, model, changed)
	if !reflect.DeepEqual(model.jobs, original) || model.refreshInFlight {
		t.Fatalf("changed-query refresh mutated model: jobs=%#v inFlight=%v", model.jobs, model.refreshInFlight)
	}
}

func TestWorkspaceManualJobsLoadInvalidatesOlderAutomaticRefresh(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaJobs
	model.refreshGeneration = 4
	model.refreshInFlight = true
	oldQuery := model.state.Queries.Jobs
	_ = model.loadAreaCmd(AreaJobs)
	if model.refreshGeneration != 5 || model.refreshInFlight {
		t.Fatalf("manual load refresh state generation=%d inFlight=%v", model.refreshGeneration, model.refreshInFlight)
	}
	original := model.jobs
	model = updateWorkspace(t, model, workspaceRefreshMsg{generation: 4, area: AreaJobs, jobsQuery: oldQuery,
		jobs: domain.Page[domain.Job]{Items: []domain.Job{{ID: "stale"}}, Total: 1}})
	if !reflect.DeepEqual(model.jobs, original) {
		t.Fatalf("older automatic refresh replaced manual result: %#v", model.jobs)
	}
}

func TestWorkspaceJobConfirmationUsesSnapshottedID(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaJobs
	model.jobs.Items = []domain.Job{{ID: "job-original", State: domain.JobRunning}, {ID: "job-new", State: domain.JobRunning}}
	model.state.Cursors[AreaJobs] = 0
	model.confirm = model.confirmationFor("job_cancel")
	model.confirm.Input = model.confirm.Phrase
	model.modal = modalConfirm
	model.jobs.Items[0], model.jobs.Items[1] = model.jobs.Items[1], model.jobs.Items[0]
	next, command := model.executeConfirmedAction()
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if !reflect.DeepEqual(app.cancelled, []domain.JobID{"job-original"}) {
		t.Fatalf("cancelled IDs=%#v", app.cancelled)
	}
}

func TestSanitizeTableCellRejectsC0C1AndFormatControls(t *testing.T) {
	value := sanitizeTableCell("safe\x1b[31m\u009bspoof\u009d\u202ebad")
	if value != "safe[31mspoofbad" {
		t.Fatalf("sanitized value=%q", value)
	}
}

func TestWorkspaceManualAreaLoadRejectsOutOfOrderQueryResult(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaArticles

	model.state.Queries.Articles = domain.ArticleQuery{Keyword: "old", Offset: 0, Limit: 20}
	oldCommand := model.loadAreaCmd(AreaArticles)
	model.state.Queries.Articles = domain.ArticleQuery{Keyword: "current", Offset: 20, Limit: 20}
	currentCommand := model.loadAreaCmd(AreaArticles)

	current := runCommand(t, currentCommand).(areaLoadedMsg)
	current.articles.Items = []domain.Article{{ID: "current-page", Title: "Current page"}}
	current.articles.Total = 40
	model = updateWorkspace(t, model, current)
	if got := model.articles.Items[0].ID; got != "current-page" {
		t.Fatalf("current result was not applied: %q", got)
	}

	old := runCommand(t, oldCommand).(areaLoadedMsg)
	old.articles.Items = []domain.Article{{ID: "old-page", Title: "Old page"}}
	old.articles.Total = 40
	model = updateWorkspace(t, model, old)
	if got := model.articles.Items[0].ID; got != "current-page" {
		t.Fatalf("out-of-order result replaced current page: %q", got)
	}
}

func TestWorkspaceExportAreaNeverUsesExportIDsAsArticleSelection(t *testing.T) {
	app := newFakeWorkspaceApplication()
	extensions := &fakeWorkspaceExtensions{}
	model := loadedWorkspace(t, app, extensions)
	model.state.Area = AreaExports
	model.state.Selection.Toggle(AreaExports, "export-a")
	if ids := model.exportSelectionIDs(); len(ids) != 0 {
		t.Fatalf("export selection IDs=%#v", ids)
	}
	if actions := model.actionsForArea(); actions[0].Kind != string(OperationExportConfig) {
		t.Fatalf("first export action=%#v", actions[0])
	}
}

func TestParseArticleQueryInputRejectsUnknownAndMalformedFilters(t *testing.T) {
	for _, value := range []string{"unknown=value", "read=100..10", "content=maybe", "sort=published:sideways",
		`{"readMin":100,"readMax":10}`, `{"sorts":[{"field":"not-a-field","direction":"asc"}]}`,
		`{"sort":"invalid"}`,
		`{"keyword":"ok"} trailing`} {
		if _, err := parseArticleQueryInput(value, 20); err == nil {
			t.Fatalf("parseArticleQueryInput(%q) error=nil", value)
		}
	}
}

func TestParseArticleQueryInputNormalizesDateEndAndLimit(t *testing.T) {
	query, err := parseArticleQueryInput(`{"publishedTo":"2026-07-22T00:00:00Z","limit":9999}`, 20)
	if err != nil {
		t.Fatal(err)
	}
	if query.Limit != 500 {
		t.Fatalf("limit=%d", query.Limit)
	}
	query, err = parseArticleQueryInput("to=2026-07-22", 20)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 7, 22, 23, 59, 59, 999999999, time.UTC)
	if !query.PublishedTo.Equal(want) {
		t.Fatalf("publishedTo=%s want=%s", query.PublishedTo, want)
	}
	query, err = parseArticleQueryInput("", 9999)
	if err != nil || query.Limit != 500 {
		t.Fatalf("empty query=%#v error=%v", query, err)
	}
}

func TestWorkspaceLegacyKeywordInputStillAvailableForAccounts(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaAccounts
	model = updateWorkspace(t, model, keyRune("/"))
	for _, character := range "agent" {
		model = updateWorkspace(t, model, keyRune(string(character)))
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if model.state.Queries.Accounts.Keyword != "agent" || app.accountQueries[len(app.accountQueries)-1].Keyword != "agent" {
		t.Fatalf("query=%#v calls=%#v", model.state.Queries.Accounts, app.accountQueries)
	}
}

func TestWorkspaceAccountDiscoverySavesReturnedAccounts(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaAccounts
	model = updateWorkspace(t, model, keyRune("d"))
	for _, character := range "fixture" {
		model = updateWorkspace(t, model, keyRune(string(character)))
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	message := runCommand(t, command).(actionResultMsg)
	if message.err != nil || len(app.savedAccounts) != 1 || app.savedAccounts[0].FakeID != "search-a" {
		t.Fatalf("saved accounts=%#v message=%#v", app.savedAccounts, message)
	}
}

func TestIsInteractiveRejectsOrdinaryFilesAndBuffers(t *testing.T) {
	if IsInteractive(bytes.NewBuffer(nil), bytes.NewBuffer(nil)) {
		t.Fatal("buffers must not be treated as interactive")
	}
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if IsInteractive(file, file) {
		t.Fatal("ordinary files must not be treated as TTYs")
	}
}

func loadedWorkspace(t *testing.T, app *fakeWorkspaceApplication, extensions WorkspaceExtensions) Model {
	t.Helper()
	model := NewWorkspace(WorkspaceOptions{Context: context.Background(), Application: app, Extensions: extensions,
		NoColor: true, ASCII: true, Width: 100, Height: 30})
	return updateWorkspace(t, model, runCommand(t, model.Init()))
}

func updateWorkspace(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}

func runCommand(t *testing.T, command tea.Cmd) tea.Msg {
	t.Helper()
	if command == nil {
		t.Fatal("command is nil")
	}
	return command()
}

func keyRune(value string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)} }

func scaledQRPNG(t *testing.T, modules, scale, quiet int) []byte {
	t.Helper()
	size := (modules + quiet*2) * scale
	imageValue := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			imageValue.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	for moduleY := 0; moduleY < modules; moduleY++ {
		for moduleX := 0; moduleX < modules; moduleX++ {
			if (moduleX+moduleY)%3 != 0 && moduleX != 0 && moduleY != 0 {
				continue
			}
			for y := 0; y < scale; y++ {
				for x := 0; x < scale; x++ {
					imageValue.SetGray((moduleX+quiet)*scale+x, (moduleY+quiet)*scale+y, color.Gray{Y: 0})
				}
			}
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, imageValue); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type fakeWorkspaceExtensions struct {
	preview           PreviewDocument
	opened            int
	requests          []OperationRequest
	defaultExportRoot string
}

func (extensions *fakeWorkspaceExtensions) DefaultExportRoot(context.Context) (string, error) {
	return extensions.defaultExportRoot, nil
}

func (extensions *fakeWorkspaceExtensions) Panel(_ context.Context, area Area) (OperationResult, error) {
	return OperationResult{Title: areaLabel(area), Message: "fixture panel"}, nil
}
func (*fakeWorkspaceExtensions) QueryExports(_ context.Context, offset, limit int) (domain.Page[ExportSummary], error) {
	return domain.Page[ExportSummary]{Items: []ExportSummary{{
		ID: "export-a", Format: "markdown", State: "completed", OutputRoot: "/tmp/export-a",
		ProvenanceState: "ready", ProvenancePath: "export-a-manifest.json", ProvenanceGeneration: 1,
	}}, Total: 1, Offset: offset, Limit: limit}, nil
}
func (extensions *fakeWorkspaceExtensions) PreviewArticle(context.Context, domain.ArticleID) (PreviewDocument, error) {
	return extensions.preview, nil
}
func (extensions *fakeWorkspaceExtensions) OpenHTMLPreview(context.Context, domain.ArticleID) error {
	extensions.opened++
	return nil
}
func (extensions *fakeWorkspaceExtensions) Operate(_ context.Context, request OperationRequest) (OperationResult, error) {
	extensions.requests = append(extensions.requests, request)
	return OperationResult{Title: string(request.Kind), Message: "complete"}, nil
}

type fakeWorkspaceApplication struct {
	calls          []string
	session        wechat.Session
	accountQueries []domain.AccountQuery
	articleQueries []domain.ArticleQuery
	albumQueries   []domain.AlbumQuery
	jobQueries     []domain.JobQuery
	downloads      []domain.DownloadRequest
	exports        []domain.ExportRequest
	syncs          []domain.SynchronizeAccountRequest
	deleted        [][]domain.AccountID
	cancelled      []domain.JobID
	loginSessions  []string
	loginPoll      wechat.PollResult
	savedAccounts  []domain.Account
	savedQueries   []domain.SavedArticleQuery
}

func newFakeWorkspaceApplication() *fakeWorkspaceApplication {
	return &fakeWorkspaceApplication{session: wechat.Session{State: wechat.SessionAuthenticated, AccountName: "Fixture"}}
}

func (app *fakeWorkspaceApplication) record(name string) { app.calls = append(app.calls, name) }
func (app *fakeWorkspaceApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	app.record("RuntimeStatus")
	return domain.RuntimeStatus{Version: "test", Profile: "profile-a", OfflineReady: true}, nil
}
func (app *fakeWorkspaceApplication) BeginLogin(_ context.Context, sessionID string) (wechat.LoginFlow, error) {
	app.record("BeginLogin")
	app.loginSessions = append(app.loginSessions, sessionID)
	return wechat.LoginFlow{SessionID: "fixture", ExpiresAt: time.Date(2026, 7, 22, 12, 5, 0, 0, time.UTC)}, nil
}
func (app *fakeWorkspaceApplication) PollLogin(context.Context) (wechat.PollResult, error) {
	app.record("PollLogin")
	if app.loginPoll.State != "" {
		return app.loginPoll, nil
	}
	return wechat.PollResult{State: wechat.QRConfirmed, AccountCount: 1}, nil
}
func (app *fakeWorkspaceApplication) CompleteLogin(context.Context) (wechat.Session, error) {
	app.record("CompleteLogin")
	app.session = wechat.Session{State: wechat.SessionAuthenticated, AccountName: "Fixture"}
	return app.session, nil
}
func (app *fakeWorkspaceApplication) SessionStatus(context.Context) (wechat.Session, error) {
	app.record("SessionStatus")
	return app.session, nil
}
func (app *fakeWorkspaceApplication) ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error) {
	return []wechat.SwitchableAccount{{ID: "fixture", Name: "Fixture"}}, nil
}
func (app *fakeWorkspaceApplication) SwitchAccount(context.Context, string) (wechat.Session, error) {
	return app.session, nil
}
func (app *fakeWorkspaceApplication) Logout(context.Context) error {
	app.record("Logout")
	app.session = wechat.Session{State: wechat.SessionMissing}
	return nil
}
func (app *fakeWorkspaceApplication) SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	app.record("SearchAccounts")
	return domain.Page[domain.Account]{Items: []domain.Account{{FakeID: "search-a", Name: "Search result"}}, Total: 1}, nil
}
func (*fakeWorkspaceApplication) ResolveAccountName(context.Context, string) (string, error) {
	return "Fixture", nil
}
func (*fakeWorkspaceApplication) ResolveAccountFromArticle(context.Context, string) (domain.Account, error) {
	return domain.Account{ID: "account-a", FakeID: "fake-a", Name: "Fixture"}, nil
}
func (*fakeWorkspaceApplication) AccountDetails(context.Context, string) (wechat.AccountDetails, error) {
	return wechat.AccountDetails{}, nil
}
func (*fakeWorkspaceApplication) AuthorInfo(context.Context, string) (wechat.AuthorInfo, error) {
	return wechat.AuthorInfo{}, nil
}
func (*fakeWorkspaceApplication) ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	return wechat.ArticlePage{}, nil
}
func (app *fakeWorkspaceApplication) SaveAccount(_ context.Context, account domain.Account) (domain.Account, error) {
	if account.ID == "" {
		account.ID = domain.AccountID("saved-" + account.FakeID)
	}
	app.savedAccounts = append(app.savedAccounts, account)
	return account, nil
}
func (*fakeWorkspaceApplication) UpdateAccount(_ context.Context, account domain.Account) (domain.Account, error) {
	return account, nil
}
func (*fakeWorkspaceApplication) GetAccount(context.Context, domain.AccountID) (domain.Account, error) {
	return domain.Account{}, nil
}
func (*fakeWorkspaceApplication) GetAccountByFakeID(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (app *fakeWorkspaceApplication) QueryAccounts(_ context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	app.record("QueryAccounts")
	app.accountQueries = append(app.accountQueries, query)
	return domain.Page[domain.Account]{Items: []domain.Account{
		{ID: "account-a", FakeID: "fake-a", Name: "First account", ArticleCount: 12},
		{ID: "account-b", FakeID: "fake-b", Name: "Second account", ArticleCount: 4},
	}, Total: 2, Offset: query.Offset, Limit: query.Limit}, nil
}
func (*fakeWorkspaceApplication) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return domain.AccountManifest{}, nil
}
func (*fakeWorkspaceApplication) ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error) {
	return domain.AccountImportReport{}, nil
}
func (app *fakeWorkspaceApplication) DeleteAccounts(_ context.Context, ids []domain.AccountID) (domain.AccountDeleteReport, error) {
	app.record("DeleteAccounts")
	app.deleted = append(app.deleted, append([]domain.AccountID(nil), ids...))
	return domain.AccountDeleteReport{AccountsDeleted: len(ids), ArticlesDeleted: 12}, nil
}
func (app *fakeWorkspaceApplication) QueryArticles(_ context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	app.record("QueryArticles")
	app.articleQueries = append(app.articleQueries, query)
	return domain.Page[domain.Article]{Items: []domain.Article{
		{ID: "article-a", Title: "First article", Author: "Alice", HasContent: true},
		{ID: "article-b", Title: "Second article", Author: "Bob"},
	}, Total: 2, Offset: query.Offset, Limit: query.Limit}, nil
}
func (app *fakeWorkspaceApplication) SaveArticleQuery(_ context.Context, name string, query domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	item := domain.SavedArticleQuery{Name: name, Query: query}
	app.savedQueries = append(app.savedQueries, item)
	return item, nil
}
func (app *fakeWorkspaceApplication) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return append([]domain.SavedArticleQuery(nil), app.savedQueries...), nil
}
func (app *fakeWorkspaceApplication) DeleteSavedArticleQuery(_ context.Context, name string) (bool, error) {
	for index, item := range app.savedQueries {
		if item.Name == name {
			app.savedQueries = append(app.savedQueries[:index], app.savedQueries[index+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (app *fakeWorkspaceApplication) QueryAlbums(_ context.Context, query domain.AlbumQuery) (domain.Page[domain.Album], error) {
	app.record("QueryAlbums")
	app.albumQueries = append(app.albumQueries, query)
	return domain.Page[domain.Album]{Items: []domain.Album{{ID: "album-a", Name: "Album", ArticleCount: 3}},
		Total: 1, Offset: query.Offset, Limit: query.Limit}, nil
}
func (app *fakeWorkspaceApplication) SynchronizeAccount(_ context.Context, request domain.SynchronizeAccountRequest) (domain.Job, error) {
	app.record("SynchronizeAccount")
	app.syncs = append(app.syncs, request)
	return domain.Job{ID: "job-sync", Kind: "account_sync", State: domain.JobQueued}, nil
}
func (app *fakeWorkspaceApplication) SynchronizeAlbum(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (app *fakeWorkspaceApplication) StartDownload(_ context.Context, request domain.DownloadRequest) (domain.Job, error) {
	app.record("StartDownload")
	app.downloads = append(app.downloads, request)
	return domain.Job{ID: "job-download", Kind: "article_download", State: domain.JobQueued}, nil
}
func (app *fakeWorkspaceApplication) StartExport(_ context.Context, request domain.ExportRequest) (domain.Job, error) {
	app.record("StartExport")
	app.exports = append(app.exports, request)
	return domain.Job{ID: "job-export", Kind: "export", State: domain.JobQueued}, nil
}
func (*fakeWorkspaceApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	return domain.Job{}, nil
}
func (app *fakeWorkspaceApplication) QueryJobs(_ context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	app.record("QueryJobs")
	app.jobQueries = append(app.jobQueries, query)
	return domain.Page[domain.Job]{Items: []domain.Job{{ID: "job-a", Kind: "account_sync", State: domain.JobRunning,
		Counts: map[string]int{"done": 2, "total": 5}}}, Total: 1, Offset: query.Offset, Limit: query.Limit}, nil
}
func (app *fakeWorkspaceApplication) CancelJob(_ context.Context, id domain.JobID) (domain.Job, error) {
	app.record("CancelJob")
	app.cancelled = append(app.cancelled, id)
	return domain.Job{ID: id, Kind: "account_sync", State: domain.JobCancelled}, nil
}
func (app *fakeWorkspaceApplication) StorageStatus(context.Context) (domain.StorageStatus, error) {
	app.record("StorageStatus")
	return domain.StorageStatus{DatabaseAvailable: true, ObjectStoreReady: true, Accounts: 2, Articles: 2, Albums: 1, Jobs: 1}, nil
}
func (*fakeWorkspaceApplication) DiscoverBrowser(context.Context) (runtimeenv.Browser, error) {
	return runtimeenv.Browser{Path: "/browser"}, nil
}
func (*fakeWorkspaceApplication) ProcessSignals() <-chan os.Signal { return nil }
