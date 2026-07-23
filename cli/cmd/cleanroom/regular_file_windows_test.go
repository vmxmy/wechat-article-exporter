//go:build windows

package main

import "testing"

func TestWindowsNTPathPrefixes(t *testing.T) {
	for input, expected := range map[string]string{
		`C:\artifact.zip`:               `\??\C:\artifact.zip`,
		`\\server\share\artifact.zip`:   `\??\UNC\server\share\artifact.zip`,
		`\\?\C:\artifact.zip`:           `\??\C:\artifact.zip`,
		`\\?\UNC\server\share\artifact`: `\??\UNC\server\share\artifact`,
		`\??\C:\artifact.zip`:           `\??\C:\artifact.zip`,
	} {
		if actual := windowsNTPath(input); actual != expected {
			t.Fatalf("windowsNTPath(%q) = %q, want %q", input, actual, expected)
		}
	}
}
