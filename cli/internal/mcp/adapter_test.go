package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestToolSchemasAreStableCompleteAndAnnotated(t *testing.T) {
	adapter := New(&fakeApplication{})
	tools := adapter.Tools()
	names := make([]string, len(tools))
	for index, tool := range tools {
		names[index] = tool.Name
		if tool.Description == "" || tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil {
			t.Fatalf("incomplete tool definition: %#v", tool)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok || schema["type"] != "object" || schema["additionalProperties"] != false {
			t.Fatalf("unstable input schema for %s: %#v", tool.Name, tool.InputSchema)
		}
	}
	want := []string{
		"accounts.query", "accounts.resolve", "accounts.search", "albums.query", "articles.query", "comments.start",
		"content.get", "credentials.invoke", "downloads.start", "exports.start", "jobs.cancel", "jobs.get", "jobs.query",
		"metadata.start", "runtime.status", "storage.status", "sync.account", "sync.album",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tool names = %#v, want %#v", names, want)
	}
	if !adapter.tools["articles.query"].Tool.Annotations.ReadOnlyHint ||
		adapter.tools["downloads.start"].Tool.Annotations.ReadOnlyHint ||
		adapter.tools["jobs.cancel"].Tool.Annotations.DestructiveHint == nil ||
		!*adapter.tools["jobs.cancel"].Tool.Annotations.DestructiveHint {
		t.Fatalf("tool annotations = %#v %#v %#v", adapter.tools["articles.query"].Tool.Annotations,
			adapter.tools["downloads.start"].Tool.Annotations, adapter.tools["jobs.cancel"].Tool.Annotations)
	}
}

func TestSharedApplicationContractReturnsMatchingQueriesAndPersistentJobs(t *testing.T) {
	shared := &fakeApplication{
		articles:    domain.Page[domain.Article]{Items: []domain.Article{{ID: "article-a", Title: "Shared"}}, Total: 1, Limit: 20},
		downloadJob: domain.Job{ID: "job-download", Kind: "article_download", State: domain.JobQueued, Profile: "profile-a"},
		job:         domain.Job{ID: "job-download", Kind: "article_download", State: domain.JobRunning, Profile: "profile-a"},
	}
	adapter := New(shared, Options{Profile: "profile-a"})

	_, err := shared.QueryArticles(context.Background(), domain.ArticleQuery{Keyword: "shared", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	mcpResult, err := adapter.Call(context.Background(), "articles.query", json.RawMessage(`{"keyword":"shared","limit":20}`))
	if err != nil {
		t.Fatal(err)
	}
	encodedResult, _ := json.Marshal(mcpResult)
	if !strings.Contains(string(encodedResult), `"id":"article-a"`) ||
		!strings.Contains(string(encodedResult), `"title":"Shared"`) ||
		!strings.Contains(string(encodedResult), `"total":1`) || !strings.Contains(string(encodedResult), `"limit":20`) {
		t.Fatalf("MCP result = %s", encodedResult)
	}
	if shared.articleQuery.Keyword != "shared" || shared.articleQuery.Limit != 20 {
		t.Fatalf("shared application query = %#v", shared.articleQuery)
	}

	started, err := adapter.Call(context.Background(), "downloads.start", json.RawMessage(`{"articleIds":["article-a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	startedJSON, _ := json.Marshal(started)
	if !strings.Contains(string(startedJSON), `"jobId":"job-download"`) ||
		!strings.Contains(string(startedJSON), `"state":"queued"`) ||
		!strings.Contains(string(startedJSON), `"kind":"article_download"`) {
		t.Fatalf("downloads.start = %#v", started)
	}
	if len(shared.downloadRequest.ArticleIDs) != 1 || shared.downloadRequest.ArticleIDs[0] != "article-a" {
		t.Fatalf("download request = %#v", shared.downloadRequest)
	}
	status, err := adapter.Call(context.Background(), "jobs.get", json.RawMessage(`{"jobId":"job-download"}`))
	statusJSON, _ := json.Marshal(status)
	if err != nil || !strings.Contains(string(statusJSON), `"id":"job-download"`) ||
		!strings.Contains(string(statusJSON), `"state":"running"`) {
		t.Fatalf("jobs.get = %#v, %v", status, err)
	}
}

func TestAlbumSyncAndDownloadAliasesPreserveToolSemantics(t *testing.T) {
	shared := &fakeApplication{downloadJob: domain.Job{ID: "job-a", Kind: "album_sync", State: domain.JobQueued}}
	adapter := New(shared)
	if _, err := adapter.Call(context.Background(), "sync.album", json.RawMessage(`{"accountId":"account-a","albumId":"album-a"}`)); err != nil {
		t.Fatal(err)
	}
	if shared.albumAccount != "account-a" || shared.albumID != "album-a" {
		t.Fatalf("album synchronization request account=%q album=%q", shared.albumAccount, shared.albumID)
	}
	if _, err := adapter.Call(context.Background(), "metadata.start", json.RawMessage(`{"kind":"comments","articleIds":["article-a"]}`)); err != nil {
		t.Fatal(err)
	}
	if shared.downloadRequest.Kind != "metadata" {
		t.Fatalf("metadata alias kind = %q", shared.downloadRequest.Kind)
	}
	if _, err := adapter.Call(context.Background(), "comments.start", json.RawMessage(`{"kind":"paid","articleIds":["article-a"]}`)); err != nil {
		t.Fatal(err)
	}
	if shared.downloadRequest.Kind != "comments" {
		t.Fatalf("comments alias kind = %q", shared.downloadRequest.Kind)
	}
}

func TestPolicyReadOnlyAllowDenyConfirmationAndSensitiveRestrictions(t *testing.T) {
	shared := &fakeApplication{job: domain.Job{ID: "job-a", State: domain.JobCancelled}}
	readOnly := New(shared, Options{Policy: profiles.MCPPolicy{ReadOnly: true}})
	if _, err := readOnly.Call(context.Background(), "downloads.start", json.RawMessage(`{}`)); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only download error = %v", err)
	}
	if _, err := readOnly.Call(context.Background(), "articles.query", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("read-only query error = %v", err)
	}

	filtered := New(shared, Options{Policy: profiles.MCPPolicy{
		Allow: []string{"articles.query", "jobs.cancel"},
		Deny:  []string{"jobs.cancel"},
	}})
	if _, err := filtered.Call(context.Background(), "accounts.query", json.RawMessage(`{}`)); !errors.Is(err, ErrToolDenied) {
		t.Fatalf("allow-list error = %v", err)
	}
	if _, err := filtered.Call(context.Background(), "jobs.cancel", json.RawMessage(`{"jobId":"job-a"}`)); !errors.Is(err, ErrToolDenied) {
		t.Fatalf("deny-list precedence error = %v", err)
	}

	mutable := New(shared)
	if _, err := mutable.Call(context.Background(), "jobs.cancel", json.RawMessage(`{"jobId":"job-a"}`)); !errors.Is(err, ErrConfirmationRequired) || !strings.Contains(err.Error(), DestructiveConfirmation("jobs.cancel")) {
		t.Fatalf("missing destructive confirmation error = %v", err)
	}
	confirmed, err := mutable.Call(context.Background(), "jobs.cancel", json.RawMessage(
		`{"jobId":"job-a","confirm":"`+DestructiveConfirmation("jobs.cancel")+`"}`))
	confirmedJSON, _ := json.Marshal(confirmed)
	if err != nil || !strings.Contains(string(confirmedJSON), `"id":"job-a"`) ||
		!strings.Contains(string(confirmedJSON), `"state":"cancelled"`) || shared.cancelled != "job-a" {
		t.Fatalf("confirmed cancellation = %#v, %v, cancelled=%q", confirmed, err, shared.cancelled)
	}

	sensitive := &fakeSensitive{}
	restricted := New(shared, Options{Sensitive: sensitive})
	if _, err := restricted.Call(context.Background(), "credentials.invoke", json.RawMessage(`{"operation":"import","confirmSensitive":"proof"}`)); !errors.Is(err, ErrSensitiveOperation) {
		t.Fatalf("disabled sensitive operation error = %v", err)
	}
	enabled := New(shared, Options{Sensitive: sensitive, AllowSensitive: true, SensitiveConfirm: "proof"})
	if _, err := enabled.Call(context.Background(), "credentials.invoke", json.RawMessage(`{"operation":"import","confirmSensitive":"wrong"}`)); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("wrong sensitive confirmation error = %v", err)
	}
	value, err := enabled.Call(context.Background(), "credentials.invoke", json.RawMessage(`{"operation":"import","confirmSensitive":"proof","payload":{"access_token":"secret"}}`))
	if err != nil || sensitive.operation != "import" {
		t.Fatalf("sensitive result = %#v, %v, operation=%q", value, err, sensitive.operation)
	}
	encoded, _ := json.Marshal(value)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("sensitive result leaked: %s", encoded)
	}
}

func TestExportOutputMustStayWithinConfiguredAllowedRoots(t *testing.T) {
	root := t.TempDir()
	shared := &fakeApplication{downloadJob: domain.Job{ID: "export-a", Kind: "export", State: domain.JobQueued}}
	adapter := New(shared, Options{AllowedRoots: []string{root}, DefaultOutputRoot: filepath.Join(root, "default-exports")})

	inside := filepath.Join(root, "exports")
	if _, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(
		`{"format":"markdown","outputRoot":`+strconv.Quote(inside)+`}`)); err != nil {
		t.Fatalf("allowed export error = %v", err)
	}
	if shared.exportRequest.OutputRoot != inside {
		t.Fatalf("export output root = %q", shared.exportRequest.OutputRoot)
	}
	if shared.exportRequest.OutputAuthorization == nil || shared.exportRequest.OutputAuthorization.Root != root ||
		shared.exportRequest.OutputAuthorization.RelativePath != "exports" {
		t.Fatalf("export authorization = %#v", shared.exportRequest.OutputAuthorization)
	}
	if _, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(`{"format":"markdown"}`)); err != nil {
		t.Fatalf("default export error = %v", err)
	}
	if shared.exportRequest.OutputRoot != filepath.Join(root, "default-exports") {
		t.Fatalf("default export output root = %q", shared.exportRequest.OutputRoot)
	}
	if shared.exportRequest.OutputAuthorization == nil || shared.exportRequest.OutputAuthorization.RelativePath != "default-exports" {
		t.Fatalf("default export authorization = %#v", shared.exportRequest.OutputAuthorization)
	}

	outside := filepath.Join(filepath.Dir(root), "outside")
	if _, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(
		`{"format":"markdown","outputRoot":`+strconv.Quote(outside)+`}`)); err == nil || !strings.Contains(err.Error(), "outside configured allowed roots") {
		t.Fatalf("outside export error = %v", err)
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(filepath.Dir(root), link); err == nil {
		if _, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(
			`{"format":"markdown","outputRoot":`+strconv.Quote(filepath.Join(link, "escaped"))+`}`)); err == nil {
			t.Fatal("symlink escape was accepted")
		}
	}

	disabled := New(shared)
	if _, err := disabled.Call(context.Background(), "exports.start", json.RawMessage(
		`{"format":"markdown","outputRoot":`+strconv.Quote(inside)+`}`)); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("unconfigured root error = %v", err)
	}
}

func TestExportStartRejectsNullArgumentsInsteadOfPanicking(t *testing.T) {
	root := t.TempDir()
	shared := &fakeApplication{}
	adapter := New(shared, Options{AllowedRoots: []string{root}, DefaultOutputRoot: filepath.Join(root, "exports")})
	if _, err := adapter.Call(context.Background(), "exports.start", json.RawMessage(`null`)); err == nil ||
		!strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("exports.start(null) error=%v", err)
	}
}

func TestServerFramingIsolationMalformedBoundsAndEOF(t *testing.T) {
	shared := &fakeApplication{storage: domain.StorageStatus{DatabaseAvailable: true, Articles: 7}}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"storage.status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	server := NewServer(New(shared, Options{Version: "test-version", Profile: "profile-a", MaxMessageBytes: 2048}))
	if err := server.Serve(context.Background(), strings.NewReader(input), stdout, stderr); err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyLines(stdout.String())
	if len(lines) != 3 {
		t.Fatalf("stdout lines = %d: %s", len(lines), stdout.String())
	}
	for _, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("stdout contained non-protocol output %q: %v", line, err)
		}
		if response["jsonrpc"] != "2.0" {
			t.Fatalf("response = %#v", response)
		}
	}
	if !strings.Contains(lines[0], `"remoteOAuth":false`) || !strings.Contains(lines[0], `"localOnly":true`) ||
		!strings.Contains(lines[1], `"articles":7`) || !strings.Contains(lines[2], `"code":-32700`) {
		t.Fatalf("responses = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "mcp:") || strings.Contains(stdout.String(), "server") && strings.Contains(stdout.String(), "log") {
		t.Fatalf("stdout was polluted: %s", stdout.String())
	}

	oversizedOutput := &bytes.Buffer{}
	oversizedLogs := &bytes.Buffer{}
	oversized := `{"jsonrpc":"2.0","id":9,"method":"ping","padding":"` + strings.Repeat("x", 256) + `"}` + "\n"
	if err := NewServer(New(shared, Options{MaxMessageBytes: 96})).Serve(context.Background(), strings.NewReader(oversized), oversizedOutput, oversizedLogs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(oversizedOutput.String(), "maximum message size") || !strings.Contains(oversizedLogs.String(), "oversized") {
		t.Fatalf("oversized stdout=%q stderr=%q", oversizedOutput.String(), oversizedLogs.String())
	}
}

func TestServerNegotiatesUnsupportedProtocolVersion(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-01-01"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`,
	}, "\n") + "\n"
	stdout := &bytes.Buffer{}
	if err := NewServer(New(&fakeApplication{})).Serve(context.Background(), strings.NewReader(input), stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyLines(stdout.String())
	if len(lines) != 2 || !strings.Contains(lines[0], `"protocolVersion":"`+protocolVersion+`"`) ||
		strings.Contains(lines[0], `"error"`) || !strings.Contains(lines[1], "requires protocolVersion") {
		t.Fatalf("initialize responses = %s", stdout.String())
	}
}

func TestServerBoundsResponsesAndStopsWhenContextCancelsAClosableInput(t *testing.T) {
	shared := &fakeApplication{articles: domain.Page[domain.Article]{
		Items: []domain.Article{{ID: "article-large", Title: strings.Repeat("large-title-", 100)}}, Total: 1,
	}}
	responseOutput := &bytes.Buffer{}
	if err := NewServer(New(shared, Options{MaxMessageBytes: 256})).Serve(context.Background(), strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"articles.query","arguments":{}}}`+"\n"),
		responseOutput, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(responseOutput.String(), "response exceeds maximum message size") ||
		strings.Contains(responseOutput.String(), "large-title") {
		t.Fatalf("bounded response = %s", responseOutput.String())
	}

	reader := newBlockingReader()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- NewServer(New(shared)).Serve(ctx, reader, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after context cancellation")
	}
}

func TestToolResultDoesNotDuplicateStructuredContentInText(t *testing.T) {
	value := map[string]any{"body": strings.Repeat("x", 1024), "sha256": "fixture"}
	result := toolResult(value, nil)
	if result.StructuredContent == nil || len(result.Content) != 1 || result.Content[0].Text != "Tool completed successfully; use structuredContent for the result." {
		t.Fatalf("tool result=%#v", result)
	}
	if strings.Contains(result.Content[0].Text, strings.Repeat("x", 128)) {
		t.Fatalf("structured payload was duplicated in text content: %#v", result)
	}
}

func TestToolErrorsAreRedactedAndPackageHasNoRemoteOAuthDependency(t *testing.T) {
	shared := &fakeApplication{queryErr: errors.New("request https://mp.weixin.qq.com/s/x?access_token=raw-secret failed")}
	server := NewServer(New(shared))
	output := &bytes.Buffer{}
	if err := server.Serve(context.Background(), strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"articles.query","arguments":{}}}`+"\n"), output, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "raw-secret") || !strings.Contains(output.String(), "[REDACTED]") {
		t.Fatalf("tool error output = %s", output.String())
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"internal/oauth", "internal/legacyremote", "internal/mcpclient", "golang.org/x/oauth2", "StreamableHTTP"} {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("%s contains remote dependency %q", entry.Name(), forbidden)
			}
		}
	}
}

