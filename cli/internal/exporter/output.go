package exporter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

const (
	temporaryOutputPrefix = ".wechat-export-"
	temporaryOutputSuffix = ".tmp"
)

var (
	ErrDestinationExists = errors.New("export destination already exists")
	ErrUnsafePath        = errors.New("unsafe export output path")
	ErrInvalidCollision  = errors.New("invalid export collision policy")
)

var windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)

type CollisionPolicy string

const (
	CollisionFail    CollisionPolicy = "fail"
	CollisionSkip    CollisionPolicy = "skip"
	CollisionReplace CollisionPolicy = "replace"
)

type OutputStatus string

const (
	OutputWritten  OutputStatus = "written"
	OutputSkipped  OutputStatus = "skipped"
	OutputReplaced OutputStatus = "replaced"
)

type OutputFile struct {
	ArticleID  domain.ArticleID   `json:"articleId,omitempty"`
	ArticleIDs []domain.ArticleID `json:"articleIds,omitempty"`
	Path       string             `json:"path"`
	Size       int64              `json:"size"`
	SHA256     string             `json:"sha256"`
	Status     OutputStatus       `json:"status"`
}

// StagedOutput describes a fully written and synced private output that has
// not necessarily been published at its destination yet. TemporaryPath is
// relative to the same OutputManager root, so callers may persist this value
// in a durable job checkpoint and resume publication after a process crash.
type StagedOutput struct {
	Output        OutputFile      `json:"output"`
	TemporaryPath string          `json:"temporaryPath,omitempty"`
	Policy        CollisionPolicy `json:"policy"`
}

type CleanupReport struct {
	Removed  []string `json:"removed"`
	Warnings []string `json:"warnings,omitempty"`
}

type OutputManager struct {
	root       string
	handle     *os.Root
	lifecycle  sync.RWMutex
	capability bool
}

func NewOutputManager(root string) (*OutputManager, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("export output root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve export output root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("create export output root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect export output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("export output root is not a directory")
	}
	handle, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, fmt.Errorf("open export output root: %w", err)
	}
	openedInfo, err := handle.Stat(".")
	if err != nil {
		handle.Close()
		return nil, fmt.Errorf("inspect opened export output root: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		handle.Close()
		return nil, errors.New("export output root was replaced while opening")
	}
	return &OutputManager{root: filepath.Clean(absolute), handle: handle}, nil
}

// NewOutputManagerFromRoot derives an output capability from an already-open
// trusted root. The returned manager owns only its child handle; the caller
// retains ownership of authorizedRoot.
func NewOutputManagerFromRoot(authorizedRoot *os.Root, relative string) (*OutputManager, error) {
	if authorizedRoot == nil {
		return nil, errors.New("authorized export output root is required")
	}
	relative = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if relative == "." {
		relative = ""
	}
	if relative != "" {
		if _, err := normalizeRelativeOutputPath(relative); err != nil {
			return nil, err
		}
		if err := ensureOutputDirectories(authorizedRoot, relative, nil); err != nil {
			return nil, fmt.Errorf("create authorized export output directory %q: %w", relative, err)
		}
	}
	childName := relative
	if childName == "" {
		childName = "."
	}
	if relative != "" {
		if err := rejectSymlinkComponents(authorizedRoot, relative, true); err != nil {
			return nil, err
		}
	}
	handle, err := authorizedRoot.OpenRoot(childName)
	if err != nil {
		return nil, fmt.Errorf("open authorized export output directory %q: %w", childName, err)
	}
	info, err := handle.Stat(".")
	if err != nil {
		handle.Close()
		return nil, err
	}
	if !info.IsDir() {
		handle.Close()
		return nil, errors.New("authorized export output is not a directory")
	}
	return &OutputManager{root: filepath.Clean(handle.Name()), handle: handle, capability: true}, nil
}

func (manager *OutputManager) Root() string {
	if manager == nil {
		return ""
	}
	return manager.root
}

