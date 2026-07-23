package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	syncrunner "github.com/wechat-article/wechat-article-exporter/cli/internal/sync"
)

type asyncOptions struct {
	wait         bool
	follow       bool
	pollInterval time.Duration
}

func (options *asyncOptions) addFlags(command *cobra.Command) {
	command.Flags().BoolVar(&options.wait, "wait", false, "wait until the persistent job reaches a terminal state")
	command.Flags().BoolVar(&options.follow, "follow", false, "stream state changes to stderr and wait for completion")
	command.Flags().DurationVar(&options.pollInterval, "poll-interval", time.Second, "job polling interval between 100ms and 30s")
}

func (options asyncOptions) validate() error {
	if options.pollInterval < 100*time.Millisecond || options.pollInterval > 30*time.Second {
		return usage("--poll-interval must be between 100ms and 30s")
	}
	return nil
}

func (a *App) syncCommand() *cobra.Command {
	command := &cobra.Command{Use: "sync", Short: "Start persistent synchronization jobs"}
	var incremental bool
	var rangeValue, notBefore string
	var pageSize int
	var pageDelay, jitter time.Duration
	var persistPacing bool
	var confirmation string
	var async asyncOptions
	account := &cobra.Command{
		Use: "account <id> [id...]", Short: "Synchronize one or many saved accounts in one persistent job", Args: cobra.MinimumNArgs(1),
		Example: `  wechat-article sync account account-id --range 7d --follow
	  wechat-article sync account account-a account-b --range all --wait
  wechat-article sync account account-id --not-before 2026-01-01 --wait --json`,
		RunE: func(command *cobra.Command, args []string) error {
			if err := async.validate(); err != nil {
				return err
			}
			if pageSize < 1 || pageSize > 50 {
				return usage("--page-size must be between 1 and 50")
			}
			if pageDelay < 0 || jitter < 0 {
				return usage("--page-delay and --jitter must be non-negative")
			}
			rangeOption, boundary, err := parseSyncBoundary(rangeValue, notBefore)
			if err != nil {
				return err
			}
			unsafe := persistPacing && pageDelay < syncrunner.RecommendedMinimumDelay
			required := "unsafe-sync-pacing:" + strings.Join(args, ",")
			if unsafe && confirmation != required {
				return usage("persistent sync pacing below 3s may increase account risk; use --confirm " + required)
			}
			accountIDs := make([]domain.AccountID, len(args))
			for index, value := range args {
				if strings.TrimSpace(value) == "" {
					return usage("account IDs must not be empty")
				}
				accountIDs[index] = domain.AccountID(value)
			}
			job, err := a.core.SynchronizeAccount(command.Context(), domain.SynchronizeAccountRequest{
				AccountIDs: accountIDs, Incremental: incremental, Range: rangeOption, NotBefore: boundary,
				PageSize: pageSize, PageDelay: pageDelay, Jitter: jitter, PersistentPacing: persistPacing,
				ConfirmUnsafePacing: unsafe,
			})
			if err != nil {
				return err
			}
			if persistPacing {
				if a.active == nil {
					return errors.New("active profile runtime is unavailable")
				}
				_, err := profiles.NewConfigStore(a.active.Profile.Paths.Config).Update(func(configuration *profiles.ProfileConfig) error {
					configuration.Preferences.Sync.Range = string(rangeOption)
					configuration.Preferences.Sync.DatePoint = boundary
					configuration.Preferences.Sync.PageDelay = pageDelay
					configuration.Preferences.Sync.Jitter = jitter
					configuration.Preferences.Sync.PageSize = pageSize
					configuration.Preferences.Sync.Incremental = incremental
					configuration.Preferences.Sync.UnsafePacingSaved = unsafe
					return nil
				})
				if err != nil {
					return fmt.Errorf("persist synchronization preferences after queuing job %s: %w", job.ID, err)
				}
			}
			return a.outputJob(command.Context(), job, async)
		},
	}
	account.Flags().BoolVar(&incremental, "incremental", true, "refresh new and changed article-list records")
	account.Flags().StringVar(&rangeValue, "range", "", "optional boundary: 24h, 1d, 3d, 7d, 1m, 3m, 6m, 1y, all, or point; default uses incremental history")
	account.Flags().StringVar(&notBefore, "not-before", "", "RFC3339 timestamp or YYYY-MM-DD boundary; implies point range")
	account.Flags().IntVar(&pageSize, "page-size", 20, "upstream page size between 1 and 50")
	account.Flags().DurationVar(&pageDelay, "page-delay", 5*time.Second, "delay between upstream pages")
	account.Flags().DurationVar(&jitter, "jitter", 500*time.Millisecond, "maximum additional pacing jitter")
	account.Flags().BoolVar(&persistPacing, "persist-pacing", false, "persist these pacing values for later runs")
	account.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation for unsafe persistent pacing")
	async.addFlags(account)
	command.AddCommand(account)
	return command
}

