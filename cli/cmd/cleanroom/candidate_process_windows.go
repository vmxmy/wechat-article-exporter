//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsCandidateProcessTree struct {
	job       windows.Handle
	closeOnce sync.Once
	closeErr  error
}

func configureCandidateProcess(command *exec.Cmd) {
	command.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func attachCandidateProcessTree(process *os.Process) (candidateProcessTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, setErr := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); setErr != nil {
		_ = windows.CloseHandle(job)
		return nil, setErr
	}
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &windowsCandidateProcessTree{job: job}, nil
}

func (tree *windowsCandidateProcessTree) Kill() error { return tree.Close() }

func (tree *windowsCandidateProcessTree) Close() error {
	tree.closeOnce.Do(func() {
		if tree.job != 0 {
			tree.closeErr = windows.CloseHandle(tree.job)
			tree.job = 0
		}
	})
	if errors.Is(tree.closeErr, windows.ERROR_INVALID_HANDLE) {
		return nil
	}
	return tree.closeErr
}
