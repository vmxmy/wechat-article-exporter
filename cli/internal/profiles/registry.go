package profiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

var profileNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

type Profile struct {
	ID        domain.ProfileID `json:"id"`
	Name      string           `json:"name"`
	Active    bool             `json:"active"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Paths     ProfilePaths     `json:"paths"`
}

type registryFile struct {
	SchemaVersion int               `json:"schemaVersion"`
	Active        domain.ProfileID  `json:"active"`
	Profiles      []registryProfile `json:"profiles"`
}

type registryProfile struct {
	ID        domain.ProfileID `json:"id"`
	Name      string           `json:"name"`
	CreatedAt int64            `json:"createdAt"`
	UpdatedAt int64            `json:"updatedAt"`
}

type Registry struct {
	paths   Paths
	secrets secrets.Store
	now     func() time.Time
}

func NewRegistry(paths Paths, secretStore secrets.Store) *Registry {
	return &Registry{paths: paths, secrets: secretStore, now: time.Now}
}

func (registry *Registry) Create(name string) (Profile, error) {
	if !profileNamePattern.MatchString(name) {
		return Profile{}, errors.New("profile name must be 1-64 characters using letters, numbers, dot, underscore, or hyphen")
	}
	unlock, err := lockConfig(registry.paths.RegistryFile())
	if err != nil {
		return Profile{}, err
	}
	defer unlock()
	file, err := registry.readUnlocked()
	if err != nil {
		return Profile{}, err
	}
	for _, profile := range file.Profiles {
		if profile.Name == name || profile.ID == domain.ProfileID(name) {
			return Profile{}, fmt.Errorf("profile %q already exists", name)
		}
	}
	now := registry.now()
	entry := registryProfile{ID: domain.ProfileID(name), Name: name, CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli()}
	file.Profiles = append(file.Profiles, entry)
	if file.Active == "" {
		file.Active = entry.ID
	}
	if err := registry.writeUnlocked(file); err != nil {
		return Profile{}, err
	}
	profilePaths := registry.paths.ForProfile(entry.ID)
	for _, directory := range []string{filepath.Dir(profilePaths.Config), profilePaths.Data, profilePaths.Cache, profilePaths.State, profilePaths.Objects} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return Profile{}, fmt.Errorf("create profile directory: %w", err)
		}
	}
	configuration := DefaultConfig(string(entry.ID))
	if err := NewConfigStore(profilePaths.Config).Write(configuration); err != nil {
		return Profile{}, err
	}
	return registry.materialize(entry, file.Active), nil
}

func (registry *Registry) List() ([]Profile, error) {
	unlock, err := lockConfig(registry.paths.RegistryFile())
	if err != nil {
		return nil, err
	}
	defer unlock()
	file, err := registry.readUnlocked()
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(file.Profiles))
	for _, entry := range file.Profiles {
		profiles = append(profiles, registry.materialize(entry, file.Active))
	}
	sort.Slice(profiles, func(left, right int) bool { return profiles[left].Name < profiles[right].Name })
	return profiles, nil
}

func (registry *Registry) Use(id domain.ProfileID) (Profile, error) {
	unlock, err := lockConfig(registry.paths.RegistryFile())
	if err != nil {
		return Profile{}, err
	}
	defer unlock()
	file, err := registry.readUnlocked()
	if err != nil {
		return Profile{}, err
	}
	for index := range file.Profiles {
		if file.Profiles[index].ID == id {
			file.Active = id
			file.Profiles[index].UpdatedAt = registry.now().UnixMilli()
			if err := registry.writeUnlocked(file); err != nil {
				return Profile{}, err
			}
			return registry.materialize(file.Profiles[index], file.Active), nil
		}
	}
	return Profile{}, fmt.Errorf("profile %q does not exist", id)
}

func (registry *Registry) Active() (Profile, error) {
	profiles, err := registry.List()
	if err != nil {
		return Profile{}, err
	}
	for _, profile := range profiles {
		if profile.Active {
			return profile, nil
		}
	}
	return Profile{}, errors.New("no active profile")
}

func (registry *Registry) Delete(id domain.ProfileID) error {
	unlock, err := lockConfig(registry.paths.RegistryFile())
	if err != nil {
		return err
	}
	defer unlock()
	file, err := registry.readUnlocked()
	if err != nil {
		return err
	}
	if file.Active == id {
		return errors.New("cannot delete the active profile; activate another profile first")
	}
	index := -1
	for candidate := range file.Profiles {
		if file.Profiles[candidate].ID == id {
			index = candidate
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("profile %q does not exist", id)
	}
	if registry.secrets != nil {
		if err := registry.secrets.DeleteProfile(string(id)); err != nil {
			return fmt.Errorf("delete profile secrets: %w", err)
		}
	}
	file.Profiles = append(file.Profiles[:index], file.Profiles[index+1:]...)
	if err := registry.writeUnlocked(file); err != nil {
		return err
	}
	profilePaths := registry.paths.ForProfile(id)
	for _, target := range []string{filepath.Dir(profilePaths.Config), profilePaths.Data, profilePaths.Cache, profilePaths.State} {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove profile state: %w", err)
		}
	}
	return nil
}

func (registry *Registry) readUnlocked() (registryFile, error) {
	data, err := os.ReadFile(registry.paths.RegistryFile())
	if errors.Is(err, os.ErrNotExist) {
		return registryFile{SchemaVersion: 1, Profiles: []registryProfile{}}, nil
	}
	if err != nil {
		return registryFile{}, err
	}
	var file registryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return registryFile{}, fmt.Errorf("decode profile registry: %w", err)
	}
	if file.SchemaVersion != 1 {
		return registryFile{}, fmt.Errorf("unsupported profile registry schema %d", file.SchemaVersion)
	}
	return file, nil
}

func (registry *Registry) writeUnlocked(file registryFile) error {
	file.SchemaVersion = 1
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(registry.paths.RegistryFile(), append(data, '\n'), 0o600)
}

func (registry *Registry) materialize(entry registryProfile, active domain.ProfileID) Profile {
	return Profile{
		ID: entry.ID, Name: entry.Name, Active: entry.ID == active,
		CreatedAt: time.UnixMilli(entry.CreatedAt), UpdatedAt: time.UnixMilli(entry.UpdatedAt), Paths: registry.paths.ForProfile(entry.ID),
	}
}
