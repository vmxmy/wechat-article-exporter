package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	keyring "github.com/zalando/go-keyring"
)

const defaultKeyringService = "wechat-article-exporter"

const (
	keyringChunkEncodedLimit       = 1024
	keyringMaximumSecretSize       = 16 << 20
	keyringMaximumManifestValueLen = 32 << 10
	keyringChunkPrefix             = "wechat-article-keyring-chunks:v1:"
	keyringProfileIndexKind        = "__profile_index__"
	keyringMaximumRetired          = 8
)

type keyringChunkSet struct {
	Generation string `json:"generation"`
	Chunks     int    `json:"chunks"`
	EncodedLen int    `json:"encodedLength,omitempty"`
}

type keyringChunkManifest struct {
	Version    int               `json:"version"`
	Generation string            `json:"generation,omitempty"`
	Chunks     int               `json:"chunks,omitempty"`
	EncodedLen int               `json:"encodedLength,omitempty"`
	Inline     string            `json:"inline,omitempty"`
	SHA256     string            `json:"sha256,omitempty"`
	Absent     bool              `json:"absent,omitempty"`
	Retired    []keyringChunkSet `json:"retired,omitempty"`
}

type keyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (systemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

type KeyringStore struct {
	service  string
	backend  keyringBackend
	lockPath string
	mu       sync.Mutex
}

func NewKeyringStore(service string) *KeyringStore {
	if strings.TrimSpace(service) == "" {
		service = defaultKeyringService
	}
	return &KeyringStore{
		service: service, backend: systemKeyring{}, lockPath: defaultKeyringLockPath(service),
	}
}

func defaultKeyringLockPath(service string) string {
	root, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(root) == "" {
		root = os.TempDir()
	}
	digest := sha256.Sum256([]byte(service))
	return filepath.Join(root, "wechat-article-exporter", "keyring-locks", hex.EncodeToString(digest[:])[:24]+".lock")
}

func (*KeyringStore) Backend() string { return "os-keyring" }

func (store *KeyringStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	unlock, err := store.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := validateKeyringRef(ref); err != nil {
		return nil, err
	}
	user := keyringUser(ref)
	value, err := store.getValueLocked(user)
	if err != nil {
		return nil, err
	}
	if err := store.ensureProfileUserLocked(ref.Profile, user); err != nil {
		return nil, fmt.Errorf("index existing OS credential: %w", err)
	}
	return value, nil
}

func (store *KeyringStore) Set(ctx context.Context, ref Ref, value []byte) error {
	unlock, err := store.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := validateKeyringRef(ref); err != nil {
		return err
	}
	if len(value) > keyringMaximumSecretSize {
		return fmt.Errorf("write OS credential: secret exceeds %d bytes", keyringMaximumSecretSize)
	}
	user := keyringUser(ref)
	users, err := store.profileUsersLocked(ref.Profile)
	if err != nil {
		return err
	}
	_, indexed := users[user]
	if !indexed {
		users[user] = struct{}{}
		if err := store.writeProfileUsersLocked(ref.Profile, users); err != nil {
			return err
		}
	}
	if err := store.setValueLocked(user, value); err != nil {
		committed, committedErr := store.getValueLocked(user)
		if committedErr == nil && bytes.Equal(committed, value) {
			return err
		}
		if !indexed {
			delete(users, user)
			return errors.Join(err, committedErr, store.writeProfileUsersLocked(ref.Profile, users))
		}
		return errors.Join(err, committedErr)
	}
	return nil
}

func (store *KeyringStore) Delete(ctx context.Context, ref Ref) error {
	unlock, err := store.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err := validateKeyringRef(ref); err != nil {
		return err
	}
	users, err := store.profileUsersLocked(ref.Profile)
	if err != nil {
		return err
	}
	user := keyringUser(ref)
	if err := store.deleteValueLocked(user); err != nil {
		return err
	}
	delete(users, user)
	return store.writeProfileUsersLocked(ref.Profile, users)
}

func (store *KeyringStore) DeleteProfile(profile string) error {
	unlock, err := store.lock(context.Background())
	if err != nil {
		return err
	}
	defer unlock()
	if err := validateKeyringPart("profile", profile); err != nil {
		return err
	}
	users, err := store.profileUsersLocked(profile)
	if err != nil {
		return err
	}
	failed := make(map[string]struct{})
	var deleteErrors []error
	for user := range users {
		if err := store.deleteValueLocked(user); err != nil {
			deleteErrors = append(deleteErrors, fmt.Errorf("delete %q: %w", user, err))
			failed[user] = struct{}{}
		}
	}
	if len(failed) > 0 {
		if err := store.writeProfileUsersLocked(profile, failed); err != nil {
			deleteErrors = append(deleteErrors, err)
		}
		return errors.Join(deleteErrors...)
	}
	if err := store.deleteValueLocked(profileIndexUser(profile)); err != nil {
		return err
	}
	return nil
}

func (store *KeyringStore) lock(ctx context.Context) (func(), error) {
	store.mu.Lock()
	unlockFile, err := lockKeyring(ctx, store.lockPath)
	if err != nil {
		store.mu.Unlock()
		return nil, fmt.Errorf("lock OS credential store: %w", err)
	}
	return func() {
		unlockFile()
		store.mu.Unlock()
	}, nil
}

func validateKeyringRef(ref Ref) error {
	if err := validateKeyringPart("profile", ref.Profile); err != nil {
		return err
	}
	if err := validateKeyringPart("kind", ref.Kind); err != nil {
		return err
	}
	if ref.Kind == keyringProfileIndexKind || ref.Kind == "__chunks__" {
		return errors.New("invalid OS credential kind")
	}
	return validateKeyringPart("name", ref.Name)
}

func validateKeyringPart(label, value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "/\\\x00") {
		return fmt.Errorf("invalid OS credential %s", label)
	}
	return nil
}

