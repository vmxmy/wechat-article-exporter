package objects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultUnreferencedRetention = 24 * time.Hour
	defaultTemporaryRetention    = 24 * time.Hour
)

var ErrConfirmationRequired = errors.New("garbage collection confirmation is required")

type RetentionPolicy struct {
	UnreferencedObjects time.Duration
	TemporaryFiles      time.Duration
	Now                 time.Time
}

type Reclaimable struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

type GCPlan struct {
	GeneratedAt  time.Time     `json:"generatedAt"`
	Unreferenced Reclaimable   `json:"unreferencedObjects"`
	Temporary    Reclaimable   `json:"temporaryFiles"`
	Confirmation string        `json:"confirmation"`
	Candidates   []GCCandidate `json:"candidates"`
}

type GCResult struct {
	DeletedObjects   Reclaimable `json:"deletedObjects"`
	DeletedTemporary Reclaimable `json:"deletedTemporaryFiles"`
	Skipped          []GCSkip    `json:"skipped"`
}

type GCSkip struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type GCCandidate struct {
	Kind         string    `json:"kind"`
	Path         string    `json:"path"`
	Digest       string    `json:"digest,omitempty"`
	Size         int64     `json:"size"`
	ModifiedAt   time.Time `json:"modifiedAt"`
	DeleteBefore time.Time `json:"deleteBefore"`
}

type ReferenceCheck func(context.Context, string) (bool, error)

func (store *FileStore) PlanGarbageCollection(
	ctx context.Context,
	referenced map[string]struct{},
	policy RetentionPolicy,
) (GCPlan, error) {
	now := policy.Now
	if now.IsZero() {
		now = time.Now()
	}
	if policy.UnreferencedObjects <= 0 {
		policy.UnreferencedObjects = defaultUnreferencedRetention
	}
	if policy.TemporaryFiles <= 0 {
		policy.TemporaryFiles = defaultTemporaryRetention
	}
	plan := GCPlan{GeneratedAt: now, Candidates: []GCCandidate{}}
	if err := store.planObjects(ctx, referenced, now.Add(-policy.UnreferencedObjects), &plan); err != nil {
		return GCPlan{}, err
	}
	if err := store.planTemporary(ctx, now.Add(-policy.TemporaryFiles), &plan); err != nil {
		return GCPlan{}, err
	}
	sort.Slice(plan.Candidates, func(left, right int) bool {
		return plan.Candidates[left].Path < plan.Candidates[right].Path
	})
	plan.Confirmation = fmt.Sprintf(
		"garbage-collect:%d:%d:%d",
		plan.Unreferenced.Count,
		plan.Temporary.Count,
		plan.Unreferenced.Bytes+plan.Temporary.Bytes,
	)
	return plan, nil
}

func (store *FileStore) ApplyGarbageCollection(
	ctx context.Context,
	plan GCPlan,
	confirmation string,
	isReferenced ReferenceCheck,
) (GCResult, error) {
	if confirmation == "" || confirmation != plan.Confirmation {
		return GCResult{}, fmt.Errorf("%w: use %q", ErrConfirmationRequired, plan.Confirmation)
	}
	result := GCResult{Skipped: []GCSkip{}}
	for _, candidate := range plan.Candidates {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		if candidate.Kind == "object" && isReferenced != nil {
			referenced, err := isReferenced(ctx, candidate.Digest)
			if err != nil {
				return result, fmt.Errorf("recheck object %s reference: %w", candidate.Digest, err)
			}
			if referenced {
				result.Skipped = append(result.Skipped, GCSkip{
					Kind: candidate.Kind, Path: candidate.Path, Reason: "object became referenced after the dry run",
				})
				continue
			}
		}
		deleted, reason, err := store.deleteCandidate(candidate)
		if err != nil {
			result.Skipped = append(result.Skipped, GCSkip{Kind: candidate.Kind, Path: candidate.Path, Reason: err.Error()})
			continue
		}
		if !deleted {
			result.Skipped = append(result.Skipped, GCSkip{Kind: candidate.Kind, Path: candidate.Path, Reason: reason})
			continue
		}
		if candidate.Kind == "object" {
			result.DeletedObjects.Count++
			result.DeletedObjects.Bytes += candidate.Size
			store.pruneObjectDirectories(candidate.Path)
		} else {
			result.DeletedTemporary.Count++
			result.DeletedTemporary.Bytes += candidate.Size
		}
	}
	return result, nil
}

func (store *FileStore) planObjects(
	ctx context.Context,
	referenced map[string]struct{},
	deleteBy time.Time,
	plan *GCPlan,
) error {
	root := filepath.Join(store.root, "sha256")
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		digest := entry.Name()
		expected, err := store.pathForDigest(digest)
		if err != nil || !samePath(path, expected) {
			return nil
		}
		if _, ok := referenced[digest]; ok || info.ModTime().After(deleteBy) {
			return nil
		}
		plan.Unreferenced.Count++
		plan.Unreferenced.Bytes += info.Size()
		plan.Candidates = append(plan.Candidates, GCCandidate{
			Kind: "object", Path: path, Digest: digest, Size: info.Size(), ModifiedAt: info.ModTime(), DeleteBefore: deleteBy,
		})
		return nil
	})
}

func (store *FileStore) planTemporary(ctx context.Context, deleteBy time.Time, plan *GCPlan) error {
	root := filepath.Join(store.root, "tmp")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read object temporary directory: %w", err)
	}
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		path := filepath.Join(root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.ModTime().After(deleteBy) {
			continue
		}
		plan.Temporary.Count++
		plan.Temporary.Bytes += info.Size()
		plan.Candidates = append(plan.Candidates, GCCandidate{
			Kind: "temporary", Path: path, Size: info.Size(), ModifiedAt: info.ModTime(), DeleteBefore: deleteBy,
		})
	}
	return nil
}

func (store *FileStore) deleteCandidate(candidate GCCandidate) (bool, string, error) {
	info, err := os.Lstat(candidate.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "candidate no longer exists", nil
	}
	if err != nil {
		return false, "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, "candidate is no longer a regular file", nil
	}
	if info.Size() != candidate.Size || !info.ModTime().Equal(candidate.ModifiedAt) {
		return false, "candidate changed after the dry run", nil
	}
	if info.ModTime().After(candidate.DeleteBefore) {
		return false, "candidate no longer satisfies the retention policy", nil
	}
	if candidate.Kind == "object" {
		expected, err := store.pathForDigest(candidate.Digest)
		if err != nil || !samePath(candidate.Path, expected) {
			return false, "object path is not canonical", nil
		}
	} else {
		temporaryRoot := filepath.Join(store.root, "tmp")
		relative, err := filepath.Rel(temporaryRoot, candidate.Path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return false, "temporary path escaped the object store", nil
		}
	}
	if err := os.Remove(candidate.Path); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func (store *FileStore) pruneObjectDirectories(path string) {
	root := filepath.Join(store.root, "sha256")
	directory := filepath.Dir(path)
	for !samePath(directory, root) {
		if err := os.Remove(directory); err != nil {
			return
		}
		directory = filepath.Dir(directory)
	}
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}
