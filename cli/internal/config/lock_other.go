//go:build !(aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris || windows)

package config

import "fmt"

func lockConfig(string) (func(), error) {
	return nil, fmt.Errorf("cross-process config locking is unsupported on this platform")
}
