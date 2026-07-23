//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsCandidatePTY struct {
	hpc        windows.Handle
	input      *os.File
	output     *os.File
	process    *os.Process
	job        windows.Handle
	attrs      *windows.ProcThreadAttributeListContainer
	cancelStop func() bool
	cancelMu   sync.Mutex
	context    context.Context
	closed     bool
	canceled   bool
	closeOnce  sync.Once
	closeErr   error
}

func newCandidatePTYHarness(width, height int) (candidatePTYHarness, error) {
	ptyInput, input, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	output, ptyOutput, err := os.Pipe()
	if err != nil {
		_ = ptyInput.Close()
		_ = input.Close()
		return nil, err
	}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: int16(width), Y: int16(height)}, windows.Handle(ptyInput.Fd()), windows.Handle(ptyOutput.Fd()), 0, &hpc); err != nil {
		_ = ptyInput.Close()
		_ = input.Close()
		_ = output.Close()
		_ = ptyOutput.Close()
		return nil, err
	}
	_ = ptyInput.Close()
	_ = ptyOutput.Close()
	return &windowsCandidatePTY{hpc: hpc, input: input, output: output}, nil
}

func (harness *windowsCandidatePTY) start(ctx context.Context, binary string, environment []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	if err := attrs.Update(0x20016, unsafe.Pointer(harness.hpc), unsafe.Sizeof(harness.hpc)); err != nil {
		attrs.Delete()
		return err
	}
	harness.attrs = attrs
	application, err := windows.UTF16PtrFromString(binary)
	if err != nil {
		return err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{binary}))
	if err != nil {
		return err
	}
	startup := new(windows.StartupInfoEx)
	startup.Cb = uint32(unsafe.Sizeof(*startup))
	startup.ProcThreadAttributeList = attrs.List()
	process := new(windows.ProcessInformation)
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(application, commandLine, nil, nil, false, flags, candidateEnvironmentBlock(environment), nil, &startup.StartupInfo, process); err != nil {
		return err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_ = windows.CloseHandle(process.Thread)
		_ = windows.CloseHandle(process.Process)
		return err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		_ = windows.TerminateProcess(process.Process, 1)
		_ = windows.CloseHandle(process.Thread)
		_ = windows.CloseHandle(process.Process)
		return err
	}
	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		_ = windows.CloseHandle(job)
		_ = windows.TerminateProcess(process.Process, 1)
		_ = windows.CloseHandle(process.Thread)
		_ = windows.CloseHandle(process.Process)
		return err
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		_ = windows.CloseHandle(process.Thread)
		_ = windows.CloseHandle(process.Process)
		return err
	}
	_ = windows.CloseHandle(process.Thread)
	harness.job = job
	harness.process, err = os.FindProcess(int(process.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_ = windows.CloseHandle(process.Process)
		return err
	}
	_ = windows.CloseHandle(process.Process)
	stop := context.AfterFunc(ctx, func() { _ = harness.cancel() })
	harness.cancelMu.Lock()
	harness.cancelStop = stop
	harness.context = ctx
	alreadyClosed := harness.closed
	harness.cancelMu.Unlock()
	if alreadyClosed {
		stop()
	}
	return nil
}

func candidateEnvironmentBlock(environment []string) *uint16 {
	encoded := utf16.Encode([]rune(strings.Join(environment, "\x00") + "\x00\x00"))
	return &encoded[0]
}

func (harness *windowsCandidatePTY) Read(value []byte) (int, error) {
	return harness.output.Read(value)
}
func (harness *windowsCandidatePTY) Write(value []byte) (int, error) {
	return harness.input.Write(value)
}
func (harness *windowsCandidatePTY) resize(width, height int) error {
	return windows.ResizePseudoConsole(harness.hpc, windows.Coord{X: int16(width), Y: int16(height)})
}
func (harness *windowsCandidatePTY) wait() error {
	if harness.process == nil {
		return fmt.Errorf("candidate process was not started")
	}
	_, err := harness.process.Wait()
	return err
}

func (harness *windowsCandidatePTY) close() error {
	return harness.shutdown(false)
}

func (harness *windowsCandidatePTY) cancel() error {
	return harness.shutdown(true)
}

func (harness *windowsCandidatePTY) shutdown(canceled bool) error {
	harness.closeOnce.Do(func() {
		harness.cancelMu.Lock()
		harness.closed = true
		harness.canceled = harness.canceled || canceled || (harness.context != nil && harness.context.Err() != nil)
		stop := harness.cancelStop
		exitCode := uint32(0)
		if harness.canceled {
			exitCode = 1
		}
		harness.cancelMu.Unlock()
		if stop != nil {
			stop()
		}
		if harness.job != 0 {
			harness.closeErr = errors.Join(harness.closeErr, windows.TerminateJobObject(harness.job, exitCode), windows.CloseHandle(harness.job))
			harness.job = 0
		}
		if harness.input != nil {
			harness.closeErr = errors.Join(harness.closeErr, harness.input.Close())
		}
		windows.ClosePseudoConsole(harness.hpc)
		if harness.attrs != nil {
			harness.attrs.Delete()
		}
		harness.closeErr = errors.Join(harness.closeErr, harness.output.Close())
	})
	return harness.closeErr
}
