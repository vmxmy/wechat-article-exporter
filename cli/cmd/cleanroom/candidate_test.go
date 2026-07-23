package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCandidateEnvironmentIsAllowlistedAndIsolated(t *testing.T) {
	t.Setenv("CLOUD_TOKEN", "must-not-leak")
	t.Setenv("HTTPS_PROXY", "http://must-not-leak.invalid")
	t.Setenv("WECHAT_ARTICLE_PROFILE", "host-profile")
	portableRoot := filepath.Join(t.TempDir(), "portable")
	environment, err := candidateEnvironment([]string{
		"WECHAT_ARTICLE_PORTABLE_ROOT=" + portableRoot,
		"WECHAT_ARTICLE_CLEAN_ROOM=1",
		"PATH=/clean/path",
	})
	if err != nil {
		t.Fatal(err)
	}
	normalizedRoot := environmentEntryValue(environment, "WECHAT_ARTICLE_PORTABLE_ROOT")
	joined := strings.Join(environment, "\n")
	for _, forbidden := range []string{"CLOUD_TOKEN=", "HTTPS_PROXY=", "WECHAT_ARTICLE_PROFILE=host-profile"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("candidate environment leaked %q: %s", forbidden, joined)
		}
	}
	for _, required := range []string{"WECHAT_ARTICLE_CLEAN_ROOM=1", "PATH=/clean/path", "HOME=" + filepath.Join(normalizedRoot, "clean-room-home")} {
		if !strings.Contains(joined, required) {
			t.Fatalf("candidate environment missing %q: %s", required, joined)
		}
	}
	for _, name := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
		t.Setenv(name, `C:\fixture`)
		hostEnvironment, err := candidateEnvironment(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(hostEnvironment, "\n"), name+`=C:\fixture`) {
			t.Fatalf("candidate environment omitted browser discovery variable %q", name)
		}
	}
}

func environmentEntryValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func TestCandidateEnvironmentRejectsSymlinkedIsolationDirectory(t *testing.T) {
	root := t.TempDir()
	portableRoot := filepath.Join(root, "portable")
	if err := os.MkdirAll(portableRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(portableRoot, "clean-room-home")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := candidateEnvironment([]string{"WECHAT_ARTICLE_PORTABLE_ROOT=" + portableRoot}); err == nil ||
		(!strings.Contains(err.Error(), "real directory") && !strings.Contains(err.Error(), "not a directory")) {
		t.Fatalf("candidateEnvironment error = %v", err)
	}
}

func TestCandidateEnvironmentRejectsSymlinkedPortableRootAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "portable-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := candidateEnvironment([]string{"WECHAT_ARTICLE_PORTABLE_ROOT=" + filepath.Join(link, "nested")}); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("candidateEnvironment error = %v", err)
	}
}

func TestCandidateEnvironmentRejectsRelativeAndFilesystemRoot(t *testing.T) {
	for _, portableRoot := range []string{"relative-root", string(filepath.Separator)} {
		if _, err := candidateEnvironment([]string{"WECHAT_ARTICLE_PORTABLE_ROOT=" + portableRoot}); err == nil ||
			!strings.Contains(err.Error(), "absolute non-root") {
			t.Fatalf("portable root %q error = %v", portableRoot, err)
		}
	}
}

func TestBoundedBufferRejectsOversizedOutput(t *testing.T) {
	buffer := newBoundedBuffer(8)
	if written, err := buffer.Write([]byte("0123456789")); err != nil || written != 10 {
		t.Fatalf("Write = %d, %v", written, err)
	}
	if got := string(buffer.Bytes()); got != "01234567" || !buffer.Overflowed() {
		t.Fatalf("bounded buffer = %q overflow=%v", got, buffer.Overflowed())
	}
}

func TestCandidateRunnerReportsExecutionAndDecodeFailures(t *testing.T) {
	missing := candidateRunner{binary: filepath.Join(t.TempDir(), "missing")}
	if _, err := missing.runJSON(context.Background(), "status", "--json"); err == nil || !strings.Contains(err.Error(), "exited -1") {
		t.Fatalf("missing binary error = %v", err)
	}

	runner := candidateRunner{binary: helperShell(t)}
	result, err := runner.runJSON(context.Background(), "exit-empty")
	if err == nil || result.ExitCode == 0 || !strings.Contains(err.Error(), "decode candidate") {
		t.Fatalf("empty nonzero result=%#v error=%v", result, err)
	}
	if _, err := runner.runJSON(context.Background(), "malformed"); err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("malformed error = %v", err)
	}
	if _, err := runner.runJSON(context.Background(), "multiple"); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("multiple error = %v", err)
	}
	if _, err := runner.runJSON(context.Background(), "wrong-schema"); err == nil || !strings.Contains(err.Error(), "unsupported candidate schemaVersion") {
		t.Fatalf("wrong-schema error = %v", err)
	}
	if _, err := runner.runJSON(context.Background(), "unknown-field"); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}
	if _, err := runner.runJSON(context.Background(), "contradictory"); err == nil || !strings.Contains(err.Error(), "must not contain an error") {
		t.Fatalf("contradictory error = %v", err)
	}
	if _, err := runner.runJSON(context.Background(), "missing-data"); err == nil || !strings.Contains(err.Error(), "has no data") {
		t.Fatalf("missing-data error = %v", err)
	}
}

func TestCandidateRunnerCancellationIsBounded(t *testing.T) {
	runner := candidateRunner{binary: helperShell(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runner.runJSON(ctx, "hang")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("candidate cancellation took %s", time.Since(started))
	}
}

func helperShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell helper is Unix-only")
	}
	path := filepath.Join(t.TempDir(), "candidate-helper")
	body := []byte(`#!/bin/sh
case "$1" in
  exit-empty) exit 7 ;;
  malformed) printf '{bad'; exit 0 ;;
	  multiple) printf '{"schemaVersion":"wechat-article-cli/v1","success":true,"data":{}}\n{"schemaVersion":"wechat-article-cli/v1","success":true,"data":{}}\n'; exit 0 ;;
	  wrong-schema) printf '{"schemaVersion":"wechat-article-cli/v999","success":true,"data":{}}\n'; exit 0 ;;
	  unknown-field) printf '{"schemaVersion":"wechat-article-cli/v1","success":true,"data":{},"extra":true}\n'; exit 0 ;;
	  contradictory) printf '{"schemaVersion":"wechat-article-cli/v1","success":true,"data":{},"error":{"code":"bad"}}\n'; exit 0 ;;
	  missing-data) printf '{"schemaVersion":"wechat-article-cli/v1","success":true}\n'; exit 0 ;;
  hang) sleep 30 ;;
esac
`)
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCandidateProcessConfigurationCompiles(t *testing.T) {
	command := exec.Command("true")
	configureCandidateProcess(command)
}