func TestLocalStdioClientConformsToInitializeListCallAndShutdown(t *testing.T) {
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()
	shared := &fakeApplication{storage: domain.StorageStatus{DatabaseAvailable: true, Articles: 3}}
	server := NewServer(New(shared, Options{Version: "conformance"}))
	serverDone := make(chan error, 1)
	go func() {
		err := server.Serve(context.Background(), serverReader, serverWriter, &bytes.Buffer{})
		_ = serverWriter.Close()
		serverDone <- err
	}()

	requests := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"conformance-client","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"storage.status","arguments":{}}}`,
	}, "\n") + "\n"
	if _, err := io.WriteString(clientWriter, requests); err != nil {
		t.Fatal(err)
	}
	if err := clientWriter.Close(); err != nil {
		t.Fatal(err)
	}
	responses, err := io.ReadAll(clientReader)
	if err != nil {
		t.Fatal(err)
	}
	lines := nonEmptyLines(string(responses))
	if len(lines) != 3 || !strings.Contains(lines[0], `"protocolVersion":"2025-06-18"`) ||
		!strings.Contains(lines[1], `"storage.status"`) || !strings.Contains(lines[2], `"articles":3`) {
		t.Fatalf("conformance responses = %s", responses)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("server error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("local MCP server did not terminate after SDK client close")
	}
}

