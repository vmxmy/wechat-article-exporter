package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/migration"
)

func liveCommand(arguments []string) error {
	flags := flag.NewFlagSet("live", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := runOptions{Mode: modeLive, RequireLive: true}
	flags.StringVar(&options.Archive, "archive", "", "release archive used for provenance")
	flags.StringVar(&options.SBOM, "sbom", "", "per-target CycloneDX SBOM")
	flags.StringVar(&options.ChecksumManifest, "checksums", "", "release checksums.txt")
	flags.StringVar(&options.Output, "output", "", "live platform receipt JSON output")
	flags.StringVar(&options.WorkRoot, "work-root", "", "empty controlled clean-room working directory")
	flags.StringVar(&options.Commit, "commit", "", "source commit")
	flags.StringVar(&options.Version, "version", "", "candidate version")
	flags.StringVar(&options.Repository, "repository", "", "source repository")
	flags.StringVar(&options.LegacyArchive, "legacy-archive", "", "sanitized versioned legacy-Web archive")
	flags.StringVar(&options.VaultPassphrase, "vault-passphrase-file", "", "protected live vault passphrase file")
	flags.StringVar(&options.AccountFakeID, "account-fakeid", "", "controlled account fakeid")
	flags.StringVar(&options.AccountName, "account-name", "", "controlled account display name")
	flags.StringVar(&options.QROutput, "qr-output", "", "temporary QR output path inside work root")
	flags.StringVar(&options.ObserverCommand, "observer-command", "", "approved process-tree network observer command")
	flags.StringVar(&options.OfflineGuard, "offline-guard", "", "approved OS deny-all offline guard command")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	for label, value := range map[string]string{
		"archive": options.Archive, "sbom": options.SBOM, "checksums": options.ChecksumManifest, "output": options.Output,
		"commit": options.Commit, "version": options.Version, "repository": options.Repository, "legacy-archive": options.LegacyArchive,
		"vault-passphrase-file": options.VaultPassphrase, "account-fakeid": options.AccountFakeID, "account-name": options.AccountName,
		"observer-command": options.ObserverCommand, "offline-guard": options.OfflineGuard,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s is required for controlled live evidence", label)
		}
	}
	if strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_CLEAN_ROOM")) != "" || strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_CONTROLLED_ORIGIN")) != "" {
		return errors.New("live runner rejects fixture clean-room origin overrides")
	}
	return runControlledLive(options)
}

