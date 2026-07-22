package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/input"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/legacyremote"
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
	store    *config.Store
	core     application.Application
	legacy   *legacyremote.Adapter
	profiles *profiles.Registry
	secret   secrets.Store
	proxy    proxyManager
	runtimes *runtimeManager
	active   *ProfileRuntime

	server  string
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
		store := config.NewStore("")
		fallback := application.New(application.Options{Version: Version})
		return &App{stdin: stdin, stdout: stdout, stderr: stderr, store: store, core: fallback,
			legacy: legacyremote.New(store, Version, http.DefaultClient), secret: secrets.NewMemoryStore()}
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
		secretStore = secrets.NewKeyringStore("")
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
	store := dependencies.LegacyConfig
	if store == nil {
		store = config.NewStore("")
	}
	httpDoer := dependencies.HTTP
	if httpDoer == nil {
		httpDoer = http.DefaultClient
	}
	return &App{
		stdin: stdin, stdout: stdout, stderr: stderr,
		store: store, core: active.Core, legacy: newLegacyAdapter(store, httpDoer),
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
	a.server = ""
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
		Long:  "Manage a profile-isolated WeChat article library locally. Remote OAuth and MCP compatibility is available only below the legacy command.",
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
				a.debugf("config=%s", a.store.Path())
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
		a.legacyCommand(),
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
			migration, migrationErr := a.legacyMigration()
			if migrationErr != nil {
				return migrationErr
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
				"legacyMigration": migration,
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

func (a *App) legacyCommand() *cobra.Command {
	legacy := &cobra.Command{
		Use:   "legacy",
		Short: "Deprecated remote OAuth/MCP compatibility during migration",
		Long: "The remote OAuth/MCP client is deprecated and is not used by normal commands. " +
			"Create a local profile and run `wechat-article login`; legacy tokens are never copied into the local WeChat session store.",
		Example: `  wechat-article legacy status
  wechat-article legacy api list
  wechat-article profile create local
  wechat-article login --qr-output ./wechat-login.png`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			a.debugf("using deprecated legacy remote compatibility adapter")
		},
	}
	legacy.PersistentFlags().StringVar(&a.server, "server", "", "deprecated remote MCP server base URL")
	legacy.AddCommand(
		a.loginCommand(), a.logoutCommand(), a.statusCommand(), a.apiCommand("api"), a.apiCommand("mcp"),
		a.legacyArticleCommand(), a.legacyAccountCommand(), a.legacyAlbumCommand(),
	)
	return legacy
}

func (a *App) loginCommand() *cobra.Command {
	var headless bool
	var noOpen bool
	command := &cobra.Command{
		Use:   "login",
		Short: "Authorize this CLI with the remote MCP server",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if headless && noOpen {
				return usage("--headless and --no-open cannot be used together")
			}
			return a.login(command.Context(), headless, noOpen)
		},
	}
	command.Flags().BoolVar(&headless, "headless", false, "show a callback command for a browserless machine")
	command.Flags().BoolVar(&noOpen, "no-open", false, "print the authorization URL without opening a browser")
	return command
}

func (a *App) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove saved OAuth credentials",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cleared, err := a.legacy.Logout()
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{"server": cleared.Server, "authenticated": false}})
		},
	}
}

func (a *App) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show deprecated remote authentication state",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.printLegacyStatus()
		},
	}
}

func (a *App) apiCommand(name string) *cobra.Command {
	command := &cobra.Command{Use: name, Short: "Discover and call the server-advertised MCP tool surface"}
	listName := "list"
	if name == "mcp" {
		listName = "tools"
	}
	command.AddCommand(&cobra.Command{
		Use:   listName,
		Short: "List remote MCP tools",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			tools, err := a.legacy.ListTools(command.Context(), a.server)
			if err != nil {
				return err
			}
			sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
			return a.output(map[string]any{"success": true, "data": map[string]any{"count": len(tools), "tools": tools}})
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "describe <tool>",
		Short: "Describe one MCP tool",
		Args:  exactArgs(1, "api describe requires a tool name"),
		RunE: func(command *cobra.Command, args []string) error {
			tool, err := a.legacy.FindTool(command.Context(), a.server, args[0])
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": tool})
		},
	})
	command.AddCommand(a.callCommand())
	return command
}