func (manager *OutputManager) Close() error {
	if manager == nil {
		return nil
	}
	manager.lifecycle.Lock()
	defer manager.lifecycle.Unlock()
	if manager.handle == nil {
		return nil
	}
	err := manager.handle.Close()
	manager.handle = nil
	return err
}

func (manager *OutputManager) WriteFile(
	ctx context.Context,
	relativePath string,
	policy CollisionPolicy,
	write func(io.Writer) error,
) (OutputFile, error) {
	staged, err := manager.StageFile(ctx, relativePath, policy, write)
	if err != nil {
		return OutputFile{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = manager.AbortStaged(staged)
		}
	}()
	output, err := manager.CommitStaged(ctx, staged)
	if err != nil {
		return OutputFile{}, err
	}
	committed = true
	return output, nil
}

// StageFile writes and syncs an output into a private file without publishing
// it. CollisionSkip returns an already committed output with no temporary
// path. The returned descriptor is safe to serialize into a job checkpoint.
func (manager *OutputManager) StageFile(
	ctx context.Context,
	relativePath string,
	policy CollisionPolicy,
	write func(io.Writer) error,
) (StagedOutput, error) {
	if manager == nil {
		return StagedOutput{}, errors.New("export output manager is not initialized")
	}
	manager.lifecycle.Lock()
	defer manager.lifecycle.Unlock()
	if manager.root == "" || manager.handle == nil {
		return StagedOutput{}, errors.New("export output manager is not initialized")
	}
	if write == nil {
		return StagedOutput{}, errors.New("export output writer is required")
	}
	if err := validateCollisionPolicy(policy); err != nil {
		return StagedOutput{}, err
	}
	if err := manager.validateRoot(); err != nil {
		return StagedOutput{}, err
	}
	canonicalPath, err := normalizeRelativeOutputPath(relativePath)
	if err != nil {
		return StagedOutput{}, err
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(canonicalPath)))
	if parent == "." {
		parent = ""
	}
	if parent != "" {
		if err := ensureOutputDirectories(manager.handle, parent, manager.syncDirectory); err != nil {
			return StagedOutput{}, fmt.Errorf("create output directory %q: %w", parent, err)
		}
	}
	select {
	case <-ctx.Done():
		return StagedOutput{}, ctx.Err()
	default:
	}

	existing, statErr := manager.handle.Lstat(canonicalPath)
	switch {
	case statErr == nil:
		if !existing.Mode().IsRegular() {
			return StagedOutput{}, fmt.Errorf("destination %q is not a regular file: %w", canonicalPath, ErrUnsafePath)
		}
		switch policy {
		case CollisionFail:
			return StagedOutput{}, fmt.Errorf("destination %q: %w", canonicalPath, ErrDestinationExists)
		case CollisionSkip:
			digest, size, err := manager.hashRegularFile(ctx, canonicalPath)
			if err != nil {
				return StagedOutput{}, fmt.Errorf("checksum skipped destination %q: %w", canonicalPath, err)
			}
			return StagedOutput{Output: OutputFile{
				Path: canonicalPath, Size: size, SHA256: digest, Status: OutputSkipped,
			}, Policy: policy}, nil
		}
	case errors.Is(statErr, os.ErrNotExist):
	case statErr != nil:
		return StagedOutput{}, fmt.Errorf("inspect destination %q: %w", canonicalPath, statErr)
	}

	temporaryName := filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), temporaryOutputPrefix+uuid.NewString()+temporaryOutputSuffix))
	temporary, err := manager.handle.OpenFile(temporaryName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return StagedOutput{}, fmt.Errorf("create staging file for %q: %w", canonicalPath, err)
	}
	staged := false
	defer func() {
		_ = temporary.Close()
		if !staged {
			_ = manager.handle.Remove(temporaryName)
		}
	}()
	hash := sha256.New()
	counting := &countingWriter{writer: io.MultiWriter(temporary, hash)}
	if err := writeContextWriter(ctx, counting, write); err != nil {
		return StagedOutput{}, err
	}
	if err := temporary.Sync(); err != nil {
		return StagedOutput{}, fmt.Errorf("sync staging file for %q: %w", canonicalPath, err)
	}
	if err := temporary.Close(); err != nil {
		return StagedOutput{}, fmt.Errorf("close staging file for %q: %w", canonicalPath, err)
	}
	if err := manager.syncDirectory(parent); err != nil {
		return StagedOutput{}, fmt.Errorf("sync staging directory for %q: %w", canonicalPath, err)
	}
	if err := manager.validateRoot(); err != nil {
		return StagedOutput{}, err
	}

	status := OutputWritten
	if statErr == nil {
		status = OutputReplaced
	}
	staged = true
	return StagedOutput{
		Output: OutputFile{
			Path: canonicalPath, Size: counting.count, SHA256: hex.EncodeToString(hash.Sum(nil)), Status: status,
		},
		TemporaryPath: temporaryName,
		Policy:        policy,
	}, nil
}

