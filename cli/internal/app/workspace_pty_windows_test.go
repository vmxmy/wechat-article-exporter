//go:build windows

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCLIWorkspaceRealPTYNavigationResizeAndCleanExit(t *testing.T) {
	testCLIWorkspaceRealPTYNavigationResizeAndCleanExit(t)
}

type workspacePTYHarness struct {
	hpc       windows.Handle
	input     *os.File
	output    *os.File
	process   *os.Process
	attrs     *windows.ProcThreadAttributeListContainer
	closeOnce sync.Once
	closeErr  error
}

func newWorkspacePTYHarness(t *testing.T) *workspacePTYHarness {
	t.Helper()
	ptyInput, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, ptyOutput, err := os.Pipe()
	if err != nil {
		_ = ptyInput.Close()
		_ = input.Close()
		t.Fatal(err)
	}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 100, Y: 30}, windows.Handle(ptyInput.Fd()), windows.Handle(ptyOutput.Fd()), 0, &hpc); err != nil {
		_ = ptyInput.Close()
		_ = input.Close()
		_ = output.Close()
		_ = ptyOutput.Close()
		failOrSkipUnavailablePTY(t, "create", err)
	}
	_ = ptyInput.Close()
	_ = ptyOutput.Close()
	return &workspacePTYHarness{hpc: hpc, input: input, output: output}
}

func (harness *workspacePTYHarness) start(_ context.Context, helper string, environment []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	if err := attrs.Update(0x20016, unsafe.Pointer(harness.hpc), unsafe.Sizeof(harness.hpc)); err != nil {
		attrs.Delete()
		return err
	}
	harness.attrs = attrs
	application, err := windows.UTF16PtrFromString(helper)
	if err != nil {
		return err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine([]string{helper}))
	if err != nil {
		return err
	}
	startup := new(windows.StartupInfoEx)
	startup.Cb = uint32(unsafe.Sizeof(*startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.ProcThreadAttributeList = attrs.List()
	process := new(windows.ProcessInformation)
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(application, commandLine, nil, nil, false, flags, workspaceEnvironmentBlock(environment), nil, &startup.StartupInfo, process); err != nil {
		return err
	}
	_ = windows.CloseHandle(process.Thread)
	harness.process, err = os.FindProcess(int(process.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_ = windows.CloseHandle(process.Process)
		return err
	}
	_ = windows.CloseHandle(process.Process)
	return nil
}

func workspaceEnvironmentBlock(environment []string) *uint16 {
	values := append([]string(nil), environment...)
	slices.SortFunc(values, func(left, right string) int {
		return strings.Compare(strings.ToLower(left), strings.ToLower(right))
	})
	encoded := utf16.Encode([]rune(strings.Join(values, "\x00") + "\x00\x00"))
	return &encoded[0]
}

func (harness *workspacePTYHarness) Read(value []byte) (int, error) {
	return harness.output.Read(value)
}
func (harness *workspacePTYHarness) Write(value []byte) (int, error) {
	return harness.input.Write(value)
}
func (harness *workspacePTYHarness) resize(width, height int) error {
	return windows.ResizePseudoConsole(harness.hpc, windows.Coord{X: int16(width), Y: int16(height)})
}
func (harness *workspacePTYHarness) wait() error {
	if harness.process == nil {
		return fmt.Errorf("workspace helper process was not started")
	}
	_, err := harness.process.Wait()
	return err
}
func (harness *workspacePTYHarness) close() error {
	harness.closeOnce.Do(func() {
		windows.ClosePseudoConsole(harness.hpc)
		if harness.attrs != nil {
			harness.attrs.Delete()
		}
		harness.closeErr = errors.Join(harness.input.Close(), harness.output.Close())
	})
	return harness.closeErr
}

var _ io.ReadWriter = (*workspacePTYHarness)(nil)
