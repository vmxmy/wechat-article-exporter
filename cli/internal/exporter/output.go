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
	"sort"
	"strings"
	"time"

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
	ArticleID domain.ArticleID `json:"articleId,omitempty"`
	Path      string           `json:"path"`
	Size      int64            `json:"size"`
	SHA256    string           `json:"sha256"`
	Status    OutputStatus     `json:"status"`
}

type CleanupReport struct {
	Removed  []string `json:"removed"`
	Warnings []string `json:"warnings,omitempty"`
}

type OutputManager struct {
	root       string
	rootDevice uint64
	rootInode  uint64
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
	device, inode, err := fileIdentity(info)
	if err != nil {
		return nil, fmt.Errorf("identify export output root: %w", err)
	}
	return &OutputManager{root: filepath.Clean(absolute), rootDevice: device, rootInode: inode}, nil
}

func (manager *OutputManager) Root() string {
	if manager == nil {
		return ""
	}
	return manager.root
}

func (manager *OutputManager) WriteFile(
	ctx context.Context,
	relativePath string,
	policy CollisionPolicy,
	write func(io.Writer) error,
) (OutputFile, error) {
	if manager == nil || manager.root == "" {
		return OutputFile{}, errors.New("export output manager is not initialized")
	}
	if write == nil {
		return OutputFile{}, errors.New("export output writer is required")
	}
	if err := validateCollisionPolicy(policy); err != nil {
		return OutputFile{}, err
	}
	if err := manager.validateRoot(); err != nil {
		return OutputFile{}, err
	}
	canonicalPath, destination, err := manager.resolveSafeOutputPath(relativePath, true)
	if err != nil {
		return OutputFile{}, err
	}
	select {
	case <-ctx.Done():
		return OutputFile{}, ctx.Err()
	default:
	}

	existing, statErr := os.Lstat(destination)
	switch {
	case statErr == nil:
		if !existing.Mode().IsRegular() {
			return OutputFile{}, fmt.Errorf("destination %q is not a regular file: %w", canonicalPath, ErrUnsafePath)
		}
		switch policy {
		case CollisionFail:
			return OutputFile{}, fmt.Errorf("destination %q: %w", canonicalPath, ErrDestinationExists)
		case CollisionSkip:
			digest, size, err := hashRegularFile(ctx, destination)
			if err != nil {
				return OutputFile{}, fmt.Errorf("checksum skipped destination %q: %w", canonicalPath, err)
			}
			return OutputFile{Path: canonicalPath, Size: size, SHA256: digest, Status: OutputSkipped}, nil
		}
	case errors.Is(statErr, os.ErrNotExist):
	case statErr != nil:
		return OutputFile{}, fmt.Errorf("inspect destination %q: %w", canonicalPath, statErr)
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), temporaryOutputPrefix+"*"+temporaryOutputSuffix)
	if err != nil {
		return OutputFile{}, fmt.Errorf("create staging file for %q: %w", canonicalPath, err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return OutputFile{}, fmt.Errorf("secure staging file for %q: %w", canonicalPath, err)
	}
	hash := sha256.New()
	counting := &countingWriter{writer: io.MultiWriter(temporary, hash)}
	if err := writeContextWriter(ctx, counting, write); err != nil {
		return OutputFile{}, err
	}
	if err := temporary.Sync(); err != nil {
		return OutputFile{}, fmt.Errorf("sync staging file for %q: %w", canonicalPath, err)
	}
	if err := temporary.Close(); err != nil {
		return OutputFile{}, fmt.Errorf("close staging file for %q: %w", canonicalPath, err)
	}
	if err := manager.validateResolvedDestination(canonicalPath, destination); err != nil {
		return OutputFile{}, err
	}

	status := OutputWritten
	if statErr == nil {
		status = OutputReplaced
	}
	if err := commitStagedFile(temporaryPath, destination, policy); err != nil {
		return OutputFile{}, fmt.Errorf("commit output %q: %w", canonicalPath, err)
	}
	committed = true
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return OutputFile{}, fmt.Errorf("sync output directory for %q: %w", canonicalPath, err)
	}
	return OutputFile{
		Path: canonicalPath, Size: counting.count, SHA256: hex.EncodeToString(hash.Sum(nil)), Status: status,
	}, nil
}

