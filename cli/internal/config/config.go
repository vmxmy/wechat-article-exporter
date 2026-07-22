package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const envConfigPath = "WECHAT_ARTICLE_CLI_CONFIG"

type Tokens struct {
	AccessToken  string `json:"access_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type ClientInformation struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

type File struct {
	Server            string             `json:"server,omitempty"`
	Tokens            *Tokens            `json:"tokens,omitempty"`
	ClientInformation *ClientInformation `json:"clientInformation,omitempty"`
	CodeVerifier      string             `json:"codeVerifier,omitempty"`
	OAuthState        string             `json:"oauthState,omitempty"`
	TokenSavedAt      int64              `json:"tokenSavedAt,omitempty"`
	TokenEndpoint     string             `json:"tokenEndpoint,omitempty"`
}

func (f File) TokenExpiry() time.Time {
	if f.Tokens == nil || f.Tokens.ExpiresIn <= 0 || f.TokenSavedAt <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(f.TokenSavedAt).Add(time.Duration(f.Tokens.ExpiresIn) * time.Second)
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	if path == "" {
		path = DefaultPath()
	}
	return &Store{path: path}
}

func DefaultPath() string {
	if value := os.Getenv(envConfigPath); value != "" {
		absolute, err := filepath.Abs(value)
		if err == nil {
			return absolute
		}
		return value
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if root != "" && !filepath.IsAbs(root) {
		root = ""
	}
	if root == "" {
		if userConfig, err := os.UserConfigDir(); err == nil && filepath.IsAbs(userConfig) {
			root = userConfig
		} else if home, homeErr := os.UserHomeDir(); homeErr == nil && filepath.IsAbs(home) {
			root = filepath.Join(home, ".config")
		} else {
			root = filepath.Join(os.TempDir(), "wechat-article-exporter-cli")
		}
	}
	return filepath.Join(root, "wechat-article-exporter", "cli.json")
}

func (s *Store) Path() string { return s.path }

func (s *Store) Read() (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readUnlocked()
}

func (s *Store) Write(value File) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := lockConfig(s.path)
	if err != nil {
		return fmt.Errorf("lock CLI config: %w", err)
	}
	defer unlock()
	return s.writeUnlocked(value)
}

func (s *Store) Update(update func(*File) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := lockConfig(s.path)
	if err != nil {
		return fmt.Errorf("lock CLI config: %w", err)
	}
	defer unlock()
	value, err := s.readUnlocked()
	if err != nil {
		return err
	}
	if err := update(&value); err != nil {
		return err
	}
	return s.writeUnlocked(value)
}

func (s *Store) ClearSession() (File, error) {
	var cleared File
	err := s.Update(func(value *File) error {
		cleared = File{Server: value.Server}
		*value = cleared
		return nil
	})
	return cleared, err
}

func (s *Store) readUnlocked() (File, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, fmt.Errorf("read CLI config: %w", err)
	}
	var value File
	if err := json.Unmarshal(data, &value); err != nil {
		return File{}, fmt.Errorf("CLI config must be a JSON object: %w", err)
	}
	return value, nil
}

func (s *Store) writeUnlocked(value File) error {
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create CLI config directory: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI config: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".cli.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary CLI config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary CLI config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary CLI config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary CLI config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary CLI config: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace CLI config: %w", err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("secure CLI config: %w", err)
	}
	return nil
}
