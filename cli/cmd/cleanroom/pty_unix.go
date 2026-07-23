//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"

	pty "github.com/aymanbagabas/go-pty"
)

type unixCandidatePTY struct {
	p         pty.Pty
	c         *pty.Cmd
	mu        sync.Mutex
	waited    bool
	closeOnce sync.Once
	closeErr  error
}

func newCandidatePTYHarness(width, height int) (candidatePTYHarness, error) {
	terminal, err := pty.New()
	if err != nil {
		return nil, err
	}
	if err := terminal.Resize(width, height); err != nil {
		_ = terminal.Close()
		return nil, err
	}
	return &unixCandidatePTY{p: terminal}, nil
}

func (harness *unixCandidatePTY) start(ctx context.Context, binary string, environment []string) error {
	harness.c = harness.p.CommandContext(ctx, binary)
	harness.c.Env = environment
	harness.c.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	harness.c.Cancel = func() error {
		if harness.c == nil || harness.c.Process == nil {
			return nil
		}
		err := syscall.Kill(-harness.c.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return harness.c.Start()
}

func (harness *unixCandidatePTY) Read(value []byte) (int, error)  { return harness.p.Read(value) }
func (harness *unixCandidatePTY) Write(value []byte) (int, error) { return harness.p.Write(value) }
func (harness *unixCandidatePTY) resize(width, height int) error {
	return harness.p.Resize(width, height)
}
func (harness *unixCandidatePTY) wait() error {
	err := harness.c.Wait()
	harness.mu.Lock()
	harness.waited = true
	harness.mu.Unlock()
	return err
}
func (harness *unixCandidatePTY) close() error {
	harness.closeOnce.Do(func() {
		// Wait only reaps the direct Bubble Tea process. It does not prove a
		// candidate-spawned background child left the session, so always clear
		// the PTY session/process group before releasing its descriptors.
		if harness.c != nil && harness.c.Process != nil {
			if err := syscall.Kill(-harness.c.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				harness.closeErr = errors.Join(harness.closeErr, err)
			}
		}
		if harness.p != nil {
			if err := harness.p.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
				harness.closeErr = errors.Join(harness.closeErr, err)
			}
		}
	})
	return harness.closeErr
}
