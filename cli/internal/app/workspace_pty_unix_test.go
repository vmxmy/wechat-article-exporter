//go:build !windows

package app

import (
	"context"
	"io"
	"testing"

	pty "github.com/aymanbagabas/go-pty"
)

func TestCLIWorkspaceRealPTYNavigationResizeAndCleanExit(t *testing.T) {
	testCLIWorkspaceRealPTYNavigationResizeAndCleanExit(t)
}

type workspacePTYHarness struct {
	t *testing.T
	p pty.Pty
	c *pty.Cmd
}

func newWorkspacePTYHarness(t *testing.T) *workspacePTYHarness {
	t.Helper()
	terminal, err := pty.New()
	if err != nil {
		failOrSkipUnavailablePTY(t, "create", err)
	}
	if err := terminal.Resize(100, 30); err != nil {
		_ = terminal.Close()
		failOrSkipUnavailablePTY(t, "resize", err)
	}
	return &workspacePTYHarness{t: t, p: terminal}
}

func (harness *workspacePTYHarness) start(ctx context.Context, helper string, environment []string) error {
	harness.c = harness.p.CommandContext(ctx, helper)
	harness.c.Env = environment
	return harness.c.Start()
}

func (harness *workspacePTYHarness) Read(value []byte) (int, error)  { return harness.p.Read(value) }
func (harness *workspacePTYHarness) Write(value []byte) (int, error) { return harness.p.Write(value) }
func (harness *workspacePTYHarness) resize(width, height int) error {
	return harness.p.Resize(width, height)
}
func (harness *workspacePTYHarness) wait() error  { return harness.c.Wait() }
func (harness *workspacePTYHarness) close() error { return harness.p.Close() }

var _ io.ReadWriter = (*workspacePTYHarness)(nil)
