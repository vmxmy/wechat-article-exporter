package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
	"golang.org/x/term"
)

var Version = "2.0.0"

type App struct {
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	core     application.Application
	profiles *profiles.Registry
	secret   secrets.Store
	proxy    proxyManager
	runtimes *runtimeManager
	active   *ProfileRuntime
	initErr  error

	jsonOut bool
	debug   bool

	workspaceRunner func(context.Context, tui.WorkspaceOptions) error
	forceWorkspace  bool
	startupArgs     []string
}

func New(stdin io.Reader, stdout, stderr io.Writer) *App {
	return newWithStartupArgs(stdin, stdout, stderr, nil)
}

// NewForArgs constructs the executable adapter with the exact argument vector
// that will later be passed to Execute. Embedders should use New, which never
// inspects or binds itself to the host process's os.Args.
func NewForArgs(stdin io.Reader, stdout, stderr io.Writer, args []string) *App {
	dependencies := Dependencies{StartupArgs: args}
	configuration, enabled, configurationErr := controlledCleanRoomDependencies()
	if configurationErr != nil {
		return newInitializationFailure(stdin, stdout, stderr, dependencies.StartupArgs, configurationErr)
	}
	if enabled {
		dependencies.WeChatOrigin = configuration.origin
		dependencies.DownloadDestinationPolicy = configuration.policy
		dependencies.Worker = foregroundProcessWorker{}
	}
	return newWithStartupArgsAndDependencies(stdin, stdout, stderr, dependencies)
}

func newWithStartupArgs(stdin io.Reader, stdout, stderr io.Writer, args []string) *App {
	return newWithStartupArgsAndDependencies(stdin, stdout, stderr, Dependencies{StartupArgs: args})
}

func newWithStartupArgsAndDependencies(stdin io.Reader, stdout, stderr io.Writer, dependencies Dependencies) *App {
	appInstance, err := NewWithDependencies(context.Background(), stdin, stdout, stderr, dependencies)
	if err != nil {
		return newInitializationFailure(stdin, stdout, stderr, dependencies.StartupArgs, err)
	}
	return appInstance
}

func newInitializationFailure(stdin io.Reader, stdout, stderr io.Writer, args []string, err error) *App {
	// Preserve the historical constructor signature for embedders. Runtime
	// initialization errors are surfaced by status and local operations. The
	// fallback deliberately does not initialize network or storage adapters.
	fallback := application.New(application.Options{Version: Version})
	result := &App{stdin: stdin, stdout: stdout, stderr: stderr, core: fallback, secret: secrets.NewMemoryStore(), initErr: err}
	result.startupArgs = append([]string(nil), args...)
	if paths, pathErr := defaultPaths(pathOptionsFromEnvironment()); pathErr == nil {
		result.runtimes = &runtimeManager{paths: paths}
	}
	return result
}

func NewWithDependencies(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	dependencies Dependencies,
) (*App, error) {
	if dependencies.PathOptions == (profiles.PathOptions{}) {
		dependencies.PathOptions = pathOptionsFromEnvironment()
	}
	if dependencies.Secrets == nil && startupRequestsVaultCommand(dependencies.StartupArgs) {
		dependencies.Secrets = secrets.NewMemoryStore()
	}
	paths, err := defaultPaths(dependencies.PathOptions)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime paths: %w", err)
	}
	secretStore := dependencies.Secrets
	if secretStore == nil {
		secretStore, err = defaultSecretStoreFromEnvironment(paths, stdin, stderr)
		if err != nil {
			return nil, err
		}
		dependencies.Secrets = secretStore
	}
	registry := profiles.NewRegistry(paths, secretStore)
	activeProfile, err := selectedProfile(registry, os.Getenv("WECHAT_ARTICLE_PROFILE"))
	if err != nil {
		return nil, err
	}
	manager := newRuntimeManager(Version, paths, dependencies)
	active, err := manager.Build(ctx, activeProfile)
	if err != nil {
		return nil, err
	}
	return &App{
		stdin: stdin, stdout: stdout, stderr: stderr,
		core:     active.Core,
		profiles: registry, secret: secretStore, proxy: active.Network, runtimes: manager, active: active,
		startupArgs: append([]string(nil), dependencies.StartupArgs...),
	}, nil
}

