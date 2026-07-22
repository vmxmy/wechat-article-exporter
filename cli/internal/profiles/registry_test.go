package profiles

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestRegistryCreateUseListAndDeleteAreIsolated(t *testing.T) {
	paths := testPaths(t)
	secretStore := secrets.NewMemoryStore()
	registry := NewRegistry(paths, secretStore)
	left, err := registry.Create("left")
	if err != nil {
		t.Fatal(err)
	}
	right, err := registry.Create("right")
	if err != nil {
		t.Fatal(err)
	}
	if !left.Active || right.Active {
		t.Fatalf("created profiles: left=%#v right=%#v", left, right)
	}
	if err := secretStore.Set(context.Background(), secrets.Ref{Profile: "left", Kind: "session", Name: "current"}, []byte("left-secret")); err != nil {
		t.Fatal(err)
	}
	if err := secretStore.Set(context.Background(), secrets.Ref{Profile: "right", Kind: "session", Name: "current"}, []byte("right-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Use("right"); err != nil {
		t.Fatal(err)
	}
	active, err := registry.Active()
	if err != nil {
		t.Fatal(err)
	}
	if active.ID != "right" {
		t.Fatalf("active profile = %#v", active)
	}
	if err := registry.Delete("left"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(left.Paths.Config)); !os.IsNotExist(err) {
		t.Fatalf("left profile config still exists: %v", err)
	}
	if _, err := secretStore.Get(context.Background(), secrets.Ref{Profile: "left", Kind: "session", Name: "current"}); err != secrets.ErrNotFound {
		t.Fatalf("left secret error = %v", err)
	}
	rightSecret, err := secretStore.Get(context.Background(), secrets.Ref{Profile: "right", Kind: "session", Name: "current"})
	if err != nil || string(rightSecret) != "right-secret" {
		t.Fatalf("right secret = %q, error = %v", rightSecret, err)
	}
	profiles, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].ID != "right" || !profiles[0].Active {
		t.Fatalf("profiles = %#v", profiles)
	}
}

func TestRegistryRefusesDeletingActiveProfile(t *testing.T) {
	registry := NewRegistry(testPaths(t), secrets.NewMemoryStore())
	if _, err := registry.Create("active"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Delete("active"); err == nil {
		t.Fatal("Delete(active) error = nil")
	}
}

func testPaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	paths, err := ResolvePaths(PathOptions{
		ConfigRoot: filepath.Join(root, "config"), DataRoot: filepath.Join(root, "data"),
		CacheRoot: filepath.Join(root, "cache"), StateRoot: filepath.Join(root, "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	return paths
}