func nonEmptyLines(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, part)
		}
	}
	return result
}

type fakeSensitive struct{ operation string }

func (sensitive *fakeSensitive) InvokeSensitive(_ context.Context, operation string, _ json.RawMessage) (any, error) {
	sensitive.operation = operation
	return map[string]any{"access_token": "secret", "status": "accepted"}, nil
}

type fakeApplication struct {
	articles        domain.Page[domain.Article]
	articleQuery    domain.ArticleQuery
	downloadJob     domain.Job
	downloadRequest domain.DownloadRequest
	exportRequest   domain.ExportRequest
	albumAccount    domain.AccountID
	albumID         domain.AlbumID
	job             domain.Job
	cancelled       domain.JobID
	storage         domain.StorageStatus
	queryErr        error
}

var _ application.Application = (*fakeApplication)(nil)

func (*fakeApplication) RuntimeStatus(context.Context) (domain.RuntimeStatus, error) {
	return domain.RuntimeStatus{}, nil
}
func (*fakeApplication) BeginLogin(context.Context, string) (wechat.LoginFlow, error) {
	return wechat.LoginFlow{}, nil
}
func (*fakeApplication) PollLogin(context.Context) (wechat.PollResult, error) {
	return wechat.PollResult{}, nil
}
func (*fakeApplication) CompleteLogin(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (*fakeApplication) SessionStatus(context.Context) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (*fakeApplication) ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error) {
	return nil, nil
}
func (*fakeApplication) SwitchAccount(context.Context, string) (wechat.Session, error) {
	return wechat.Session{}, nil
}
func (*fakeApplication) Logout(context.Context) error { return nil }
func (*fakeApplication) SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (*fakeApplication) ResolveAccountName(context.Context, string) (string, error) { return "", nil }
func (*fakeApplication) ResolveAccountFromArticle(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (*fakeApplication) AccountDetails(context.Context, string) (wechat.AccountDetails, error) {
	return wechat.AccountDetails{}, nil
}
func (*fakeApplication) AuthorInfo(context.Context, string) (wechat.AuthorInfo, error) {
	return wechat.AuthorInfo{}, nil
}
func (*fakeApplication) ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	return wechat.ArticlePage{}, nil
}
func (*fakeApplication) SaveAccount(context.Context, domain.Account) (domain.Account, error) {
	return domain.Account{}, nil
}
func (*fakeApplication) UpdateAccount(context.Context, domain.Account) (domain.Account, error) {
	return domain.Account{}, nil
}
func (*fakeApplication) GetAccount(context.Context, domain.AccountID) (domain.Account, error) {
	return domain.Account{}, nil
}
func (*fakeApplication) GetAccountByFakeID(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (*fakeApplication) QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (*fakeApplication) ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error) {
	return domain.AccountManifest{}, nil
}
func (*fakeApplication) ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error) {
	return domain.AccountImportReport{}, nil
}
func (*fakeApplication) DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error) {
	return domain.AccountDeleteReport{}, nil
}
func (application *fakeApplication) QueryArticles(_ context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	application.articleQuery = query
	return application.articles, application.queryErr
}
func (*fakeApplication) SaveArticleQuery(_ context.Context, name string, query domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	return domain.SavedArticleQuery{Name: name, Query: query}, nil
}
func (*fakeApplication) ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error) {
	return nil, nil
}
func (*fakeApplication) DeleteSavedArticleQuery(context.Context, string) (bool, error) {
	return false, nil
}
func (*fakeApplication) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return domain.Page[domain.Album]{}, nil
}
func (application *fakeApplication) SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error) {
	return application.downloadJob, nil
}
func (application *fakeApplication) SynchronizeAlbum(_ context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	application.albumAccount = accountID
	application.albumID = albumID
	return application.downloadJob, nil
}
func (application *fakeApplication) StartDownload(_ context.Context, request domain.DownloadRequest) (domain.Job, error) {
	application.downloadRequest = request
	return application.downloadJob, nil
}
func (application *fakeApplication) StartExport(_ context.Context, request domain.ExportRequest) (domain.Job, error) {
	application.exportRequest = request
	return application.downloadJob, nil
}
func (application *fakeApplication) GetJob(context.Context, domain.JobID) (domain.Job, error) {
	return application.job, nil
}
func (*fakeApplication) QueryJobs(context.Context, domain.JobQuery) (domain.Page[domain.Job], error) {
	return domain.Page[domain.Job]{}, nil
}
func (application *fakeApplication) CancelJob(_ context.Context, id domain.JobID) (domain.Job, error) {
	application.cancelled = id
	return application.job, nil
}
func (application *fakeApplication) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return application.storage, nil
}
func (*fakeApplication) DiscoverBrowser(context.Context) (runtimeenv.Browser, error) {
	return runtimeenv.Browser{}, nil
}
func (*fakeApplication) ProcessSignals() <-chan os.Signal { return make(chan os.Signal) }

var _ = time.Second

type blockingReader struct {
	done chan struct{}
	once sync.Once
}

func newBlockingReader() *blockingReader { return &blockingReader{done: make(chan struct{})} }

func (reader *blockingReader) Read([]byte) (int, error) {
	<-reader.done
	return 0, io.EOF
}

func (reader *blockingReader) Close() error {
	reader.once.Do(func() { close(reader.done) })
	return nil
}