func keyringUser(ref Ref) string { return ref.Profile + "/" + ref.Kind + "/" + ref.Name }

func profileIndexUser(profile string) string { return profile + "/" + keyringProfileIndexKind }

func keyringChunkUser(user string, manifest keyringChunkManifest, index int) string {
	return fmt.Sprintf("%s/__chunks__/%s/%06d", user, manifest.Generation, index)
}

func keyringChunkSetUser(user string, chunks keyringChunkSet, index int) string {
	return fmt.Sprintf("%s/__chunks__/%s/%06d", user, chunks.Generation, index)
}

func (store *KeyringStore) getValueLocked(user string) ([]byte, error) {
	value, err := store.backend.Get(store.service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read OS credential: %w", err)
	}
	manifest, chunked, err := decodeKeyringChunkManifest(value)
	if err != nil {
		return nil, fmt.Errorf("decode OS credential manifest: %w", err)
	}
	if chunked && manifest.Absent {
		return nil, ErrNotFound
	}
	encoded := value
	if chunked {
		if manifest.Inline != "" {
			encoded = manifest.Inline
		} else {
			var builder strings.Builder
			if manifest.EncodedLen > 0 {
				builder.Grow(manifest.EncodedLen)
			}
			for index := 0; index < manifest.Chunks; index++ {
				chunk, chunkErr := store.backend.Get(store.service, keyringChunkUser(user, manifest, index))
				if errors.Is(chunkErr, keyring.ErrNotFound) {
					return nil, fmt.Errorf("read OS credential: chunk %d is missing", index)
				}
				if chunkErr != nil {
					return nil, fmt.Errorf("read OS credential chunk %d: %w", index, chunkErr)
				}
				if len(chunk) == 0 || len(chunk) > keyringChunkEncodedLimit || (index < manifest.Chunks-1 && len(chunk) != keyringChunkEncodedLimit) {
					return nil, fmt.Errorf("read OS credential: chunk %d has invalid length", index)
				}
				if manifest.EncodedLen > 0 && builder.Len()+len(chunk) > manifest.EncodedLen {
					return nil, fmt.Errorf("read OS credential: chunk %d exceeds encoded length", index)
				}
				builder.WriteString(chunk)
			}
			if manifest.EncodedLen > 0 && builder.Len() != manifest.EncodedLen {
				return nil, errors.New("read OS credential: encoded length mismatch")
			}
			encoded = builder.String()
		}
	}
	maximumEncodedLength := base64.RawStdEncoding.EncodedLen(keyringMaximumSecretSize)
	if len(encoded) > maximumEncodedLength {
		return nil, errors.New("decode OS credential: value exceeds size limit")
	}
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode OS credential: %w", err)
	}
	if chunked {
		digest := sha256.Sum256(decoded)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), manifest.SHA256) {
			return nil, errors.New("decode OS credential: chunk checksum mismatch")
		}
	}
	return decoded, nil
}