func pathOptionsFromEnvironment() profiles.PathOptions {
	if root := strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_PORTABLE_ROOT")); root != "" {
		return profiles.PathOptions{Portable: true, PortableRoot: root}
	}
	return profiles.PathOptions{
		ConfigRoot: strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_CONFIG_ROOT")),
		DataRoot:   strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_DATA_ROOT")),
		CacheRoot:  strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_CACHE_ROOT")),
		StateRoot:  strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_STATE_ROOT")),
	}
}

func defaultSecretStoreFromEnvironment(paths profiles.Paths, stdin io.Reader, stderr io.Writer) (secrets.Store, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_SECRET_BACKEND"))) {
	case "", "os-keyring":
		return secrets.NewKeyringStore(""), nil
	case "vault", "encrypted-vault":
		store := secrets.NewVaultStore(paths.VaultFile(), secrets.DefaultVaultParameters)
		if _, statErr := os.Stat(paths.VaultFile()); errors.Is(statErr, os.ErrNotExist) {
			return nil, errors.New("encrypted vault is not initialized; run `wechat-article vault init`")
		} else if statErr != nil {
			return nil, fmt.Errorf("inspect encrypted vault: %w", statErr)
		}
		passphrase, err := vaultPassphraseFromEnvironmentOrTerminal(stdin, stderr)
		if err != nil {
			return nil, err
		}
		defer zeroSecret(passphrase)
		if err := store.Unlock(passphrase); err != nil {
			return nil, err
		}
		return store, nil
	case "memory":
		return secrets.NewMemoryStore(), nil
	default:
		return nil, errors.New("WECHAT_ARTICLE_SECRET_BACKEND must be os-keyring, vault, or memory")
	}
}

func startupRequestsVaultCommand(args []string) bool {
	for _, argument := range args {
		if argument == "vault" {
			return true
		}
		if !strings.HasPrefix(argument, "-") {
			return false
		}
	}
	return false
}

func vaultPassphraseFromEnvironmentOrTerminal(stdin io.Reader, stderr io.Writer) ([]byte, error) {
	if path := strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE")); path != "" {
		return readPassphraseFile(path)
	}
	if value := os.Getenv("WECHAT_ARTICLE_VAULT_PASSPHRASE"); value != "" {
		return []byte(value), nil
	}
	file, ok := stdin.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return nil, errors.New("encrypted vault unlock requires WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE, WECHAT_ARTICLE_VAULT_PASSPHRASE, or an interactive terminal")
	}
	return readTerminalPassphrase(stdin, stderr, "Encrypted vault passphrase: ")
}

func zeroSecret(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func selectedProfile(registry *profiles.Registry, selected string) (profiles.Profile, error) {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return defaultProfile(registry)
	}
	items, err := registry.List()
	if err != nil {
		return profiles.Profile{}, err
	}
	for _, profile := range items {
		if profile.ID == domain.ProfileID(selected) {
			return profile, nil
		}
	}
	return profiles.Profile{}, fmt.Errorf("profile %q does not exist", selected)
}

func (a *App) Close() error {
	if vault, ok := a.secret.(*secrets.VaultStore); ok {
		defer vault.Lock()
	}
	if a.runtimes == nil {
		return nil
	}
	return a.runtimes.Close()
}

func (a *App) Execute(ctx context.Context, args []string) error {
	a.jsonOut = false
	a.debug = false
	if a.initErr == nil && len(a.startupArgs) > 0 && !slices.Equal(a.startupArgs, args) {
		return usage("this application instance was initialized for a different argument set; create a new App for each command")
	}
	if a.initErr != nil && !bootstrapCommand(args) {
		return safety.RedactError(a.initErr)
	}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetContext(ctx)
	err := root.Execute()
	if err == nil {
		return nil
	}
	var usageError *UsageError
	if errors.As(err, &usageError) {
		return usage(safety.RedactText(usageError.Error()))
	}
	if isCobraUsageError(err) {
		return usage(safety.RedactText(err.Error()))
	}
	return safety.RedactError(err)
}

