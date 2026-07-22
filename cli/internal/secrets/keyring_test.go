package secrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"
)

type fakeKeyring struct {
	mu         sync.Mutex
	values     map[string]string
	maxBytes   int
	failSet    map[string]error
	failSetNth map[string]map[int]error
	setCalls   map[string]int
	failDelete map[string]error
	setHook    func(string, int)
}

func newFakeKeyring() *fakeKeyring {
	return &fakeKeyring{
		values: make(map[string]string), failSet: make(map[string]error), failSetNth: make(map[string]map[int]error),
		setCalls: make(map[string]int), failDelete: make(map[string]error),
	}
}

func (backend *fakeKeyring) Get(service, user string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	value, ok := backend.values[service+":"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (backend *fakeKeyring) Set(service, user, password string) error {
	key := service + ":" + user
	backend.mu.Lock()
	backend.setCalls[key]++
	call := backend.setCalls[key]
	hook := backend.setHook
	if err := backend.failSet[key]; err != nil {
		backend.mu.Unlock()
		return err
	}
	if err := backend.failSetNth[key][call]; err != nil {
		backend.mu.Unlock()
		return err
	}
	if backend.maxBytes > 0 && len(password) > backend.maxBytes {
		backend.mu.Unlock()
		return keyring.ErrSetDataTooBig
	}
	backend.values[key] = password
	backend.mu.Unlock()
	if hook != nil {
		hook(key, call)
	}
	return nil
}

func (backend *fakeKeyring) Delete(service, user string) error {
	key := service + ":" + user
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := backend.failDelete[key]; err != nil {
		return err
	}
	if _, ok := backend.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(backend.values, key)
	return nil
}

func (backend *fakeKeyring) raw(key string) string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.values[key]
}

func (backend *fakeKeyring) exists(key string) bool {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	_, exists := backend.values[key]
	return exists
}

func (backend *fakeKeyring) keys() []string {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	keys := make([]string, 0, len(backend.values))
	for key := range backend.values {
		keys = append(keys, key)
	}
	return keys
}

func testKeyringStore(t *testing.T, backend *fakeKeyring) *KeyringStore {
	t.Helper()
	store := NewKeyringStore("test-service")
	store.backend = backend
	store.lockPath = t.TempDir() + "/keyring.lock"
	return store
}

func TestKeyringStoreRollsBackNewIndexWhenSecretWriteFails(t *testing.T) {
	backend := newFakeKeyring()
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "rollback", Kind: "session", Name: "current"}
	backend.failSet["test-service:"+keyringUser(ref)] = errors.New("secret unavailable")
	if err := store.Set(context.Background(), ref, []byte("secret")); err == nil || !strings.Contains(err.Error(), "secret unavailable") {
		t.Fatalf("Set() error = %v", err)
	}
	if backend.exists("test-service:" + keyringUser(ref)) {
		t.Fatal("secret remained after write failed")
	}
	if backend.exists("test-service:" + profileIndexUser(ref.Profile)) {
		t.Fatal("profile index remained after secret write failed")
	}
}

func TestKeyringStoreExistingLegacyScalarSurvivesIndexWriteFailure(t *testing.T) {
	backend := newFakeKeyring()
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "legacy", Kind: "wechat-article", Name: "credential-a"}
	backend.values["test-service:"+keyringUser(ref)] = base64Raw([]byte("old-secret"))
	backend.failSet["test-service:"+profileIndexUser(ref.Profile)] = errors.New("index unavailable")
	if err := store.Set(context.Background(), ref, []byte("new-secret")); err == nil || !strings.Contains(err.Error(), "profile index") {
		t.Fatalf("Set() error = %v", err)
	}
	got, err := store.getValueLocked(keyringUser(ref))
	if err != nil || string(got) != "old-secret" {
		t.Fatalf("legacy value after index failure = %q, %v", got, err)
	}
}

func TestDecodeKeyringChunkManifestRejectsNonHexGeneration(t *testing.T) {
	manifest := keyringChunkManifest{Version: 1, Generation: strings.Repeat("z", 64), Chunks: 1, SHA256: strings.Repeat("0", 64)}
	value, err := encodeKeyringChunkManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, chunked, err := decodeKeyringChunkManifest(value); !chunked || err == nil {
		t.Fatalf("decode result chunked=%v error=%v", chunked, err)
	}
}