func parseSyncBoundary(rangeValue, notBefore string) (domain.SyncRange, time.Time, error) {
	rangeOption := domain.SyncRange(strings.TrimSpace(rangeValue))
	if rangeOption == "" {
		if strings.TrimSpace(notBefore) == "" {
			return "", time.Time{}, nil
		}
		rangeOption = domain.SyncRangePoint
	}
	allowed := map[domain.SyncRange]struct{}{
		domain.SyncRange24Hours: {}, domain.SyncRange1Day: {}, domain.SyncRange3Days: {}, domain.SyncRange7Days: {},
		domain.SyncRange1Month: {}, domain.SyncRange3Months: {}, domain.SyncRange6Months: {}, domain.SyncRange1Year: {},
		domain.SyncRangeAll: {}, domain.SyncRangePoint: {},
	}
	if _, ok := allowed[rangeOption]; !ok {
		return "", time.Time{}, usage("--range must be one of 24h, 1d, 3d, 7d, 1m, 3m, 6m, 1y, all, or point")
	}
	boundary, err := parseOptionalTime(notBefore)
	if err != nil {
		return "", time.Time{}, usage("--not-before: " + err.Error())
	}
	if !boundary.IsZero() {
		rangeOption = domain.SyncRangePoint
	}
	if rangeOption == domain.SyncRangePoint && boundary.IsZero() {
		return "", time.Time{}, usage("--range point requires --not-before")
	}
	return rangeOption, boundary, nil
}

func (a *App) downloadCommand() *cobra.Command {
	command := &cobra.Command{Use: "download", Short: "Start persistent local download jobs"}
	command.AddCommand(a.downloadStartCommand("article", "Download and process article HTML", "article"))
	command.AddCommand(a.downloadStartCommand("resources", "Download missing article resources", "resources"))
	return command
}

func (a *App) metadataCommand() *cobra.Command {
	command := &cobra.Command{Use: "metadata", Short: "Manage engagement metadata downloads"}
	command.AddCommand(a.downloadStartCommand("download", "Queue metadata downloads for selected articles", "metadata"))
	return command
}

func (a *App) commentsCommand() *cobra.Command {
	command := &cobra.Command{Use: "comments", Short: "Manage comment and reply downloads"}
	command.AddCommand(a.downloadStartCommand("download", "Queue comment and reply downloads for selected articles", "comments"))
	return command
}

func (a *App) downloadStartCommand(name, short, kind string) *cobra.Command {
	var articleIDs, urls []string
	var force bool
	var async asyncOptions
	command := &cobra.Command{
		Use: name, Short: short, Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := async.validate(); err != nil {
				return err
			}
			if len(articleIDs) == 0 && len(urls) == 0 {
				return usage("at least one --article or --url is required")
			}
			for _, value := range append(append([]string{}, articleIDs...), urls...) {
				if strings.TrimSpace(value) == "" {
					return usage("--article and --url values must not be empty")
				}
			}
			ids := make([]domain.ArticleID, len(articleIDs))
			for index, id := range articleIDs {
				ids[index] = domain.ArticleID(id)
			}
			job, err := a.core.StartDownload(command.Context(), domain.DownloadRequest{
				Kind: kind, ArticleIDs: ids, URLs: urls, Force: force,
			})
			if err != nil {
				return err
			}
			return a.outputJob(command.Context(), job, async)
		},
	}
	command.Flags().StringSliceVar(&articleIDs, "article", nil, "stable article ID; repeat or comma-separate")
	command.Flags().StringSliceVar(&urls, "url", nil, "WeChat article URL; repeat or comma-separate")
	command.Flags().BoolVar(&force, "force", false, "refresh content even when valid local data exists")
	async.addFlags(command)
	return command
}

