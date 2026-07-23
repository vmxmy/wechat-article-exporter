package app

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWebCommandPrintsOnlyLocalURLAndHonorsNoOpen(t *testing.T) {
	applicationAdapter, stdout, stderr := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := false
	applicationAdapter.webOpenBrowser = func(context.Context, string) error {
		opened = true
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- applicationAdapter.Execute(ctx, []string{"web", "--no-open"}) }()
	url := waitForWebURL(t, stdout)
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
	applicationAdapter, stdout, _ := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan string, 1)
	applicationAdapter.webOpenBrowser = func(_ context.Context, target string) error {
		opened <- target
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- applicationAdapter.Execute(ctx, []string{"web"}) }()
	url := waitForWebURL(t, stdout)
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

func waitForWebURL(t *testing.T, stdout interface{ String() string }) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if value := strings.TrimSpace(stdout.String()); value != "" {
			return value
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("web command did not print its local URL")
	return ""
}
