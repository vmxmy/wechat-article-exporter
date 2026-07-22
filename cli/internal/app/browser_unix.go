//go:build !windows

package app

import (
	"io"
	"os/exec"
	"runtime"
)

func openBrowser(target string) error {
	commandName := "xdg-open"
	if runtime.GOOS == "darwin" {
		commandName = "open"
	}
	command := exec.Command(commandName, target)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Start()
}