func bootstrapCommand(args []string) bool {
	for _, argument := range args {
		if argument == "--" {
			break
		}
		if argument == "--help" || argument == "-h" || argument == "--version" {
			return true
		}
	}
	for _, argument := range args {
		if argument == "--" {
			return false
		}
		if argument == "help" || argument == "version" {
			return true
		}
		if argument == "vault" {
			return true
		}
		if !strings.HasPrefix(argument, "-") {
			return false
		}
	}
	return false
}

func (a *App) JSONOutputEnabled() bool { return a.jsonOut }

func (a *App) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "wechat-article",
		Short: "Local-first WeChat article library and exporter",
		Long:  "Manage a profile-isolated WeChat article library locally. Project-operated Web, remote MCP, and remote OAuth services are retired.",
		Example: `  wechat-article profile create work
  wechat-article login --qr-output ./wechat-login.png
  wechat-article account search "OpenAI" --json
  wechat-article sync account account-id --follow
  wechat-article export start --format markdown --account account-id --output ./exports --wait`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			if a.debug {
				if a.active != nil {
					a.debugf("config=%s", a.active.Profile.Paths.Config)
				}
			}
		},
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if a.jsonOut {
				return usage("a command is required with --json")
			}
			if !a.forceWorkspace && !tui.IsInteractive(a.stdin, a.stdout) {
				return command.Help()
			}
			return a.runDashboard(command.Context())
		},
	}
	root.SetIn(a.stdin)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage(err.Error()) })
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, "force machine-readable JSON output")
	root.PersistentFlags().BoolVar(&a.debug, "debug", false, "enable verbose development logging")
	root.AddCommand(
		a.localStatusCommand(),
		a.localLoginCommand(),
		a.localLogoutCommand(),
		a.localSessionCommand(),
		a.profileCommand(),
		a.articleCommand(),
		a.accountCommand(),
		a.albumCommand(),
		a.syncCommand(),
		a.downloadCommand(),
		a.metadataCommand(),
		a.commentsCommand(),
		a.credentialCommand(),
		a.proxyCommand(),
		a.jobCommand(),
		a.exportCommand(),
		a.databaseCommand(),
		a.migrationCommand(),
		a.diagnosticsCommand(),
		a.vaultCommand(),
		a.mcpCommand(),
		a.completionCommand(root),
	)
	return root
}

func (a *App) profileCommand() *cobra.Command {
	profile := &cobra.Command{Use: "profile", Short: "Manage isolated local profiles"}
	profile.AddCommand(&cobra.Command{
		Use: "create <name>", Short: "Create a local profile", Args: exactArgs(1, "profile create requires <name>"),
		RunE: func(_ *cobra.Command, args []string) error {
			created, err := a.profiles.Create(args[0])
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": created})
		},
	})
	profile.AddCommand(&cobra.Command{
		Use: "list", Short: "List local profiles", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			items, err := a.profiles.List()
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{"profiles": items, "count": len(items)}})
		},
	})
	profile.AddCommand(&cobra.Command{
		Use: "use <name>", Short: "Activate a local profile", Args: exactArgs(1, "profile use requires <name>"),
		RunE: func(_ *cobra.Command, args []string) error {
			selected, err := a.findProfile(domain.ProfileID(args[0]))
			if err != nil {
				return err
			}
			prepared, err := a.prepareProfile(context.Background(), selected)
			if err != nil {
				return err
			}
			selected, err = a.profiles.Use(selected.ID)
			if err != nil {
				_ = prepared.Close()
				return err
			}
			if err := a.commitProfile(prepared); err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": selected})
		},
	})
	var confirmation string
	deleteCommand := &cobra.Command{
		Use: "delete <name>", Short: "Delete a non-active local profile", Args: exactArgs(1, "profile delete requires <name>"),
		RunE: func(_ *cobra.Command, args []string) error {
			required := "delete-profile:" + args[0]
			if confirmation != required {
				return usage("profile deletion requires --confirm " + required)
			}
			if err := a.profiles.Delete(domain.ProfileID(args[0])); err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{"deleted": args[0]}})
		},
	}
	deleteCommand.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation value")
	profile.AddCommand(deleteCommand)
	return profile
}

