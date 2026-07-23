//go:build windows

package main

import (
	"os/exec"
	"strings"
)

func candidateCommandWithObserver(binary, observer string, arguments ...string) (*exec.Cmd, error) {
	if strings.TrimSpace(observer) == "" {
		return exec.Command(binary, arguments...), nil
	}
	argv := append([]string{"--", binary}, arguments...)
	return exec.Command(observer, argv...), nil
}
