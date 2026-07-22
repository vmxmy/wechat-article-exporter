//go:build !windows

package exporter

import (
	"os/exec"
	"syscall"
)

func configurePDFProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func terminatePDFProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
		_ = command.Process.Kill()
	}
}
