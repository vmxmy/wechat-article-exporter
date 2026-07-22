package app

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

func (a *App) credentialCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "credential", Short: "Import and manage account-scoped WeChat credentials",
		Example: `  wechat-article credential import --file credential.json
  wechat-article credential import --env
  wechat-article credential status --json
  wechat-article credential remove CREDENTIAL_ID --confirm remove-credential:CREDENTIAL_ID`,
	}
	var filePath string
	var fromEnvironment bool
	importCommand := &cobra.Command{
		Use: "import", Short: "Import one credential from JSON, stdin, or environment", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if fromEnvironment && filePath != "" {
				return usage("credential import accepts exactly one of --file or --env")
			}
			var record credentials.Record
			var err error
			if fromEnvironment {
				record, err = credentials.ParseEnvironment(os.Getenv)
			} else {
				record, err = credentialRecordFromInput(filePath, a.stdin)
			}
			if err != nil {
				return usage(err.Error())
			}
			service, err := a.credentialService()
			if err != nil {
				return err
			}
			metadata, err := service.Import(command.Context(), record)
			if err != nil {
				return err
			}
			return a.output(metadata)
		},
	}
	importCommand.Flags().StringVarP(&filePath, "file", "f", "", "credential JSON file; default stdin")
	importCommand.Flags().BoolVar(&fromEnvironment, "env", false, "read WECHAT_ARTICLE_* credential variables")

	status := &cobra.Command{
		Use: "status", Short: "List non-secret credential metadata", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service, err := a.credentialService()
			if err != nil {
				return err
			}
			items, err := service.Status(command.Context())
			if err != nil {
				return err
			}
			return a.output(map[string]any{"credentials": items, "count": len(items)})
		},
	}

	validate := &cobra.Command{
		Use: "validate <id>", Short: "Validate a stored credential when a validator is available", Args: exactArgs(1, "credential validate requires <id>"),
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("credential validation: %w; import and status are available locally", application.ErrUnavailable)
		},
	}

	var confirmation string
	remove := &cobra.Command{
		Use: "remove <id>", Short: "Remove credential metadata and secret bytes", Args: exactArgs(1, "credential remove requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			required := "remove-credential:" + args[0]
			if confirmation != required {
				return usage("credential removal requires --confirm " + required)
			}
			service, err := a.credentialService()
			if err != nil {
				return err
			}
			if err := service.Remove(command.Context(), args[0]); err != nil {
				return err
			}
			return a.output(map[string]any{"removed": args[0]})
		},
	}
	remove.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation value")

	command.AddCommand(importCommand, status, validate, remove)
	return command
}

func (a *App) credentialService() (*credentials.Service, error) {
	if a.active == nil || a.active.Library == nil || a.secret == nil {
		return nil, errors.New("active profile credential storage is unavailable")
	}
	return credentials.NewService(credentials.ServiceOptions{
		Profile: string(a.active.Profile.ID), Repository: a.active.Library, Accounts: a.active.Library,
		Secrets: a.secret,
	}), nil
}

func credentialRecordFromInput(path string, stdin interface{ Read([]byte) (int, error) }) (credentials.Record, error) {
	if path == "" || path == "-" {
		return credentials.ParseStdin(stdin)
	}
	file, err := os.Open(path)
	if err != nil {
		return credentials.Record{}, err
	}
	defer file.Close()
	return credentials.ParseJSON(file)
}

