//go:build integration

package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPlatformKeyringIntegration(t *testing.T) {
	if os.Getenv("WECHAT_ARTICLE_KEYRING_INTEGRATION") != "1" {
		t.Logf("KEYRING_SMOKE_SKIP platform=%s reason=opt-in-disabled", runtime.GOOS)
		t.Skip("set WECHAT_ARTICLE_KEYRING_INTEGRATION=1 to exercise the native credential service")
	}
	required := os.Getenv("WECHAT_ARTICLE_KEYRING_REQUIRED") == "1"
	service := fmt.Sprintf("wechat-article-exporter-integration-%s", uuid.NewString())
	profile := "integration-" + uuid.NewString()
	ref := Ref{Profile: profile, Kind: "native-smoke", Name: "roundtrip"}
	value := []byte("keyring-smoke-" + uuid.NewString())
	store := NewKeyringStore(service)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() {
		_ = store.Delete(context.Background(), ref)
		_ = store.DeleteProfile(profile)
	})

	if err := store.Set(ctx, ref, value); err != nil {
		handleUnavailablePlatformKeyring(t, required, "set", err)
	}
	got, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("native keyring get after set: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Fatal("native keyring roundtrip returned different bytes")
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("native keyring delete: %v", err)
	}
	if _, err := store.Get(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("native keyring get after delete = %v, want ErrNotFound", err)
	}
	t.Logf("KEYRING_SMOKE_PASS platform=%s backend=%s operations=set,get,delete", runtime.GOOS, store.Backend())
}

func handleUnavailablePlatformKeyring(t *testing.T, required bool, operation string, err error) {
	t.Helper()
	if required || !platformKeyringUnavailable(err) {
		t.Fatalf("native keyring %s: %v", operation, err)
	}
	t.Logf("KEYRING_SMOKE_SKIP platform=%s reason=credential-service-unavailable operation=%s", runtime.GOOS, operation)
	t.Skip("native credential service is unavailable on this runner")
}

func platformKeyringUnavailable(err error) bool {
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"dbus", "org.freedesktop.secrets", "secret service", "secret-service", "secretservice",
		"the name is not activatable", "no such interface", "keychain is not available", "security executable",
		"credential manager is not available", "the stub received bad data", "element not found", "not supported",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
