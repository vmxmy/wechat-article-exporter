package profiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrProfileBusy = errors.New("profile is busy or under maintenance")

type ProfileLock struct {
	file     *os.File
	shared   bool
	close    sync.Once
	closeErr error
}

func AcquireRuntimeLock(ctx context.Context, paths ProfilePaths) (*ProfileLock, error) {
	path, err := stableProfileLockPath(paths, "runtime.lock")
	if err != nil {
		return nil, err
	}
	return acquireProfileLock(ctx, path, true)
}

func AcquireMaintenanceRuntimeLock(ctx context.Context, paths ProfilePaths) (*ProfileLock, error) {
	path, err := stableProfileLockPath(paths, "runtime.lock")
	if err != nil {
		return nil, err
	}
	return acquireProfileLock(ctx, path, false)
}

func AcquireRuntimeGate(ctx context.Context, paths ProfilePaths) (*ProfileLock, error) {
	path, err := stableProfileLockPath(paths, "maintenance-gate.lock")
	if err != nil {
		return nil, err
	}
	return acquireProfileLock(ctx, path, true)
}

func AcquireMaintenanceGate(ctx context.Context, paths ProfilePaths) (*ProfileLock, error) {
	path, err := stableProfileLockPath(paths, "maintenance-gate.lock")
	if err != nil {
		return nil, err
	}
	return acquireProfileLock(ctx, path, false)
}

func stableProfileLockPath(paths ProfilePaths, name string) (string, error) {
	protected := filepath.Clean(paths.Database)
	lockRoot := ""
	if strings.TrimSpace(protected) == "" || protected == "." {
		protected = filepath.Clean(paths.Data)
	} else {
		lockRoot = filepath.Join(filepath.Dir(filepath.Dir(protected)), ".wechat-article-profile-locks")
	}
	if strings.TrimSpace(protected) == "" || protected == "." {
		protected = filepath.Clean(paths.State)
	}
	if strings.TrimSpace(protected) == "" || protected == "." {
		return "", errors.New("profile lock requires a database, data, or state path")
	}
	identity, err := canonicalProfileLockIdentity(protected)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(identity))
	if lockRoot == "" {
		state := filepath.Clean(paths.State)
		if strings.TrimSpace(state) == "" || state == "." {
			state = filepath.Dir(protected)
		}
		lockRoot = filepath.Join(filepath.Dir(filepath.Dir(state)), "profile-locks")
	}
	return filepath.Join(lockRoot, hex.EncodeToString(sum[:16])+"-"+name), nil
}

func canonicalProfileLockIdentity(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve profile lock identity: %w", err)
	}
	absolute = filepath.Clean(absolute)
	current := absolute
	var suffix []string
	for {
		canonical, evalErr := filepath.EvalSymlinks(current)
		if evalErr == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				canonical = filepath.Join(canonical, suffix[index])
			}
			return canonicalPlatformLockIdentity(filepath.Clean(canonical)), nil
		}
		if !errors.Is(evalErr, os.ErrNotExist) {
			return "", fmt.Errorf("canonicalize profile lock identity: %w", evalErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("canonicalize profile lock identity: %w", evalErr)
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func acquireProfileLock(ctx context.Context, path string, shared bool) (*ProfileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockProfileFile(ctx, file, shared); err != nil {
		_ = file.Close()
		if errors.Is(err, ErrProfileBusy) {
			return nil, err
		}
		return nil, fmt.Errorf("lock profile runtime: %w", err)
	}
	return &ProfileLock{file: file, shared: shared}, nil
}

func (lock *ProfileLock) Close() error {
	if lock == nil {
		return nil
	}
	lock.close.Do(func() {
		if lock.file == nil {
			return
		}
		lock.closeErr = errors.Join(unlockProfileFile(lock.file), lock.file.Close())
	})
	return lock.closeErr
}
