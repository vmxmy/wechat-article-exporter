//go:build windows

package application

import (
	"context"

	"golang.org/x/sys/windows"
)

func workspaceOpenOutput(ctx context.Context, directory string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(directory)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, 1)
}
