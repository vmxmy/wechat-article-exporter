package main

import (
	"testing"
)

func TestIsolatedRootComponentsRejectsTraversalAndRootedPaths(t *testing.T) {
	for _, path := range []string{"", ".", "..", "../escape", "nested/../escape", "/absolute", `C:\absolute`, `\\server\share`, "nested//child", `nested\\child`, `nested/\child`, `nested\/child`, "nested/", `nested\`} {
		if _, err := isolatedRootComponents(path); err == nil {
			t.Fatalf("isolatedRootComponents(%q) accepted unsafe path", path)
		}
	}
	components, err := isolatedRootComponents("clean-room-home/config")
	if err != nil || len(components) != 2 || components[0] != "clean-room-home" || components[1] != "config" {
		t.Fatalf("isolatedRootComponents nested = %#v, %v", components, err)
	}
}