func (a *App) callCommand() *cobra.Command {
	var options input.Options
	var dryRun bool
	var confirmation string
	command := &cobra.Command{
		Use:   "call <tool>",
		Short: "Call one MCP tool with structured JSON input",
		Args:  exactArgs(1, "api call requires a tool name"),
		RunE: func(command *cobra.Command, args []string) error {
			options.InlineSet = command.Flags().Changed("input")
			options.FileSet = command.Flags().Changed("file")
			arguments, err := input.Load(options, a.stdin)
			if err != nil {
				return usage(err.Error())
			}
			return a.executeTool(command.Context(), args[0], arguments, dryRun, confirmation)
		},
	}
	command.Flags().StringVar(&options.Inline, "input", "", "inline JSON object")
	command.Flags().StringVar(&options.File, "file", "", "read JSON object from a file")
	command.Flags().BoolVar(&options.Stdin, "stdin", false, "read JSON object from stdin")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "print a redacted preview without connecting")
	command.Flags().StringVar(&confirmation, "confirm", "", "exact tool name required for protected operations")
	return command
}

func (a *App) legacyArticleCommand() *cobra.Command {
	article := &cobra.Command{Use: "article", Short: "Download and list WeChat articles"}
	var format string
	var dryRun bool
	article.AddCommand(&cobra.Command{
		Use:   "download <url>",
		Short: "Download an article",
		Args:  exactArgs(1, "article download requires <url>"),
		RunE: func(command *cobra.Command, args []string) error {
			return a.executeTool(command.Context(), "download_article", map[string]any{"url": args[0], "format": format}, dryRun, "")
		},
	})
	download := article.Commands()[0]
	download.Flags().StringVar(&format, "format", "markdown", "markdown, text, html, or json")
	download.Flags().BoolVar(&dryRun, "dry-run", false, "print a redacted preview without connecting")

	var keyword string
	var begin, size int
	article.AddCommand(&cobra.Command{
		Use:   "list <fakeid>",
		Short: "List articles for an account",
		Args:  exactArgs(1, "article list requires <fakeid>"),
		RunE: func(command *cobra.Command, args []string) error {
			if begin < 0 || size < 0 {
				return usage("--begin and --size must be non-negative integers")
			}
			arguments := map[string]any{"fakeid": args[0], "begin": begin, "size": size}
			if keyword != "" {
				arguments["keyword"] = keyword
			}
			return a.executeTool(command.Context(), "list_articles", arguments, false, "")
		},
	})
	list := article.Commands()[1]
	list.Flags().StringVar(&keyword, "keyword", "", "filter by title keyword")
	list.Flags().IntVar(&begin, "begin", 0, "pagination offset")
	list.Flags().IntVar(&size, "size", 5, "number of articles")
	return article
}

func (a *App) legacyAccountCommand() *cobra.Command {
	account := &cobra.Command{Use: "account", Short: "Search and inspect WeChat accounts"}
	var begin, size int
	search := &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search WeChat accounts",
		Args:  exactArgs(1, "account search requires <keyword>"),
		RunE: func(command *cobra.Command, args []string) error {
			if begin < 0 || size < 0 {
				return usage("--begin and --size must be non-negative integers")
			}
			return a.executeTool(command.Context(), "search_accounts", map[string]any{"keyword": args[0], "begin": begin, "size": size}, false, "")
		},
	}
	search.Flags().IntVar(&begin, "begin", 0, "pagination offset")
	search.Flags().IntVar(&size, "size", 5, "number of accounts")
	account.AddCommand(search)
	account.AddCommand(a.aliasCommand("from-url <url>", "Resolve account information from an article URL", "get_account_by_url", "url", "account from-url requires <url>"))
	account.AddCommand(a.aliasCommand("details <fakeid>", "Get account details", "get_account_details", "fakeid", "account details requires <fakeid>"))
	account.AddCommand(a.aliasCommand("author <fakeid>", "Get account author information", "get_author_info", "fakeid", "account author requires <fakeid>"))
	account.AddCommand(a.aliasCommand("name <url>", "Get account name from an article URL", "get_account_name", "url", "account name requires <url>"))
	return account
}

func (a *App) legacyAlbumCommand() *cobra.Command {
	album := &cobra.Command{Use: "album", Short: "List articles in a WeChat album"}
	var count int
	var beginMsgID, beginItemIndex string
	list := &cobra.Command{
		Use:   "list <fakeid> <albumId>",
		Short: "List album articles",
		Args:  exactArgs(2, "album list requires <fakeid> <albumId>"),
		RunE: func(command *cobra.Command, args []string) error {
			if count < 0 {
				return usage("--count must be a non-negative integer")
			}
			arguments := map[string]any{"fakeid": args[0], "album_id": args[1], "count": count}
			if beginMsgID != "" {
				arguments["begin_msgid"] = beginMsgID
			}
			if beginItemIndex != "" {
				arguments["begin_itemidx"] = beginItemIndex
			}
			return a.executeTool(command.Context(), "list_album", arguments, false, "")
		},
	}
	list.Flags().IntVar(&count, "count", 10, "number of articles")
	list.Flags().StringVar(&beginMsgID, "begin-msgid", "", "pagination message ID")
	list.Flags().StringVar(&beginItemIndex, "begin-itemidx", "", "pagination item index")
	album.AddCommand(list)
	return album
}