func TestDecodeKeyringChunkManifestRejectsOversizedValuesBeforeDecode(t *testing.T) {
	value := keyringChunkPrefix + strings.Repeat("A", keyringMaximumManifestValueLen)
	if _, chunked, err := decodeKeyringChunkManifest(value); !chunked || err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("decode result chunked=%v error=%v", chunked, err)
	}
	manifest := keyringChunkManifest{
		Version: 3, Generation: strings.Repeat("a", 32), Chunks: 1 << 30, EncodedLen: 1 << 30, SHA256: strings.Repeat("0", 64),
	}
	encoded, err := encodeKeyringChunkManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, chunked, err := decodeKeyringChunkManifest(encoded); !chunked || err == nil {
		t.Fatalf("oversized manifest chunked=%v error=%v", chunked, err)
	}
}

func TestKeyringStoreChunksLargeSecretsAndReassemblesThem(t *testing.T) {
	backend := newFakeKeyring()
	backend.maxBytes = 1200
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "large", Kind: "wechat-session", Name: "current"}
	value := bytes.Repeat([]byte("session-cookie-payload-"), 800)
	if err := store.Set(context.Background(), ref, value); err != nil {
		t.Fatal(err)
	}
	manifestValue := backend.raw("test-service:" + keyringUser(ref))
	manifest, chunked, err := decodeKeyringChunkManifest(manifestValue)
	if err != nil || !chunked || manifest.Chunks < 2 {
		t.Fatalf("manifest = %#v, chunked=%v, error=%v", manifest, chunked, err)
	}
	for index := 0; index < manifest.Chunks; index++ {
		stored := backend.raw("test-service:" + keyringChunkUser(keyringUser(ref), manifest, index))
		if stored == "" || len(stored) > backend.maxBytes {
			t.Fatalf("chunk %d length = %d", index, len(stored))
		}
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatalf("Get() length=%d error=%v", len(got), err)
	}
}

func TestKeyringStoreSameLargeValueRewriteDoesNotReplaceGeneration(t *testing.T) {
	backend := newFakeKeyring()
	backend.maxBytes = 1200
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "same-value", Kind: "wechat-session", Name: "current"}
	value := bytes.Repeat([]byte("same-session"), 1000)
	if err := store.Set(context.Background(), ref, value); err != nil {
		t.Fatal(err)
	}
	before := backend.raw("test-service:" + keyringUser(ref))
	manifest, _, err := decodeKeyringChunkManifest(before)
	if err != nil {
		t.Fatal(err)
	}
	backend.failSet["test-service:"+keyringChunkUser(keyringUser(ref), manifest, 1)] = errors.New("must not write chunks")
	if err := store.Set(context.Background(), ref, value); err != nil {
		t.Fatalf("same-value Set() = %v", err)
	}
	if after := backend.raw("test-service:" + keyringUser(ref)); after != before {
		t.Fatal("same-value rewrite changed the manifest")
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatalf("Get() after same-value rewrite length=%d error=%v", len(got), err)
	}
}

func TestKeyringStoreFailedLargeRewriteKeepsOldValueReadable(t *testing.T) {
	backend := newFakeKeyring()
	backend.maxBytes = 1200
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "rewrite", Kind: "wechat-session", Name: "current"}
	oldValue := bytes.Repeat([]byte("old-session"), 1000)
	if err := store.Set(context.Background(), ref, oldValue); err != nil {
		t.Fatal(err)
	}
	oldManifest, _, err := decodeKeyringChunkManifest(backend.raw("test-service:" + keyringUser(ref)))
	if err != nil {
		t.Fatal(err)
	}
	newValue := bytes.Repeat([]byte("new-session"), 1000)
	backend.setHook = func(key string, _ int) {
		if strings.Contains(key, "/__chunks__/") && !strings.Contains(key, oldManifest.Generation) {
			backend.mu.Lock()
			if len(backend.failSet) == 0 {
				backend.mu.Unlock()
				return
			}
			backend.mu.Unlock()
		}
	}
	// The next generation is random; fail its second chunk after observing the
	// preservation manifest that references it.
	preservationSeen := make(chan keyringChunkManifest, 1)
	backend.setHook = func(key string, _ int) {
		if key != "test-service:"+keyringUser(ref) {
			return
		}
		raw := backend.raw(key)
		manifest, chunked, decodeErr := decodeKeyringChunkManifest(raw)
		if decodeErr == nil && chunked && len(manifest.Retired) == 1 && manifest.Retired[0].Generation != oldManifest.Generation {
			select {
			case preservationSeen <- manifest:
			default:
			}
		}
	}
	// Prime the exact generated chunk failure from a backend hook before the
	// writer reaches the chunk loop.
	backend.setHook = func(key string, _ int) {
		if key != "test-service:"+keyringUser(ref) {
			return
		}
		raw := backend.raw(key)
		manifest, chunked, decodeErr := decodeKeyringChunkManifest(raw)
		if decodeErr != nil || !chunked || len(manifest.Retired) != 1 || manifest.Retired[0].Generation == oldManifest.Generation {
			return
		}
		pending := manifest.Retired[0]
		backend.mu.Lock()
		backend.failSet["test-service:"+keyringChunkSetUser(keyringUser(ref), pending, 1)] = errors.New("second chunk failed")
		backend.mu.Unlock()
		select {
		case preservationSeen <- manifest:
		default:
		}
	}
	if err := store.Set(context.Background(), ref, newValue); err == nil || !strings.Contains(err.Error(), "second chunk failed") {
		t.Fatalf("Set(rewrite) error = %v", err)
	}
	select {
	case <-preservationSeen:
	default:
		t.Fatal("rewrite did not publish a preservation manifest")
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil || !bytes.Equal(got, oldValue) {
		t.Fatalf("old value after failed rewrite length=%d error=%v", len(got), err)
	}
}