// CommitStaged idempotently publishes a staged output. If the destination
// already contains the expected bytes, it is treated as committed and any
// surviving private alias is cleaned up. This is the recovery primitive used
// after a process exits between publication and database finalization.
func (manager *OutputManager) CommitStaged(ctx context.Context, staged StagedOutput) (OutputFile, error) {
	if manager == nil {
		return OutputFile{}, errors.New("export output manager is not initialized")
	}
	manager.lifecycle.Lock()
	defer manager.lifecycle.Unlock()
	if manager.root == "" || manager.handle == nil {
		return OutputFile{}, errors.New("export output manager is not initialized")
	}
	if err := validateCollisionPolicy(staged.Policy); err != nil {
		return OutputFile{}, err
	}
	if err := manager.validateRoot(); err != nil {
		return OutputFile{}, err
	}
	destination, err := normalizeRelativeOutputPath(staged.Output.Path)
	if err != nil {
		return OutputFile{}, err
	}
	if staged.Output.SHA256 == "" || staged.Output.Size < 0 {
		return OutputFile{}, errors.New("staged output digest and size are required")
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(destination)))
	if parent == "." {
		parent = ""
	}
	temporary, err := manager.validateStagedTemporaryPath(staged.TemporaryPath, parent)
	if err != nil {
		return OutputFile{}, fmt.Errorf("staged output %q has an unsafe private path: %w", destination, err)
	}
	if digest, size, hashErr := manager.hashRegularFile(ctx, destination); hashErr == nil {
		if digest == staged.Output.SHA256 && size == staged.Output.Size {
			if err := manager.syncDirectory(parent); err != nil {
				return OutputFile{}, fmt.Errorf("sync recovered output directory for %q: %w", destination, err)
			}
			if temporary != "" {
				if err := manager.handle.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
					return OutputFile{}, fmt.Errorf("remove recovered staging file for %q: %w", destination, err)
				}
				if err := manager.syncDirectory(parent); err != nil {
					return OutputFile{}, fmt.Errorf("sync recovered staging cleanup for %q: %w", destination, err)
				}
			}
			return staged.Output, nil
		}
		if staged.Policy != CollisionReplace {
			return OutputFile{}, fmt.Errorf("destination %q: %w", destination, ErrDestinationExists)
		}
	} else if !errors.Is(hashErr, os.ErrNotExist) {
		return OutputFile{}, fmt.Errorf("inspect staged destination %q: %w", destination, hashErr)
	}
	if temporary == "" {
		return OutputFile{}, fmt.Errorf("staged output %q is missing its private file", destination)
	}
	digest, size, err := manager.hashRegularFile(ctx, temporary)
	if err != nil {
		return OutputFile{}, fmt.Errorf("verify staged output %q: %w", destination, err)
	}
	if digest != staged.Output.SHA256 || size != staged.Output.Size {
		return OutputFile{}, fmt.Errorf("staged output %q does not match its checkpoint", destination)
	}
	if err := manager.commitStagedFile(temporary, destination, staged.Policy); err != nil {
		return OutputFile{}, fmt.Errorf("commit output %q: %w", destination, err)
	}
	if err := manager.syncDirectory(parent); err != nil {
		return OutputFile{}, fmt.Errorf("sync output directory for %q: %w", destination, err)
	}
	return staged.Output, nil
}

