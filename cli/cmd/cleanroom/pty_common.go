package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

func runTUISmoke(ctx context.Context, binary string, environment []string) (map[string]string, error) {
	harness, err := newCandidatePTYHarness(100, 30)
	if err != nil {
		return nil, err
	}
	defer harness.close()
	if err := harness.start(ctx, binary, candidateEnvironment(append(append([]string(nil), environment...),
		"TERM=xterm-256color", "LANG=en_US.UTF-8"))); err != nil {
		return nil, fmt.Errorf("start candidate TUI in PTY: %w", err)
	}
	transcript := &lockedBuffer{}
	readDone := make(chan error, 1)
	go func() {
		_, readErr := io.Copy(transcript, harness)
		readDone <- readErr
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- harness.wait() }()
	if err := awaitCandidatePTYText(ctx, transcript, "WeChat Article Workspace", 25*time.Second); err != nil {
		return nil, err
	}
	if err := awaitCandidatePTYText(ctx, transcript, "Profile and session", 25*time.Second); err != nil {
		return nil, fmt.Errorf("initial home view: %w", err)
	}
	navigationOffset := transcript.Len()
	if _, err := harness.Write([]byte{'\t'}); err != nil {
		return nil, err
	}
	if err := awaitCandidatePTYTextAfter(ctx, transcript, "2 Accounts", navigationOffset, 25*time.Second); err != nil {
		return nil, fmt.Errorf("accounts navigation: %w", err)
	}
	if err := harness.resize(48, 16); err != nil {
		return nil, err
	}
	resizeOffset := transcript.Len()
	if err := awaitCandidatePTYTextAfter(ctx, transcript, "2 Accounts", resizeOffset, 25*time.Second); err != nil {
		return nil, fmt.Errorf("resize redraw: %w", err)
	}
	returnOffset := transcript.Len()
	if _, err := harness.Write([]byte("\x1b[D")); err != nil {
		return nil, err
	}
	if err := awaitCandidatePTYTextAfter(ctx, transcript, "Profile and session", returnOffset, 25*time.Second); err != nil {
		return nil, fmt.Errorf("resize and reverse navigation: %w", err)
	}
	if _, err := harness.Write([]byte{'q'}); err != nil {
		return nil, err
	}
	select {
	case waitErr := <-waitDone:
		if waitErr != nil {
			return nil, fmt.Errorf("candidate TUI did not exit cleanly: %w", waitErr)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		return nil, errors.New("candidate TUI did not exit after q")
	}
	_ = harness.close()
	select {
	case readErr := <-readDone:
		if readErr != nil && !isPTYCompletionError(readErr) {
			return nil, readErr
		}
	case <-time.After(5 * time.Second):
		return nil, errors.New("candidate PTY transcript reader did not stop")
	}
	return map[string]string{"pty": "native", "candidateBinary": "true", "navigation": "passed", "resize": "passed"}, nil
}

type candidatePTYHarness interface {
	io.ReadWriter
	start(context.Context, string, []string) error
	resize(int, int) error
	wait() error
	close() error
}

type lockedBuffer struct {
	mu sync.RWMutex
	b  bytes.Buffer
}

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.b.Write(value)
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return buffer.b.String()
}

func (buffer *lockedBuffer) Len() int {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return buffer.b.Len()
}

func awaitCandidatePTYText(ctx context.Context, transcript *lockedBuffer, expected string, timeout time.Duration) error {
	return awaitCandidatePTYTextAfter(ctx, transcript, expected, 0, timeout)
}

func awaitCandidatePTYTextAfter(ctx context.Context, transcript *lockedBuffer, expected string, offset int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		value := transcript.String()
		if offset < 0 {
			offset = 0
		}
		if offset > len(value) {
			offset = len(value)
		}
		if strings.Contains(value[offset:], expected) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("candidate PTY transcript missing %q", expected)
		case <-ticker.C:
		}
	}
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
