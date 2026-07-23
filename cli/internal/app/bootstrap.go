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
	"strings"
	"sync"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
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
	StartupArgs []string

	// WeChatOrigin is a release-test seam for a loopback-controlled upstream.
	// Production must leave it nil so only the approved WeChat origin is used.
	WeChatOrigin *url.URL

	// DownloadDestinationPolicy is a release-test seam for loopback article and
	// resource fixtures. Production must leave it zero-valued.
	DownloadDestinationPolicy network.DestinationPolicy

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
	Profile     profiles.Profile
	Paths       profiles.Paths
	Library     *library.Database
	Objects     *objects.FileStore
	Jobs        *library.JobStore
	WeChat      *wechat.Client
	Network     *network.Service
	Downloads   application.DownloadJobs
	Credentials *credentials.Service
	Syncs       application.SyncJobs
	Exports     application.ExportJobs
	Core        application.Application
	runtimeLock *profiles.ProfileLock
	close       sync.Once
	closeErr    error
}

func (runtime *ProfileRuntime) Close() error {
	if runtime == nil {
		return nil
	}
	runtime.close.Do(func() {
		if runtime.Library != nil {
			runtime.closeErr = runtime.Library.Close()
		}
		if runtime.runtimeLock != nil {
			runtime.closeErr = errors.Join(runtime.closeErr, runtime.runtimeLock.Close())
		}
	})
	return runtime.closeErr
}

type runtimeManager struct {
	mu              sync.Mutex
	version         string
	paths           profiles.Paths
	http            network.Doer
	clock           runtimeenv.Clock
	filesystem      runtimeenv.Filesystem
	browser         runtimeenv.BrowserDiscovery
	browserExplicit bool
	pdfRunner       exporter.ProcessRunner
	signals         runtimeenv.SignalSource
	secrets         secrets.Store
	executable      string
	worker          WorkerLauncher
	factory         func(*ProfileRuntime) application.Application
	wechatOrigin    *url.URL
	downloadPolicy  network.DestinationPolicy
	active          *ProfileRuntime
	databaseOpen    func(context.Context, library.OpenOptions) (*library.Database, error)
}