func TestKeyringStoreDeletesChunkedSecretsAcrossProcessRestart(t *testing.T) {
	backend := newFakeKeyring()
	backend.maxBytes = 1200
	first := testKeyringStore(t, backend)
	ref := Ref{Profile: "restart", Kind: "wechat-session", Name: "current"}
	if err := first.Set(context.Background(), ref, bytes.Repeat([]byte("x"), 10000)); err != nil {
		t.Fatal(err)
	}
	second := testKeyringStore(t, backend)
	second.lockPath = first.lockPath
	if err := second.DeleteProfile("restart"); err != nil {
		t.Fatal(err)
	}
	for _, key := range backend.keys() {
		if strings.Contains(key, "restart/") {
			t.Fatalf("profile key still present after restart deletion: %s", key)
		}
	}
	if _, err := second.Get(context.Background(), ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v", err)
	}
}

func TestKeyringStorePartialDeleteProfileRetainsFailedIndexAndRetries(t *testing.T) {
	backend := newFakeKeyring()
	store := testKeyringStore(t, backend)
	left := Ref{Profile: "partial", Kind: "session", Name: "current"}
	right := Ref{Profile: "partial", Kind: "proxy-authorization", Name: "trusted"}
	if err := store.Set(context.Background(), left, []byte("left")); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), right, []byte("right")); err != nil {
		t.Fatal(err)
	}
	backend.failDelete["test-service:"+keyringUser(right)] = errors.New("delete unavailable")
	if err := store.DeleteProfile("partial"); err == nil || !strings.Contains(err.Error(), "delete unavailable") {
		t.Fatalf("DeleteProfile() error = %v", err)
	}
	indexed, err := store.profileUsersLocked("partial")
	if err != nil {
		t.Fatal(err)
	}
	if len(indexed) != 1 {
		t.Fatalf("retained index = %#v", indexed)
	}
	delete(backend.failDelete, "test-service:"+keyringUser(right))
	second := testKeyringStore(t, backend)
	second.lockPath = store.lockPath
	if err := second.DeleteProfile("partial"); err != nil {
		t.Fatal(err)
	}
	for _, key := range backend.keys() {
		if strings.Contains(key, "partial/") {
			t.Fatalf("profile key remains after retry: %s", key)
		}
	}
}

func TestKeyringStoreRejectsCrossProfileIndexWithoutDeletingSecrets(t *testing.T) {
	backend := newFakeKeyring()
	store := testKeyringStore(t, backend)
	victim := Ref{Profile: "victim", Kind: "session", Name: "current"}
	backend.values["test-service:"+keyringUser(victim)] = base64Raw([]byte("victim-secret"))
	backend.values["test-service:"+profileIndexUser("attacker")] = base64Raw([]byte(`["victim/session/current"]`))
	if err := store.DeleteProfile("attacker"); err == nil || !strings.Contains(err.Error(), "invalid user") {
		t.Fatalf("DeleteProfile(corrupt index) error = %v", err)
	}
	if !backend.exists("test-service:" + keyringUser(victim)) {
		t.Fatal("cross-profile index deleted another profile's secret")
	}
}

