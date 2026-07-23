package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type fileCommitError struct {
	Published bool
	Err       error
}

func (err *fileCommitError) Error() string { return err.Err.Error() }
func (err *fileCommitError) Unwrap() error { return err.Err }

// commitFileNoReplace publishes a fully written temporary file without ever
// replacing an existing destination. Platform-specific implementations use an
// atomic no-replace primitive, including filesystems that do not support hard
// links.
func commitFileNoReplace(temporary, destination string) error {
	if err := platformCommitFileNoReplace(temporary, destination); err != nil {
		return err
	}
	// Persist the published destination entry while the temporary recovery
	// alias still exists. Removing the alias first can lose both names if the
	// machine crashes before the directory update reaches stable storage.
	if err := syncParentDirectory(destination); err != nil {
		return &fileCommitError{Published: true, Err: err}
	}
	cleanupErr := removeTemporaryAlias(temporary)
	durabilityErr := syncTemporaryAliasRemoval(temporary, destination)
	if cleanupErr != nil || durabilityErr != nil {
		return &fileCommitError{Published: true, Err: errors.Join(cleanupErr, durabilityErr)}
	}
	return nil
}

func syncTemporaryAliasRemoval(temporary, _ string) error {
	// This is deliberately a second sync when both names share a parent.
	return syncParentDirectory(temporary)
}

func removeTemporaryAlias(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove committed temporary alias %s: %w", filepath.Base(path), err)
	}
	return nil
}