func (manager *OutputManager) CleanupAbandoned(ctx context.Context, removeBefore time.Time) (CleanupReport, error) {
	report := CleanupReport{Removed: []string{}, Warnings: []string{}}
	if manager == nil || manager.root == "" {
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

func (manager *OutputManager) resolveSafeOutputPath(relativePath string, createParents bool) (string, string, error) {
	if err := manager.validateRoot(); err != nil {
		return "", "", err
	}
	canonical, err := normalizeRelativeOutputPath(relativePath)
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(canonical, "/")
	current := manager.root
	for index, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		switch {
		case statErr == nil:
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return "", "", fmt.Errorf("path component %q escapes or is not a directory: %w",
					strings.Join(parts[:index+1], "/"), ErrUnsafePath)
			}
		case errors.Is(statErr, os.ErrNotExist):
			if !createParents {
				return canonical, filepath.Join(manager.root, filepath.FromSlash(canonical)), nil
			}
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return "", "", fmt.Errorf("create output directory %q: %w", strings.Join(parts[:index+1], "/"), err)
			}
			createdInfo, err := os.Lstat(current)
			if err != nil || createdInfo.Mode()&os.ModeSymlink != 0 || !createdInfo.IsDir() {
				return "", "", fmt.Errorf("created output directory %q is unsafe: %w", strings.Join(parts[:index+1], "/"), ErrUnsafePath)
			}
		default:
			return "", "", fmt.Errorf("inspect output directory %q: %w", strings.Join(parts[:index+1], "/"), statErr)
		}
	}
	destination := filepath.Join(current, parts[len(parts)-1])
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("destination %q is a symlink: %w", canonical, ErrUnsafePath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect destination %q: %w", canonical, err)
	}
	return canonical, destination, nil
}

func (manager *OutputManager) validateRoot() error {
	info, err := os.Lstat(manager.root)
	if err != nil {
		return fmt.Errorf("inspect export output root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("export output root changed type: %w", ErrUnsafePath)
	}
	device, inode, err := fileIdentity(info)
	if err != nil {
		return fmt.Errorf("identify export output root: %w", err)
	}
	if device != manager.rootDevice || inode != manager.rootInode {
		return fmt.Errorf("export output root was replaced: %w", ErrUnsafePath)
	}
	return nil
}

func (manager *OutputManager) validateResolvedDestination(canonicalPath, destination string) error {
	if err := manager.validateRoot(); err != nil {
		return err
	}
	parts := strings.Split(canonicalPath, "/")
	current := manager.root
	for index, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("path component %q changed during write: %w", strings.Join(parts[:index+1], "/"), ErrUnsafePath)
		}
	}
	if filepath.Clean(destination) != filepath.Join(current, parts[len(parts)-1]) {
		return fmt.Errorf("destination %q changed during write: %w", canonicalPath, ErrUnsafePath)
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("destination %q became a symlink: %w", canonicalPath, ErrUnsafePath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination %q before commit: %w", canonicalPath, err)
	}
	return nil
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

func commitStagedFile(source, destination string, policy CollisionPolicy) error {
	switch policy {
	case CollisionFail:
		if _, err := os.Lstat(destination); err == nil {
			return ErrDestinationExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.Rename(source, destination)
	case CollisionReplace:
		if info, err := os.Lstat(destination); err == nil && !info.Mode().IsRegular() {
			return ErrUnsafePath
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return os.Rename(source, destination)
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

func hashRegularFile(ctx context.Context, path string) (string, int64, error) {
	file, err := os.Open(path)
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
	hash := sha256.New()
	written, err := copyWithContext(ctx, hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
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

func syncDirectory(path string) error {
	directory, err := os.Open(path)
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
