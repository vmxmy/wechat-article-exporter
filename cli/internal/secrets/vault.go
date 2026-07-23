package secrets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

var (
	ErrVaultUninitialized = errors.New("encrypted vault is not initialized")
	ErrVaultLocked        = errors.New("encrypted vault is locked")
)

type VaultParameters struct {
	Memory      uint32 `json:"memoryKiB"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
}

var DefaultVaultParameters = VaultParameters{Memory: 64 * 1024, Iterations: 3, Parallelism: 2}

const (
	minimumVaultMemory      = 8 * 1024
	maximumVaultMemory      = 256 * 1024
	minimumVaultIterations  = 1
	maximumVaultIterations  = 8
	minimumVaultParallelism = 1
	maximumVaultParallelism = 8
	vaultSaltSize           = 16
	maximumVaultFileSize    = 16 << 20
)

type vaultEnvelope struct {
	Version    int             `json:"version"`
	KDF        string          `json:"kdf"`
	Parameters VaultParameters `json:"parameters"`
	Salt       string          `json:"salt"`
	Nonce      string          `json:"nonce"`
	Ciphertext string          `json:"ciphertext"`
}

type vaultPayload struct {
	Secrets map[string]string `json:"secrets"`
}

type VaultStore struct {
	path       string
	parameters VaultParameters
	mu         sync.Mutex
	key        []byte
}

func NewVaultStore(path string, parameters VaultParameters) *VaultStore {
	if parameters.Memory == 0 || parameters.Iterations == 0 || parameters.Parallelism == 0 {
		parameters = DefaultVaultParameters
	}
	return &VaultStore{path: path, parameters: parameters}
}

func (*VaultStore) Backend() string { return "encrypted-vault" }

// ValidateEnvelope verifies the non-secret vault container structure without
// deriving a key or decrypting its payload. It is suitable for status output.
func (store *VaultStore) ValidateEnvelope() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	_, err := store.readEnvelope()
	return err
}

func (store *VaultStore) Initialize(passphrase []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(passphrase) == 0 {
		return errors.New("vault passphrase must not be empty")
	}
	if err := validateVaultParameters(store.parameters); err != nil {
		return err
	}
	unlock, err := lockKeyring(context.Background(), store.path+".init")
	if err != nil {
		return fmt.Errorf("lock encrypted vault initialization: %w", err)
	}
	defer unlock()
	if _, err := os.Stat(store.path); err == nil {
		return errors.New("encrypted vault is already initialized")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	key := deriveVaultKey(passphrase, salt, store.parameters)
	if err := store.writePayload(key, salt, vaultPayload{Secrets: make(map[string]string)}); err != nil {
		zeroBytes(key)
		return err
	}
	store.key = key
	return nil
}

func (store *VaultStore) Unlock(passphrase []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	envelope, err := store.readEnvelope()
	if err != nil {
		return err
	}
	salt, err := decodeVaultField("salt", envelope.Salt)
	if err != nil {
		return err
	}
	key := deriveVaultKey(passphrase, salt, envelope.Parameters)
	if _, err := decryptEnvelope(envelope, key); err != nil {
		zeroBytes(key)
		return errors.New("unlock encrypted vault: invalid passphrase or corrupted vault")
	}
	zeroBytes(store.key)
	store.key = key
	store.parameters = envelope.Parameters
	return nil
}

func (store *VaultStore) Lock() {
	store.mu.Lock()
	defer store.mu.Unlock()
	zeroBytes(store.key)
	store.key = nil
}

func (store *VaultStore) Get(_ context.Context, ref Ref) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	payload, _, err := store.readPayload()
	if err != nil {
		return nil, err
	}
	value, ok := payload.Secrets[vaultRef(ref)]
	if !ok {
		return nil, ErrNotFound
	}
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted secret: %w", err)
	}
	return decoded, nil
}

func (store *VaultStore) Set(_ context.Context, ref Ref, value []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	payload, salt, err := store.readPayload()
	if err != nil {
		return err
	}
	payload.Secrets[vaultRef(ref)] = base64.RawStdEncoding.EncodeToString(value)
	return store.writePayload(store.key, salt, payload)
}

func (store *VaultStore) Delete(_ context.Context, ref Ref) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	payload, salt, err := store.readPayload()
	if err != nil {
		return err
	}
	delete(payload.Secrets, vaultRef(ref))
	return store.writePayload(store.key, salt, payload)
}

func (store *VaultStore) DeleteProfile(profile string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	payload, salt, err := store.readPayload()
	if err != nil {
		return err
	}
	prefix := profile + "/"
	for key := range payload.Secrets {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(payload.Secrets, key)
		}
	}
	return store.writePayload(store.key, salt, payload)
}

func (store *VaultStore) readPayload() (vaultPayload, []byte, error) {
	if len(store.key) == 0 {
		if _, err := os.Stat(store.path); errors.Is(err, os.ErrNotExist) {
			return vaultPayload{}, nil, ErrVaultUninitialized
		}
		return vaultPayload{}, nil, ErrVaultLocked
	}
	envelope, err := store.readEnvelope()
	if err != nil {
		return vaultPayload{}, nil, err
	}
	salt, err := decodeVaultField("salt", envelope.Salt)
	if err != nil {
		return vaultPayload{}, nil, err
	}
	payload, err := decryptEnvelope(envelope, store.key)
	return payload, salt, err
}

func (store *VaultStore) readEnvelope() (vaultEnvelope, error) {
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return vaultEnvelope{}, ErrVaultUninitialized
	}
	if err != nil {
		return vaultEnvelope{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return vaultEnvelope{}, err
	}
	if !info.Mode().IsRegular() {
		return vaultEnvelope{}, errors.New("encrypted vault must be a regular file")
	}
	if info.Size() > maximumVaultFileSize {
		return vaultEnvelope{}, errors.New("encrypted vault exceeds supported size")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumVaultFileSize+1))
	if err != nil {
		return vaultEnvelope{}, err
	}
	if len(data) > maximumVaultFileSize {
		return vaultEnvelope{}, errors.New("encrypted vault exceeds supported size")
	}
	var envelope vaultEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return vaultEnvelope{}, fmt.Errorf("decode encrypted vault: %w", err)
	}
	if envelope.Version != 1 || envelope.KDF != "argon2id" {
		return vaultEnvelope{}, errors.New("unsupported encrypted vault format")
	}
	if err := validateVaultParameters(envelope.Parameters); err != nil {
		return vaultEnvelope{}, err
	}
	salt, err := decodeVaultField("salt", envelope.Salt)
	if err != nil || len(salt) != vaultSaltSize {
		return vaultEnvelope{}, errors.New("encrypted vault has invalid salt")
	}
	nonce, err := decodeVaultField("nonce", envelope.Nonce)
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return vaultEnvelope{}, errors.New("encrypted vault has invalid nonce")
	}
	ciphertext, err := decodeVaultField("ciphertext", envelope.Ciphertext)
	if err != nil || len(ciphertext) < chacha20poly1305.Overhead {
		return vaultEnvelope{}, errors.New("encrypted vault has invalid ciphertext")
	}
	return envelope, nil
}

func validateVaultParameters(parameters VaultParameters) error {
	if parameters.Memory < minimumVaultMemory || parameters.Memory > maximumVaultMemory ||
		parameters.Iterations < minimumVaultIterations || parameters.Iterations > maximumVaultIterations ||
		parameters.Parallelism < minimumVaultParallelism || parameters.Parallelism > maximumVaultParallelism {
		return errors.New("encrypted vault has invalid key-derivation parameters")
	}
	return nil
}

func (store *VaultStore) writePayload(key, salt []byte, payload vaultPayload) error {
	plain, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	cipher, err := chacha20poly1305.NewX(key)
	if err != nil {
		return err
	}
	nonce := make([]byte, cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	envelope := vaultEnvelope{
		Version: 1, KDF: "argon2id", Parameters: store.parameters,
		Salt: base64.RawStdEncoding.EncodeToString(salt), Nonce: base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(cipher.Seal(nil, nonce, plain, []byte("wechat-article-vault-v1"))),
	}
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded)+1 > maximumVaultFileSize {
		return errors.New("encrypted vault exceeds supported size")
	}
	return writeSecretAtomic(store.path, append(encoded, '\n'))
}

func decryptEnvelope(envelope vaultEnvelope, key []byte) (vaultPayload, error) {
	nonce, err := decodeVaultField("nonce", envelope.Nonce)
	if err != nil {
		return vaultPayload{}, err
	}
	ciphertext, err := decodeVaultField("ciphertext", envelope.Ciphertext)
	if err != nil {
		return vaultPayload{}, err
	}
	cipher, err := chacha20poly1305.NewX(key)
	if err != nil {
		return vaultPayload{}, err
	}
	plain, err := cipher.Open(nil, nonce, ciphertext, []byte("wechat-article-vault-v1"))
	if err != nil {
		return vaultPayload{}, err
	}
	var payload vaultPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return vaultPayload{}, err
	}
	if payload.Secrets == nil {
		payload.Secrets = make(map[string]string)
	}
	return payload, nil
}

func deriveVaultKey(passphrase, salt []byte, parameters VaultParameters) []byte {
	return argon2.IDKey(passphrase, salt, parameters.Iterations, parameters.Memory, parameters.Parallelism, chacha20poly1305.KeySize)
}

func decodeVaultField(name, value string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode vault %s: %w", name, err)
	}
	return decoded, nil
}

func writeSecretAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".vault-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func vaultRef(ref Ref) string { return ref.Profile + "/" + ref.Kind + "/" + ref.Name }

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

var _ Store = (*VaultStore)(nil)
