package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var fastVaultParameters = VaultParameters{Memory: 8 * 1024, Iterations: 1, Parallelism: 1}

func TestVaultRequiresInitializationAndUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	store := NewVaultStore(path, fastVaultParameters)
	ref := Ref{Profile: "profile-a", Kind: "session", Name: "current"}
	if err := store.Set(context.Background(), ref, []byte("secret")); !errors.Is(err, ErrVaultUninitialized) {
		t.Fatalf("Set(uninitialized) error = %v", err)
	}
	if err := store.Initialize([]byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), ref, []byte("secret-cookie")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-cookie") {
		t.Fatalf("vault contains plaintext: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("vault mode = %o", info.Mode().Perm())
	}
	store.Lock()
	if _, err := store.Get(context.Background(), ref); !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("Get(locked) error = %v", err)
	}
	if err := store.Unlock([]byte("wrong")); err == nil {
		t.Fatal("Unlock(wrong) error = nil")
	}
	if err := store.Unlock([]byte("correct horse battery staple")); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(context.Background(), ref)
	if err != nil || string(value) != "secret-cookie" {
		t.Fatalf("Get(unlocked) = %q, %v", value, err)
	}
}

func TestVaultDeleteProfilePreservesOtherProfiles(t *testing.T) {
	store := NewVaultStore(filepath.Join(t.TempDir(), "vault.json"), fastVaultParameters)
	if err := store.Initialize([]byte("password")); err != nil {
		t.Fatal(err)
	}
	left := Ref{Profile: "left", Kind: "session", Name: "current"}
	right := Ref{Profile: "right", Kind: "session", Name: "current"}
	if err := store.Set(context.Background(), left, []byte("left")); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), right, []byte("right")); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProfile("left"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), left); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(left) error = %v", err)
	}
	value, err := store.Get(context.Background(), right)
	if err != nil || string(value) != "right" {
		t.Fatalf("Get(right) = %q, %v", value, err)
	}
}

func TestVaultDetectsTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	store := NewVaultStore(path, fastVaultParameters)
	if err := store.Initialize([]byte("password")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store.Lock()
	if err := store.Unlock([]byte("password")); err == nil {
		t.Fatal("Unlock(tampered) error = nil")
	}
}
