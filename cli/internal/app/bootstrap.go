package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

// Dependencies contains the process-level seams used by the composition root.
// Tests and embedders can replace every external dependency without changing
// Cobra, Bubble Tea, MCP, or application behavior.
type Dependencies struct {
	PathOptions profiles.PathOptions
	HTTP        network.Doer
	Clock       runtimeenv.Clock
	Filesystem  runtimeenv.Filesystem
	Browser     runtimeenv.BrowserDiscovery
	PDFRunner   exporter.ProcessRunner
	Signals     runtimeenv.SignalSource
	Secrets     secrets.Store
	Executable  string
	Worker      WorkerLauncher

	// ApplicationFactory is primarily a contract-test seam. Production leaves
	// it nil so the full profile-isolated SQLite/object/session runtime is built.
	ApplicationFactory func(*ProfileRuntime) application.Application
}

type processSignalSource struct {
	channel chan os.Signal
}

func newProcessSignalSource() *processSignalSource {
	channel := make(chan os.Signal, 2)
	signal.Notify(channel, os.Interrupt)
	return &processSignalSource{channel: channel}
}

func (source *processSignalSource) Signals() <-chan os.Signal { return source.channel }

func (source *processSignalSource) Close() {
	if source == nil || source.channel == nil {
		return
	}
	signal.Stop(source.channel)
}

type ProfileRuntime struct {
	Profile   profiles.Profile
	Paths     profiles.Paths
	Library   *library.Database
	Objects   *objects.FileStore
	Jobs      *library.JobStore
	WeChat    *wechat.Client
	Network   *network.Service
	Downloads application.DownloadJobs
	Syncs     application.SyncJobs
	Exports   application.ExportJobs
	Core      application.Application
	close     sync.Once
	closeErr  error
}

func (runtime *ProfileRuntime) Close() error {
	if runtime == nil || runtime.Library == nil {
		return nil
	}
	runtime.close.Do(func() { runtime.closeErr = runtime.Library.Close() })
	return runtime.closeErr
}

type runtimeManager struct {
	mu           sync.Mutex
	version      string
	paths        profiles.Paths
	http         network.Doer
	clock        runtimeenv.Clock
	filesystem   runtimeenv.Filesystem
	browser      runtimeenv.BrowserDiscovery
	pdfRunner    exporter.ProcessRunner
	signals      runtimeenv.SignalSource
	secrets      secrets.Store
	executable   string
	worker       WorkerLauncher
	factory      func(*ProfileRuntime) application.Application
	active       *ProfileRuntime
	databaseOpen func(context.Context, library.OpenOptions) (*library.Database, error)
}

func newRuntimeManager(version string, paths profiles.Paths, dependencies Dependencies) *runtimeManager {
	httpDoer := dependencies.HTTP
	if httpDoer == nil {
		httpDoer = http.DefaultClient
	}
	clock := dependencies.Clock
	if clock == nil {
		clock = runtimeenv.RealClock{}
	}
	filesystem := dependencies.Filesystem
	if filesystem == nil {
		filesystem = runtimeenv.OSFilesystem{}
	}
	secretStore := dependencies.Secrets
	if secretStore == nil {
		secretStore = secrets.NewKeyringStore("")
	}
	signals := dependencies.Signals
	if signals == nil {
		signals = newProcessSignalSource()
	}
	executable := dependencies.Executable
	if executable == "" {
		executable, _ = os.Executable()
	}
	worker := dependencies.Worker
	if worker == nil {
		worker = processWorkerLauncher{}
	}
	return &runtimeManager{
		version: version, paths: paths, http: httpDoer, clock: clock,
		filesystem: filesystem, browser: dependencies.Browser, pdfRunner: dependencies.PDFRunner, signals: signals,
		secrets: secretStore, executable: executable, worker: worker, factory: dependencies.ApplicationFactory,
		databaseOpen: library.Open,
	}
}

func (manager *runtimeManager) Build(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.buildLocked(ctx, profile)
}

func (manager *runtimeManager) Prepare(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.prepareLocked(ctx, profile)
}

