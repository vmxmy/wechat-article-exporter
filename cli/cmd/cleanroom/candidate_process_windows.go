//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	createThreadSnapshot = windows.CreateToolhelp32Snapshot
	firstThread          = windows.Thread32First
	nextThread           = windows.Thread32Next
	openThreadHandle     = windows.OpenThread
	resumeThreadHandle   = windows.ResumeThread
)

type windowsCandidateProcessTree struct {
	job       windows.Handle
	closeOnce sync.Once
	closeErr  error
}

func configureCandidateProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
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
	if err := resumeCandidateProcessThreads(uint32(process.Pid)); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &windowsCandidateProcessTree{job: job}, nil
}

func resumeCandidateProcessThreads(processID uint32) error {
	snapshot, err := createThreadSnapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := firstThread(snapshot, &entry); err != nil {
		return err
	}
	resumed := 0
	for {
		if entry.OwnerProcessID == processID {
			thread, openErr := openThreadHandle(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return openErr
			}
			previousCount, resumeErr := resumeThreadHandle(thread)
			closeErr := windows.CloseHandle(thread)
			if resumeErr != nil || closeErr != nil {
				return errors.Join(resumeErr, closeErr)
			}
			if previousCount != 1 {
				return fmt.Errorf("candidate thread %d had unexpected suspend count %d", entry.ThreadID, previousCount)
			}
			resumed++
		}
		if err := nextThread(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return err
		}
	}
	if resumed == 0 {
		return errors.New("candidate process has no resumable primary thread")
	}
	return nil
}

func (tree *windowsCandidateProcessTree) Kill() error {
	if tree == nil || tree.job == 0 {
		return nil
	}
	err := windows.TerminateJobObject(tree.job, 1)
	if errors.Is(err, windows.ERROR_INVALID_HANDLE) {
		return nil
	}
	return err
}

func (tree *windowsCandidateProcessTree) MarkExited() {}

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
