package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtimeutil"
)

func TestConfigStoreWritesAtomicVersionedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile", "config.json")
	store := NewConfigStore(path)
	configuration := DefaultConfig("profile-a")
	configuration.Preferences.ExportRoot = "/exports"
	configuration.Extensions = json.RawMessage(`{"future":{"enabled":true}}`)
	if err := store.Write(configuration); err != nil {
		t.Fatal(err)
	}
	runtimeutil.AssertPrivatePermissions(t, path, 0o600)
	read, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if read.SchemaVersion != CurrentConfigVersion || read.ProfileID != "profile-a" || !jsonEqual(read.Extensions, configuration.Extensions) {
		t.Fatalf("Read() = %#v", read)
	}
}

func jsonEqual(left, right []byte) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}

func TestConfigStoreMigratesVersionZeroWithBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := []byte(`{"profileId":"legacy","downloadConcurrency":7,"exportRoot":"/legacy","extensions":{"keep":1}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, backup, err := NewConfigStore(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("migration backup is empty")
	}
	backedUp, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backedUp) != string(legacy) {
		t.Fatalf("backup = %s", backedUp)
	}
	if configuration.SchemaVersion != CurrentConfigVersion || configuration.Preferences.Download.Concurrency != 7 ||
		configuration.Preferences.DownloadConcurrency != 7 || configuration.Preferences.Export.Root != "/legacy" ||
		configuration.Preferences.ExportRoot != "/legacy" {
		t.Fatalf("migrated config = %#v", configuration)
	}
}

func TestConfigStoreRoundTripsAllRetainedPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewConfigStore(path)
	configuration := DefaultConfig("profile-a")
	configuration.Preferences = Preferences{
		Sync: SyncPreferences{
			Range: "point", DatePoint: time.Date(2025, 2, 3, 0, 0, 0, 0, time.UTC),
			PageDelay: 2 * time.Second, Jitter: 250 * time.Millisecond, PageSize: 13,
			Incremental: false, UnsafePacingSaved: true,
		},
		Download: DownloadPreferences{Concurrency: 9, ForceContent: true, MetadataOverridesContent: true},
		Export: ExportPreferences{
			Root: "/tmp/exports", NamingTemplate: "{account}/{title}", MaximumNameBytes: 120,
			CollisionPolicy: "suffix", ExcelIncludeContent: false, JSONIncludeContent: false,
			JSONIncludeComments: false, HTMLIncludeComments: false,
		},
		Display: DisplayPreferences{NoColor: true, ASCII: true, Plain: true, HideDeleted: false},
		Proxy:   ProxyPreferences{DirectFirst: false, FallbackEnabled: true},
	}
	if err := store.Write(configuration); err != nil {
		t.Fatal(err)
	}
	read, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read.Preferences.Sync, configuration.Preferences.Sync) ||
		!reflect.DeepEqual(read.Preferences.Download, configuration.Preferences.Download) ||
		!reflect.DeepEqual(read.Preferences.Export, configuration.Preferences.Export) ||
		!reflect.DeepEqual(read.Preferences.Display, configuration.Preferences.Display) ||
		!reflect.DeepEqual(read.Preferences.Proxy, configuration.Preferences.Proxy) {
		t.Fatalf("round-tripped preferences = %#v, want %#v", read.Preferences, configuration.Preferences)
	}
	if read.Preferences.DownloadConcurrency != 9 || read.Preferences.ExportRoot != "/tmp/exports" || !read.Preferences.NoColor {
		t.Fatalf("compatibility preferences were not normalized: %#v", read.Preferences)
	}
}

func TestConfigStoreMigratesVersionOneWithBackupAndPreservesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := []byte(`{"schemaVersion":1,"profileId":"legacy-v1","preferences":{"downloadConcurrency":11,"exportRoot":"/v1","noColor":true},"mcp":{"readOnly":true,"allow":["query_articles"]},"extensions":{"future":true}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, backup, err := NewConfigStore(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".v1.bak" {
		t.Fatalf("backup = %q", backup)
	}
	if configuration.SchemaVersion != CurrentConfigVersion || configuration.ProfileID != "legacy-v1" ||
		configuration.Preferences.Download.Concurrency != 11 || configuration.Preferences.Export.Root != "/v1" ||
		!configuration.Preferences.Display.NoColor || !configuration.MCP.ReadOnly ||
		!reflect.DeepEqual(configuration.MCP.Allow, []string{"query_articles"}) ||
		!jsonEqual(configuration.Extensions, json.RawMessage(`{"future":true}`)) {
		t.Fatalf("migrated v1 config = %#v", configuration)
	}
	backedUp, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(backedUp) != string(legacy) {
		t.Fatalf("backup = %s", backedUp)
	}
}

func TestConfigStoreRejectsUnsafePacingWithoutConfirmation(t *testing.T) {
	store := NewConfigStore(filepath.Join(t.TempDir(), "config.json"))
	configuration := DefaultConfig("profile-a")
	configuration.Preferences.Sync.PageDelay = 2 * time.Second
	if err := store.Write(configuration); err == nil || !reflect.ValueOf(err).IsValid() {
		t.Fatal("Write() accepted unsafe pacing without confirmation")
	}
	configuration.Preferences.Sync.UnsafePacingSaved = true
	if err := store.Write(configuration); err != nil {
		t.Fatalf("Write() rejected explicitly confirmed pacing: %v", err)
	}
}

func TestConfigStoreDecodesOmittedVersionTwoFieldsWithSafeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":2,"profileId":"minimal","preferences":{},"mcp":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, backup, err := NewConfigStore(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	defaults := DefaultConfig("minimal")
	if backup != path+".v2.bak" || configuration.SchemaVersion != CurrentConfigVersion {
		t.Fatalf("version-two migration backup=%q config=%#v", backup, configuration)
	}
	if !reflect.DeepEqual(configuration.Preferences, defaults.Preferences) {
		t.Fatalf("minimal preferences = %#v, want defaults %#v", configuration.Preferences, defaults.Preferences)
	}
}

func TestConfigStoreMigratesVersionTwoAllowedOutputRoots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := []byte(`{"schemaVersion":2,"profileId":"profile-a","preferences":{},"mcp":{"allowedOutputRoots":["/safe/export"]}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	configuration, backup, err := NewConfigStore(path).Read()
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".v2.bak" || configuration.SchemaVersion != CurrentConfigVersion ||
		!reflect.DeepEqual(configuration.MCP.AllowedOutputRoots, []string{"/safe/export"}) {
		t.Fatalf("migrated v2 config=%#v backup=%q", configuration, backup)
	}
}

func TestConfigStoreSerializesConcurrentUpdates(t *testing.T) {
	store := NewConfigStore(filepath.Join(t.TempDir(), "config.json"))
	configuration := DefaultConfig("profile-a")
	configuration.Extensions = json.RawMessage(`{"counter":0}`)
	if err := store.Write(configuration); err != nil {
		t.Fatal(err)
	}
	const updates = 20
	var wait sync.WaitGroup
	errorsChannel := make(chan error, updates)
	for range updates {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.Update(func(configuration *ProfileConfig) error {
				var extension struct {
					Counter int `json:"counter"`
				}
				if err := json.Unmarshal(configuration.Extensions, &extension); err != nil {
					return err
				}
				extension.Counter++
				encoded, err := json.Marshal(extension)
				configuration.Extensions = encoded
				return err
			})
			errorsChannel <- err
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	final, _, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	var extension struct {
		Counter int `json:"counter"`
	}
	if err := json.Unmarshal(final.Extensions, &extension); err != nil {
		t.Fatal(err)
	}
	if extension.Counter != updates {
		t.Fatalf("counter = %d, want %d", extension.Counter, updates)
	}
}

func TestConfigStoreRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewConfigStore(path).Read(); err == nil {
		t.Fatal("Read(newer config) error = nil")
	}
}

func TestConfigStoreValidatesMCPAllowedOutputRoots(t *testing.T) {
	configuration := DefaultConfig("profile-a")
	configuration.MCP.AllowedOutputRoots = []string{"relative/path"}
	if err := NewConfigStore(filepath.Join(t.TempDir(), "config.json")).Write(configuration); err == nil ||
		!strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("relative MCP root error = %v", err)
	}
	configuration.MCP.AllowedOutputRoots = []string{filepath.Join(t.TempDir(), "exports")}
	if err := NewConfigStore(filepath.Join(t.TempDir(), "config.json")).Write(configuration); err != nil {
		t.Fatal(err)
	}
}