func (store *KeyringStore) setValueLocked(user string, value []byte) error {
	if len(value) > keyringMaximumSecretSize {
		return fmt.Errorf("write OS credential: secret exceeds %d bytes", keyringMaximumSecretSize)
	}
	encoded := base64.RawStdEncoding.EncodeToString(value)
	oldRaw, oldErr := store.backend.Get(store.service, user)
	if oldErr != nil && !errors.Is(oldErr, keyring.ErrNotFound) {
		return fmt.Errorf("read existing OS credential: %w", oldErr)
	}
	oldPresent := oldErr == nil
	oldManifest, oldChunked, err := decodeKeyringChunkManifest(oldRaw)
	if oldPresent && err != nil {
		return fmt.Errorf("decode existing OS credential manifest: %w", err)
	}

	if oldChunked && len(oldManifest.Retired) > 0 {
		remaining, cleanupErr := store.cleanupRetiredLocked(user, oldManifest.Retired)
		oldManifest.Retired = remaining
		persisted, persistErr := store.persistManifestLocked(user, oldManifest)
		if persistErr != nil {
			return errors.Join(fmt.Errorf("persist OS credential cleanup state: %w", persistErr), cleanupErr)
		}
		oldRaw = persisted
		oldPresent = persisted != ""
		oldChunked = strings.HasPrefix(persisted, keyringChunkPrefix)
		if cleanupErr != nil {
			return fmt.Errorf("clean retired OS credential chunks: %w", cleanupErr)
		}
		if !oldPresent {
			oldManifest = keyringChunkManifest{}
		}
	}

	var oldValue []byte
	if oldPresent && !(oldChunked && oldManifest.Absent) {
		oldValue, err = store.getValueLocked(user)
		if err != nil {
			return err
		}
		if bytes.Equal(oldValue, value) {
			return nil
		}
		if oldChunked && oldManifest.EncodedLen == 0 {
			oldManifest.EncodedLen = base64.RawStdEncoding.EncodedLen(len(oldValue))
		}
	}

	oldActive := keyringChunkSet{}
	if oldChunked && !oldManifest.Absent && oldManifest.Chunks > 0 {
		oldActive = keyringChunkSet{
			Generation: oldManifest.Generation, Chunks: oldManifest.Chunks, EncodedLen: oldManifest.EncodedLen,
		}
	}

	if len(encoded) <= keyringChunkEncodedLimit && oldActive.Chunks == 0 {
		if err := store.backend.Set(store.service, user, encoded); err != nil {
			return fmt.Errorf("write OS credential: %w", err)
		}
		return nil
	}

	digest := sha256.Sum256(value)
	newManifest := keyringChunkManifest{
		Version: 3, EncodedLen: len(encoded), SHA256: hex.EncodeToString(digest[:]),
	}
	if oldActive.Chunks > 0 {
		newManifest.Retired = []keyringChunkSet{oldActive}
	}

	if len(encoded) <= keyringChunkEncodedLimit {
		newManifest.Inline = encoded
	} else {
		generation, err := randomKeyringGeneration()
		if err != nil {
			return fmt.Errorf("create OS credential generation: %w", err)
		}
		newManifest.Generation = generation
		newManifest.Chunks = (len(encoded) + keyringChunkEncodedLimit - 1) / keyringChunkEncodedLimit
		pending := keyringChunkSet{Generation: generation, Chunks: newManifest.Chunks, EncodedLen: len(encoded)}
		preserved, err := preservationManifest(oldPresent, oldChunked, oldRaw, oldManifest, oldValue, pending)
		if err != nil {
			return err
		}
		preservedRaw, err := store.persistManifestLocked(user, preserved)
		if err != nil {
			return fmt.Errorf("record pending OS credential chunks: %w", err)
		}
		for index := 0; index < newManifest.Chunks; index++ {
			start := index * keyringChunkEncodedLimit
			end := min(start+keyringChunkEncodedLimit, len(encoded))
			if err := store.backend.Set(store.service, keyringChunkUser(user, newManifest, index), encoded[start:end]); err != nil {
				cleanupErr := store.deleteChunkSetLocked(user, pending)
				restoreErr := error(nil)
				if cleanupErr == nil {
					restoreErr = store.restoreRootLocked(user, oldPresent, oldRaw)
				}
				return errors.Join(fmt.Errorf("write OS credential chunk %d: %w", index, err), cleanupErr, restoreErr)
			}
		}
		_ = preservedRaw
	}

	manifestValue, err := encodeKeyringChunkManifest(newManifest)
	if err != nil {
		return err
	}
	if err := store.backend.Set(store.service, user, manifestValue); err != nil {
		cleanupErr := store.deleteActiveChunksLocked(user, newManifest)
		restoreErr := error(nil)
		if cleanupErr == nil {
			restoreErr = store.restoreRootLocked(user, oldPresent, oldRaw)
		}
		return errors.Join(fmt.Errorf("write OS credential manifest: %w", err), cleanupErr, restoreErr)
	}

	remaining, cleanupErr := store.cleanupRetiredLocked(user, newManifest.Retired)
	newManifest.Retired = remaining
	if _, err := store.persistManifestLocked(user, newManifest); err != nil {
		return errors.Join(fmt.Errorf("persist OS credential cleanup state: %w", err), cleanupErr)
	}
	if cleanupErr != nil {
		return fmt.Errorf("clean retired OS credential chunks after committed write: %w", cleanupErr)
	}
	return nil
}

