//go:build windows

package exporter

import "os/exec"

func configurePDFProcess(_ *exec.Cmd) {}

func terminatePDFProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
}