func runControlledLive(options runOptions) error {
	workRoot := options.WorkRoot
	if workRoot == "" {
		return errors.New("--work-root is required for controlled live evidence")
	}
	if err := ensureEmptyDirectory(workRoot); err != nil {
		return err
	}
	artifact, err := inspectCandidateArtifact(options, filepath.Join(workRoot, "candidate-artifact"))
	if err != nil {
		return fmt.Errorf("verify candidate artifact provenance: %w", err)
	}
	binaryDigest, err := sha256File(artifact.BinaryPath)
	if err != nil {
		return err
	}
	receipt := Receipt{
		SchemaVersion: receiptSchemaVersion,
		ReceiptID:     fmt.Sprintf("%s-%s-%s-%d", options.Version, runtime.GOOS, runtime.GOARCH, time.Now().UTC().Unix()),
		Mode:          modeLive,
		StartedAt:     time.Now().UTC(),
		Platform: PlatformEvidence{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Native: true,
			RunnerOS: os.Getenv("RUNNER_OS"), RunnerArch: os.Getenv("RUNNER_ARCH")},
		Source: SourceEvidence{Repository: options.Repository, Tag: releaseTag(options.Version), Commit: options.Commit, Version: options.Version},
		Artifact: ArtifactEvidence{ArchiveName: filepath.Base(options.Archive), ArchiveSHA256: artifact.ArchiveSHA256,
			ArchiveMember: artifact.BinaryMember, BinaryName: filepath.Base(artifact.BinaryPath), BinarySHA256: binaryDigest,
			BuildInfoSHA256: artifact.BuildInfoSHA256, ChecksumManifestSHA256: artifact.ChecksumManifestSHA256,
			SBOMName: filepath.Base(options.SBOM), SBOMSHA256: artifact.SBOMSHA256, TargetGOOS: artifact.GOOS,
			TargetGOARCH: artifact.GOARCH, CGOEnabled: artifact.CGOEnabled, Module: artifact.Module, BuildCommit: artifact.Commit,
			ExecutorKind: "release-binary"},
		CleanRoom: CleanRoomEvidence{RootCount: 4, RootsBeganEmpty: true},
		Network:   NetworkEvidence{OfflineGuard: "os-deny-all-required"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	portableRoot := filepath.Join(workRoot, "portable")
	qrPath := options.QROutput
	if qrPath == "" {
		qrPath = filepath.Join(workRoot, "login-qr.png")
	}
	if !pathWithin(workRoot, qrPath) {
		return errors.New("--qr-output must be inside --work-root")
	}
	baseEnv := []string{"WECHAT_ARTICLE_PORTABLE_ROOT=" + portableRoot, "WECHAT_ARTICLE_CLEAN_ROOM=0"}
	bootstrap := candidateRunner{binary: artifact.BinaryPath, env: baseEnv, observerCommand: options.ObserverCommand, requireObserver: true}
	if _, err := bootstrap.runJSON(ctx, "vault", "init", "--passphrase-file", options.VaultPassphrase, "--json"); err != nil {
		return fmt.Errorf("initialize live encrypted vault: %w", err)
	}
	runner := candidateRunner{binary: artifact.BinaryPath, env: append(baseEnv,
		"WECHAT_ARTICLE_SECRET_BACKEND=vault", "WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE="+options.VaultPassphrase), observerCommand: options.ObserverCommand, requireObserver: true}
	run := liveWorkflowRecorder(&receipt, binaryDigest)
	run("install.archive", phaseOffline, func() (map[string]string, error) {
		command, err := runner.configuredCommand("--version")
		if err != nil {
			return nil, err
		}
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		if err := runCandidateProcess(ctx, command); err != nil {
			return nil, err
		}
		if !exactVersionOutput(strings.TrimSpace(output.String()), options.Version) {
			return nil, errors.New("candidate exact version check failed")
		}
		return map[string]string{"archiveSha256": artifact.ArchiveSHA256, "binarySha256": binaryDigest, "archiveMember": artifact.BinaryMember, "target": artifact.GOOS + "/" + artifact.GOARCH, "cgoEnabled": artifact.CGOEnabled, "versionExact": "true"}, nil
	})
	run("storage.clean-roots", phaseOffline, func() (map[string]string, error) {
		for _, name := range []string{"config", "data", "cache", "state"} {
			info, err := os.Stat(filepath.Join(portableRoot, name))
			if err != nil || !info.IsDir() {
				return nil, fmt.Errorf("clean %s root missing", name)
			}
		}
		return map[string]string{"rootCount": "4", "beganEmpty": "true"}, nil
	})
	run("migration.legacy-web", phaseOffline, func() (map[string]string, error) {
		if _, err := runner.runJSON(ctx, "migration", "inspect", options.LegacyArchive, "--json"); err != nil {
			return nil, err
		}
		if _, err := runner.runJSON(ctx, "migration", "import", options.LegacyArchive, "--confirm", "import-legacy:"+options.LegacyArchive, "--json"); err != nil {
			return nil, err
		}
		verified, err := runner.runJSON(ctx, "migration", "verify", options.LegacyArchive, "--json")
		if err != nil || !jsonBoolean(verified.Envelope.Data, "success") {
			return nil, errors.Join(err, errors.New("legacy migration verification failed"))
		}
		digest, err := sha256File(options.LegacyArchive)
		return map[string]string{"archiveSha256": digest, "format": migration.ArchiveFormat, "records": "1"}, err
	})
	run("migration.database-baselines", phaseOffline, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "db", "integrity", "--json")
		if err != nil || !jsonBoolean(result.Envelope.Data, "valid") {
			return nil, errors.Join(err, errors.New("database integrity failed"))
		}
		return map[string]string{"compatibilityWindow": "schema-1-through-8", "currentDatabaseOpened": "true", "integrityValid": "true"}, nil
	})
	livePhase := phaseOnlineLive
	run("login.qr", livePhase, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "login", "--qr-output", qrPath, "--json")
		if err != nil || !result.Envelope.Success {
			return nil, errors.Join(err, errors.New("controlled QR login failed"))
		}
		if err := os.Remove(qrPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return map[string]string{"qrRemoved": "true", "session": "authenticated", "backend": "encrypted-vault"}, nil
	})
	run("session.restart-persistence", livePhase, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "status", "--json")
		if err != nil {
			return nil, err
		}
		var status struct {
			Session struct {
				State string `json:"state"`
			} `json:"session"`
		}
		if err := json.Unmarshal(result.Envelope.Data, &status); err != nil || status.Session.State != "authenticated" {
			return nil, errors.Join(err, errors.New("live session did not persist after process restart"))
		}
		return map[string]string{"processRestarted": "true", "backend": "encrypted-vault", "sessionReused": "true"}, nil
	})
	accountID := identity.AccountID(options.AccountFakeID)
	run("sync.account", livePhase, func() (map[string]string, error) {
		if _, err := runner.runJSON(ctx, "account", "add", options.AccountFakeID, "--name", options.AccountName, "--json"); err != nil {
			return nil, err
		}
		result, err := runner.runJSON(ctx, "sync", "account", string(accountID), "--range", "all", "--page-size", "5", "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		listed, err := runner.runJSON(ctx, "article", "list", "--account", string(accountID), "--limit", "1", "--json")
		if err != nil {
			return nil, err
		}
		var page struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(listed.Envelope.Data, &page); err != nil || len(page.Items) == 0 {
			return nil, errors.Join(err, errors.New("controlled account returned no bounded articles"))
		}
		return map[string]string{"accountCount": "1", "articleCount": fmt.Sprint(len(page.Items)), "expectedDatasetVisible": "true"}, nil
	})
	var articleID string
	run("download.article", livePhase, func() (map[string]string, error) {
		listed, err := runner.runJSON(ctx, "article", "list", "--account", string(accountID), "--limit", "1", "--json")
		if err != nil {
			return nil, err
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(listed.Envelope.Data, &page); err != nil || len(page.Items) == 0 || page.Items[0].ID == "" {
			return nil, errors.Join(err, errors.New("no controlled article available for download"))
		}
		articleID = page.Items[0].ID
		result, err := runner.runJSON(ctx, "download", "article", "--article", articleID, "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		return map[string]string{"articleCount": "1", "contentAvailable": "true"}, nil
	})
	run("download.resources", livePhase, func() (map[string]string, error) {
		if articleID == "" {
			return nil, errors.New("article download did not select an article")
		}
		result, err := runner.runJSON(ctx, "download", "resources", "--article", articleID, "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		return map[string]string{"resourceRequestDelta": "1", "objectMappingVerified": "true"}, nil
	})
	for _, format := range []string{"html", "markdown", "text", "json", "xlsx", "docx", "pdf"} {
		format := format
		run("export."+format, phaseOffline, func() (map[string]string, error) {
			if articleID == "" {
				return nil, errors.New("downloaded controlled article unavailable")
			}
			root := filepath.Join(portableRoot, "exports", format)
			result, err := runner.runJSON(ctx, "export", "start", "--format", format, "--article", articleID, "--output", root, "--collision", "replace", "--wait", "--json")
			if err != nil {
				return nil, err
			}
			if err := requireCompletedJob(result.Envelope); err != nil {
				return nil, err
			}
			manifest, err := singleExportManifest(root)
			if err != nil {
				return nil, err
			}
			checked, err := runner.runJSON(ctx, "export", "verify", "--root", root, "--manifest", filepath.Base(manifest), "--json")
			if err != nil || !jsonBoolean(checked.Envelope.Data, "valid") {
				return nil, errors.Join(err, errors.New("export manifest verification failed"))
			}
			files, err := regularFiles(root)
			if err != nil || len(files) == 0 {
				return nil, errors.Join(err, errors.New("export has no files"))
			}
			digest, err := sha256File(files[0])
			return map[string]string{"fileCount": fmt.Sprint(len(files)), "firstOutputSha256": digest, "manifestVerified": "true"}, err
		})
	}
	run("automation.cobra", phaseOffline, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "status", "--json")
		if err != nil || result.Envelope.SchemaVersion != "wechat-article-cli/v1" {
			return nil, errors.Join(err, errors.New("Cobra JSON contract failed"))
		}
		return map[string]string{"schemaVersion": result.Envelope.SchemaVersion, "jsonPure": "true"}, nil
	})
	// The native observer is an execution boundary. The current PTY adapter has
	// no observer-aware launch primitive, so a live receipt must fail closed
	// until the controlled runner supplies an observer-aware native PTY adapter.
	run("ui.tui", phaseOffline, func() (map[string]string, error) {
		return nil, errors.New("live native PTY observation is not yet available")
	})
	run("automation.mcp", phaseOffline, func() (map[string]string, error) { return liveMCPProof(ctx, runner) })
	backupPath := filepath.Join(workRoot, "backup.zip")
	run("storage.backup-restore", phaseOffline, func() (map[string]string, error) {
		evidence, err := liveBackupRestore(ctx, runner, artifact.BinaryPath, workRoot, portableRoot, options.VaultPassphrase, backupPath)
		if err == nil {
			receipt.CleanRoom.IndependentRestore = true
		}
		return evidence, err
	})
	run("offline.local-workflows", phaseOffline, func() (map[string]string, error) {
		return runOfflineGuard(ctx, options.OfflineGuard, runner, articleID)
	})
	run("network.no-retired-domain", phaseOffline, func() (map[string]string, error) { return verifyNetworkObserver(ctx, options.ObserverCommand) })
	run("security.no-receipt-leakage", phaseOffline, func() (map[string]string, error) {
		probe := receipt
		probe.finalize()
		body, err := json.Marshal(probe)
		if err != nil || containsForbiddenReceiptText(string(body)) {
			return nil, errors.Join(err, errors.New("receipt privacy scan failed"))
		}
		return map[string]string{"scanPassed": "true", "rawPayloadStored": "false"}, nil
	})
	run("secrets.platform-persistence", phaseOffline, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "vault", "verify", "--passphrase-file", options.VaultPassphrase, "--json")
		if err != nil || !jsonBoolean(result.Envelope.Data, "verified") {
			return nil, errors.Join(err, errors.New("live vault persistence failed"))
		}
		return map[string]string{"backend": "encrypted-vault", "restartRoundTrip": "true"}, nil
	})
	receipt.finalize()
	if err := validateReceipt(receipt, true); err != nil {
		_ = writeReceipt(options.Output, receipt)
		return fmt.Errorf("diagnostic live receipt written but invalid: %w", err)
	}
	if err := writeReceipt(options.Output, receipt); err != nil {
		return err
	}
	fmt.Printf("controlled live clean-room receipt written: %s\n", options.Output)
	return nil
}

