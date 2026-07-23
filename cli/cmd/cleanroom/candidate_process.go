package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

const candidateProcessWaitLimit = 5 * time.Second

func runCandidateProcess(ctx context.Context, command *exec.Cmd) (resultErr error) {
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
	defer func() {
		resultErr = errors.Join(resultErr, tree.Close())
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	select {
	case err := <-waitDone:
		tree.MarkExited()
		resultErr = err
		return resultErr
	case <-ctx.Done():
		select {
		case err := <-waitDone:
			tree.MarkExited()
			resultErr = err
			return resultErr
		default:
		}
		killErr := tree.Kill()
		deadline := time.NewTimer(candidateProcessWaitLimit)
		defer deadline.Stop()
		select {
		case waitErr := <-waitDone:
			tree.MarkExited()
			resultErr = &candidateContextError{contextErr: ctx.Err(), killErr: killErr, waitErr: waitErr}
			return resultErr
		case <-deadline.C:
			fallbackErr := command.Process.Kill()
			resultErr = errors.Join(ctx.Err(), killErr, fallbackErr, errors.New("candidate process tree did not exit after cancellation"))
			return resultErr
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
	MarkExited()
	Close() error
}
