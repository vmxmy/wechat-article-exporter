package application

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

// workspaceOpenExportArtifact opens one manifest-listed regular file beneath
// the recorded output authorization. It keeps the descriptor open after
// validating the expected byte count and digest, preventing a rename between
// validation and browser streaming from changing the downloaded file.
func workspaceOpenExportArtifact(authorization *domain.ExportOutputAuthorization, file library.ExportFileRecord) (io.ReadCloser, error) {
	rootPath, err := workspaceAuthorizedOutputRoot(authorization)
	if err != nil {
		return nil, err
	}
	relative, err := workspaceSafeArtifactPath(file.RelativePath)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open authorized export root: %w", err)
	}
	defer root.Close()
	identity, err := root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("inspect opened export root: %w", err)
	}
	device, inode, identityErr := workspaceExportRootIdentityFromFile(identity)
	closeErr := identity.Close()
	if err := errors.Join(identityErr, closeErr); err != nil {
		return nil, fmt.Errorf("identify opened export root: %w", err)
	}
	if device != authorization.Device || inode != authorization.Inode {
		return nil, errors.New("authorized export directory was replaced")
	}

	components := strings.Split(relative, "/")
	current := root
	for _, component := range components[:len(components)-1] {
		info, err := current.Lstat(component)
		if err != nil {
			if current != root {
				_ = current.Close()
			}
			return nil, fmt.Errorf("inspect artifact directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			if current != root {
				_ = current.Close()
			}
			return nil, errors.New("artifact directory must be a non-symlink directory")
		}
		next, err := current.OpenRoot(component)
		if current != root {
			_ = current.Close()
		}
		if err != nil {
			return nil, fmt.Errorf("open artifact directory: %w", err)
		}
		current = next
	}
	defer func() {
		if current != root {
			_ = current.Close()
		}
	}()

	name := components[len(components)-1]
	info, err := current.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("artifact must be a regular non-symlink file")
	}
	artifact, err := current.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	opened, err := artifact.Stat()
	if err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("inspect opened artifact: %w", err)
	}
	if !opened.Mode().IsRegular() || opened.Size() != file.SizeBytes {
		_ = artifact.Close()
		return nil, errors.New("artifact does not match its export record")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, artifact); err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("verify artifact contents: %w", err)
	}
	if fmt.Sprintf("%x", digest.Sum(nil)) != strings.ToLower(strings.TrimSpace(file.SHA256)) {
		_ = artifact.Close()
		return nil, errors.New("artifact digest does not match its export record")
	}
	if _, err := artifact.Seek(0, io.SeekStart); err != nil {
		_ = artifact.Close()
		return nil, fmt.Errorf("rewind verified artifact: %w", err)
	}
	return artifact, nil
}
