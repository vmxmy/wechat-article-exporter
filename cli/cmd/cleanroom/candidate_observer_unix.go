//go:build unix

package main

import (
	"os/exec"
	"strings"
)

// candidateCommandWithObserver invokes the native observer as the candidate's
// direct parent. The observer receives the exact candidate argv after `--`,
// must preserve child stdout/stderr byte-for-byte, and is responsible for
// tracking all descendants it creates (including Chromium).
func candidateCommandWithObserver(binary, observer string, arguments ...string) (*exec.Cmd, error) {
	if strings.TrimSpace(observer) == "" {
		return exec.Command(binary, arguments...), nil
	}
	argv := append([]string{"--", binary}, arguments...)
	return exec.Command(observer, argv...), nil
}
