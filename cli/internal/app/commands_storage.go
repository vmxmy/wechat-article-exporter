package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
	"golang.org/x/term"
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
	var interactive bool
	importCommand := &cobra.Command{
		Use: "import", Short: "Import one credential from JSON, stdin, environment, or secure prompts", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			selectedInputs := 0
			if fromEnvironment {
				selectedInputs++
			}
			if strings.TrimSpace(filePath) != "" {
				selectedInputs++
			}
			if interactive {
				selectedInputs++
			}
			if selectedInputs > 1 {
				return usage("credential import accepts exactly one of --file, --env, or --interactive")
			}
			var record credentials.Record
			var err error
			if fromEnvironment {
				record, err = credentials.ParseEnvironment(os.Getenv)
			} else if interactive {
				record, err = credentialRecordFromTerminal(a.stdin, a.stderr)
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
	importCommand.Flags().BoolVar(&interactive, "interactive", false, "prompt for credential fields without echoing secret values")

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
		RunE: func(command *cobra.Command, args []string) error {
			service, err := a.credentialService()
			if err != nil {
				return err
			}
			metadata, err := service.Validate(command.Context(), args[0])
			if err != nil {
				return err
			}
			return a.output(metadata)
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
	if a.active.Credentials != nil {
		return a.active.Credentials, nil
	}
	return credentials.NewService(credentials.ServiceOptions{Profile: string(a.active.Profile.ID), Repository: a.active.Library,
		Accounts: a.active.Library, Secrets: a.secret}), nil
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

func credentialRecordFromTerminal(stdin io.Reader, stderr io.Writer) (credentials.Record, error) {
	file, ok := stdin.(interface{ Fd() uintptr })
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return credentials.Record{}, errors.New("interactive credential import requires a terminal")
	}
	readPublic := func(label string) (string, error) {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "%s: ", label)
		}
		value, err := readTerminalLine(stdin)
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	readSecret := func(label string) (string, error) {
		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "%s: ", label)
		}
		value, err := term.ReadPassword(int(file.Fd()))
		if stderr != nil {
			_, _ = fmt.Fprintln(stderr)
		}
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(value)), nil
	}

	var input credentials.InteractiveInput
	var err error
	if input.Nickname, err = readPublic("Nickname (optional)"); err != nil {
		return credentials.Record{}, fmt.Errorf("read credential nickname: %w", err)
	}
	if input.Biz, err = readPublic("Account biz/fakeid"); err != nil {
		return credentials.Record{}, fmt.Errorf("read credential biz: %w", err)
	}
	secretFields := []struct {
		label  string
		target *string
	}{
		{label: "UIN", target: &input.UIN},
		{label: "Key", target: &input.Key},
		{label: "Pass ticket", target: &input.PassTicket},
		{label: "Wap SID2", target: &input.WapSID2},
		{label: "App message token", target: &input.AppMsgToken},
		{label: "Cookie (optional)", target: &input.Cookie},
	}
	for _, field := range secretFields {
		if *field.target, err = readSecret(field.label); err != nil {
			return credentials.Record{}, fmt.Errorf("read credential %s: %w", strings.ToLower(field.label), err)
		}
	}
	return credentials.ParseInteractive(input)
}

