//go:build windows

package profiles

import "strings"

func canonicalPlatformLockIdentity(path string) string { return strings.ToLower(path) }
