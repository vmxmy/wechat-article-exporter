package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CurrentConfigVersion = 3

type ProfileConfig struct {
	SchemaVersion int             `json:"schemaVersion"`
	ProfileID     string          `json:"profileId"`
	Preferences   Preferences     `json:"preferences"`
	MCP           MCPPolicy       `json:"mcp"`
	Extensions    json.RawMessage `json:"extensions,omitempty"`
}

type Preferences struct {
	Sync     SyncPreferences     `json:"sync"`
	Download DownloadPreferences `json:"download"`
	Export   ExportPreferences   `json:"export"`
	Display  DisplayPreferences  `json:"display"`
	Proxy    ProxyPreferences    `json:"proxy"`

	// Deprecated flattened fields are retained for one compatibility window.
	// They are normalized into the grouped preferences and preserved on writes
	// so release N-1 automation does not silently lose user choices.
	DownloadConcurrency int    `json:"downloadConcurrency,omitempty"`
	ExportRoot          string `json:"exportRoot,omitempty"`
	NoColor             bool   `json:"noColor,omitempty"`
}

type SyncPreferences struct {
	Range             string        `json:"range"`
	DatePoint         time.Time     `json:"datePoint,omitempty"`
	PageDelay         time.Duration `json:"pageDelay"`
	Jitter            time.Duration `json:"jitter"`
	PageSize          int           `json:"pageSize"`
	Incremental       bool          `json:"incremental"`
	UnsafePacingSaved bool          `json:"unsafePacingSaved,omitempty"`
}

type DownloadPreferences struct {
	Concurrency              int  `json:"concurrency"`
	ForceContent             bool `json:"forceContent"`
	MetadataOverridesContent bool `json:"metadataOverridesContent"`
}

type ExportPreferences struct {
	Root                string `json:"root,omitempty"`
	NamingTemplate      string `json:"namingTemplate"`
	MaximumNameBytes    int    `json:"maximumNameBytes"`
	CollisionPolicy     string `json:"collisionPolicy"`
	ExcelIncludeContent bool   `json:"excelIncludeContent"`
	JSONIncludeContent  bool   `json:"jsonIncludeContent"`
	JSONIncludeComments bool   `json:"jsonIncludeComments"`
	HTMLIncludeComments bool   `json:"htmlIncludeComments"`
}

type DisplayPreferences struct {
	NoColor     bool `json:"noColor"`
	ASCII       bool `json:"ascii"`
	Plain       bool `json:"plain"`
	HideDeleted bool `json:"hideDeleted"`
}

type ProxyPreferences struct {
	DirectFirst     bool `json:"directFirst"`
	FallbackEnabled bool `json:"fallbackEnabled"`
}

type MCPPolicy struct {
	ReadOnly           bool     `json:"readOnly"`
	Allow              []string `json:"allow,omitempty"`
	Deny               []string `json:"deny,omitempty"`
	AllowedOutputRoots []string `json:"allowedOutputRoots,omitempty"`
}

type EffectiveConfig struct {
	Path            string      `json:"path"`
	SchemaVersion   int         `json:"schemaVersion"`
	ProfileID       string      `json:"profileId"`
	Preferences     Preferences `json:"preferences"`
	MCP             MCPPolicy   `json:"mcp"`
	MigrationBackup string      `json:"migrationBackup,omitempty"`
}

type ConfigStore struct {
	path string
}

func NewConfigStore(path string) *ConfigStore { return &ConfigStore{path: path} }
func (store *ConfigStore) Path() string       { return store.path }

func DefaultConfig(profileID string) ProfileConfig {
	return ProfileConfig{
		SchemaVersion: CurrentConfigVersion,
		ProfileID:     profileID,
		Preferences: Preferences{
			Sync: SyncPreferences{Range: "all", PageDelay: 5 * time.Second, Jitter: 500 * time.Millisecond,
				PageSize: 20, Incremental: true},
			Download: DownloadPreferences{Concurrency: 4},
			Export: ExportPreferences{NamingTemplate: "{published}-{title}", MaximumNameBytes: 180,
				CollisionPolicy: "fail", ExcelIncludeContent: true, JSONIncludeContent: true,
				JSONIncludeComments: true, HTMLIncludeComments: true},
			Display:             DisplayPreferences{HideDeleted: true},
			Proxy:               ProxyPreferences{DirectFirst: true},
			DownloadConcurrency: 4,
		},
		MCP: MCPPolicy{},
	}
}

