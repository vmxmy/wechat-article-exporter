//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type unixCandidateProcessTree struct{ process *os.Process }

func configureCandidateProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachCandidateProcessTree(process *os.Process) (candidateProcessTree, error) {
	return unixCandidateProcessTree{process: process}, nil
}

func (tree unixCandidateProcessTree) Kill() error {
	if tree.process == nil {
		return nil
	}
	err := syscall.Kill(-tree.process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (unixCandidateProcessTree) Close() error { return nil }
