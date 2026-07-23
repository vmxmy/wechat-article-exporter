//go:build !windows

package application

import (
	"context"
	"io"
	"os/exec"
	"runtime"
)

func workspaceOpenOutput(ctx context.Context, directory string) error {
	commandName := "xdg-open"
	if runtime.GOOS == "darwin" {
		commandName = "open"
	}
	command := exec.CommandContext(ctx, commandName, directory)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Start()
}
