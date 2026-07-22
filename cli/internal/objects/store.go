package objects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrIntegrity = errors.New("object integrity check failed")

type FileStore struct {
	root string
}

func NewFileStore(root string) (*FileStore, error) {
	if root == "" {
		return nil, errors.New("object root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve object root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(absolute, "sha256"), 0o700); err != nil {
		return nil, fmt.Errorf("create object root: %w", err)
	}
	return &FileStore{root: absolute}, nil
}

func (store *FileStore) Root() string { return store.root }

func (store *FileStore) Ready() bool {
	info, err := os.Stat(filepath.Join(store.root, "sha256"))
	return err == nil && info.IsDir()
}

// Stat reports the persisted size of one content-addressed object without
// opening its body. Callers use this to distinguish an absent object from a
// corrupt digest/size collision before a staged import starts writing.
func (store *FileStore) Stat(digest string) (Object, error) {
	path, err := store.pathForDigest(digest)
	if err != nil {
		return Object{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Object{}, err
	}
	if !info.Mode().IsRegular() {
		return Object{}, fmt.Errorf("object %s is not a regular file: %w", digest, ErrIntegrity)
	}
	return Object{Digest: digest, Size: info.Size()}, nil
}

func (store *FileStore) Put(ctx context.Context, source io.Reader, mediaType string) (Object, error) {
	select {
	case <-ctx.Done():
		return Object{}, ctx.Err()
	default:
	}
	temporaryDirectory := filepath.Join(store.root, "tmp")
	if err := os.MkdirAll(temporaryDirectory, 0o700); err != nil {
		return Object{}, fmt.Errorf("create object temporary directory: %w", err)
	}
	temporary, err := os.CreateTemp(temporaryDirectory, ".object-*.tmp")
	if err != nil {
		return Object{}, fmt.Errorf("create object temporary file: %w", err)
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
		return Object{}, fmt.Errorf("secure object temporary file: %w", err)
	}
	hash := sha256.New()
	written, err := copyWithContext(ctx, io.MultiWriter(temporary, hash), source)
	if err != nil {
		return Object{}, fmt.Errorf("write object: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return Object{}, fmt.Errorf("sync object: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Object{}, fmt.Errorf("close object: %w", err)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	object := Object{Digest: digest, Size: written, MediaType: mediaType}
	finalPath, err := store.pathForDigest(digest)
	if err != nil {
		return Object{}, err
	}
	if info, statErr := os.Stat(finalPath); statErr == nil {
		if info.Size() != written {
			return Object{}, fmt.Errorf("existing object %s has unexpected size: %w", digest, ErrIntegrity)
		}
		committed = true
		_ = os.Remove(temporaryPath)
		return object, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Object{}, fmt.Errorf("inspect existing object: %w", statErr)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return Object{}, fmt.Errorf("create object digest directory: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		if info, statErr := os.Stat(finalPath); statErr == nil && info.Size() == written {
			committed = true
			return object, nil
		}
		return Object{}, fmt.Errorf("commit object: %w", err)
	}
	committed = true
	if err := os.Chmod(finalPath, 0o600); err != nil {
		return Object{}, fmt.Errorf("secure object file: %w", err)
	}
	return object, nil
}

func (store *FileStore) Open(_ context.Context, digest string) (io.ReadCloser, Object, error) {
	path, err := store.pathForDigest(digest)
	if err != nil {
		return nil, Object{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, Object{}, fmt.Errorf("open object %s: %w", digest, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, Object{}, fmt.Errorf("stat object %s: %w", digest, err)
	}
	return file, Object{Digest: digest, Size: info.Size()}, nil
}

func (store *FileStore) Validate(ctx context.Context, digest string) error {
	reader, _, err := store.Open(ctx, digest)
	if err != nil {
		return err
	}
	defer reader.Close()
	hash := sha256.New()
	if _, err := copyWithContext(ctx, hash, reader); err != nil {
		return fmt.Errorf("hash object %s: %w", digest, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != digest {
		return fmt.Errorf("object %s produced digest %s: %w", digest, actual, ErrIntegrity)
	}
	return nil
}

func (store *FileStore) pathForDigest(digest string) (string, error) {
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return "", errors.New("object digest must be a lowercase SHA-256 hex string")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", errors.New("object digest must be a lowercase SHA-256 hex string")
	}
	return filepath.Join(store.root, "sha256", digest[:2], digest[2:4], digest), nil
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

var _ Store = (*FileStore)(nil)