func readTerminalLine(reader io.Reader) (string, error) {
	var value strings.Builder
	buffer := []byte{0}
	for value.Len() <= 64<<10 {
		count, err := reader.Read(buffer)
		if count == 1 {
			if buffer[0] == '\n' {
				return value.String(), nil
			}
			if buffer[0] != '\r' {
				value.WriteByte(buffer[0])
			}
		}
		if err != nil {
			return value.String(), err
		}
	}
	return "", errors.New("terminal input exceeds 65536 bytes")
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
			report, err := a.restoreActiveProfile(command.Context(), args[0], library.RestoreConflictPolicy(restorePolicy))
			if err != nil {
				return err
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
			gate, err := profiles.AcquireMaintenanceGate(command.Context(), active.Profile.Paths)
			if err != nil {
				return err
			}
			defer gate.Close()
			blockers, err := active.Jobs.RestoreBlockers(command.Context())
			if err != nil {
				return fmt.Errorf("check garbage-collection blockers: %w", err)
			}
			if len(blockers) > 0 {
				return fmt.Errorf("garbage collection blocked by %d running job or active lease: %w", len(blockers), profiles.ErrProfileBusy)
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
	var bundleOutput string
	bundle := &cobra.Command{
		Use: "bundle", Short: "Create a private diagnostic ZIP without article bodies or secret bytes", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.TrimSpace(bundleOutput) == "" {
				return usage("diagnostics bundle requires --output")
			}
			report, err := a.createDiagnosticBundle(command.Context(), bundleOutput)
			if err != nil {
				return err
			}
			return a.output(report)
		},
	}
	bundle.Flags().StringVarP(&bundleOutput, "output", "o", "", "destination .zip archive")
	command.AddCommand(status, bundle)
	return command
}

type diagnosticBundleReport struct {
	Path     string   `json:"path"`
	SHA256   string   `json:"sha256"`
	Bytes    int64    `json:"bytes"`
	Included []string `json:"included"`
	Omitted  []string `json:"omitted"`
}

type diagnosticJobRecord struct {
	Job       diagnosticJob      `json:"job"`
	Logs      []diagnosticJobLog `json:"logs"`
	LogsError string             `json:"logsError,omitempty"`
}

type diagnosticJob struct {
	ID        domain.JobID     `json:"id"`
	Kind      string           `json:"kind"`
	State     domain.JobState  `json:"state"`
	Profile   domain.ProfileID `json:"profile"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Counts    map[string]int   `json:"counts,omitempty"`
}

type diagnosticJobLog struct {
	ID        int64          `json:"id"`
	ItemID    string         `json:"itemId,omitempty"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

const (
	diagnosticLogEntryBudget = 32 << 10
	diagnosticLogTotalBudget = 2 << 20
	diagnosticErrorBudget    = 8 << 10
)

func (a *App) createDiagnosticBundle(ctx context.Context, path string) (diagnosticBundleReport, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return diagnosticBundleReport{}, errors.New("diagnostic bundle output path is required")
	}
	active, err := a.requireActiveStorage()
	if err != nil {
		return diagnosticBundleReport{}, err
	}
	var runtimeStatus domain.RuntimeStatus
	runtimeStatus, runtimeErr := a.core.RuntimeStatus(ctx)
	var session wechat.Session
	session, sessionErr := a.core.SessionStatus(ctx)
	configuration := profiles.DefaultConfig(string(active.Profile.ID))
	migrationBackup := ""
	configurationValue, backupValue, configurationErr := profiles.NewConfigStore(active.Profile.Paths.Config).Read()
	if configurationErr == nil {
		configuration, migrationBackup = configurationValue, backupValue
	}
	integrity, integrityErr := active.Library.CheckIntegrityWithOptions(ctx, active.Objects, library.IntegrityOptions{})
	browser, browserErr := a.core.DiscoverBrowser(ctx)
	var jobDiagnostics any
	jobsPage, jobsErr := a.core.QueryJobs(ctx, domain.JobQuery{Limit: 50})
	if jobsErr != nil {
		jobDiagnostics = map[string]any{"jobsError": boundedDiagnosticError(jobsErr)}
	} else {
		records := make([]diagnosticJobRecord, 0, len(jobsPage.Items))
		remainingLogBytes := diagnosticLogTotalBudget
		for _, job := range jobsPage.Items {
			var logs []diagnosticJobLog
			var logsErr error
			if remainingLogBytes > 0 {
				storedLogs, err := active.Jobs.ListLogsBounded(ctx, job.ID, library.JobLogBudget{
					MaximumRows: 100, MaximumRawBytes: remainingLogBytes, MaximumEntryBytes: diagnosticLogEntryBudget,
				})
				logs, logsErr = boundedDiagnosticLogs(storedLogs, &remainingLogBytes), err
			}
			record := diagnosticJobRecord{Job: safeDiagnosticJob(job), Logs: logs}
			if logsErr != nil {
				record.LogsError = boundedDiagnosticError(logsErr)
			}
			records = append(records, record)
		}
		jobDiagnostics = map[string]any{
			"total": jobsPage.Total, "offset": jobsPage.Offset, "limit": jobsPage.Limit, "jobs": records,
		}
	}
	system := map[string]any{
		"goos": runtime.GOOS, "goarch": runtime.GOARCH, "goVersion": runtime.Version(),
		"runtime": safeDiagnosticRuntime(runtimeStatus), "session": safeDiagnosticSession(session),
		"browser": map[string]any{"configured": browser.Path != "", "version": browser.Version}, "retirement": retirementState(),
	}
	addDiagnosticError(system, "runtimeError", runtimeErr)
	addDiagnosticError(system, "sessionError", sessionErr)
	addDiagnosticError(system, "browserError", browserErr)
	configurationSection := map[string]any{"profile": safeDiagnosticConfiguration(configuration),
		"migrationBackupPresent": migrationBackup != ""}
	addDiagnosticError(configurationSection, "error", configurationErr)
	integritySection := any(integrity)
	if integrityErr != nil {
		integritySection = map[string]any{"report": integrity, "error": boundedDiagnosticError(integrityErr)}
	}
	data, err := safety.AssembleDiagnosticBundle(safety.DiagnosticBundleInput{
		System:        system,
		Configuration: configurationSection,
		SchemaVersion: map[string]any{"database": library.CurrentSchemaVersion, "configuration": profiles.CurrentConfigVersion},
		Logs:          jobDiagnostics, Integrity: integritySection,
	}, safety.DiagnosticBundleOptions{})
	if err != nil {
		return diagnosticBundleReport{}, err
	}
	checksum, size, err := writeDiagnosticBundle(path, data)
	if err != nil {
		return diagnosticBundleReport{}, err
	}
	return diagnosticBundleReport{
		Path: path, SHA256: checksum, Bytes: size,
		Included: []string{"system metadata", "redacted configuration", "schema versions", "recent job metadata and logs", "integrity report"},
		Omitted:  []string{"article bodies", "WeChat sessions", "credential bytes", "proxy authorization", "encrypted vault"},
	}, nil
}

func addDiagnosticError(section map[string]any, name string, err error) {
	if err != nil {
		section[name] = boundedDiagnosticError(err)
	}
}

func boundedDiagnosticError(err error) string {
	if err == nil {
		return ""
	}
	return truncateDiagnosticText(err.Error(), diagnosticErrorBudget)
}

func boundedDiagnosticLogs(logs []library.JobLog, remaining *int) []diagnosticJobLog {
	if remaining == nil || *remaining <= 0 {
		return []diagnosticJobLog{}
	}
	result := make([]diagnosticJobLog, 0, len(logs))
	for _, log := range logs {
		if *remaining <= 0 {
			break
		}
		entry := diagnosticJobLog{ID: log.ID, ItemID: log.ItemID, Level: log.Level,
			Message: truncateDiagnosticText(log.Message, diagnosticLogEntryBudget/4),
			Fields:  safeDiagnosticLogFields(log.Fields), CreatedAt: log.CreatedAt}
		encoded, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		if len(encoded) > *remaining {
			break
		}
		*remaining -= len(encoded)
		result = append(result, entry)
	}
	return result
}

func safeDiagnosticJob(job domain.Job) diagnosticJob {
	return diagnosticJob{ID: job.ID, Kind: job.Kind, State: job.State, Profile: job.Profile,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Counts: job.Counts}
}

func safeDiagnosticRuntime(status domain.RuntimeStatus) map[string]any {
	return map[string]any{"version": status.Version, "profile": status.Profile, "portable": status.Portable,
		"offlineReady": status.OfflineReady, "secretBackend": status.SecretBackend, "storage": status.Storage,
		"checkedAt": status.CheckedAt}
}

func safeDiagnosticSession(session wechat.Session) map[string]any {
	return map[string]any{"state": session.State, "accountId": session.AccountID, "createdAt": session.CreatedAt,
		"expiresAt": session.ExpiresAt, "lastValidatedAt": session.LastValidatedAt, "validation": session.Validation}
}

func safeDiagnosticConfiguration(configuration profiles.ProfileConfig) map[string]any {
	return map[string]any{"schemaVersion": configuration.SchemaVersion, "profileId": configuration.ProfileID,
		"preferences": map[string]any{
			"sync": configuration.Preferences.Sync, "download": configuration.Preferences.Download,
			"export": map[string]any{
				"rootConfigured":   strings.TrimSpace(configuration.Preferences.Export.Root) != "",
				"namingTemplate":   configuration.Preferences.Export.NamingTemplate,
				"maximumNameBytes": configuration.Preferences.Export.MaximumNameBytes,
				"collisionPolicy":  configuration.Preferences.Export.CollisionPolicy,
			},
			"display": configuration.Preferences.Display, "proxy": configuration.Preferences.Proxy,
		},
		"mcp": map[string]any{"readOnly": configuration.MCP.ReadOnly, "allow": configuration.MCP.Allow,
			"deny": configuration.MCP.Deny, "allowedOutputRootCount": len(configuration.MCP.AllowedOutputRoots)},
	}
}

func safeDiagnosticLogFields(fields map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"attempt": {}, "duration": {}, "failureClass": {}, "kind": {}, "ownerPrefix": {}, "requestId": {},
		"retryDelay": {}, "routeId": {}, "state": {}, "status": {}, "truncated": {}, "bytes": {},
	}
	result := map[string]any{}
	for key, value := range fields {
		if _, ok := allowed[key]; ok {
			result[key] = value
		}
	}
	return result
}