func (a *App) exportCommand() *cobra.Command {
	command := &cobra.Command{Use: "export", Short: "Start and inspect local export jobs"}
	var format, outputRoot, accountID, albumID, savedQuery, naming, collision, htmlResourcePolicy, htmlBatchArchive string
	var urls, articleIDs []string
	var allMatching, includeContent, includeComments, includeMetadata bool
	var maxNameBytes int
	var async asyncOptions
	start := &cobra.Command{
		Use: "start", Short: "Resolve a stable selection and queue an export job", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := async.validate(); err != nil {
				return err
			}
			if err := validateExportFormat(format); err != nil {
				return err
			}
			format = strings.ToLower(strings.TrimSpace(format))
			if strings.TrimSpace(outputRoot) == "" {
				return usage("export start requires --output")
			}
			selection, err := buildExportSelection(urls, articleIDs, accountID, albumID, savedQuery, allMatching)
			if err != nil {
				return err
			}
			if collision != "fail" && collision != "skip" && collision != "replace" && collision != "suffix" {
				return usage("--collision must be fail, skip, replace, or suffix")
			}
			if maxNameBytes < 32 || maxNameBytes > 255 {
				return usage("--max-name-bytes must be between 32 and 255")
			}
			if htmlResourcePolicy != "best-effort" && htmlResourcePolicy != "strict" {
				return usage("--html-resource-policy must be best-effort or strict")
			}
			if htmlBatchArchive != "" && format != "html" {
				return usage("--html-batch-archive requires --format html")
			}
			job, err := a.core.StartExport(command.Context(), domain.ExportRequest{
				Selection: selection, Format: format, OutputRoot: outputRoot,
				Options: domain.ExportOptions{NamingTemplate: naming, MaximumNameBytes: maxNameBytes, CollisionPolicy: collision,
					FormatOptions: map[string]any{
						"content": includeContent, "comments": includeComments, "metadata": includeMetadata,
						"htmlResourcePolicy": htmlResourcePolicy, "htmlBatchArchive": htmlBatchArchive,
					}},
			})
			if err != nil {
				return err
			}
			return a.outputJob(command.Context(), job, async)
		},
	}
	start.Flags().StringVar(&format, "format", "markdown", "html, markdown, text, json, xlsx, docx, or pdf")
	start.Flags().StringVarP(&outputRoot, "output", "o", "", "export destination root")
	start.Flags().StringSliceVar(&urls, "url", nil, "select article URL; repeat or comma-separate")
	start.Flags().StringSliceVar(&articleIDs, "article", nil, "select stable article ID; repeat or comma-separate")
	start.Flags().StringVar(&accountID, "account", "", "select all articles for an account")
	start.Flags().StringVar(&albumID, "album", "", "select all articles in an album")
	start.Flags().StringVar(&savedQuery, "saved-query", "", "select a saved local query")
	start.Flags().BoolVar(&allMatching, "all-matching", false, "select all articles matching the empty local query")
	start.Flags().StringVar(&naming, "naming", "{published}-{title}", "deterministic output naming template")
	start.Flags().IntVar(&maxNameBytes, "max-name-bytes", 180, "maximum encoded file name length")
	start.Flags().StringVar(&collision, "collision", "fail", "fail, skip, replace, or suffix")
	start.Flags().BoolVar(&includeContent, "include-content", true, "include rendered article content where supported")
	start.Flags().BoolVar(&includeComments, "include-comments", false, "include locally stored comments")
	start.Flags().BoolVar(&includeMetadata, "include-metadata", true, "include normalized article metadata")
	start.Flags().StringVar(&htmlResourcePolicy, "html-resource-policy", "best-effort", "HTML resource handling: best-effort or strict")
	start.Flags().StringVar(&htmlBatchArchive, "html-batch-archive", "", "package all selected HTML articles into this .zip file")
	async.addFlags(start)
	var verifyRoot, verifyManifest string
	verify := &cobra.Command{
		Use: "verify", Short: "Verify an export provenance manifest and every recorded output checksum", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			root := strings.TrimSpace(verifyRoot)
			manifest := strings.TrimSpace(verifyManifest)
			if root == "" || manifest == "" {
				return usage("export verify requires --root and --manifest")
			}
			report, err := exporter.VerifyProvenanceManifest(command.Context(), root, manifest)
			if err != nil {
				return err
			}
			if !report.Valid {
				return &ResultError{Kind: "verification", Message: "export verification failed", ExitCode: 1, Data: report}
			}
			return a.output(report)
		},
	}
	verify.Flags().StringVar(&verifyRoot, "root", "", "export root containing the manifest and outputs")
	verify.Flags().StringVar(&verifyManifest, "manifest", "", "root-relative provenance manifest path")
	command.AddCommand(start, verify)
	return command
}