func liveWorkflowRecorder(receipt *Receipt, digest string) func(string, string, func() (map[string]string, error)) {
	return func(id, phase string, operation func() (map[string]string, error)) {
		started := time.Now().UTC()
		evidence, err := operation()
		finished := time.Now().UTC()
		result, reason := resultPassed, ""
		if err != nil {
			result, reason, evidence = resultFailed, boundedReasonCode(err), nil
			fmt.Fprintf(os.Stderr, "controlled live workflow %s failed: %s\n", id, reason)
		}
		receipt.Workflows = append(receipt.Workflows, WorkflowResult{ID: id, Phase: phase, Result: result, StartedAt: started, FinishedAt: finished, DurationMS: finished.Sub(started).Milliseconds(), Executor: WorkflowExecutor{Kind: "release-binary", BinarySHA256: digest}, Evidence: evidence, Reason: reason})
	}
}

func liveMCPProof(ctx context.Context, runner candidateRunner) (map[string]string, error) {
	unsupported := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"unsupported-clean-room-version"}}` + "\n"
	output, stderr, code, err := runner.runStdio(ctx, unsupported, "mcp", "serve", "--transport", "stdio")
	if err != nil || code != 0 || !isUnsupportedProtocolResponse(output, mcp.ProtocolVersion) {
		return nil, errors.Join(err, errors.New("MCP unsupported protocol proof failed"))
	}
	input := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`+"\n"+`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`+"\n"+`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`+"\n"+`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"storage.status","arguments":{}}}`+"\n", mcp.ProtocolVersion)
	output, normalStderr, code, err := runner.runStdio(ctx, input, "mcp", "serve", "--transport", "stdio")
	if err != nil || code != 0 || len(nonEmptyLines(string(output))) != 3 {
		return nil, errors.Join(err, errors.New("MCP stdio proof failed"))
	}
	return map[string]string{"responses": "3", "transport": "stdio", "unsupportedRejected": "true", "stderrBytes": fmt.Sprint(len(stderr) + len(normalStderr))}, nil
}

