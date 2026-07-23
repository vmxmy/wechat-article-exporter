package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtimeutil"
	"golang.org/x/crypto/chacha20poly1305"
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
	runtimeutil.AssertPrivatePermissions(t, path, 0o600)
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

func TestVaultEnvelopeRejectsUnboundedKDFAndInvalidFieldLengths(t *testing.T) {
	valid := vaultEnvelope{
		Version: 1, KDF: "argon2id", Parameters: fastVaultParameters,
		Salt:       base64.RawStdEncoding.EncodeToString(make([]byte, vaultSaltSize)),
		Nonce:      base64.RawStdEncoding.EncodeToString(make([]byte, chacha20poly1305.NonceSizeX)),
		Ciphertext: base64.RawStdEncoding.EncodeToString(make([]byte, chacha20poly1305.Overhead)),
	}
	tests := []func(*vaultEnvelope){
		func(value *vaultEnvelope) { value.Parameters.Memory = maximumVaultMemory + 1 },
		func(value *vaultEnvelope) { value.Parameters.Iterations = maximumVaultIterations + 1 },
		func(value *vaultEnvelope) { value.Parameters.Parallelism = maximumVaultParallelism + 1 },
		func(value *vaultEnvelope) { value.Salt = "" },
		func(value *vaultEnvelope) { value.Nonce = base64.RawStdEncoding.EncodeToString([]byte("short")) },
		func(value *vaultEnvelope) { value.Ciphertext = "" },
	}
	for index, mutate := range tests {
		envelope := valid
		mutate(&envelope)
		path := filepath.Join(t.TempDir(), "vault.json")
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := NewVaultStore(path, fastVaultParameters).ValidateEnvelope(); err == nil {
			t.Fatalf("invalid envelope case %d validated", index)
		}
	}
}

func TestVaultInitializeRejectsInvalidConfiguredParametersBeforeDerivation(t *testing.T) {
	parameters := fastVaultParameters
	parameters.Memory = maximumVaultMemory + 1
	store := NewVaultStore(filepath.Join(t.TempDir(), "vault.json"), parameters)
	if err := store.Initialize([]byte("password")); err == nil || !strings.Contains(err.Error(), "key-derivation parameters") {
		t.Fatalf("Initialize(invalid parameters) error=%v", err)
	}
}

func TestVaultRejectsOversizedWriteWithoutReplacingExistingVault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.json")
	store := NewVaultStore(path, fastVaultParameters)
	passphrase := []byte("password")
	if err := store.Initialize(passphrase); err != nil {
		t.Fatal(err)
	}
	ref := Ref{Profile: "profile-a", Kind: "session", Name: "current"}
	if err := store.Set(context.Background(), ref, []byte("original")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	oversized := make([]byte, maximumVaultFileSize)
	if err := store.Set(context.Background(), Ref{Profile: "profile-a", Kind: "session", Name: "oversized"}, oversized); err == nil ||
		!strings.Contains(err.Error(), "exceeds supported size") {
		t.Fatalf("oversized Set error=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("oversized write replaced the existing vault")
	}
	value, err := store.Get(context.Background(), ref)
	if err != nil || string(value) != "original" {
		t.Fatalf("original secret=%q error=%v", value, err)
	}
}
