package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
