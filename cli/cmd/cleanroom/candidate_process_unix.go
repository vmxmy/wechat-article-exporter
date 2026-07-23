//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

type unixCandidateProcessTree struct {
	process   *os.Process
	mu        sync.Mutex
	requested bool
	closeOnce sync.Once
	closeErr  error
}

func configureCandidateProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachCandidateProcessTree(process *os.Process) (candidateProcessTree, error) {
	return &unixCandidateProcessTree{process: process}, nil
}

func (tree *unixCandidateProcessTree) Kill() error {
	tree.mu.Lock()
	if tree.requested {
		tree.mu.Unlock()
		return nil
	}
	tree.requested = true
	tree.mu.Unlock()
	if tree.process == nil {
		return nil
	}
	err := syscall.Kill(-tree.process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// MarkExited records that the direct child was reaped. It deliberately does
// not suppress Close: a process-group leader can exit while background
// descendants in the same group continue to run.
func (tree *unixCandidateProcessTree) MarkExited() {}

func (tree *unixCandidateProcessTree) Close() error {
	tree.closeOnce.Do(func() { tree.closeErr = tree.Kill() })
	return tree.closeErr
}