func truncateDiagnosticText(value string, maximum int) string {
	if maximum <= 0 || len(value) <= maximum {
		return value
	}
	return value[:maximum] + "...[truncated]"
}

func writeDiagnosticBundle(path string, data map[string]any) (string, int64, error) {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", 0, fmt.Errorf("encode diagnostic bundle: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", 0, fmt.Errorf("create diagnostic destination: %w", err)
	}
	file, err := createPrivateTemp(filepath.Dir(path), ".diagnostics-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("create diagnostic bundle: %w", err)
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "diagnostics.json", Method: zip.Deflate}
	header.SetMode(0o600)
	part, err := writer.CreateHeader(header)
	if err != nil {
		return "", 0, fmt.Errorf("create diagnostic entry: %w", err)
	}
	if _, err := part.Write(encoded); err != nil {
		return "", 0, fmt.Errorf("write diagnostic entry: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", 0, fmt.Errorf("close diagnostic archive: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync diagnostic archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", 0, err
	}
	temporaryFile, err := os.Open(temporary)
	if err != nil {
		return "", 0, err
	}
	hasher := sha256.New()
	size, copyErr := io.Copy(hasher, temporaryFile)
	closeErr := temporaryFile.Close()
	if copyErr != nil || closeErr != nil {
		return "", 0, errors.Join(copyErr, closeErr)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if err := commitFileNoReplace(temporary, path); err != nil {
		var committedErr *fileCommitError
		if errors.As(err, &committedErr) && committedErr.Published {
			committed = true
		}
		if errors.Is(err, os.ErrExist) {
			return "", 0, fmt.Errorf("diagnostic destination already exists: %s", path)
		}
		return "", 0, fmt.Errorf("commit diagnostic bundle: %w", err)
	}
	committed = true
	return checksum, size, nil
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