func (a *App) aliasCommand(use, short, tool, key, message string) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, message),
		RunE: func(command *cobra.Command, args []string) error {
			return a.executeTool(command.Context(), tool, map[string]any{key: args[0]}, false, "")
		},
	}
}

func (a *App) executeTool(ctx context.Context, toolName string, arguments map[string]any, dryRun bool, confirmation string) error {
	if dryRun {
		preview := safety.DryRun(toolName, arguments)
		preview["requiredConfirmation"] = safety.RequiredConfirmation(&mcp.Tool{Name: toolName, InputSchema: map[string]any{"type": "object"}})
		return a.output(preview)
	}
	spinner := a.spinner("Connecting to legacy remote MCP server…")
	result, err := a.legacy.InvokeTool(ctx, a.server, toolName, arguments, func(tool *mcp.Tool) error {
		return safety.AssertConfirmation(tool, confirmation)
	})
	if err != nil {
		spinner.Stop("Legacy remote tool call failed", false)
		return err
	}
	spinner.Stop("Remote tool call completed", !result.IsError)
	if err := a.output(result); err != nil {
		return err
	}
	if result.IsError {
		return errors.New("legacy remote MCP tool returned an error")
	}
	return nil
}

func (a *App) login(ctx context.Context, headless, noOpen bool) error {
	server, err := a.legacy.ResolveServer(a.server)
	if err != nil {
		return usage(err.Error())
	}
	callback, err := newCallbackServer(5 * time.Minute)
	if err != nil {
		return err
	}
	defer callback.Close()

	spinner := a.spinner("Waiting for legacy OAuth authorization…")
	toolCount, err := a.legacy.Login(ctx, legacyremote.LoginOptions{
		Server: a.server, RedirectURL: callback.RedirectURL,
		FetchCode: func(flowContext context.Context, rawURL string) (string, string, error) {
			authorizationURL, parseErr := url.Parse(rawURL)
			if parseErr != nil {
				return "", "", parseErr
			}
			if headless {
				query := authorizationURL.Query()
				query.Set("headless", "1")
				authorizationURL.RawQuery = query.Encode()
			}
			fmt.Fprintf(a.stderr, "Open this authorization URL:\n%s\n", authorizationURL.String())
			if !noOpen && !headless {
				if err := a.openBrowser(flowContext, authorizationURL.String()); err != nil {
					return "", "", err
				}
			}
			result, waitErr := callback.Wait(flowContext, authorizationURL.Query().Get("state"))
			if waitErr != nil {
				return "", "", waitErr
			}
			return result.Code, result.State, nil
		},
	})
	if err != nil {
		spinner.Stop("Legacy OAuth authorization failed", false)
		return err
	}
	spinner.Stop("Legacy OAuth authorization completed", true)
	return a.output(map[string]any{"success": true, "data": map[string]any{"server": server, "authenticated": true, "toolCount": toolCount, "legacy": true}})
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

func (a *App) printLegacyStatus() error {
	status, err := a.legacy.Status(a.server)
	if err != nil {
		return usage(err.Error())
	}
	data := map[string]any{
		"server": status.Server, "authenticated": status.Authenticated,
		"refreshable": status.Refreshable, "configPath": status.ConfigPath, "legacy": true,
	}
	if a.jsonOut || !tui.IsInteractive(a.stdin, a.stdout) {
		return a.output(map[string]any{"success": true, "data": data})
	}
	detail := fmt.Sprintf("legacy server: %s\nconfig: %s", status.Server, status.ConfigPath)
	tui.RenderStatus(a.stdout, tui.Status{Label: statusLabel(status.Authenticated), Detail: detail, Success: status.Authenticated})
	return nil
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

func renderToolResult(output io.Writer, result *mcp.CallToolResult) bool {
	if result == nil || len(result.Content) == 0 {
		return false
	}
	texts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			return false
		}
		texts = append(texts, safety.RedactText(text.Text))
	}
	for _, text := range texts {
		fmt.Fprintln(output, text)
	}
	return true
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