func TestKeyringStoreRetiredCleanupFailureRemainsReferencedAndRetries(t *testing.T) {
	backend := newFakeKeyring()
	backend.maxBytes = 1200
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "cleanup", Kind: "wechat-session", Name: "current"}
	oldValue := bytes.Repeat([]byte("old"), 4000)
	if err := store.Set(context.Background(), ref, oldValue); err != nil {
		t.Fatal(err)
	}
	oldManifest, _, err := decodeKeyringChunkManifest(backend.raw("test-service:" + keyringUser(ref)))
	if err != nil {
		t.Fatal(err)
	}
	failedChunk := "test-service:" + keyringChunkUser(keyringUser(ref), oldManifest, 0)
	backend.failDelete[failedChunk] = errors.New("cleanup unavailable")
	newValue := bytes.Repeat([]byte("new"), 4000)
	if err := store.Set(context.Background(), ref, newValue); err == nil || !strings.Contains(err.Error(), "cleanup unavailable") {
		t.Fatalf("Set(cleanup failure) error = %v", err)
	}
	indexed, err := store.profileUsersLocked(ref.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := indexed[keyringUser(ref)]; !exists {
		t.Fatal("committed value was removed from the profile index after cleanup failure")
	}
	committed, chunked, err := decodeKeyringChunkManifest(backend.raw("test-service:" + keyringUser(ref)))
	if err != nil || !chunked || len(committed.Retired) != 1 {
		t.Fatalf("committed manifest = %#v chunked=%v error=%v", committed, chunked, err)
	}
	got, err := store.Get(context.Background(), ref)
	if err != nil || !bytes.Equal(got, newValue) {
		t.Fatalf("new value after cleanup failure length=%d error=%v", len(got), err)
	}
	delete(backend.failDelete, failedChunk)
	if err := store.Set(context.Background(), ref, bytes.Repeat([]byte("newer"), 4000)); err != nil {
		t.Fatal(err)
	}
	if backend.exists(failedChunk) {
		t.Fatal("retired chunk remained after cleanup retry")
	}
}

func TestKeyringStoreSerializesCrossInstanceIndexUpdates(t *testing.T) {
	backend := newFakeKeyring()
	first := testKeyringStore(t, backend)
	second := testKeyringStore(t, backend)
	second.lockPath = first.lockPath
	refs := []Ref{
		{Profile: "concurrent", Kind: "session", Name: "first"},
		{Profile: "concurrent", Kind: "session", Name: "second"},
	}
	start := make(chan struct{})
	errorsCh := make(chan error, len(refs))
	for index, ref := range refs {
		store := first
		if index == 1 {
			store = second
		}
		go func(store *KeyringStore, ref Ref) {
			<-start
			errorsCh <- store.Set(context.Background(), ref, []byte(ref.Name))
		}(store, ref)
	}
	close(start)
	for range refs {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	users, err := first.profileUsersLocked("concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("profile index lost concurrent update: %#v", users)
	}
}

func TestKeyringStoreLockHonorsContextCancellation(t *testing.T) {
	backend := newFakeKeyring()
	first := testKeyringStore(t, backend)
	second := testKeyringStore(t, backend)
	second.lockPath = first.lockPath
	unlock, err := first.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if _, err := second.Get(ctx, Ref{Profile: "blocked", Kind: "session", Name: "current"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get(blocked) error = %v", err)
	}
}

func TestKeyringStoreRejectsMissingOrTamperedChunks(t *testing.T) {
	backend := newFakeKeyring()
	backend.maxBytes = 1200
	store := testKeyringStore(t, backend)
	ref := Ref{Profile: "corrupt", Kind: "session", Name: "current"}
	if err := store.Set(context.Background(), ref, bytes.Repeat([]byte("secret"), 2000)); err != nil {
		t.Fatal(err)
	}
	manifest, _, err := decodeKeyringChunkManifest(backend.raw("test-service:" + keyringUser(ref)))
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	delete(backend.values, "test-service:"+keyringChunkUser(keyringUser(ref), manifest, 0))
	backend.mu.Unlock()
	if _, err := store.Get(context.Background(), ref); err == nil || !strings.Contains(err.Error(), "chunk 0 is missing") {
		t.Fatalf("missing chunk error = %v", err)
	}
	backend.mu.Lock()
	backend.values["test-service:"+keyringChunkUser(keyringUser(ref), manifest, 0)] = fmt.Sprintf("%0*d", keyringChunkEncodedLimit, 0)
	backend.mu.Unlock()
	if _, err := store.Get(context.Background(), ref); err == nil {
		t.Fatal("tampered chunk error = nil")
	}
}

func TestKeyringStoreEncodesValuesAndDeletesProfile(t *testing.T) {
	backend := newFakeKeyring()
	store := testKeyringStore(t, backend)
	left := Ref{Profile: "left", Kind: "session", Name: "current"}
	right := Ref{Profile: "right", Kind: "session", Name: "current"}
	if err := store.Set(context.Background(), left, []byte{0, 1, 2, 255}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), right, []byte("right")); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(context.Background(), left)
	if err != nil || len(value) != 4 || value[3] != 255 {
		t.Fatalf("Get(left) = %v, %v", value, err)
	}
	if err := store.DeleteProfile("left"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), left); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v", err)
	}
	if value, err := store.Get(context.Background(), right); err != nil || string(value) != "right" {
		t.Fatalf("Get(right) = %q, %v", value, err)
	}
}

func base64Raw(value []byte) string {
	return base64.RawStdEncoding.EncodeToString(value)
}
