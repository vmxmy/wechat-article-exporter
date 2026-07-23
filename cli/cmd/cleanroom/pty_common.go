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

func runTUISmoke(ctx context.Context, binary string, environment []string) (evidence map[string]string, resultErr error) {
	harnessContext, cancelHarness := context.WithCancel(ctx)
	defer cancelHarness()
	environment, err := candidateEnvironment(append(append([]string(nil), environment...),
		"TERM=xterm-256color", "LANG=en_US.UTF-8"))
	if err != nil {
		return nil, fmt.Errorf("configure candidate TUI environment: %w", err)
	}
	harness, err := newCandidatePTYHarness(100, 30)
	if err != nil {
		return nil, err
	}
	defer func() {
		cancelHarness()
		resultErr = errors.Join(resultErr, harness.close())
	}()
	if err := harness.start(harnessContext, binary, environment); err != nil {
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
	if err := awaitCandidatePTYTextAfter(ctx, transcript, "d discover", navigationOffset, 25*time.Second); err != nil {
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
	if err := harness.close(); err != nil {
		return nil, fmt.Errorf("close candidate PTY: %w", err)
	}
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
	mu       sync.RWMutex
	b        bytes.Buffer
	overflow bool
}

const maximumPTYTranscriptBytes = 8 << 20

func (buffer *lockedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	original := len(value)
	remaining := maximumPTYTranscriptBytes - buffer.b.Len()
	written := original
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = buffer.b.Write(value)
	}
	if written > remaining {
		written = max(remaining, 0)
		buffer.overflow = true
		return written, errors.New("candidate PTY transcript exceeded size limit")
	}
	return written, nil
}

func (buffer *lockedBuffer) Len() int {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return buffer.b.Len()
}

func (buffer *lockedBuffer) String() string {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return buffer.b.String()
}

func (buffer *lockedBuffer) Overflowed() bool {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return buffer.overflow
}

func (buffer *lockedBuffer) ContainsAfter(offset int, expected string) bool {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	value := buffer.b.Bytes()
	if offset < 0 {
		offset = 0
	}
	if offset > len(value) {
		offset = len(value)
	}
	return bytes.Contains(value[offset:], []byte(expected))
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
		if transcript.Overflowed() {
			return errors.New("candidate PTY transcript exceeded size limit")
		}
		if transcript.ContainsAfter(offset, expected) {
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
