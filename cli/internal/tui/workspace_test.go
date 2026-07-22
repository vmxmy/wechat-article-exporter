package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
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
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
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

func TestWorkspaceAreasExposeAllOpenSpecWorkflows(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, &fakeWorkspaceExtensions{})
	want := map[Area][]string{
		AreaAccounts:    {"Synchronize", "Import manifest", "Export manifest", "Delete local data"},
		AreaArticles:    {"Download selected", "Export selected", "Comments", "Metrics", "Resource completeness"},
		AreaAlbums:      {"Traverse all forward", "Traverse all reverse", "Batch download", "Export album"},
		AreaJobs:        {"Show logs and lease", "Pause", "Resume", "Retry", "Route health", "Cancel"},
		AreaExports:     {"Configure export", "Result manifest", "Open output"},
		AreaSettings:    {"Credentials", "Proxies", "Preferences"},
		AreaStorage:     {"Backup", "Restore", "Integrity check", "Garbage collection"},
		AreaDiagnostics: {"Refresh diagnostics", "Route health"},
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

func TestWorkspaceQuerySearchPagesAndColumnSelection(t *testing.T) {
	app := newFakeWorkspaceApplication()
	model := loadedWorkspace(t, app, nil)
	model.state.Area = AreaArticles
	model = updateWorkspace(t, model, keyRune("/"))
	for _, character := range "agent" {
		model = updateWorkspace(t, model, keyRune(string(character)))
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model = updateWorkspace(t, model, runCommand(t, command))
	if model.state.Queries.Articles.Keyword != "agent" || app.articleQueries[len(app.articleQueries)-1].Keyword != "agent" {
		t.Fatalf("query=%#v calls=%#v", model.state.Queries.Articles, app.articleQueries)
	}
	model = updateWorkspace(t, model, keyRune("c"))
	before := append([]string(nil), model.state.Columns[AreaArticles]...)
	model = updateWorkspace(t, model, tea.KeyMsg{Type: tea.KeySpace})
	if reflect.DeepEqual(before, model.state.Columns[AreaArticles]) {
		t.Fatalf("columns did not change: %#v", before)
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

type fakeWorkspaceExtensions struct {
	preview  PreviewDocument
	opened   int
	requests []OperationRequest
}

func (extensions *fakeWorkspaceExtensions) Panel(_ context.Context, area Area) (OperationResult, error) {
	return OperationResult{Title: areaLabel(area), Message: "fixture panel"}, nil
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
	loginSessions  []string
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
func (*fakeWorkspaceApplication) SaveAccount(_ context.Context, account domain.Account) (domain.Account, error) {
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
func (app *fakeWorkspaceApplication) CancelJob(context.Context, domain.JobID) (domain.Job, error) {
	app.record("CancelJob")
	return domain.Job{ID: "job-a", Kind: "account_sync", State: domain.JobCancelled}, nil
}
func (app *fakeWorkspaceApplication) StorageStatus(context.Context) (domain.StorageStatus, error) {
	app.record("StorageStatus")
	return domain.StorageStatus{DatabaseAvailable: true, ObjectStoreReady: true, Accounts: 2, Articles: 2, Albums: 1, Jobs: 1}, nil
}
func (*fakeWorkspaceApplication) DiscoverBrowser(context.Context) (runtimeenv.Browser, error) {
	return runtimeenv.Browser{Path: "/browser"}, nil
}
func (*fakeWorkspaceApplication) ProcessSignals() <-chan os.Signal { return nil }
