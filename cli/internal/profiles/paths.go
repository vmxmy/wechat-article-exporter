package profiles

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

const ApplicationDirectory = "wechat-article-exporter"

type PathOptions struct {
	ConfigRoot   string
	DataRoot     string
	CacheRoot    string
	StateRoot    string
	Portable     bool
	PortableRoot string
}

type Paths struct {
	ConfigRoot string
	DataRoot   string
	CacheRoot  string
	StateRoot  string
	Portable   bool
}

func ResolvePaths(options PathOptions) (Paths, error) {
	if options.Portable {
		if options.PortableRoot == "" {
			return Paths{}, errors.New("portable mode requires an explicit root")
		}
		root, err := secureAbsoluteRoot(options.PortableRoot)
		if err != nil {
			return Paths{}, fmt.Errorf("portable root: %w", err)
		}
		return Paths{
			ConfigRoot: filepath.Join(root, "config"),
			DataRoot:   filepath.Join(root, "data"),
			CacheRoot:  filepath.Join(root, "cache"),
			StateRoot:  filepath.Join(root, "state"),
			Portable:   true,
		}, nil
	}
	if options.PortableRoot != "" {
		return Paths{}, errors.New("portable root requires portable mode")
	}
	configRoot, err := explicitOrDefault(options.ConfigRoot, defaultConfigRoot)
	if err != nil {
		return Paths{}, fmt.Errorf("config root: %w", err)
	}
	dataRoot, err := explicitOrDefault(options.DataRoot, defaultDataRoot)
	if err != nil {
		return Paths{}, fmt.Errorf("data root: %w", err)
	}
	cacheRoot, err := explicitOrDefault(options.CacheRoot, defaultCacheRoot)
	if err != nil {
		return Paths{}, fmt.Errorf("cache root: %w", err)
	}
	stateRoot, err := explicitOrDefault(options.StateRoot, defaultStateRoot)
	if err != nil {
		return Paths{}, fmt.Errorf("state root: %w", err)
	}
	return Paths{ConfigRoot: configRoot, DataRoot: dataRoot, CacheRoot: cacheRoot, StateRoot: stateRoot}, nil
}

func (paths Paths) Ensure() error {
	for _, root := range []string{paths.ConfigRoot, paths.DataRoot, paths.CacheRoot, paths.StateRoot} {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return fmt.Errorf("create runtime root %s: %w", root, err)
		}
		if err := os.Chmod(root, 0o700); err != nil {
			return fmt.Errorf("secure runtime root %s: %w", root, err)
		}
	}
	return nil
}

func (paths Paths) Runtime() domain.RuntimePaths {
	return domain.RuntimePaths{Config: paths.ConfigRoot, Data: paths.DataRoot, Cache: paths.CacheRoot, State: paths.StateRoot}
}

func (paths Paths) RegistryFile() string { return filepath.Join(paths.ConfigRoot, "profiles.json") }

func (paths Paths) ForProfile(id domain.ProfileID) ProfilePaths {
	name := string(id)
	return ProfilePaths{
		Config:   filepath.Join(paths.ConfigRoot, "profiles", name, "config.json"),
		Data:     filepath.Join(paths.DataRoot, "profiles", name),
		Cache:    filepath.Join(paths.CacheRoot, "profiles", name),
		State:    filepath.Join(paths.StateRoot, "profiles", name),
		Database: filepath.Join(paths.DataRoot, "profiles", name, "library.sqlite3"),
		Objects:  filepath.Join(paths.DataRoot, "profiles", name, "objects"),
	}
}

type ProfilePaths struct {
	Config   string
	Data     string
	Cache    string
	State    string
	Database string
	Objects  string
}

func explicitOrDefault(explicit string, fallback func() (string, error)) (string, error) {
	if explicit != "" {
		return secureAbsoluteRoot(explicit)
	}
	root, err := fallback()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ApplicationDirectory), nil
}

func secureAbsoluteRoot(value string) (string, error) {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(absolute)
	volume := filepath.VolumeName(clean)
	if clean == string(filepath.Separator) || clean == volume+string(filepath.Separator) {
		return "", errors.New("filesystem root is not an allowed application root")
	}
	return clean, nil
}

func defaultConfigRoot() (string, error) {
	if runtime.GOOS != "windows" {
		if value := absoluteEnv("XDG_CONFIG_HOME"); value != "" {
			return value, nil
		}
	}
	return os.UserConfigDir()
}

func defaultDataRoot() (string, error) {
	if runtime.GOOS != "windows" {
		if value := absoluteEnv("XDG_DATA_HOME"); value != "" {
			return value, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share"), nil
	}
	return os.UserConfigDir()
}

func defaultCacheRoot() (string, error) {
	if runtime.GOOS != "windows" {
		if value := absoluteEnv("XDG_CACHE_HOME"); value != "" {
			return value, nil
		}
	}
	return os.UserCacheDir()
}

func defaultStateRoot() (string, error) {
	if runtime.GOOS != "windows" {
		if value := absoluteEnv("XDG_STATE_HOME"); value != "" {
			return value, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state"), nil
	}
	return os.UserConfigDir()
}

func absoluteEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return ""
}