func preservationManifest(oldPresent, oldChunked bool, oldRaw string, oldManifest keyringChunkManifest, oldValue []byte, pending keyringChunkSet) (keyringChunkManifest, error) {
	if oldChunked {
		preserved := oldManifest
		preserved.Version = 3
		preserved.Retired = append(append([]keyringChunkSet(nil), oldManifest.Retired...), pending)
		if len(preserved.Retired) > keyringMaximumRetired {
			return keyringChunkManifest{}, fmt.Errorf("write OS credential: %d retired chunk generations require cleanup", len(preserved.Retired))
		}
		return preserved, nil
	}
	if oldPresent {
		if len(oldRaw) > keyringChunkEncodedLimit {
			return keyringChunkManifest{}, errors.New("write OS credential: existing scalar exceeds keyring limit")
		}
		digest := sha256.Sum256(oldValue)
		return keyringChunkManifest{
			Version: 3, Inline: oldRaw, EncodedLen: len(oldRaw), SHA256: hex.EncodeToString(digest[:]),
			Retired: []keyringChunkSet{pending},
		}, nil
	}
	return keyringChunkManifest{Version: 3, Absent: true, Retired: []keyringChunkSet{pending}}, nil
}

func (store *KeyringStore) restoreRootLocked(user string, oldPresent bool, oldRaw string) error {
	if oldPresent {
		if err := store.backend.Set(store.service, user, oldRaw); err != nil {
			return fmt.Errorf("restore existing OS credential: %w", err)
		}
		return nil
	}
	if err := store.backend.Delete(store.service, user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("remove pending OS credential manifest: %w", err)
	}
	return nil
}

func randomKeyringGeneration() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (store *KeyringStore) deleteValueLocked(user string) error {
	value, err := store.backend.Get(store.service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OS credential before deletion: %w", err)
	}
	manifest, chunked, err := decodeKeyringChunkManifest(value)
	if err != nil {
		return fmt.Errorf("decode OS credential manifest before deletion: %w", err)
	}
	if chunked {
		var deleteErrors []error
		if err := store.deleteActiveChunksLocked(user, manifest); err != nil {
			deleteErrors = append(deleteErrors, err)
		}
		for _, retired := range manifest.Retired {
			if err := store.deleteChunkSetLocked(user, retired); err != nil {
				deleteErrors = append(deleteErrors, err)
			}
		}
		if len(deleteErrors) > 0 {
			return errors.Join(deleteErrors...)
		}
	}
	if err := store.backend.Delete(store.service, user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete OS credential: %w", err)
	}
	return nil
}

