package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
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

	jsonOut bool
	debug   bool

	workspaceRunner func(context.Context, tui.WorkspaceOptions) error
	forceWorkspace  bool
}

func New(stdin io.Reader, stdout, stderr io.Writer) *App {
	appInstance, err := NewWithDependencies(context.Background(), stdin, stdout, stderr, Dependencies{})
	if err != nil {
		// Preserve the historical constructor signature for embedders. Runtime
		// initialization errors are surfaced by status and local operations.
		fallback := application.New(application.Options{Version: Version})
		return &App{stdin: stdin, stdout: stdout, stderr: stderr, core: fallback, secret: secrets.NewMemoryStore()}
	}
	return appInstance
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
	paths, err := defaultPaths(dependencies.PathOptions)
	if err != nil {
		return nil, fmt.Errorf("resolve runtime paths: %w", err)
	}
	secretStore := dependencies.Secrets
	if secretStore == nil {
		secretStore, err = defaultSecretStoreFromEnvironment()
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

func defaultSecretStoreFromEnvironment() (secrets.Store, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_SECRET_BACKEND"))) {
	case "", "os-keyring":
		return secrets.NewKeyringStore(""), nil
	case "memory":
		return secrets.NewMemoryStore(), nil
	default:
		return nil, errors.New("WECHAT_ARTICLE_SECRET_BACKEND must be os-keyring or memory")
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
	if a.runtimes == nil {
		return nil
	}
	return a.runtimes.Close()
}

func (a *App) Execute(ctx context.Context, args []string) error {
	a.jsonOut = false
	a.debug = false
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
		return err
	}
	a.active = runtime
	a.core = runtime.Core
	if runtime.Network != nil {
		a.proxy = runtime.Network
	}
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
					case wechat.QRConfirmed:
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

func (a *App) openBrowser(ctx context.Context, target string) error {
	if a.runtimes != nil && a.runtimes.browser != nil {
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