func (manager *runtimeManager) Activate(runtime *ProfileRuntime) error {
	if runtime == nil {
		return errors.New("profile runtime is required")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	previous := manager.active
	manager.active = runtime
	if previous != nil && previous != runtime {
		if err := previous.Close(); err != nil {
			manager.active = previous
			return fmt.Errorf("close previous profile database: %w", err)
		}
	}
	return nil
}

func (manager *runtimeManager) buildLocked(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {
	runtime, err := manager.prepareLocked(ctx, profile)
	if err != nil {
		return nil, err
	}
	previous := manager.active
	manager.active = runtime
	if previous != nil {
		if err := previous.Close(); err != nil {
			_ = runtime.Close()
			manager.active = previous
			return nil, fmt.Errorf("close previous profile database: %w", err)
		}
	}
	return runtime, nil
}

func (manager *runtimeManager) prepareLocked(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {

	profilePaths := manager.paths.ForProfile(profile.ID)
	if err := ensureProfilePaths(manager.filesystem, profilePaths); err != nil {
		return nil, err
	}
	objectStore, err := objects.NewFileStore(profilePaths.Objects)
	if err != nil {
		return nil, fmt.Errorf("open profile object store: %w", err)
	}
	database, err := manager.databaseOpen(ctx, library.OpenOptions{
		Path: profilePaths.Database, ProfileID: profile.ID, ProfileName: profile.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("open profile database: %w", err)
	}
	database.SetObjectStoreReadyProbe(func() bool {
		info, statErr := manager.filesystem.Stat(profilePaths.Objects)
		return statErr == nil && info.IsDir()
	})

	httpClient, ok := manager.http.(*http.Client)
	if !ok {
		httpClient = &http.Client{Transport: roundTripperFromDoer{doer: manager.http}}
	}
	wechatClient := wechat.NewClient(httpClient, manager.secrets, string(profile.ID))
	jobsStore := library.NewJobStore(database)
	routeService := network.NewService(network.ServiceOptions{
		Profile: string(profile.ID), Routes: database,
		Secrets: manager.secrets, HTTP: manager.http,
		EndpointPolicy: network.DestinationPolicy{AllowLoopback: true},
		TargetPolicy: network.DestinationPolicy{
			AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}},
		},
		ProbeTarget: mustURL("https://mp.weixin.qq.com/robots.txt"),
		Now:         manager.clock.Now,
	})
	runtime := &ProfileRuntime{
		Profile: profile, Paths: manager.paths, Library: database, Objects: objectStore,
		Jobs: jobsStore, WeChat: wechatClient, Network: routeService,
	}
	if manager.factory != nil {
		runtime.Core = manager.factory(runtime)
	} else {
		downloads := newLocalDownloadRuntime(runtime, manager.secrets, manager.http)
		runtime.Downloads = downloads
		syncs := newLocalSyncRuntime(runtime, manager.clock)
		exports := newLocalExportRuntime(runtime, manager.clock, manager.browser, manager.pdfRunner)
		starter := persistentJobStarter{executable: manager.executable, paths: manager.paths, profile: profile.ID, launcher: manager.worker}
		if syncs != nil {
			syncs.starter = starter
		}
		runtime.Syncs = syncs
		runtime.Exports = exports
		runtime.Core = application.New(application.Options{
			Version: manager.version,
			Runtime: runtimeenv.Dependencies{
				Clock: manager.clock, FS: manager.filesystem, Paths: profileRuntimePaths(profilePaths), HTTP: manager.http,
				Browser: manager.browser, Secrets: manager.secrets, Signals: manager.signals,
				Portable: manager.paths.Portable, Profile: profile.ID,
			},
			Library: database, Jobs: jobsStore, Downloads: downloads, Syncs: syncs, Exports: exports,
			Starter: starter,
			WeChat:  wechatClient, Session: wechatClient,
		})
		if downloads != nil {
			_, _ = downloads.Recover(ctx)
		}
		if syncs != nil {
			_, _ = syncs.Recover(ctx)
		}
		if exports != nil {
			_, _ = exports.Recover(ctx)
		}
	}

	return runtime, nil
}

func (manager *runtimeManager) Close() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if closable, ok := manager.signals.(interface{ Close() }); ok {
		closable.Close()
	}
	if manager.active == nil {
		return nil
	}
	err := manager.active.Close()
	manager.active = nil
	return err
}

func ensureProfilePaths(filesystem runtimeenv.Filesystem, paths profiles.ProfilePaths) error {
	if filesystem == nil {
		filesystem = runtimeenv.OSFilesystem{}
	}
	for _, directory := range []string{filepath.Dir(paths.Config), paths.Data, paths.Cache, paths.State, paths.Objects} {
		if err := filesystem.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create profile runtime directory %s: %w", directory, err)
		}
		if err := filesystem.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("secure profile runtime directory %s: %w", directory, err)
		}
	}
	return nil
}

func profileRuntimePaths(paths profiles.ProfilePaths) domain.RuntimePaths {
	return domain.RuntimePaths{Config: paths.Config, Data: paths.Data, Cache: paths.Cache, State: paths.State}
}

type roundTripperFromDoer struct{ doer network.Doer }

func (adapter roundTripperFromDoer) RoundTrip(request *http.Request) (*http.Response, error) {
	if adapter.doer == nil {
		return nil, errors.New("HTTP transport is unavailable")
	}
	return adapter.doer.Do(request)
}

func mustURL(value string) *url.URL {
	parsed, err := url.Parse(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func defaultPaths(options profiles.PathOptions) (profiles.Paths, error) {
	paths, err := profiles.ResolvePaths(options)
	if err != nil {
		return profiles.Paths{}, err
	}
	if err := paths.Ensure(); err != nil {
		return profiles.Paths{}, err
	}
	return paths, nil
}

func defaultProfile(registry *profiles.Registry) (profiles.Profile, error) {
	active, err := registry.Active()
	if err == nil {
		return active, nil
	}
	created, createErr := registry.Create("default")
	if createErr != nil {
		return profiles.Profile{}, fmt.Errorf("load active profile: %v; create default profile: %w", err, createErr)
	}
	return created, nil
}

var _ jobs.Manager = (*library.JobStore)(nil)