func (store *KeyringStore) deleteActiveChunksLocked(user string, manifest keyringChunkManifest) error {
	if manifest.Chunks == 0 {
		return nil
	}
	return store.deleteChunkSetLocked(user, keyringChunkSet{
		Generation: manifest.Generation, Chunks: manifest.Chunks, EncodedLen: manifest.EncodedLen,
	})
}

func (store *KeyringStore) deleteChunkSetLocked(user string, chunks keyringChunkSet) error {
	var deleteErrors []error
	for index := 0; index < chunks.Chunks; index++ {
		if err := store.backend.Delete(store.service, keyringChunkSetUser(user, chunks, index)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			deleteErrors = append(deleteErrors, fmt.Errorf("delete OS credential chunk %d: %w", index, err))
		}
	}
	return errors.Join(deleteErrors...)
}

func (store *KeyringStore) cleanupRetiredLocked(user string, retired []keyringChunkSet) ([]keyringChunkSet, error) {
	remaining := make([]keyringChunkSet, 0, len(retired))
	var cleanupErrors []error
	for _, chunks := range retired {
		if err := store.deleteChunkSetLocked(user, chunks); err != nil {
			remaining = append(remaining, chunks)
			cleanupErrors = append(cleanupErrors, err)
		}
	}
	return remaining, errors.Join(cleanupErrors...)
}

func (store *KeyringStore) persistManifestLocked(user string, manifest keyringChunkManifest) (string, error) {
	if manifest.Absent && len(manifest.Retired) == 0 {
		if err := store.backend.Delete(store.service, user); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return "", err
		}
		return "", nil
	}
	if manifest.Inline != "" && len(manifest.Retired) == 0 {
		if err := store.backend.Set(store.service, user, manifest.Inline); err != nil {
			return "", err
		}
		return manifest.Inline, nil
	}
	manifest.Version = 3
	encoded, err := encodeKeyringChunkManifest(manifest)
	if err != nil {
		return "", err
	}
	if err := store.backend.Set(store.service, user, encoded); err != nil {
		return "", err
	}
	return encoded, nil
}

func encodeKeyringChunkManifest(manifest keyringChunkManifest) (string, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	value := keyringChunkPrefix + base64.RawURLEncoding.EncodeToString(encoded)
	if len(value) > keyringMaximumManifestValueLen {
		return "", errors.New("OS credential manifest exceeds size limit")
	}
	return value, nil
}

func decodeKeyringChunkManifest(value string) (keyringChunkManifest, bool, error) {
	if !strings.HasPrefix(value, keyringChunkPrefix) {
		return keyringChunkManifest{}, false, nil
	}
	if len(value) > keyringMaximumManifestValueLen {
		return keyringChunkManifest{}, true, errors.New("chunk manifest exceeds size limit")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, keyringChunkPrefix))
	if err != nil {
		return keyringChunkManifest{}, true, err
	}
	var manifest keyringChunkManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return keyringChunkManifest{}, true, err
	}
	maximumEncodedLength := base64.RawStdEncoding.EncodedLen(keyringMaximumSecretSize)
	maximumChunks := (maximumEncodedLength + keyringChunkEncodedLimit - 1) / keyringChunkEncodedLimit
	if manifest.Version < 1 || manifest.Version > 3 || len(manifest.Retired) > keyringMaximumRetired {
		return keyringChunkManifest{}, true, errors.New("invalid chunk manifest")
	}
	if manifest.Absent {
		if manifest.Version != 3 || manifest.Generation != "" || manifest.Chunks != 0 || manifest.EncodedLen != 0 || manifest.Inline != "" || manifest.SHA256 != "" || len(manifest.Retired) == 0 {
			return keyringChunkManifest{}, true, errors.New("invalid absent chunk manifest")
		}
	} else {
		inline := manifest.Inline != ""
		validActive := inline && manifest.Chunks == 0 && manifest.Generation == "" && manifest.EncodedLen == len(manifest.Inline) ||
			!inline && validChunkSet(keyringChunkSet{Generation: manifest.Generation, Chunks: manifest.Chunks, EncodedLen: manifest.EncodedLen}, maximumChunks, maximumEncodedLength, manifest.Version == 1)
		if !validActive || manifest.EncodedLen > maximumEncodedLength || !validSHA256Hex(manifest.SHA256) {
			return keyringChunkManifest{}, true, errors.New("invalid chunk manifest")
		}
	}
	for _, retired := range manifest.Retired {
		if !validChunkSet(retired, maximumChunks, maximumEncodedLength, true) {
			return keyringChunkManifest{}, true, errors.New("invalid retired chunk manifest")
		}
	}
	return manifest, true, nil
}