func (a *App) findProfile(id domain.ProfileID) (profiles.Profile, error) {
	items, err := a.profiles.List()
	if err != nil {
		return profiles.Profile{}, err
	}
	for _, profile := range items {
		if profile.ID == id {
			return profile, nil
		}
	}
	return profiles.Profile{}, fmt.Errorf("profile %q does not exist", id)
}

func (a *App) prepareProfile(ctx context.Context, profile profiles.Profile) (*ProfileRuntime, error) {
	if a.runtimes == nil {
		return nil, errors.New("profile runtime manager is unavailable")
	}
	runtime, err := a.runtimes.Prepare(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("prepare profile %q: %w", profile.ID, err)
	}
	return runtime, nil
}

func (a *App) commitProfile(runtime *ProfileRuntime) error {
	if err := a.runtimes.Activate(runtime); err != nil {
		_ = runtime.Close()
		a.active = nil
		a.core = application.New(application.Options{Version: Version})
		a.proxy = nil
		return err
	}
	a.active = runtime
	a.core = runtime.Core
	if runtime.Network != nil {
		a.proxy = runtime.Network
	}
	return nil
}

func (a *App) restoreActiveProfile(
	ctx context.Context,
	archivePath string,
	conflict library.RestoreConflictPolicy,
) (library.RestoreReport, error) {
	if a == nil || a.runtimes == nil || a.active == nil || a.active.Library == nil || a.active.Jobs == nil {
		return library.RestoreReport{}, errors.New("active profile storage is unavailable")
	}
	a.runtimes.mu.Lock()
	defer a.runtimes.mu.Unlock()
	active := a.active
	gate, err := profiles.AcquireMaintenanceGate(ctx, active.Profile.Paths)
	if err != nil {
		return library.RestoreReport{}, err
	}
	gateOpen := true
	defer func() {
		if gateOpen {
			_ = gate.Close()
		}
	}()
	blockers, err := active.Jobs.RestoreBlockers(ctx)
	if err != nil {
		return library.RestoreReport{}, fmt.Errorf("check restore blockers: %w", err)
	}
	if len(blockers) > 0 {
		return library.RestoreReport{}, fmt.Errorf("restore blocked by %d running job or active lease; pause, cancel, or recover jobs first: %w", len(blockers), profiles.ErrProfileBusy)
	}
	if err := active.Close(); err != nil {
		a.clearActiveRuntime()
		reopenErr := a.reopenActiveProfile(active.Profile)
		return library.RestoreReport{}, errors.Join(err, reopenErr)
	}
	a.clearActiveRuntime()
	maintenanceRuntime, err := profiles.AcquireMaintenanceRuntimeLock(ctx, active.Profile.Paths)
	if err != nil {
		reopenErr := a.reopenActiveProfile(active.Profile)
		return library.RestoreReport{}, errors.Join(err, reopenErr)
	}
	report, restoreErr := library.RestoreBackup(ctx, library.RestoreOptions{
		ArchivePath: archivePath, DatabasePath: active.Profile.Paths.Database, ObjectStore: active.Objects,
		ConfigPath: active.Profile.Paths.Config, ConflictPolicy: conflict,
		TargetProfile: active.Profile.ID, TargetName: active.Profile.Name,
	})
	lockErr := maintenanceRuntime.Close()
	reopenErr := a.reopenActiveProfile(active.Profile)
	gateErr := gate.Close()
	gateOpen = false
	if restoreErr != nil {
		return report, errors.Join(restoreErr, lockErr, reopenErr, gateErr)
	}
	if lockErr != nil || reopenErr != nil || gateErr != nil {
		return report, errors.Join(fmt.Errorf("restore committed but active runtime could not be reopened safely"), lockErr, reopenErr, gateErr)
	}
	return report, nil
}

