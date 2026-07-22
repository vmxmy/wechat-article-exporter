package runtimeenv

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time                                { return time.Now() }
func (RealClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

type Browser struct {
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type BrowserDiscovery interface {
	FindChromium(context.Context) (Browser, error)
}

type SignalSource interface {
	Signals() <-chan os.Signal
}

// Filesystem is the narrow process/filesystem seam used by application
// modules that need deterministic roots or file operations in tests. It is
// intentionally based on fs.FS instead of exposing os package globals.
type Filesystem interface {
	Open(string) (fs.File, error)
	Create(string) (io.WriteCloser, error)
	MkdirAll(string, fs.FileMode) error
	Rename(string, string) error
	Remove(string) error
	Stat(string) (fs.FileInfo, error)
	Chmod(string, fs.FileMode) error
}

type OSFilesystem struct{}

func (OSFilesystem) Open(path string) (fs.File, error)            { return os.Open(path) }
func (OSFilesystem) Create(path string) (io.WriteCloser, error)   { return os.Create(path) }
func (OSFilesystem) MkdirAll(path string, mode fs.FileMode) error { return os.MkdirAll(path, mode) }
func (OSFilesystem) Rename(oldPath, newPath string) error         { return os.Rename(oldPath, newPath) }
func (OSFilesystem) Remove(path string) error                     { return os.Remove(path) }
func (OSFilesystem) Stat(path string) (fs.FileInfo, error)        { return os.Stat(path) }
func (OSFilesystem) Chmod(path string, mode fs.FileMode) error    { return os.Chmod(path, mode) }

// JoinRoot resolves a relative path below a configured filesystem root. It is
// kept here so callers do not accidentally use executable-relative storage.
func JoinRoot(root, relative string) string { return filepath.Join(root, relative) }

type Dependencies struct {
	Clock    Clock
	FS       Filesystem
	Paths    domain.RuntimePaths
	HTTP     network.Doer
	Browser  BrowserDiscovery
	Secrets  secrets.Store
	Signals  SignalSource
	Portable bool
	Profile  domain.ProfileID
}

func (dependencies Dependencies) WithDefaults() Dependencies {
	if dependencies.Clock == nil {
		dependencies.Clock = RealClock{}
	}
	if dependencies.FS == nil {
		dependencies.FS = OSFilesystem{}
	}
	if dependencies.HTTP == nil {
		dependencies.HTTP = http.DefaultClient
	}
	if dependencies.Secrets == nil {
		dependencies.Secrets = secrets.NewMemoryStore()
	}
	if dependencies.Profile == "" {
		dependencies.Profile = "default"
	}
	return dependencies
}

func Normalize(dependencies Dependencies) Dependencies { return dependencies.WithDefaults() }
