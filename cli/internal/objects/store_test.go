package objects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreStreamsDeduplicatesAndValidates(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := strings.Repeat("wechat article object\n", 10000)
	wantHash := sha256.Sum256([]byte(contents))
	wantDigest := hex.EncodeToString(wantHash[:])
	first, err := store.Put(context.Background(), strings.NewReader(contents), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Put(context.Background(), strings.NewReader(contents), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != wantDigest || second.Digest != first.Digest || first.Size != int64(len(contents)) {
		t.Fatalf("objects = %#v, %#v", first, second)
	}
	reader, object, err := store.Open(context.Background(), first.Digest)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	read, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != contents || object.Size != first.Size {
		t.Fatalf("Open() object = %#v, bytes = %d", object, len(read))
	}
	if err := store.Validate(context.Background(), first.Digest); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(store.Root(), "sha256", "*", "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("stored object count = %d, paths = %#v", len(matches), matches)
	}
}

func TestFileStoreDetectsCorruptionAndRejectsTraversal(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.Put(context.Background(), strings.NewReader("original"), "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.pathForDigest(object.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Validate(context.Background(), object.Digest); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Validate(corrupt) error = %v", err)
	}
	if _, _, err := store.Open(context.Background(), "../../etc/passwd"); err == nil {
		t.Fatal("Open(traversal) error = nil")
	}
}

func TestFileStoreCancellationLeavesNoTemporaryObject(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Put(ctx, strings.NewReader("cancelled"), "text/plain"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Put(cancelled) error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(store.Root(), "tmp", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files = %#v", matches)
	}
}
