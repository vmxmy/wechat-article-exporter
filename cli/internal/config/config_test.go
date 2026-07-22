package config

import (
	"path/filepath"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtimeutil"
)

func TestStoreWritesMode0600AndKeepsExistingConfigShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cli.json")
	store := NewStore(path)
	want := File{
		Server: "https://example.com",
		Tokens: &Tokens{AccessToken: "access", RefreshToken: "refresh", TokenType: "bearer"},
		ClientInformation: &ClientInformation{
			ClientID:     "client-id",
			RedirectURIs: []string{"http://127.0.0.1:1234/callback"},
		},
	}
	if err := store.Write(want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	runtimeutil.AssertPrivatePermissions(t, path, 0o600)
	got, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Server != want.Server || got.Tokens.AccessToken != "access" || got.ClientInformation.ClientID != "client-id" {
		t.Fatalf("Read() = %#v, want compatible config", got)
	}
}

func TestClearSessionPreservesServerOnly(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "cli.json"))
	if err := store.Write(File{Server: "https://example.com", Tokens: &Tokens{AccessToken: "secret"}}); err != nil {
		t.Fatal(err)
	}
	cleared, err := store.ClearSession()
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Server != "https://example.com" || cleared.Tokens != nil {
		t.Fatalf("ClearSession() = %#v", cleared)
	}
}