func validateExportFormat(value string) error {
	allowed := map[string]struct{}{"html": {}, "markdown": {}, "text": {}, "json": {}, "xlsx": {}, "docx": {}, "pdf": {}}
	if _, ok := allowed[strings.ToLower(strings.TrimSpace(value))]; !ok {
		return usage("--format must be html, markdown, text, json, xlsx, docx, or pdf")
	}
	return nil
}

func buildExportSelection(urls, articleIDs []string, accountID, albumID, savedQuery string, allMatching bool) (domain.ExportSelection, error) {
	for _, value := range append(append([]string{}, urls...), articleIDs...) {
		if strings.TrimSpace(value) == "" {
			return domain.ExportSelection{}, usage("export selection values must not be empty")
		}
	}
	selected := 0
	if len(urls) > 0 {
		selected++
	}
	if len(articleIDs) > 0 {
		selected++
	}
	if accountID != "" {
		selected++
	}
	if albumID != "" {
		selected++
	}
	if savedQuery != "" {
		selected++
	}
	if allMatching {
		selected++
	}
	if selected != 1 {
		return domain.ExportSelection{}, usage("select exactly one of --url, --article, --account, --album, --saved-query, or --all-matching")
	}
	switch {
	case len(urls) > 0:
		return domain.ExportSelection{Kind: domain.ExportSelectionURLs, URLs: urls}, nil
	case len(articleIDs) > 0:
		ids := make([]domain.ArticleID, len(articleIDs))
		for index, id := range articleIDs {
			ids[index] = domain.ArticleID(id)
		}
		return domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs, ArticleIDs: ids}, nil
	case accountID != "":
		return domain.ExportSelection{Kind: domain.ExportSelectionAccount, AccountID: domain.AccountID(accountID)}, nil
	case albumID != "":
		return domain.ExportSelection{Kind: domain.ExportSelectionAlbum, AlbumID: domain.AlbumID(albumID)}, nil
	case savedQuery != "":
		return domain.ExportSelection{Kind: domain.ExportSelectionSavedQuery, SavedQueryID: savedQuery}, nil
	default:
		return domain.ExportSelection{Kind: domain.ExportSelectionAllMatching, Query: domain.ArticleQuery{}}, nil
	}
}

