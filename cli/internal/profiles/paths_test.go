package profiles

import (
	"os"
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

func TestShouldUseLegacyMacOSRootsUntilModernDataExists(t *testing.T) {
	home := t.TempDir()
	legacyData := filepath.Join(home, ".local", "share", ApplicationDirectory)
	legacyState := filepath.Join(home, ".local", "state", ApplicationDirectory)
	modern := filepath.Join(home, "Library", "Application Support", ApplicationDirectory)
	if err := os.MkdirAll(filepath.Join(legacyData, "profiles", "profile-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	useLegacy, err := shouldUseLegacyMacOSRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if !useLegacy {
		t.Fatal("legacy application root was not selected when modern root was absent")
	}
	// Creating the legacy state root on the first launch must not make the
	// second launch switch the data root to the modern layout.
	if err := os.MkdirAll(filepath.Join(legacyState, "profiles", "profile-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	useLegacy, err = shouldUseLegacyMacOSRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if !useLegacy {
		t.Fatal("legacy data/state decision changed across launches")
	}
	if err := os.MkdirAll(modern, 0o700); err != nil {
		t.Fatal(err)
	}
	useLegacy, err = shouldUseLegacyMacOSRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if !useLegacy {
		t.Fatal("empty modern application directory hid the legacy data root")
	}
	if err := os.MkdirAll(filepath.Join(modern, "profiles", "profile-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	useLegacy, err = shouldUseLegacyMacOSRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if useLegacy {
		t.Fatal("legacy application root replaced a modern root with persisted data")
	}
}

func TestShouldUseLegacyMacOSRootsAcceptsStateOnlyInstallation(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "state", ApplicationDirectory, "profiles", "profile-a"), 0o700); err != nil {
		t.Fatal(err)
	}
	useLegacy, err := shouldUseLegacyMacOSRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if !useLegacy {
		t.Fatal("state-only legacy installation did not keep the paired legacy layout")
	}
}

func TestShouldUseLegacyMacOSRootsIgnoresEmptyLegacyDirectories(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "share", ApplicationDirectory), 0o700); err != nil {
		t.Fatal(err)
	}
	useLegacy, err := shouldUseLegacyMacOSRoots(home)
	if err != nil {
		t.Fatal(err)
	}
	if useLegacy {
		t.Fatal("empty legacy directory selected the legacy layout")
	}
}

func TestLegacyMacOSDiscoveryRejectsUnexpectedFiles(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".local", "share", ApplicationDirectory)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := shouldUseLegacyMacOSRoots(home); err == nil {
		t.Fatal("unexpected legacy file error = nil")
	}
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
