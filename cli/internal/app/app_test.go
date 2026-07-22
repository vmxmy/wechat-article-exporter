package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
)

func TestHelpDocumentsStableCommandsAndStructuredInput(t *testing.T) {
	application, stdout, _ := newTestApp(t)
	if err := application.Execute(context.Background(), []string{"help"}); err != nil {
		t.Fatalf("Execute(help) error = %v", err)
	}
	output := stdout.String()
	for _, expected := range []string{"api", "article", "account", "album", "--server", "--json"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("help output missing %q:\n%s", expected, output)
		}
	}
}

func TestDryRunIsRedactedAndDoesNotNeedNetwork(t *testing.T) {
	application, stdout, _ := newTestApp(t)
	err := application.Execute(context.Background(), []string{
		"api", "call", "download_article",
		"--input", `{"url":"https://mp.weixin.qq.com/s/example","auth_key":"secret"}`,
		"--dry-run",
	})
	if err != nil {
		t.Fatalf("Execute(dry-run) error = %v", err)
	}
	if strings.Contains(stdout.String(), "secret") {
		t.Fatalf("dry-run leaked secret: %s", stdout.String())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("dry-run output is not JSON: %v", err)
	}
	if output["dryRun"] != true {
		t.Fatalf("dryRun = %#v", output["dryRun"])
	}
}

func TestAmbiguousInputAndCredentialedServerAreUsageErrors(t *testing.T) {
	application, _, _ := newTestApp(t)
	err := application.Execute(context.Background(), []string{"api", "call", "download_article", "--input", "{}", "--stdin"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "exactly one JSON input source") {
		t.Fatalf("ambiguous input error = %v, code = %d", err, ExitCode(err))
	}

	application, _, _ = newTestApp(t)
	err = application.Execute(context.Background(), []string{"status", "--server", "https://user:password@example.com"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "must not contain credentials") || strings.Contains(err.Error(), "password@example") {
		t.Fatalf("credentialed server error = %v, code = %d", err, ExitCode(err))
	}
}

func TestCobraInvocationErrorsUseExitCodeTwo(t *testing.T) {
	application, _, _ := newTestApp(t)
	err := application.Execute(context.Background(), []string{"--unknown"})
	if ExitCode(err) != 2 {
		t.Fatalf("unknown flag exit code = %d, error = %v", ExitCode(err), err)
	}
	application, _, _ = newTestApp(t)
	err = application.Execute(context.Background(), []string{"status", "extra"})
	if ExitCode(err) != 2 {
		t.Fatalf("extra argument exit code = %d, error = %v", ExitCode(err), err)
	}
}

func TestProcessContractReusesSavedTokenAndCallsDomainAlias(t *testing.T) {
	server := newMCPTestServer(t)
	application, stdout, _ := newTestApp(t)
	configPath := filepath.Join(t.TempDir(), "cli.json")
	application.store = config.NewStore(configPath)
	if err := application.store.Write(config.File{
		Server: server.URL,
		Tokens: &config.Tokens{AccessToken: "process-test-token", TokenType: "bearer", RefreshToken: "refresh-secret"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := application.Execute(context.Background(), []string{"article", "download", "https://mp.weixin.qq.com/s/example", "--format", "text"}); err != nil {
		t.Fatalf("Execute(article download) error = %v", err)
	}
	if strings.Contains(stdout.String(), "process-test-token") || strings.Contains(stdout.String(), "refresh-secret") {
		t.Fatalf("CLI output leaked tokens: %s", stdout.String())
	}
	var output mcp.CallToolResult
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("call output is not an MCP result: %v", err)
	}
	if len(output.Content) != 1 {
		t.Fatalf("content = %#v", output.Content)
	}
	text, ok := output.Content[0].(*mcp.TextContent)
	if !ok || text.Text != "text:https://mp.weixin.qq.com/s/example" {
		t.Fatalf("content[0] = %#v", output.Content[0])
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
}

func newTestApp(t *testing.T) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	application := New(strings.NewReader(""), stdout, stderr)
	application.store = config.NewStore(filepath.Join(t.TempDir(), "cli.json"))
	return application, stdout, stderr
}

func newMCPTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "process-test", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "download_article", Description: "download article"}, func(_ context.Context, _ *mcp.CallToolRequest, input struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Format + ":" + input.URL}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	testServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mcp" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer process-test-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(testServer.Close)
	return testServer
}
