package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/web"
)

func TestLocalBrowserWorkspaceSharesTemporaryProfileWithCobraAndTUI(t *testing.T) {
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	instance, err := NewWithDependencies(ctx, strings.NewReader(""), &stdout, &stderr, Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: t.TempDir()},
		Secrets:     secrets.NewMemoryStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })

	if err := instance.Execute(ctx, []string{"account", "add", "fixture-fakeid", "--name", "Cobra created", "--json"}); err != nil {
		t.Fatalf("Cobra account add: %v stderr=%s", err, stderr.String())
	}
	created := decodeCobraAccount(t, stdout.Bytes())
	if created.ID == "" || created.Name != "Cobra created" {
		t.Fatalf("Cobra created account = %#v", created)
	}

	assertWorkspaceShowsAccount(t, instance.core, "Cobra created")

	server, client, base := startLocalBrowserWorkspace(t, instance.core)
	t.Cleanup(func() { _ = server.Close() })
	accountsBody := getLocalBrowser(t, client, base+"/api/v1/accounts?limit=20")
	if strings.Contains(accountsBody, "library.sqlite") || strings.Contains(accountsBody, instance.active.Profile.Paths.Database) {
		t.Fatalf("browser account response exposed non-workspace internals: %s", accountsBody)
	}
	accounts := decodeBrowserAccounts(t, accountsBody)
	if len(accounts.Items) != 1 || accounts.Items[0].ID != created.ID || accounts.Items[0].Name != "Cobra created" {
		t.Fatalf("browser accounts = %#v, want Cobra account %#v", accounts, created)
	}

	csrf := localBrowserCSRF(t, client, base)
	response := postLocalBrowserJSON(t, client, base+"/api/v1/accounts/"+string(created.ID), csrf,
		`{"name":"Browser updated"}`, http.MethodPatch)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("browser account update status=%d body=%s", response.StatusCode, body)
	}
	var updatedEnvelope struct {
		Data domain.Account `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&updatedEnvelope); err != nil {
		t.Fatal(err)
	}
	if updatedEnvelope.Data.ID != created.ID || updatedEnvelope.Data.Name != "Browser updated" {
		t.Fatalf("browser update = %#v, want durable account ID %q with updated name", updatedEnvelope.Data, created.ID)
	}

	updated, err := instance.core.GetAccount(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Browser updated" {
		t.Fatalf("application account after browser update = %#v", updated)
	}
	assertWorkspaceShowsAccount(t, instance.core, "Browser updated")
}

func decodeCobraAccount(t *testing.T, output []byte) domain.Account {
	t.Helper()
	var envelope struct {
		Data domain.Account `json:"data"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		t.Fatalf("decode Cobra account output %q: %v", output, err)
	}
	return envelope.Data
}

func assertWorkspaceShowsAccount(t *testing.T, core application.Application, name string) {
	t.Helper()
	model := tui.NewWorkspace(tui.WorkspaceOptions{Context: context.Background(), Application: core, Plain: true, PageSize: 20})
	message := model.Init()()
	updated, _ := model.Update(message)
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	if view := updated.View(); !strings.Contains(view, name) {
		t.Fatalf("TUI workspace did not show %q:\n%s", name, view)
	}
}

func startLocalBrowserWorkspace(t *testing.T, core application.Application) (*web.Server, *http.Client, string) {
	t.Helper()
	server, err := web.New(web.Options{Application: core})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(context.Background()) }()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.Get(server.URL())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusOK {
		t.Fatalf("browser bootstrap status=%d", response.StatusCode)
	}
	return server, client, strings.TrimSuffix(strings.Split(server.URL(), "?")[0], "/")
}

func getLocalBrowser(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", target, response.StatusCode, body)
	}
	return string(body)
}

func decodeBrowserAccounts(t *testing.T, body string) domain.Page[domain.Account] {
	t.Helper()
	var payload struct {
		Items  []domain.Account `json:"items"`
		Total  int              `json:"total"`
		Offset int              `json:"offset"`
		Limit  int              `json:"limit"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return domain.Page[domain.Account]{Items: payload.Items, Total: payload.Total, Offset: payload.Offset, Limit: payload.Limit}
}

func localBrowserCSRF(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	var payload struct {
		Data struct {
			CSRF string `json:"csrfToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(getLocalBrowser(t, client, base+"/api/v1/status")), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.CSRF == "" {
		t.Fatal("browser status omitted CSRF token")
	}
	return payload.Data.CSRF
}

func postLocalBrowserJSON(t *testing.T, client *http.Client, target, csrf, body, method string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", strings.TrimSuffix(strings.Split(target, "/api/")[0], "/"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