func (a *App) clearActiveRuntime() {
	if a == nil {
		return
	}
	if a.runtimes != nil {
		a.runtimes.active = nil
	}
	a.active = nil
	a.core = application.New(application.Options{Version: Version})
	a.proxy = nil
}

func (a *App) reopenActiveProfile(profile profiles.Profile) error {
	if a == nil || a.runtimes == nil {
		return errors.New("profile runtime manager is unavailable")
	}
	reopenCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prepared, err := a.runtimes.prepareProfileLocked(reopenCtx, profile, true)
	if err != nil {
		a.clearActiveRuntime()
		return fmt.Errorf("reopen active profile runtime: %w", err)
	}
	a.runtimes.active = prepared
	a.active = prepared
	a.core = prepared.Core
	a.proxy = prepared.Network
	return nil
}

func (a *App) localStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show local runtime, storage, and WeChat session status",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			status, err := a.core.RuntimeStatus(command.Context())
			if err != nil {
				return err
			}
			session, err := a.core.SessionStatus(command.Context())
			if err != nil {
				return err
			}
			if a.active == nil {
				return errors.New("active profile runtime is unavailable")
			}
			configStore := profiles.NewConfigStore(a.active.Profile.Paths.Config)
			configuration, configurationBackup, err := configStore.Read()
			if err != nil {
				return err
			}
			effectiveConfiguration := profiles.EffectiveConfig{
				Path:            configStore.Path(),
				SchemaVersion:   configuration.SchemaVersion,
				ProfileID:       configuration.ProfileID,
				Preferences:     configuration.Preferences,
				MCP:             configuration.MCP,
				MigrationBackup: configurationBackup,
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{
				"runtime": status, "session": session, "configuration": effectiveConfiguration,
				"retirement": retirementState(),
			}})
		},
	}
}

func (a *App) localLoginCommand() *cobra.Command {
	var qrOutput string
	var pollInterval time.Duration
	var refreshes int
	command := &cobra.Command{
		Use:   "login",
		Short: "Log in to WeChat locally with a QR code",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			interactive := tui.IsInteractive(a.stdin, a.stdout)
			if !interactive && strings.TrimSpace(qrOutput) == "" {
				return usage("non-interactive login requires --qr-output <path>")
			}
			if pollInterval < 500*time.Millisecond || pollInterval > 30*time.Second {
				return usage("--poll-interval must be between 500ms and 30s")
			}
			if refreshes < 0 || refreshes > 10 {
				return usage("--refreshes must be between 0 and 10")
			}
			for attempt := 0; attempt <= refreshes; attempt++ {
				flow, err := a.core.BeginLogin(command.Context(), "")
				if err != nil {
					return err
				}
				if qrOutput != "" {
					if err := wechat.WriteQRImage(qrOutput, flow.QRBytes); err != nil {
						return err
					}
					fmt.Fprintf(a.stderr, "WeChat login QR written to %s; scan and confirm in WeChat.\n", qrOutput)
				} else {
					text, err := wechat.RenderQRImageText(flow.QRBytes)
					if err != nil {
						return err
					}
					fmt.Fprintln(a.stderr, text)
					fmt.Fprintf(a.stderr, "Scan and confirm before %s. Press Ctrl-C to cancel.\n", flow.ExpiresAt.Local().Format(time.RFC3339))
				}
				for {
					result, err := a.core.PollLogin(command.Context())
					if err != nil {
						return err
					}
					fmt.Fprintf(a.stderr, "WeChat login status: %s\n", result.State)
					switch result.State {
					case wechat.QRConfirmed, wechat.QRScanned:
						session, err := a.core.CompleteLogin(command.Context())
						if err != nil {
							return err
						}
						return a.output(map[string]any{"success": true, "data": session})
					case wechat.QRExpired:
						if attempt == refreshes {
							return wechat.ErrLoginExpired
						}
						goto refresh
					}
					select {
					case <-command.Context().Done():
						return command.Context().Err()
					case <-time.After(pollInterval):
					}
				}
			refresh:
			}
			return wechat.ErrLoginExpired
		},
	}
	command.Flags().StringVar(&qrOutput, "qr-output", "", "write the upstream login QR image to this path")
	command.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "bounded login polling interval")
	command.Flags().IntVar(&refreshes, "refreshes", 1, "number of automatic QR refreshes after expiry")
	return command
}