func (a *App) jobCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "job", Short: "Inspect and control persistent jobs",
		Example: `  wechat-article job list --state queued,running
  wechat-article job follow JOB_ID --json
  wechat-article job cancel JOB_ID --confirm cancel-job:JOB_ID`,
	}
	var kind, states string
	var offset, limit int
	list := &cobra.Command{
		Use: "list", Short: "List persistent jobs", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := validatePage(offset, limit); err != nil {
				return err
			}
			parsedStates, err := parseJobStates(states)
			if err != nil {
				return err
			}
			page, err := a.core.QueryJobs(command.Context(), domain.JobQuery{Kind: kind, States: parsedStates, Offset: offset, Limit: limit})
			if err != nil {
				return err
			}
			return a.output(page)
		},
	}
	list.Flags().StringVar(&kind, "kind", "", "filter job kind")
	list.Flags().StringVar(&states, "state", "", "comma-separated job states")
	addPageFlags(list, &offset, &limit, 50)

	get := a.jobGetCommand("get <id>", "Get one persistent job", false)
	wait := a.jobGetCommand("wait <id>", "Wait for a job to reach a terminal state", false)
	follow := a.jobGetCommand("follow <id>", "Follow state changes on stderr until the job is terminal", true)

	var cancelConfirmation string
	cancel := &cobra.Command{
		Use: "cancel <id>", Short: "Cancel a queued or active job", Args: exactArgs(1, "job cancel requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			required := "cancel-job:" + args[0]
			if cancelConfirmation != required {
				return usage("job cancellation requires --confirm " + required)
			}
			job, err := a.core.CancelJob(command.Context(), domain.JobID(args[0]))
			if err != nil {
				return err
			}
			return a.output(job)
		},
	}
	cancel.Flags().StringVar(&cancelConfirmation, "confirm", "", "exact confirmation value")

	command.AddCommand(list, get, wait, follow, cancel)
	worker := &cobra.Command{
		Use: "worker <id>", Short: "Run one persistent job worker", Args: exactArgs(1, "job worker requires <id>"),
		Hidden: true,
		RunE: func(command *cobra.Command, args []string) error {
			_, err := a.runPersistentJob(command.Context(), domain.JobID(args[0]))
			return err
		},
	}
	command.AddCommand(worker)
	if a.active != nil && a.active.Jobs != nil {
		command.AddCommand(a.directJobOperation("pause <id>", "Pause a supported job", "pause-job:", a.active.Jobs.Pause))
		command.AddCommand(a.restartJobOperation("resume <id>", "Resume a paused or authentication-blocked job", "", a.active.Jobs.Resume))
		command.AddCommand(a.restartJobOperation("retry <id>", "Retry a failed, partial, or cancelled job", "retry-job:", nil))
	}
	return command
}

func (a *App) restartJobOperation(
	use, short, confirmationPrefix string,
	operation func(context.Context, domain.JobID) (domain.Job, error),
) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, strings.Split(use, " ")[0]+" requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			if confirmationPrefix != "" {
				required := confirmationPrefix + args[0]
				if confirmation != required {
					return usage(strings.Split(use, " ")[0] + " requires --confirm " + required)
				}
			}
			var job domain.Job
			var err error
			if operation == nil {
				current, getErr := a.active.Jobs.Get(command.Context(), domain.JobID(args[0]))
				if getErr != nil {
					return getErr
				}
				if current.Kind == "export" {
					job, err = a.active.Jobs.RetryExport(command.Context(), current.ID)
				} else {
					job, err = a.active.Jobs.Retry(command.Context(), current.ID)
				}
			} else {
				job, err = operation(command.Context(), domain.JobID(args[0]))
			}
			if err != nil {
				return err
			}
			if a.runtimes == nil || a.runtimes.worker == nil {
				_, _ = a.active.Jobs.Pause(context.Background(), job.ID)
				return fmt.Errorf("restart %s worker: persistent job worker launcher is unavailable", job.Kind)
			}
			starter := persistentJobStarter{
				executable: a.runtimes.executable,
				paths:      a.runtimes.paths,
				profile:    a.active.Profile.ID,
				launcher:   a.runtimes.worker,
			}
			if err := starter.Start(command.Context(), job); err != nil {
				_, _ = a.active.Jobs.Pause(context.Background(), job.ID)
				return err
			}
			return a.output(job)
		},
	}
	if confirmationPrefix != "" {
		command.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation value")
	}
	return command
}

