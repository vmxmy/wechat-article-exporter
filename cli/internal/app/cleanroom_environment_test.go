package app

import (
	"path/filepath"
	"testing"
)

func TestControlledCleanRoomDependenciesRequireExplicitLoopbackPair(t *testing.T) {
	t.Setenv(cleanRoomEnvironment, "")
	t.Setenv(cleanRoomOrigin, "http://127.0.0.1:43125")
	if _, ok, err := controlledCleanRoomDependencies(); ok || err != nil {
		t.Fatal("origin alone enabled clean-room mode")
	}

	t.Setenv(cleanRoomEnvironment, "1")
	t.Setenv("WECHAT_ARTICLE_PORTABLE_ROOT", filepath.Join(t.TempDir(), "portable"))
	for _, origin := range []string{
		"https://127.0.0.1:43125",
		"http://example.com",
		"http://localhost:43125",
		"http://127.0.0.1",
		"http://127.0.0.1:43125/path",
		"http://user@127.0.0.1:43125",
	} {
		t.Setenv(cleanRoomOrigin, origin)
		if _, ok, err := controlledCleanRoomDependencies(); ok || err == nil {
			t.Fatalf("unsafe origin %q did not fail closed", origin)
		}
	}

	t.Setenv(cleanRoomOrigin, "http://127.0.0.1:43125")
	configuration, ok, err := controlledCleanRoomDependencies()
	if err != nil || !ok || configuration.origin == nil || configuration.origin.String() != "http://127.0.0.1:43125" || !configuration.policy.AllowLoopback {
		t.Fatalf("configuration = %#v, ok=%v, err=%v", configuration, ok, err)
	}
	if _, ok := configuration.policy.AllowedAuthorities["127.0.0.1:43125"]; !ok {
		t.Fatalf("controlled origin authority was not pinned: %#v", configuration.policy)
	}
}

func TestControlledCleanRoomDependenciesRequireIsolatedPortableRoot(t *testing.T) {
	t.Setenv(cleanRoomEnvironment, "1")
	t.Setenv(cleanRoomOrigin, "http://127.0.0.1:43125")
	normalizesToRoot := filepath.Clean(filepath.Join(string(filepath.Separator), "tmp", "..", ".."))
	for _, root := range []string{"", "relative", string(filepath.Separator), normalizesToRoot} {
		t.Setenv("WECHAT_ARTICLE_PORTABLE_ROOT", root)
		if _, ok, err := controlledCleanRoomDependencies(); ok || err == nil {
			t.Fatalf("unsafe portable root %q did not fail closed", root)
		}
	}
}
