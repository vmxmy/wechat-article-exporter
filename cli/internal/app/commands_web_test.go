package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	localweb "github.com/wechat-article/wechat-article-exporter/cli/internal/web"
)

func TestWebCommandPrintsOnlyLocalURLAndHonorsNoOpen(t *testing.T) {
	applicationAdapter, _, stderr := newTestApp(t)
	urlWritten := make(chan string, 1)
	applicationAdapter.stdout = webURLWriter{urlWritten: urlWritten}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := false
	applicationAdapter.webOpenBrowser = func(context.Context, string) error {
		opened = true
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- applicationAdapter.Execute(ctx, []string{"web", "--no-open"}) }()
	url := waitForWebURL(t, urlWritten)
	if opened {
		t.Fatal("--no-open launched a browser")
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.Contains(url, "?token=") {
		t.Fatalf("stdout URL = %q", url)
	}
	if strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("stderr = %q; want no log output", stderr.String())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("web command cancellation = %v", err)
	}
}

func TestWebCommandOpensBrowserWithoutNoOpen(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	urlWritten := make(chan string, 1)
	applicationAdapter.stdout = webURLWriter{urlWritten: urlWritten}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan string, 1)
	applicationAdapter.webOpenBrowser = func(_ context.Context, target string) error {
		opened <- target
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- applicationAdapter.Execute(ctx, []string{"web"}) }()
	url := waitForWebURL(t, urlWritten)
	if got := <-opened; got != url {
		t.Fatalf("opened URL = %q; stdout URL = %q", got, url)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("web command cancellation = %v", err)
	}
}

func TestWebCommandRejectsJSONOutput(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	err := applicationAdapter.Execute(context.Background(), []string{"web", "--json"})
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "does not support --json") {
		t.Fatalf("web --json error = %v", err)
	}
}

func TestWebCommandRequiresTheActiveExportLibrary(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	applicationAdapter.active = nil
	err := applicationAdapter.Execute(context.Background(), []string{"web", "--no-open"})
	if err == nil || !strings.Contains(err.Error(), "active export workspace is unavailable") {
		t.Fatalf("web command error = %v", err)
	}
}

func TestWebCommandInjectsMaintenanceFacades(t *testing.T) {
	applicationAdapter, _, _ := newTestApp(t)
	maintenance, storageMaintenance := newWebMaintenance(applicationAdapter)
	local, err := localweb.New(localweb.Options{
		Application: applicationAdapter.core,
		Exports:     application.NewWorkspaceExports(applicationAdapter.core, applicationAdapter.active.Library),
		Maintenance: maintenance, StorageMaintenance: storageMaintenance,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if err := local.Start(); err != nil {
		t.Fatal(err)
	}
	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	serveDone := make(chan error, 1)
	go func() { serveDone <- local.Serve(serveCtx) }()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.Get(local.URL())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d", response.StatusCode)
	}
	base := strings.TrimSuffix(strings.Split(local.URL(), "?")[0], "/")
	for _, path := range []string{
		"/api/v1/settings/credentials",
		"/api/v1/settings/proxies",
		"/api/v1/settings/preferences",
		"/api/v1/maintenance/integrity",
		"/api/v1/maintenance/diagnostics",
	} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode == http.StatusServiceUnavailable {
			t.Fatalf("GET %s returned unavailable; maintenance facade was not injected", path)
		}
	}
	cancelServe()
	if err := <-serveDone; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("serve: %v", err)
	}
}

type webURLWriter struct {
	urlWritten chan<- string
}

func (w webURLWriter) Write(p []byte) (int, error) {
	url := strings.TrimSpace(string(p))
	if url == "" {
		return 0, errors.New("local URL output is empty")
	}
	select {
	case w.urlWritten <- url:
		return len(p), nil
	default:
		return 0, errors.New("local URL was written more than once")
	}
}

func waitForWebURL(t *testing.T, urlWritten <-chan string) string {
	t.Helper()
	select {
	case url := <-urlWritten:
		return url
	case <-time.After(5 * time.Second):
		t.Fatal("web command did not print its local URL")
		return ""
	}
}
