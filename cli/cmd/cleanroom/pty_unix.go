//go:build !windows

package main

import (
	"context"

	pty "github.com/aymanbagabas/go-pty"
)

type unixCandidatePTY struct {
	p pty.Pty
	c *pty.Cmd
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
	return harness.c.Start()
}

func (harness *unixCandidatePTY) Read(value []byte) (int, error)  { return harness.p.Read(value) }
func (harness *unixCandidatePTY) Write(value []byte) (int, error) { return harness.p.Write(value) }
func (harness *unixCandidatePTY) resize(width, height int) error {
	return harness.p.Resize(width, height)
}
func (harness *unixCandidatePTY) wait() error  { return harness.c.Wait() }
func (harness *unixCandidatePTY) close() error { return harness.p.Close() }