func (store *ConfigStore) Read() (ProfileConfig, string, error) {
	unlock, err := lockConfig(store.path)
	if err != nil {
		return ProfileConfig{}, "", err
	}
	defer unlock()
	return store.readUnlocked()
}

func (store *ConfigStore) Write(configuration ProfileConfig) error {
	unlock, err := lockConfig(store.path)
	if err != nil {
		return err
	}
	defer unlock()
	return store.writeUnlocked(configuration)
}

func (store *ConfigStore) Update(update func(*ProfileConfig) error) (EffectiveConfig, error) {
	unlock, err := lockConfig(store.path)
	if err != nil {
		return EffectiveConfig{}, err
	}
	defer unlock()
	configuration, backup, err := store.readUnlocked()
	if err != nil {
		return EffectiveConfig{}, err
	}
	if err := update(&configuration); err != nil {
		return EffectiveConfig{}, err
	}
	normalizePreferences(&configuration.Preferences)
	if err := validateConfig(configuration); err != nil {
		return EffectiveConfig{}, err
	}
	if err := store.writeUnlocked(configuration); err != nil {
		return EffectiveConfig{}, err
	}
	return EffectiveConfig{
		Path: store.path, SchemaVersion: configuration.SchemaVersion, ProfileID: configuration.ProfileID,
		Preferences: configuration.Preferences, MCP: configuration.MCP, MigrationBackup: backup,
	}, nil
}

func (store *ConfigStore) readUnlocked() (ProfileConfig, string, error) {
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(""), "", nil
	}
	if err != nil {
		return ProfileConfig{}, "", fmt.Errorf("read profile config: %w", err)
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return ProfileConfig{}, "", fmt.Errorf("profile config must be a JSON object: %w", err)
	}
	if header.SchemaVersion > CurrentConfigVersion {
		return ProfileConfig{}, "", fmt.Errorf("profile config schema %d is newer than supported schema %d", header.SchemaVersion, CurrentConfigVersion)
	}
	backup := ""
	if header.SchemaVersion < CurrentConfigVersion {
		backup = store.path + ".v" + fmt.Sprint(header.SchemaVersion) + ".bak"
		if err := writeAtomic(backup, data, 0o600); err != nil {
			return ProfileConfig{}, "", fmt.Errorf("backup profile config before migration: %w", err)
		}
	}
	configuration, err := decodeAndMigrateConfig(data, header.SchemaVersion)
	if err != nil {
		return ProfileConfig{}, "", err
	}
	if backup != "" {
		if err := store.writeUnlocked(configuration); err != nil {
			return ProfileConfig{}, "", err
		}
	}
	return configuration, backup, nil
}

func decodeAndMigrateConfig(data []byte, version int) (ProfileConfig, error) {
	switch version {
	case 0:
		var legacy struct {
			ProfileID           string          `json:"profileId"`
			DownloadConcurrency int             `json:"downloadConcurrency"`
			ExportRoot          string          `json:"exportRoot"`
			Extensions          json.RawMessage `json:"extensions"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return ProfileConfig{}, err
		}
		configuration := DefaultConfig(legacy.ProfileID)
		if legacy.DownloadConcurrency > 0 {
			configuration.Preferences.Download.Concurrency = legacy.DownloadConcurrency
			configuration.Preferences.DownloadConcurrency = legacy.DownloadConcurrency
		}
		configuration.Preferences.Export.Root = legacy.ExportRoot
		configuration.Preferences.ExportRoot = legacy.ExportRoot
		configuration.Extensions = legacy.Extensions
		return configuration, validateConfig(configuration)
	case 1:
		var legacy struct {
			SchemaVersion int    `json:"schemaVersion"`
			ProfileID     string `json:"profileId"`
			Preferences   struct {
				DownloadConcurrency int    `json:"downloadConcurrency"`
				ExportRoot          string `json:"exportRoot"`
				NoColor             bool   `json:"noColor"`
			} `json:"preferences"`
			MCP        MCPPolicy       `json:"mcp"`
			Extensions json.RawMessage `json:"extensions"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return ProfileConfig{}, err
		}
		configuration := DefaultConfig(legacy.ProfileID)
		if legacy.Preferences.DownloadConcurrency > 0 {
			configuration.Preferences.Download.Concurrency = legacy.Preferences.DownloadConcurrency
			configuration.Preferences.DownloadConcurrency = legacy.Preferences.DownloadConcurrency
		}
		configuration.Preferences.Export.Root = legacy.Preferences.ExportRoot
		configuration.Preferences.ExportRoot = legacy.Preferences.ExportRoot
		configuration.Preferences.Display.NoColor = legacy.Preferences.NoColor
		configuration.Preferences.NoColor = legacy.Preferences.NoColor
		configuration.MCP = legacy.MCP
		configuration.Extensions = legacy.Extensions
		return configuration, validateConfig(configuration)
	case 2:
		configuration := DefaultConfig("")
		if err := json.Unmarshal(data, &configuration); err != nil {
			return ProfileConfig{}, err
		}
		configuration.SchemaVersion = CurrentConfigVersion
		normalizePreferences(&configuration.Preferences)
		return configuration, validateConfig(configuration)
	case CurrentConfigVersion:
		configuration := DefaultConfig("")
		if err := json.Unmarshal(data, &configuration); err != nil {
			return ProfileConfig{}, err
		}
		normalizePreferences(&configuration.Preferences)
		return configuration, validateConfig(configuration)
	default:
		return ProfileConfig{}, fmt.Errorf("unsupported profile config schema %d", version)
	}
}

