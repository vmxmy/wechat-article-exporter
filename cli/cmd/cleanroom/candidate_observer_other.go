//go:build !unix && !windows

package main

import (
	"errors"
	"os/exec"
	"strings"
)

func candidateCommandWithObserver(binary, observer string, arguments ...string) (*exec.Cmd, error) {
	if strings.TrimSpace(observer) != "" {
		return nil, errors.New("process-tree observation is unsupported on this platform")
	}
	return exec.Command(binary, arguments...), nil
}
