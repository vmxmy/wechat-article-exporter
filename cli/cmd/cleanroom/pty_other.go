//go:build !unix && !windows

package main

import (
	"context"
	"errors"
)

type unsupportedCandidatePTY struct{}

func newCandidatePTYHarness(int, int) (candidatePTYHarness, error) {
	return nil, errors.New("native clean-room PTY evidence is unsupported on this platform")
}

func (unsupportedCandidatePTY) Read([]byte) (int, error) {
	return 0, errors.New("unsupported candidate PTY")
}
func (unsupportedCandidatePTY) Write([]byte) (int, error) {
	return 0, errors.New("unsupported candidate PTY")
}
func (unsupportedCandidatePTY) start(context.Context, string, []string) error {
	return errors.New("unsupported candidate PTY")
}
func (unsupportedCandidatePTY) resize(int, int) error { return errors.New("unsupported candidate PTY") }
func (unsupportedCandidatePTY) wait() error           { return errors.New("unsupported candidate PTY") }
func (unsupportedCandidatePTY) close() error          { return nil }