func validateConfig(configuration ProfileConfig) error {
	if configuration.SchemaVersion != CurrentConfigVersion {
		return fmt.Errorf("profile config schema must be %d", CurrentConfigVersion)
	}
	preferences := configuration.Preferences
	normalizePreferences(&preferences)
	if preferences.Download.Concurrency <= 0 || preferences.Download.Concurrency > 128 {
		return errors.New("download concurrency must be between 1 and 128")
	}
	if preferences.Sync.PageSize <= 0 || preferences.Sync.PageSize > 50 {
		return errors.New("sync page size must be between 1 and 50")
	}
	if preferences.Sync.PageDelay < 0 || preferences.Sync.Jitter < 0 {
		return errors.New("sync page delay and jitter must be non-negative")
	}
	allowedRanges := map[string]struct{}{"24h": {}, "1d": {}, "3d": {}, "7d": {}, "1m": {}, "3m": {}, "6m": {}, "1y": {}, "all": {}, "point": {}}
	if _, ok := allowedRanges[preferences.Sync.Range]; !ok {
		return fmt.Errorf("unsupported sync range %q", preferences.Sync.Range)
	}
	if preferences.Sync.Range == "point" && preferences.Sync.DatePoint.IsZero() {
		return errors.New("point sync range requires a date point")
	}
	if preferences.Export.MaximumNameBytes < 32 || preferences.Export.MaximumNameBytes > 255 {
		return errors.New("export maximum name bytes must be between 32 and 255")
	}
	allowedCollision := map[string]struct{}{"fail": {}, "skip": {}, "replace": {}, "suffix": {}}
	if _, ok := allowedCollision[preferences.Export.CollisionPolicy]; !ok {
		return fmt.Errorf("unsupported export collision policy %q", preferences.Export.CollisionPolicy)
	}
	if preferences.Sync.PageDelay < 3*time.Second && !preferences.Sync.UnsafePacingSaved {
		return errors.New("sync page delay below 3s requires unsafePacingSaved confirmation")
	}
	for _, root := range configuration.MCP.AllowedOutputRoots {
		if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
			return errors.New("MCP allowed output roots must be non-empty absolute paths")
		}
	}
	return nil
}

func normalizePreferences(preferences *Preferences) {
	if preferences == nil {
		return
	}
	if preferences.Download.Concurrency == 0 {
		preferences.Download.Concurrency = preferences.DownloadConcurrency
	}
	if preferences.Export.Root == "" {
		preferences.Export.Root = preferences.ExportRoot
	}
	if preferences.Display.NoColor || preferences.NoColor {
		preferences.Display.NoColor = true
	}
	preferences.DownloadConcurrency = preferences.Download.Concurrency
	preferences.ExportRoot = preferences.Export.Root
	preferences.NoColor = preferences.Display.NoColor
}

func (store *ConfigStore) writeUnlocked(configuration ProfileConfig) error {
	normalizePreferences(&configuration.Preferences)
	if err := validateConfig(configuration); err != nil {
		return err
	}
	data, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile config: %w", err)
	}
	return writeAtomic(store.path, append(data, '\n'), 0o600)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".config-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
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
	return os.Chmod(path, mode)
}

func backupName(path string) string {
	return path + "." + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
}
