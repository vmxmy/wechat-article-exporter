//go:build !windows

package profiles

func canonicalPlatformLockIdentity(path string) string { return path }