func newRuntimeManager(version string, paths profiles.Paths, dependencies Dependencies) *runtimeManager {
	httpDoer := dependencies.HTTP
	if httpDoer == nil {
		directTransport := http.DefaultTransport
		if configured, ok := http.DefaultTransport.(*http.Transport); ok && configured != nil {
			directTransport = configured.Clone()
		}
		transport, ok := directTransport.(*http.Transport)
		if !ok {
			transport = &http.Transport{}
		}
		transport.Proxy = nil
		httpDoer = &http.Client{Transport: transport}
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
	browser := dependencies.Browser
	browserExplicit := browser != nil
	if browser == nil {
		browser = localChromiumDiscovery{}
	}
	var wechatOrigin *url.URL
	if dependencies.WeChatOrigin != nil {
		clone := *dependencies.WeChatOrigin
		wechatOrigin = &clone
	}
	return &runtimeManager{
		version: version, paths: paths, http: httpDoer, clock: clock,
		filesystem: filesystem, browser: browser, browserExplicit: browserExplicit, pdfRunner: dependencies.PDFRunner, signals: signals,
		secrets: secretStore, executable: executable, worker: worker, factory: dependencies.ApplicationFactory,
		wechatOrigin: wechatOrigin, downloadPolicy: cloneDestinationPolicy(dependencies.DownloadDestinationPolicy),
		databaseOpen: library.Open,
	}
}

func cloneDestinationPolicy(policy network.DestinationPolicy) network.DestinationPolicy {
	clone := policy
	if policy.AllowedHosts != nil {
		clone.AllowedHosts = make(map[string]struct{}, len(policy.AllowedHosts))
		for host := range policy.AllowedHosts {
			clone.AllowedHosts[host] = struct{}{}
		}
	}
	if policy.AllowedAuthorities != nil {
		clone.AllowedAuthorities = make(map[string]struct{}, len(policy.AllowedAuthorities))
		for authority := range policy.AllowedAuthorities {
			clone.AllowedAuthorities[authority] = struct{}{}
		}
	}
	return clone
}

func validateControlledOriginDependencies(origin *url.URL, policy network.DestinationPolicy) error {
	hasPolicy := policy.AllowedHosts != nil || policy.AllowedAuthorities != nil || policy.AllowSubdomains ||
		policy.AllowLoopback || policy.AllowPrivate || policy.AllowCloudMetadata || policy.Resolver != nil
	if origin == nil {
		if hasPolicy {
			return errors.New("download destination policy override requires a controlled WeChat origin")
		}
		return nil
	}
	if !policy.AllowLoopback || policy.AllowPrivate || policy.AllowCloudMetadata || policy.AllowSubdomains || policy.Resolver != nil ||
		len(policy.AllowedHosts) != 1 || len(policy.AllowedAuthorities) != 1 {
		return errors.New("controlled WeChat origin requires an exact authority-bound loopback download policy")
	}
	host := strings.ToLower(origin.Hostname())
	authority := strings.ToLower(origin.Host)
	if _, ok := policy.AllowedHosts[host]; !ok {
		return errors.New("controlled WeChat origin download policy host mismatch")
	}
	if _, ok := policy.AllowedAuthorities[authority]; !ok {
		return errors.New("controlled WeChat origin download policy authority mismatch")
	}
	return nil
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
	if previous != nil && previous != runtime {
		if err := previous.Close(); err != nil {
			manager.active = nil
			return fmt.Errorf("close previous profile database: %w", err)
		}
	}
	manager.active = runtime
	return nil
}

func (manager *runtimeManager) buildLocked(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {
	runtime, err := manager.prepareLocked(ctx, profile)
	if err != nil {
		return nil, err
	}
	previous := manager.active
	if previous != nil {
		if err := previous.Close(); err != nil {
			_ = runtime.Close()
			manager.active = nil
			return nil, fmt.Errorf("close previous profile database: %w", err)
		}
	}
	manager.active = runtime
	return runtime, nil
}

func (manager *runtimeManager) prepareLocked(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {
	return manager.prepareProfileLocked(ctx, profile, false)
}

func (manager *runtimeManager) prepareProfileLocked(ctx context.Context, profile profiles.Profile, maintenanceGateHeld bool) (*ProfileRuntime, error) {
	if err := validateControlledOriginDependencies(manager.wechatOrigin, manager.downloadPolicy); err != nil {
		return nil, err
	}

	profilePaths := manager.paths.ForProfile(profile.ID)
	if err := ensureProfilePaths(manager.filesystem, profilePaths); err != nil {
		return nil, err
	}
	var gate *profiles.ProfileLock
	var err error
	if !maintenanceGateHeld {
		gate, err = profiles.AcquireRuntimeGate(ctx, profilePaths)
		if err != nil {
			return nil, err
		}
		defer gate.Close()
	}
	runtimeLock, err := profiles.AcquireRuntimeLock(ctx, profilePaths)
	if err != nil {
		return nil, err
	}
	objectStore, err := objects.NewFileStore(profilePaths.Objects)
	if err != nil {
		_ = runtimeLock.Close()
		return nil, fmt.Errorf("open profile object store: %w", err)
	}
	database, err := manager.databaseOpen(ctx, library.OpenOptions{
		Path: profilePaths.Database, ProfileID: profile.ID, ProfileName: profile.Name,
	})
	if err != nil {
		_ = runtimeLock.Close()
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
	var wechatClient *wechat.Client
	if manager.wechatOrigin != nil {
		var wechatErr error
		wechatClient, wechatErr = wechat.NewClientForControlledOrigin(httpClient, manager.secrets, string(profile.ID), manager.wechatOrigin)
		if wechatErr != nil {
			_ = database.Close()
			_ = runtimeLock.Close()
			return nil, fmt.Errorf("configure controlled WeChat origin: %w", wechatErr)
		}
	} else {
		wechatClient = wechat.NewClient(httpClient, manager.secrets, string(profile.ID))
	}
	jobsStore := library.NewJobStore(database)
	jobsStore.SetAdmissionGuard(func(admissionCtx context.Context) (func() error, error) {
		lock, lockErr := profiles.AcquireRuntimeGate(admissionCtx, profilePaths)
		if lockErr != nil {
			return nil, lockErr
		}
		return lock.Close, nil
	})
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
		Jobs: jobsStore, WeChat: wechatClient, Network: routeService, runtimeLock: runtimeLock,
	}
	if manager.factory != nil {
		runtime.Core = manager.factory(runtime)
	} else {
		configuration, _, configErr := profiles.NewConfigStore(profilePaths.Config).Read()
		if configErr != nil {
			_ = runtime.Close()
			return nil, fmt.Errorf("read profile configuration: %w", configErr)
		}
		scheduler := jobs.NewScheduler(profileSchedulerLimits(configuration.Preferences.Download.Concurrency), jobs.SchedulerOptions{
			PermitStore: library.NewSchedulerPermitStore(database),
			Owner:       fmt.Sprintf("scheduler-%d-%p", os.Getpid(), database),
		})
		var contentBaseURL *url.URL
		if manager.wechatOrigin != nil {
			clone := *manager.wechatOrigin
			contentBaseURL = &clone
		}
		downloads, downloadErr := newLocalDownloadRuntime(runtime, manager.secrets, manager.http, downloadRuntimeOptions{
			Proxy: configuration.Preferences.Proxy, ProxyConfigured: true,
			Concurrency: configuration.Preferences.Download.Concurrency, Scheduler: scheduler,
			DestinationPolicy: manager.downloadPolicy, ContentBaseURL: contentBaseURL,
		})
		if downloadErr != nil {
			_ = runtime.Close()
			return nil, fmt.Errorf("configure download runtime: %w", downloadErr)
		}
		runtime.Downloads = downloads
		if downloads != nil {
			runtime.Credentials = downloads.credentials
		}
		syncs := newLocalSyncRuntime(runtime, manager.clock, scheduler)
		exports := newLocalExportRuntime(runtime, manager.clock, manager.browser, manager.pdfRunner, scheduler)
		if exports != nil {
			exports.admit = jobsStore.WithAdmission
		}
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

func profileSchedulerLimits(downloadConcurrency int) jobs.Limits {
	limits := downloadSchedulerLimits(downloadConcurrency)
	limits.PerOperation["account_sync"] = 1
	limits.PerOperation["album_sync"] = 1
	limits.PerOperation["export"] = 2
	return limits
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