func validChunkSet(chunks keyringChunkSet, maximumChunks, maximumEncodedLength int, legacy bool) bool {
	validGeneration := validGenerationHex(chunks.Generation) || legacy && validSHA256Hex(chunks.Generation)
	if chunks.Chunks <= 0 || chunks.Chunks > maximumChunks || !validGeneration {
		return false
	}
	if legacy && chunks.EncodedLen == 0 {
		return true
	}
	minimumEncodedLength := (chunks.Chunks-1)*keyringChunkEncodedLimit + 1
	return chunks.EncodedLen >= minimumEncodedLength && chunks.EncodedLen <= chunks.Chunks*keyringChunkEncodedLimit && chunks.EncodedLen <= maximumEncodedLength
}

func validGenerationHex(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (store *KeyringStore) profileUsersLocked(profile string) (map[string]struct{}, error) {
	users := make(map[string]struct{})
	encoded, err := store.getValueLocked(profileIndexUser(profile))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("read OS credential profile index: %w", err)
	}
	if err == nil {
		var persisted []string
		if err := json.Unmarshal(encoded, &persisted); err != nil {
			return nil, fmt.Errorf("decode OS credential profile index: %w", err)
		}
		for _, user := range persisted {
			if !validProfileUser(profile, user) {
				return nil, fmt.Errorf("decode OS credential profile index: invalid user %q", user)
			}
			users[user] = struct{}{}
		}
	}
	// These names predate the persisted profile index. Other legacy credentials
	// are re-indexed when first read or updated.
	for _, ref := range []Ref{
		{Profile: profile, Kind: "wechat-session", Name: "current"},
		{Profile: profile, Kind: "session", Name: "current"},
	} {
		user := keyringUser(ref)
		if _, exists := users[user]; exists {
			continue
		}
		if _, err := store.backend.Get(store.service, user); err == nil {
			users[user] = struct{}{}
		} else if !errors.Is(err, keyring.ErrNotFound) {
			return nil, fmt.Errorf("probe legacy OS credential %q: %w", user, err)
		}
	}
	return users, nil
}

func validProfileUser(profile, user string) bool {
	prefix := profile + "/"
	if !strings.HasPrefix(user, prefix) || user == profileIndexUser(profile) || strings.Contains(user, "/__chunks__/") {
		return false
	}
	remainder := strings.TrimPrefix(user, prefix)
	parts := strings.Split(remainder, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && parts[0] != keyringProfileIndexKind
}

func (store *KeyringStore) ensureProfileUserLocked(profile, user string) error {
	users, err := store.profileUsersLocked(profile)
	if err != nil {
		return err
	}
	if _, exists := users[user]; exists {
		return nil
	}
	users[user] = struct{}{}
	return store.writeProfileUsersLocked(profile, users)
}

func (store *KeyringStore) writeProfileUsersLocked(profile string, users map[string]struct{}) error {
	if len(users) == 0 {
		return store.deleteValueLocked(profileIndexUser(profile))
	}
	items := make([]string, 0, len(users))
	for user := range users {
		if !validProfileUser(profile, user) {
			return fmt.Errorf("write OS credential profile index: invalid user %q", user)
		}
		items = append(items, user)
	}
	sort.Strings(items)
	encoded, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err := store.setValueLocked(profileIndexUser(profile), encoded); err != nil {
		return fmt.Errorf("write OS credential profile index: %w", err)
	}
	return nil
}

var _ Store = (*KeyringStore)(nil)
