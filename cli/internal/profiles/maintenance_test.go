package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestProfileMaintenanceLockBlocksRestoreAndNewRuntime(t *testing.T) {
	paths := ProfilePaths{State: t.TempDir()}
	first, err := AcquireRuntimeLock(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := AcquireRuntimeLock(context.Background(), paths)
	if err != nil {
		t.Fatalf("second shared runtime lock: %v", err)
	}
	defer second.Close()
	if _, err := AcquireMaintenanceRuntimeLock(context.Background(), paths); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("maintenance runtime lock error = %v", err)
	}
	gate, err := AcquireMaintenanceGate(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	if _, err := AcquireRuntimeGate(context.Background(), paths); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("runtime gate error = %v", err)
	}
}

func TestProfileLockSurvivesProfileStateDirectoryDeletion(t *testing.T) {
	root := t.TempDir()
	paths := ProfilePaths{State: filepath.Join(root, "profiles", "profile-a"), Database: filepath.Join(root, "data", "library.sqlite3")}
	lock, err := AcquireMaintenanceGate(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.RemoveAll(paths.State); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireRuntimeGate(context.Background(), paths); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("runtime gate after state deletion error=%v", err)
	}
}

func TestProfileMaintenanceLockIdentityFollowsProtectedDatabase(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "data", "library.sqlite3")
	first := ProfilePaths{State: filepath.Join(root, "state-a", "profiles", "profile-a"), Database: database}
	second := ProfilePaths{State: filepath.Join(root, "state-b", "profiles", "renamed"), Database: database}
	lock, err := AcquireMaintenanceGate(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := AcquireRuntimeGate(context.Background(), second); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("same database through another state path error=%v", err)
	}
}

func TestProfileMaintenanceLockIdentityCanonicalizesSymlinkAliases(t *testing.T) {
	root := t.TempDir()
	realRoot := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(realRoot, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(root, "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	first := ProfilePaths{State: filepath.Join(realRoot, "state"), Database: filepath.Join(realRoot, "data", "library.sqlite3")}
	second := ProfilePaths{State: filepath.Join(aliasRoot, "state"), Database: filepath.Join(aliasRoot, "data", "library.sqlite3")}
	lock, err := AcquireMaintenanceGate(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := AcquireRuntimeGate(context.Background(), second); !errors.Is(err, ErrProfileBusy) {
		t.Fatalf("symlink alias lock error=%v", err)
	}
}

func TestProfileMaintenanceLockRejectsEmptyPaths(t *testing.T) {
	if _, err := AcquireRuntimeGate(context.Background(), ProfilePaths{}); err == nil {
		t.Fatal("empty profile paths were accepted")
	}
}

func TestProfileLockCloseIsConcurrentAndIdempotent(t *testing.T) {
	paths := ProfilePaths{State: t.TempDir()}
	lock, err := AcquireRuntimeLock(context.Background(), paths)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsSeen := make(chan error, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsSeen <- lock.Close()
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("Close error=%v", err)
		}
	}
}
