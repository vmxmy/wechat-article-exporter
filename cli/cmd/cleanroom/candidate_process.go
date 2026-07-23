package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const candidateProcessWaitLimit = 5 * time.Second

func runCandidateProcess(ctx context.Context, command *exec.Cmd) error {
	configureCandidateProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	tree, attachErr := attachCandidateProcessTree(command.Process)
	if attachErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("attach candidate process tree: %w", attachErr)
	}
	defer tree.Close()
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	select {
	case err := <-waitDone:
		return err
	case <-ctx.Done():
		killErr := tree.Kill()
		select {
		case waitErr := <-waitDone:
			return &candidateContextError{contextErr: ctx.Err(), killErr: killErr, waitErr: waitErr}
		case <-time.After(candidateProcessWaitLimit):
			_ = command.Process.Kill()
			return errors.Join(ctx.Err(), killErr, errors.New("candidate process tree did not exit after cancellation"))
		}
	}
}

type candidateContextError struct {
	contextErr error
	killErr    error
	waitErr    error
}

func (err *candidateContextError) Error() string {
	return errors.Join(err.contextErr, err.killErr, err.waitErr).Error()
}

func (err *candidateContextError) Unwrap() error { return err.contextErr }

type candidateProcessTree interface {
	Kill() error
	Close() error
}