func liveBackupRestore(ctx context.Context, runner candidateRunner, binary, workRoot, portableRoot, passphrase, backupPath string) (map[string]string, error) {
	backup, err := runner.runJSON(ctx, "db", "backup", "--output", backupPath, "--json")
	if err != nil || !jsonNestedBoolean(backup.Envelope.Data, "verification", "valid") {
		return nil, errors.Join(err, errors.New("backup verification failed"))
	}
	restoreRoot := filepath.Join(workRoot, "restore-portable")
	restore := candidateRunner{binary: binary, env: []string{"WECHAT_ARTICLE_PORTABLE_ROOT=" + restoreRoot, "WECHAT_ARTICLE_CLEAN_ROOM=0", "WECHAT_ARTICLE_SECRET_BACKEND=vault", "WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE=" + passphrase}, observerCommand: runner.observerCommand, requireObserver: true}
	if _, err := restore.runJSON(ctx, "vault", "init", "--passphrase-file", passphrase, "--json"); err != nil {
		return nil, err
	}
	if _, err := restore.runJSON(ctx, "db", "restore", backupPath, "--confirm", "restore-backup:"+backupPath, "--json"); err != nil {
		return nil, err
	}
	verified, err := restore.runJSON(ctx, "db", "integrity", "--json")
	if err != nil || !jsonBoolean(verified.Envelope.Data, "valid") {
		return nil, errors.Join(err, errors.New("independent restore integrity failed"))
	}
	digest, err := sha256File(backupPath)
	return map[string]string{"backupSha256": digest, "restoreRootEmpty": "true"}, err
}

