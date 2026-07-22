package profiles

import (
	"path/filepath"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtimeutil"
)

func TestResolvePortablePathsStayBelowExplicitRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "portable")
	paths, err := ResolvePaths(PathOptions{Portable: true, PortableRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !paths.Portable {
		t.Fatal("Portable = false")
	}
	for _, value := range []string{paths.ConfigRoot, paths.DataRoot, paths.CacheRoot, paths.StateRoot} {
		relative, err := filepath.Rel(root, value)
		if err != nil || relative == ".." || filepath.IsAbs(relative) {
			t.Fatalf("path %s is outside portable root %s", value, root)
		}
	}
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	runtimeutil.AssertPrivatePermissions(t, paths.DataRoot, 0o700)
}

func TestResolvePathsRejectsUnsafePortableConfiguration(t *testing.T) {
	if _, err := ResolvePaths(PathOptions{Portable: true}); err == nil {
		t.Fatal("portable mode without root error = nil")
	}
	if _, err := ResolvePaths(PathOptions{PortableRoot: t.TempDir()}); err == nil {
		t.Fatal("portable root without mode error = nil")
	}
	if _, err := ResolvePaths(PathOptions{Portable: true, PortableRoot: string(filepath.Separator)}); err == nil {
		t.Fatal("filesystem root error = nil")
	}
}

func TestProfilePathsAreIsolated(t *testing.T) {
	paths, err := ResolvePaths(PathOptions{
		ConfigRoot: filepath.Join(t.TempDir(), "config"),
		DataRoot:   filepath.Join(t.TempDir(), "data"),
		CacheRoot:  filepath.Join(t.TempDir(), "cache"),
		StateRoot:  filepath.Join(t.TempDir(), "state"),
	})
	if err != nil {
		t.Fatal(err)
	}
	left := paths.ForProfile(domain.ProfileID("left"))
	right := paths.ForProfile(domain.ProfileID("right"))
	if left.Database == right.Database || left.Objects == right.Objects || left.Config == right.Config {
		t.Fatalf("profile paths overlap: left=%#v right=%#v", left, right)
	}
}