func (a *App) jobGetCommand(use, short string, follow bool) *cobra.Command {
	var interval time.Duration
	command := &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, strings.Split(use, " ")[0]+" requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			if strings.HasPrefix(use, "get ") {
				job, err := a.core.GetJob(command.Context(), domain.JobID(args[0]))
				if err != nil {
					return err
				}
				return a.output(job)
			}
			job, err := a.waitForJob(command.Context(), domain.JobID(args[0]), follow, interval)
			if err != nil {
				return err
			}
			return a.output(job)
		},
	}
	if !strings.HasPrefix(use, "get ") {
		command.Flags().DurationVar(&interval, "poll-interval", time.Second, "job polling interval between 100ms and 30s")
	}
	return command
}

func (a *App) directJobOperation(use, short, confirmationPrefix string, operation func(context.Context, domain.JobID) (domain.Job, error)) *cobra.Command {
	var confirmation string
	command := &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, strings.Split(use, " ")[0]+" requires <id>"),
		RunE: func(command *cobra.Command, args []string) error {
			if confirmationPrefix != "" {
				required := confirmationPrefix + args[0]
				if confirmation != required {
					return usage(strings.Split(use, " ")[0] + " requires --confirm " + required)
				}
			}
			job, err := operation(command.Context(), domain.JobID(args[0]))
			if err != nil {
				return err
			}
			return a.output(job)
		},
	}
	if confirmationPrefix != "" {
		command.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation value")
	}
	return command
}

func parseJobStates(value string) ([]domain.JobState, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	allowed := map[domain.JobState]struct{}{
		domain.JobQueued: {}, domain.JobRunning: {}, domain.JobCompleted: {}, domain.JobPartial: {},
		domain.JobFailed: {}, domain.JobCancelled: {}, domain.JobBlockedAuth: {}, domain.JobPaused: {},
	}
	states := []domain.JobState{}
	for _, part := range strings.Split(value, ",") {
		state := domain.JobState(strings.TrimSpace(part))
		if _, ok := allowed[state]; !ok {
			return nil, usage(fmt.Sprintf("unsupported job state %q", state))
		}
		states = append(states, state)
	}
	return states, nil
}

func (a *App) outputJob(ctx context.Context, job domain.Job, options asyncOptions) error {
	if !options.wait && !options.follow {
		fmt.Fprintf(a.stderr, "job %s queued (%s); inspect with `wechat-article job get %s`\n", job.ID, job.Kind, job.ID)
		return a.output(job)
	}
	finished, err := a.waitForJob(ctx, job.ID, options.follow, options.pollInterval)
	if err != nil {
		return err
	}
	return a.output(finished)
}

func (a *App) waitForJob(ctx context.Context, id domain.JobID, follow bool, interval time.Duration) (domain.Job, error) {
	if interval < 100*time.Millisecond || interval > 30*time.Second {
		return domain.Job{}, usage("--poll-interval must be between 100ms and 30s")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	signals := a.core.ProcessSignals()
	lastState := domain.JobState("")
	for {
		job, err := a.core.GetJob(ctx, id)
		if err != nil {
			return domain.Job{}, err
		}
		if follow && job.State != lastState {
			fmt.Fprintf(a.stderr, "job %s: %s\n", job.ID, job.State)
			lastState = job.State
		}
		if jobTerminal(job.State) {
			return job, nil
		}
		if signals == nil {
			select {
			case <-ctx.Done():
				return domain.Job{}, fmt.Errorf("job %s wait interrupted: %w", id, ctx.Err())
			case <-ticker.C:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return domain.Job{}, fmt.Errorf("job %s wait interrupted: %w", id, ctx.Err())
		case <-signals:
			return domain.Job{}, fmt.Errorf("job %s wait interrupted: %w", id, context.Canceled)
		case <-ticker.C:
		}
	}
}

func jobTerminal(state domain.JobState) bool {
	return state == domain.JobCompleted || state == domain.JobPartial || state == domain.JobFailed ||
		state == domain.JobCancelled || state == domain.JobBlockedAuth
}
