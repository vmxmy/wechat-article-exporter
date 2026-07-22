package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func testCLIWorkspaceRealPTYNavigationResizeAndCleanExit(t *testing.T) {
	helper := buildWorkspacePTYHelper(t)
	harness := newWorkspacePTYHarness(t)
	defer harness.close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	if err := harness.start(ctx, helper, append(os.Environ(),
		"WECHAT_ARTICLE_PTY_ROOT="+t.TempDir(),
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
	)); err != nil {
		t.Fatalf("start standalone workspace helper: %v", err)
	}

	transcript := &synchronizedBuffer{}
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(transcript, harness)
		readDone <- err
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- harness.wait() }()

	awaitPTYText(t, transcript, "WeChat Article Workspace", 25*time.Second)
	awaitPTYText(t, transcript, "Profile and session", 25*time.Second)
	if _, err := harness.Write([]byte{'\t'}); err != nil {
		t.Fatal(err)
	}
	awaitPTYText(t, transcript, "No local results.", 25*time.Second)
	if err := harness.resize(48, 16); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.Write([]byte("\x1b[Z")); err != nil {
		t.Fatal(err)
	}
	awaitPTYText(t, transcript, "Profile and session", 25*time.Second)
	if _, err := harness.Write([]byte{'q'}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("workspace did not exit cleanly: %v\ntranscript:\n%s", err, transcript.String())
		}
	case <-ctx.Done():
		t.Fatalf("workspace PTY smoke timed out: %v\ntranscript:\n%s", ctx.Err(), transcript.String())
	}
	closeErr := harness.close()
	select {
	case err := <-readDone:
		if err != nil && !errors.Is(err, os.ErrClosed) && !isPTYCompletionError(err) {
			t.Fatalf("read PTY transcript: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PTY transcript reader did not stop")
	}
	if closeErr != nil && !isPTYCompletionError(closeErr) {
		t.Fatalf("close PTY after workspace exit: %v", closeErr)
	}
}

func buildWorkspacePTYHelper(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve workspace PTY test source path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	executable := filepath.Join(t.TempDir(), "workspace-pty-helper")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	command := exec.Command("go", "build", "-trimpath", "-o", executable, "./internal/app/testdata/workspace-pty-helper")
	command.Dir = moduleRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build standalone workspace PTY helper: %v\n%s", err, output)
	}
	return executable
}

type synchronizedBuffer struct {
	mu     sync.RWMutex
	buffer bytes.Buffer
}

func (buffer *synchronizedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(value)
}

func (buffer *synchronizedBuffer) String() string {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return buffer.buffer.String()
}

func awaitPTYText(t *testing.T, transcript *synchronizedBuffer, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(transcript.String(), expected) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("PTY transcript missing %q after %s (%d bytes):\n%s", expected, timeout, len(transcript.String()), transcript.String())
}

func isPTYCompletionError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "input/output error") ||
		strings.Contains(message, "pipe has been ended") ||
		strings.Contains(message, "file already closed") ||
		strings.Contains(message, "handle is invalid") ||
		strings.Contains(message, "invalid handle")
}

func failOrSkipUnavailablePTY(t *testing.T, operation string, err error) {
	t.Helper()
	if runtime.GOOS == "windows" && isUnavailableConPTYError(err) {
		t.Skipf("Windows ConPTY is unavailable during %s: %v", operation, err)
	}
	t.Fatalf("%s pseudo-terminal: %v", operation, err)
}

func isUnavailableConPTYError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not supported") ||
		strings.Contains(message, "not implemented") ||
		strings.Contains(message, "createpseudoconsole") ||
		strings.Contains(message, "resizepseudoconsole") ||
		strings.Contains(message, "pseudo console") ||
		strings.Contains(message, "pseudoconsole")
}