func (a *App) localLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of WeChat and remove the local session secret",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := a.core.Logout(command.Context()); err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{"authenticated": false, "localSessionRemoved": true}})
		},
	}
}

func (a *App) localSessionCommand() *cobra.Command {
	command := &cobra.Command{Use: "session", Short: "Inspect and switch accounts available in the authenticated WeChat session"}
	list := &cobra.Command{Use: "accounts", Short: "List switchable upstream accounts", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			items, err := a.core.ListSwitchableAccounts(command.Context())
			if err != nil {
				return err
			}
			return a.output(map[string]any{"accounts": items, "count": len(items)})
		}}
	switchCommand := &cobra.Command{Use: "switch <id>", Short: "Switch the active upstream account and persist the session", Args: exactArgs(1, "session switch requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			session, err := a.core.SwitchAccount(command.Context(), args[0])
			if err != nil {
				return err
			}
			return a.output(session)
		}}
	command.AddCommand(list, switchCommand)
	return command
}

func (a *App) openBrowser(ctx context.Context, target string) error {
	if a.runtimes != nil && a.runtimes.browser != nil && a.runtimes.browserExplicit {
		browser, err := a.runtimes.browser.FindChromium(ctx)
		if err != nil {
			return fmt.Errorf("discover local browser: %w", err)
		}
		if strings.TrimSpace(browser.Path) == "" {
			return errors.New("browser discovery returned an empty executable path")
		}
		return launchBrowserExecutable(browser.Path, target)
	}
	return openBrowser(target)
}

func (a *App) runDashboard(ctx context.Context) error {
	if a.active == nil {
		return errors.New("active profile runtime is unavailable")
	}
	configuration, _, err := profiles.NewConfigStore(a.active.Profile.Paths.Config).Read()
	if err != nil {
		return err
	}
	options := tui.WorkspaceOptions{
		Application: a.core,
		Extensions:  newWorkspaceExtensions(a),
		Input:       a.stdin,
		Output:      a.stdout,
		Force:       a.forceWorkspace,
		NoColor:     configuration.Preferences.Display.NoColor,
		ASCII:       configuration.Preferences.Display.ASCII,
		Plain:       configuration.Preferences.Display.Plain,
		Language:    configuration.Preferences.Display.Language,
		PageSize:    configuration.Preferences.Sync.PageSize,
	}
	runner := a.workspaceRunner
	if runner == nil {
		runner = tui.RunWorkspace
	}
	return runner(ctx, options)
}

func (a *App) spinner(label string) *tui.Spinner {
	if a.jsonOut || !tui.IsInteractive(a.stdin, a.stderr) {
		return nil
	}
	return tui.StartSpinner(a.stderr, label)
}

func isCobraUsageError(err error) bool {
	message := err.Error()
	return strings.HasPrefix(message, "unknown command") ||
		strings.HasPrefix(message, "unknown flag") ||
		strings.Contains(message, "accepts ") ||
		strings.Contains(message, "requires at least") ||
		strings.Contains(message, "requires no arguments")
}

func exactArgs(count int, message string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != count {
			return usage(message)
		}
		return nil
	}
}

func statusLabel(authenticated bool) string {
	if authenticated {
		return "Authenticated"
	}
	return "Not authenticated"
}

func (a *App) debugf(format string, args ...any) {
	if a.debug {
		fmt.Fprintln(a.stderr, "debug: "+safety.RedactText(fmt.Sprintf(format, args...)))
	}
}