func (a *App) databaseCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "db", Short: "Inspect, back up, restore, verify, and clean local storage",
		Example: `  wechat-article db status --json
  wechat-article db backup --output library-backup.zip
  wechat-article db verify library-backup.zip
  wechat-article db gc
  wechat-article db gc --confirm garbage-collect:...`,
	}
	status := &cobra.Command{
		Use: "status", Short: "Show active profile storage counts and paths", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			status, err := a.core.StorageStatus(command.Context())
			if err != nil {
				return err
			}
			data := map[string]any{"storage": status}
			if a.active != nil {
				data["profile"] = a.active.Profile.ID
				data["databasePath"] = a.active.Library.Path()
				data["objectsPath"] = a.active.Objects.Root()
			}
			return a.output(data)
		},
	}

	var backupOutput string
	backup := &cobra.Command{
		Use: "backup", Short: "Create and independently verify a local backup archive", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(backupOutput) == "" {
				return usage("db backup requires --output")
			}
			active, err := a.requireActiveStorage()
			if err != nil {
				return err
			}
			manifest, err := active.Library.CreateBackup(command.Context(), library.BackupOptions{
				Destination: backupOutput, ObjectStore: active.Objects, ConfigPath: active.Profile.Paths.Config,
			})
			if err != nil {
				return err
			}
			verification, err := library.VerifyBackup(command.Context(), backupOutput)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"path": backupOutput, "manifest": manifest, "verification": verification})
		},
	}
	backup.Flags().StringVarP(&backupOutput, "output", "o", "", "destination .zip archive")

	verify := &cobra.Command{
		Use: "verify <archive>", Short: "Verify backup checksums and database/object consistency", Args: exactArgs(1, "db verify requires <archive>"),
		RunE: func(command *cobra.Command, args []string) error {
			verification, err := library.VerifyBackup(command.Context(), args[0])
			if err != nil {
				return err
			}
			if !verification.Valid {
				return fmt.Errorf("backup verification failed: %s", strings.Join(verification.Failures, "; "))
			}
			return a.output(verification)
		},
	}

	integrity := &cobra.Command{
		Use: "integrity", Short: "Check SQLite and object referential integrity", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			active, err := a.requireActiveStorage()
			if err != nil {
				return err
			}
			report, err := active.Library.CheckIntegrity(command.Context(), active.Objects)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"valid": len(report.Issues) == 0, "report": report})
		},
	}

	var restorePolicy, restoreConfirmation string
	restore := &cobra.Command{
		Use: "restore <archive>", Short: "Validate, stage, and transactionally restore an active profile backup", Args: exactArgs(1, "db restore requires <archive>"),
		RunE: func(command *cobra.Command, args []string) error {
			required := "restore-backup:" + args[0]
			if restoreConfirmation != required {
				return usage("restore replaces active profile storage after staging and validation; use --confirm " + required)
			}
			if restorePolicy != string(library.RestoreRefuseConflicts) && restorePolicy != string(library.RestoreRenameConflicts) {
				return usage("--conflict must be refuse or rename")
			}
			active, err := a.requireActiveStorage()
			if err != nil {
				return err
			}
			// Close the live SQLite handle immediately before the restore commit.
			// A fresh runtime is rebuilt after a successful commit.
			if err := active.Library.Close(); err != nil {
				return err
			}
			report, restoreErr := library.RestoreBackup(command.Context(), library.RestoreOptions{
				ArchivePath: args[0], DatabasePath: active.Profile.Paths.Database, ObjectStore: active.Objects,
				ConfigPath: active.Profile.Paths.Config, ConflictPolicy: library.RestoreConflictPolicy(restorePolicy),
			})
			prepared, rebuildErr := a.prepareProfile(command.Context(), active.Profile)
			if rebuildErr == nil {
				rebuildErr = a.commitProfile(prepared)
			}
			if restoreErr != nil {
				if rebuildErr != nil {
					return errors.Join(restoreErr, fmt.Errorf("reopen active profile after restore failure: %w", rebuildErr))
				}
				return restoreErr
			}
			if rebuildErr != nil {
				return fmt.Errorf("restore committed but active runtime could not be reopened: %w", rebuildErr)
			}
			return a.output(report)
		},
	}
	restore.Flags().StringVar(&restorePolicy, "conflict", string(library.RestoreRefuseConflicts), "profile conflict policy: refuse or rename")
	restore.Flags().StringVar(&restoreConfirmation, "confirm", "", "exact confirmation value")

	var objectRetention, temporaryRetention, debugRetention, jobLogRetention time.Duration
	var gcConfirmation string
	gc := &cobra.Command{
		Use: "gc", Short: "Plan or apply safe garbage collection", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			for name, value := range map[string]time.Duration{
				"object-retention": objectRetention, "temporary-retention": temporaryRetention,
				"debug-retention": debugRetention, "job-log-retention": jobLogRetention,
			} {
				if value < 0 {
					return usage("--" + name + " must be non-negative")
				}
			}
			active, err := a.requireActiveStorage()
			if err != nil {
				return err
			}
			options := library.GarbageCollectionOptions{
				ObjectStore: active.Objects, ObjectRetention: objectRetention, TemporaryRetention: temporaryRetention,
				DebugRetention: debugRetention, CompletedJobRetention: jobLogRetention,
			}
			plan, err := active.Library.PlanGarbageCollection(command.Context(), options)
			if err != nil {
				return err
			}
			if gcConfirmation == "" {
				return a.output(map[string]any{"dryRun": true, "plan": plan})
			}
			if gcConfirmation != plan.Confirmation {
				return usage("garbage collection confirmation mismatch; rerun the dry run and use --confirm " + plan.Confirmation)
			}
			result, err := active.Library.ApplyGarbageCollection(command.Context(), options, plan, gcConfirmation)
			if err != nil {
				return err
			}
			return a.output(map[string]any{"dryRun": false, "plan": plan, "result": result})
		},
	}
	gc.Flags().DurationVar(&objectRetention, "object-retention", 24*time.Hour, "minimum age for unreferenced objects")
	gc.Flags().DurationVar(&temporaryRetention, "temporary-retention", 24*time.Hour, "minimum age for temporary files")
	gc.Flags().DurationVar(&debugRetention, "debug-retention", 30*24*time.Hour, "retention for expired debug captures")
	gc.Flags().DurationVar(&jobLogRetention, "job-log-retention", 30*24*time.Hour, "retention for completed-job logs")
	gc.Flags().StringVar(&gcConfirmation, "confirm", "", "exact confirmation from the dry-run plan")

	command.AddCommand(status, backup, verify, integrity, restore, gc)
	return command
}

