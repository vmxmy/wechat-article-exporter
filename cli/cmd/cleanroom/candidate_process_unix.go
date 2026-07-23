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
	exited    bool
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
	if tree.requested || tree.exited {
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

func (tree *unixCandidateProcessTree) MarkExited() {
	tree.mu.Lock()
	tree.exited = true
	tree.mu.Unlock()
}

func (tree *unixCandidateProcessTree) Close() error {
	tree.closeOnce.Do(func() { tree.closeErr = tree.Kill() })
	return tree.closeErr
}