// AbortStaged removes a still-private staged file. It never removes a visible
// destination and is therefore safe to call from deferred cleanup paths.
func (manager *OutputManager) AbortStaged(staged StagedOutput) error {
	if manager == nil || staged.TemporaryPath == "" {
		return nil
	}
	manager.lifecycle.Lock()
	defer manager.lifecycle.Unlock()
	if manager.handle == nil {
		return errors.New("export output manager is closed")
	}
	temporary, err := normalizeRelativeOutputPath(staged.TemporaryPath)
	if err != nil || !isTemporaryOutputName(filepath.Base(filepath.FromSlash(temporary))) {
		return ErrUnsafePath
	}
	if err := manager.handle.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// HashFile verifies an already committed relative output through the manager's
// directory capability. Export workers use it to recover a file that reached
// durable storage before its SQLite export_files row could be committed.
func (manager *OutputManager) HashFile(ctx context.Context, relativePath string) (string, int64, error) {
	if manager == nil {
		return "", 0, errors.New("export output manager is not initialized")
	}
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.root == "" || manager.handle == nil {
		return "", 0, errors.New("export output manager is not initialized")
	}
	if err := manager.validateRoot(); err != nil {
		return "", 0, err
	}
	canonicalPath, err := normalizeRelativeOutputPath(relativePath)
	if err != nil {
		return "", 0, err
	}
	return manager.hashRegularFile(ctx, canonicalPath)
}

func (manager *OutputManager) CleanupAbandoned(ctx context.Context, removeBefore time.Time) (CleanupReport, error) {
	report := CleanupReport{Removed: []string{}, Warnings: []string{}}
	if manager == nil {
		return report, errors.New("export output manager is not initialized")
	}
	manager.lifecycle.Lock()
	defer manager.lifecycle.Unlock()
	if manager.capability {
		return CleanupReport{}, errors.New("cleanup of capability-derived export roots is not supported")
	}
	if manager.root == "" || manager.handle == nil {
		return report, errors.New("export output manager is not initialized")
	}
	if removeBefore.IsZero() {
		return report, errors.New("cleanup cutoff is required")
	}
	if err := manager.validateRoot(); err != nil {
		return report, err
	}
	err := filepath.WalkDir(manager.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if path == manager.root || !isTemporaryOutputName(entry.Name()) {
			return nil
		}
		relative, relErr := filepath.Rel(manager.root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			report.Warnings = append(report.Warnings, "ignored staging symlink "+relative)
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			report.Warnings = append(report.Warnings, "ignored non-regular staging path "+relative)
			return nil
		}
		if !info.ModTime().Before(removeBefore) {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove abandoned output %q: %w", relative, err)
		}
		report.Removed = append(report.Removed, relative)
		return nil
	})
	sort.Strings(report.Removed)
	sort.Strings(report.Warnings)
	if err != nil {
		return report, fmt.Errorf("clean abandoned export outputs: %w", err)
	}
	return report, nil
}