func (a *App) requireActiveStorage() (*ProfileRuntime, error) {
	if a.active == nil || a.active.Library == nil || a.active.Objects == nil {
		return nil, errors.New("active profile storage is unavailable")
	}
	return a.active, nil
}

func (a *App) diagnosticsCommand() *cobra.Command {
	command := &cobra.Command{Use: "diagnostics", Short: "Inspect redacted runtime dependencies and migration state"}
	status := &cobra.Command{
		Use: "status", Short: "Collect redacted runtime, session, storage, proxy, browser, and recent-job diagnostics", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			runtimeStatus, err := a.core.RuntimeStatus(command.Context())
			if err != nil {
				return err
			}
			session, err := a.core.SessionStatus(command.Context())
			if err != nil {
				return err
			}
			jobs, jobsErr := a.core.QueryJobs(command.Context(), domain.JobQuery{Limit: 10})
			routes := any([]any{})
			if manager, managerErr := a.proxyManager(); managerErr == nil {
				if listed, listErr := manager.List(command.Context()); listErr == nil {
					routes = listed
				}
			}
			browser, browserErr := a.core.DiscoverBrowser(command.Context())
			data := map[string]any{
				"runtime": runtimeStatus, "session": session, "jobs": jobs, "proxies": routes,
				"browser": browser, "retirement": retirementState(),
				"system": map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH, "goVersion": runtime.Version()},
			}
			if jobsErr != nil {
				data["jobsError"] = jobsErr.Error()
			}
			if browserErr != nil {
				data["browserError"] = browserErr.Error()
			}
			return a.output(data)
		},
	}
	command.AddCommand(status)
	return command
}

func retirementState() map[string]any {
	return map[string]any{
		"phase":             "retired",
		"webRetained":       false,
		"remoteMCPRetained": false,
		"retirementBlocked": false,
		"remoteOAuth":       false,
		"message":           "Project-operated Web, remote MCP, and remote OAuth services are retired. Use local profiles and QR login.",
	}
}

func (a *App) completionCommand(root *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use: "completion [bash|zsh|fish|powershell]", Short: "Generate a shell completion script", Args: cobra.ExactArgs(1),
		Long:                  "Generate a completion script. This command writes shell code directly to stdout and intentionally does not use the JSON envelope.",
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(a.stdout)
			case "zsh":
				return root.GenZshCompletion(a.stdout)
			case "fish":
				return root.GenFishCompletion(a.stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(a.stdout)
			default:
				return usage("completion shell must be bash, zsh, fish, or powershell")
			}
		},
	}
	return command
}