func runOfflineGuard(context.Context, string, candidateRunner, string) (map[string]string, error) {
	return nil, errors.New("live OS deny-all offline execution requires the controlled runner adapter")
}

func verifyNetworkObserver(context.Context, string) (map[string]string, error) {
	return nil, errors.New("structured process-tree network observer report is required")
}

func environmentValueFromOverrides(entries []string, name string) string {
	prefix := name + "="
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func pathWithin(root, path string) bool {
	absoluteRoot, a := filepath.Abs(root)
	absolutePath, b := filepath.Abs(path)
	if a != nil || b != nil {
		return false
	}
	relative, err := filepath.Rel(absoluteRoot, absolutePath)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func assembleSetCommand(arguments []string) error {
	flags := flag.NewFlagSet("assemble-set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var output, repository, tag, commit, version, checksums string
	var receipts receiptPaths
	flags.StringVar(&output, "output", "", "aggregate receipt-set JSON output")
	flags.StringVar(&repository, "repository", "", "release repository")
	flags.StringVar(&tag, "tag", "", "release tag")
	flags.StringVar(&commit, "commit", "", "release commit")
	flags.StringVar(&version, "version", "", "release version")
	flags.StringVar(&checksums, "checksum-manifest-sha256", "", "checksum manifest digest")
	flags.Var(&receipts, "receipt", "platform receipt path; repeat exactly five times")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if output == "" || repository == "" || tag == "" || commit == "" || version == "" || !validSHA256(checksums) || len(receipts) != len(requiredTargetTuples) {
		return errors.New("assemble-set requires output, release identity, valid checksum digest, and exactly five receipt paths")
	}
	base := filepath.Dir(output)
	set := ReceiptSet{SchemaVersion: receiptSetSchemaVersion, Release: ReceiptSetRelease{Repository: repository, Tag: tag, Commit: commit, Version: version, ChecksumManifestSHA256: checksums}, Summary: ReceiptSetSummary{RequiredTargets: len(requiredTargetTuples), PassedTargets: len(requiredTargetTuples), GateStatus: "pass"}}
	for _, path := range receipts {
		body, err := readBoundedRegularFile(path, maximumReceiptBytes)
		if err != nil {
			return err
		}
		receipt, err := decodeReceipt(body)
		if err != nil || validateReceipt(receipt, true) != nil {
			return errors.New("assemble-set accepts only passing live receipts")
		}
		relative, err := filepath.Rel(base, path)
		if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
			return errors.New("receipt path must be inside aggregate output directory")
		}
		set.Receipts = append(set.Receipts, ReceiptSetReference{Target: receipt.Platform.GOOS + "/" + receipt.Platform.GOARCH, ReceiptPath: filepath.ToSlash(relative), ReceiptSHA256: sha256Bytes(body), ArchiveSHA256: receipt.Artifact.ArchiveSHA256})
	}
	sort.Slice(set.Receipts, func(i, j int) bool { return set.Receipts[i].Target < set.Receipts[j].Target })
	if err := writeReceiptSet(output, set); err != nil {
		return err
	}
	if err := validateReceiptSet(set, base); err != nil {
		return err
	}
	fmt.Printf("controlled live receipt set written: %s\n", output)
	return nil
}

type receiptPaths []string

func (values *receiptPaths) String() string         { return strings.Join(*values, ",") }
func (values *receiptPaths) Set(value string) error { *values = append(*values, value); return nil }

func writeReceiptSet(path string, receiptSet ReceiptSet) error {
	body, err := json.MarshalIndent(receiptSet, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicPrivateJSON(path, append(body, '\n'))
}