func (manager *OutputManager) validateRoot() error {
	if manager == nil || manager.handle == nil {
		return errors.New("export output manager is closed")
	}
	if manager.capability {
		info, err := manager.handle.Stat(".")
		if err != nil {
			return fmt.Errorf("inspect authorized export output handle: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("authorized export output changed type: %w", ErrUnsafePath)
		}
		return nil
	}
	info, err := os.Lstat(manager.root)
	if err != nil {
		return fmt.Errorf("inspect export output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("export output root changed type: %w", ErrUnsafePath)
	}
	openedInfo, err := manager.handle.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect opened export output root: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return fmt.Errorf("export output root was replaced: %w", ErrUnsafePath)
	}
	return nil
}

func (manager *OutputManager) resolveSafeOutputPath(relativePath string, createParents bool) (string, string, error) {
	if err := manager.validateRoot(); err != nil {
		return "", "", err
	}
	canonical, err := normalizeRelativeOutputPath(relativePath)
	if err != nil {
		return "", "", err
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(canonical)))
	if parent == "." {
		parent = ""
	}
	if createParents && parent != "" {
		if err := manager.handle.MkdirAll(parent, 0o700); err != nil {
			return "", "", err
		}
	}
	if info, err := manager.handle.Lstat(canonical); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", ErrUnsafePath
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	return canonical, filepath.Join(manager.root, filepath.FromSlash(canonical)), nil
}

func normalizeRelativeOutputPath(relativePath string) (string, error) {
	if relativePath == "" || strings.TrimSpace(relativePath) == "" {
		return "", fmt.Errorf("output path is empty: %w", ErrUnsafePath)
	}
	if strings.ContainsRune(relativePath, 0) || strings.HasPrefix(relativePath, "/") ||
		strings.HasPrefix(relativePath, `\`) || filepath.IsAbs(relativePath) || filepath.VolumeName(relativePath) != "" ||
		windowsVolumePattern.MatchString(relativePath) {
		return "", fmt.Errorf("output path %q is absolute: %w", relativePath, ErrUnsafePath)
	}
	portable := strings.ReplaceAll(relativePath, `\`, "/")
	if strings.HasPrefix(portable, "//") {
		return "", fmt.Errorf("output path %q is absolute: %w", relativePath, ErrUnsafePath)
	}
	parts := strings.Split(portable, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("output path %q contains an unsafe component: %w", relativePath, ErrUnsafePath)
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(portable)))
	if clean != portable || clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("output path %q is not canonical: %w", relativePath, ErrUnsafePath)
	}
	return clean, nil
}

func validateCollisionPolicy(policy CollisionPolicy) error {
	switch policy {
	case CollisionFail, CollisionSkip, CollisionReplace:
		return nil
	default:
		return fmt.Errorf("unsupported collision policy %q: %w", policy, ErrInvalidCollision)
	}
}

func (manager *OutputManager) commitStagedFile(source, destination string, policy CollisionPolicy) error {
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(destination)))
	if parent == "." {
		parent = ""
	}
	switch policy {
	case CollisionFail:
		if err := manager.handle.Link(source, destination); err != nil {
			if errors.Is(err, os.ErrExist) {
				return ErrDestinationExists
			}
			return err
		}
		// Persist the destination link before removing the private recovery
		// alias. Otherwise a crash between unlink and directory sync can lose
		// both names for the staged inode.
		if err := manager.syncDirectory(parent); err != nil {
			return err
		}
		if err := manager.handle.Remove(source); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := manager.syncDirectory(parent); err != nil {
			return err
		}
		return nil
	case CollisionReplace:
		if info, err := manager.handle.Lstat(destination); err == nil && !info.Mode().IsRegular() {
			return ErrUnsafePath
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return manager.handle.Rename(source, destination)
	default:
		return fmt.Errorf("cannot commit with collision policy %q: %w", policy, ErrInvalidCollision)
	}
}

func writeContextWriter(ctx context.Context, destination io.Writer, write func(io.Writer) error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := write(&contextWriter{ctx: ctx, writer: destination}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (writer *contextWriter) Write(data []byte) (int, error) {
	select {
	case <-writer.ctx.Done():
		return 0, writer.ctx.Err()
	default:
		return writer.writer.Write(data)
	}
}

type countingWriter struct {
	writer io.Writer
	count  int64
}

func (writer *countingWriter) Write(data []byte) (int, error) {
	count, err := writer.writer.Write(data)
	writer.count += int64(count)
	return count, err
}

func (manager *OutputManager) hashRegularFile(ctx context.Context, path string) (string, int64, error) {
	if err := rejectSymlinkComponents(manager.handle, path, false); err != nil {
		return "", 0, err
	}
	before, err := manager.handle.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", 0, ErrUnsafePath
	}
	file, err := manager.handle.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, ErrUnsafePath
	}
	if !os.SameFile(before, info) {
		return "", 0, ErrUnsafePath
	}
	hash := sha256.New()
	written, err := copyWithContext(ctx, hash, file)
	if err != nil {
		return "", 0, err
	}
	after, err := manager.handle.Lstat(path)
	if err != nil || after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(info, after) {
		return "", 0, ErrUnsafePath
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func rejectSymlinkComponents(root *os.Root, relative string, requireFinalDirectory bool) error {
	if root == nil {
		return errors.New("filesystem root is unavailable")
	}
	canonical, err := normalizeRelativeOutputPath(relative)
	if err != nil {
		return err
	}
	parts := strings.Split(canonical, "/")
	for index := range parts {
		current := strings.Join(parts[:index+1], "/")
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path component %q is a symlink: %w", current, ErrUnsafePath)
		}
		if index < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("output path component %q is not a directory: %w", current, ErrUnsafePath)
		}
		if requireFinalDirectory && index == len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("output path %q is not a directory: %w", current, ErrUnsafePath)
		}
	}
	return nil
}

// ensureOutputDirectories creates one component at a time and rejects every
// existing symlink. When syncDirectory is provided, each newly created entry
// is made durable before a later checkpoint can reference a file beneath it.
func ensureOutputDirectories(root *os.Root, relative string, syncDirectory func(string) error) error {
	if root == nil {
		return errors.New("filesystem root is unavailable")
	}
	canonical, err := normalizeRelativeOutputPath(relative)
	if err != nil {
		return err
	}
	parts := strings.Split(canonical, "/")
	for index := range parts {
		current := strings.Join(parts[:index+1], "/")
		info, statErr := root.Lstat(current)
		switch {
		case statErr == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return fmt.Errorf("output directory %q is not a real directory: %w", current, ErrUnsafePath)
			}
			continue
		case !errors.Is(statErr, os.ErrNotExist):
			return statErr
		}
		if err := root.Mkdir(current, 0o700); err != nil {
			return err
		}
		if syncDirectory != nil {
			parent := strings.Join(parts[:index], "/")
			if err := syncDirectory(parent); err != nil {
				return err
			}
		}
	}
	return nil
}

func (manager *OutputManager) validateStagedTemporaryPath(temporaryPath, destinationParent string) (string, error) {
	if temporaryPath == "" {
		return "", nil
	}
	temporary, err := normalizeRelativeOutputPath(temporaryPath)
	if err != nil || !isTemporaryOutputName(filepath.Base(filepath.FromSlash(temporary))) {
		return "", ErrUnsafePath
	}
	temporaryParent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(temporary)))
	if temporaryParent == "." {
		temporaryParent = ""
	}
	if temporaryParent != destinationParent {
		return "", ErrUnsafePath
	}
	return temporary, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}

func (manager *OutputManager) syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		// Windows does not provide durable directory fsync semantics through
		// os.File.Sync; opening a directory handle and flushing it commonly
		// returns ERROR_ACCESS_DENIED. The staged file itself is synced before
		// the atomic commit.
		return nil
	}
	if path == "" {
		path = "."
	}
	directory, err := manager.handle.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		// Windows and a few filesystems do not support syncing directory handles.
		if errors.Is(err, os.ErrInvalid) {
			return nil
		}
		return err
	}
	return nil
}

func isTemporaryOutputName(name string) bool {
	return strings.HasPrefix(name, temporaryOutputPrefix) && strings.HasSuffix(name, temporaryOutputSuffix)
}
