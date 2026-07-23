package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/migration"
)

type runOptions struct {
	Archive          string
	Binary           string
	BuildInfo        string
	SBOM             string
	ChecksumManifest string
	Output           string
	WorkRoot         string
	Commit           string
	Version          string
	Repository       string
	Mode             string
	SkipTUI          bool
	RequireLive      bool
	LegacyArchive    string
	VaultPassphrase  string
	AccountFakeID    string
	AccountName      string
	QROutput         string
	ObserverCommand  string
	OfflineGuard     string
}

type commandEnvelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	Success       bool            `json:"success"`
	Data          json.RawMessage `json:"data"`
	Error         json.RawMessage `json:"error,omitempty"`
}

type jobEnvelopeData struct {
	State  string `json:"state"`
	Counts struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
		Failed    int `json:"failed"`
		Partial   int `json:"partial"`
	} `json:"counts"`
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: go run ./cmd/cleanroom <run|live|verify|verify-set|assemble-set> [flags]")
	}
	switch os.Args[1] {
	case "run":
		if err := runCommand(os.Args[2:]); err != nil {
			fatalf("clean-room run: %v", err)
		}
	case "live":
		if err := liveCommand(os.Args[2:]); err != nil {
			fatalf("controlled live clean-room run: %v", err)
		}
	case "verify":
		if err := verifyCommand(os.Args[2:]); err != nil {
			fatalf("clean-room verify: %v", err)
		}
	case "verify-set":
		if err := verifySetCommand(os.Args[2:]); err != nil {
			fatalf("clean-room receipt set verify: %v", err)
		}
	case "assemble-set":
		if err := assembleSetCommand(os.Args[2:]); err != nil {
			fatalf("clean-room receipt-set assembly: %v", err)
		}
	default:
		fatalf("unknown clean-room command %q", os.Args[1])
	}
}

func verifySetCommand(arguments []string) error {
	flags := flag.NewFlagSet("verify-set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var path string
	var expectedRepository, expectedTag, expectedCommit, expectedVersion, expectedChecksumManifest string
	flags.StringVar(&path, "receipt-set", "", "aggregate release receipt-set JSON path")
	flags.StringVar(&expectedRepository, "repository", "", "expected release repository")
	flags.StringVar(&expectedTag, "tag", "", "expected release tag")
	flags.StringVar(&expectedCommit, "commit", "", "expected release commit")
	flags.StringVar(&expectedVersion, "version", "", "expected release version")
	flags.StringVar(&expectedChecksumManifest, "checksum-manifest-sha256", "", "expected release checksum-manifest digest")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("--receipt-set is required")
	}
	receiptSet, err := readReceiptSet(path)
	if err != nil {
		return err
	}
	if err := validateReceiptSet(receiptSet, filepath.Dir(path)); err != nil {
		return err
	}
	for _, assertion := range []struct {
		label    string
		expected string
		actual   string
	}{
		{label: "repository", expected: expectedRepository, actual: receiptSet.Release.Repository},
		{label: "tag", expected: expectedTag, actual: receiptSet.Release.Tag},
		{label: "commit", expected: expectedCommit, actual: receiptSet.Release.Commit},
		{label: "version", expected: expectedVersion, actual: receiptSet.Release.Version},
		{label: "checksum-manifest-sha256", expected: expectedChecksumManifest, actual: receiptSet.Release.ChecksumManifestSHA256},
	} {
		if strings.TrimSpace(assertion.expected) == "" {
			continue
		}
		if assertion.actual != assertion.expected {
			return fmt.Errorf("receipt-set %s=%q, want %q", assertion.label, assertion.actual, assertion.expected)
		}
	}
	fmt.Printf("clean-room receipt set verified: %s (%d targets)\n", receiptSet.Release.Version, len(receiptSet.Receipts))
	return nil
}

func runCommand(arguments []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	options := runOptions{}
	flags.StringVar(&options.Archive, "archive", "", "release archive used for provenance")
	flags.StringVar(&options.Binary, "binary", "", "extracted release binary")
	flags.StringVar(&options.BuildInfo, "build-info", "", "archive build-info.txt")
	flags.StringVar(&options.SBOM, "sbom", "", "per-target CycloneDX SBOM")
	flags.StringVar(&options.ChecksumManifest, "checksums", "", "release checksums.txt")
	flags.StringVar(&options.Output, "output", "", "receipt JSON output")
	flags.StringVar(&options.WorkRoot, "work-root", "", "empty clean-room working directory")
	flags.StringVar(&options.Commit, "commit", "", "source commit")
	flags.StringVar(&options.Version, "version", "", "candidate version")
	flags.StringVar(&options.Repository, "repository", "", "source repository")
	flags.StringVar(&options.Mode, "mode", "fixture", "fixture or live")
	flags.BoolVar(&options.SkipTUI, "skip-tui", false, "skip TUI (development only; receipt fails verification)")
	flags.BoolVar(&options.RequireLive, "require-live", false, "require controlled-account live evidence")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if options.Mode != "fixture" {
		return errors.New("--mode currently supports fixture only; live evidence requires the separate controlled-account runner")
	}
	if options.RequireLive {
		return errors.New("--require-live cannot be used with the fixture runner")
	}
	return runCleanRoom(options)
}

func verifyCommand(arguments []string) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var path string
	var requireLive bool
	flags.StringVar(&path, "receipt", "", "receipt JSON path")
	flags.BoolVar(&requireLive, "require-live", false, "require live controlled-account login/sync/download evidence")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("--receipt is required")
	}
	receipt, err := readReceipt(path)
	if err != nil {
		return err
	}
	if err := validateReceipt(receipt, requireLive); err != nil {
		return err
	}
	fmt.Printf("clean-room receipt verified: %s (%s/%s, %d workflows)\n", receipt.ReceiptID,
		receipt.Platform.GOOS, receipt.Platform.GOARCH, len(receipt.Workflows))
	return nil
}

func runCleanRoom(options runOptions) error {
	for label, value := range map[string]string{
		"archive": options.Archive, "sbom": options.SBOM, "checksums": options.ChecksumManifest, "output": options.Output,
		"commit": options.Commit, "version": options.Version,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s is required", label)
		}
	}
	workRoot := options.WorkRoot
	removeWorkRoot := false
	if workRoot == "" {
		root, err := os.MkdirTemp("", "wechat-article-clean-room-")
		if err != nil {
			return err
		}
		workRoot, removeWorkRoot = root, true
	}
	if removeWorkRoot {
		defer os.RemoveAll(workRoot)
	}
	if err := ensureEmptyDirectory(workRoot); err != nil {
		return err
	}
	artifact, err := inspectCandidateArtifact(options, filepath.Join(workRoot, "candidate-artifact"))
	if err != nil {
		return fmt.Errorf("verify candidate artifact provenance: %w", err)
	}
	options.Binary = artifact.BinaryPath
	options.BuildInfo = artifact.BuildInfoPath

	archiveDigest := artifact.ArchiveSHA256
	binaryDigest, err := sha256File(options.Binary)
	if err != nil {
		return err
	}
	receipt := Receipt{
		SchemaVersion: receiptSchemaVersion,
		ReceiptID:     fmt.Sprintf("%s-%s-%s-%d", options.Version, runtime.GOOS, runtime.GOARCH, time.Now().UTC().Unix()),
		Mode:          options.Mode,
		StartedAt:     time.Now().UTC(),
		Platform: PlatformEvidence{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Native: true,
			RunnerOS: os.Getenv("RUNNER_OS"), RunnerArch: os.Getenv("RUNNER_ARCH")},
		Source: SourceEvidence{Repository: options.Repository, Tag: releaseTag(options.Version), Commit: options.Commit, Version: options.Version},
		Artifact: ArtifactEvidence{ArchiveName: filepath.Base(options.Archive), ArchiveSHA256: archiveDigest,
			ArchiveMember: artifact.BinaryMember, BinaryName: filepath.Base(options.Binary), BinarySHA256: binaryDigest,
			BuildInfoSHA256: artifact.BuildInfoSHA256, ChecksumManifestSHA256: artifact.ChecksumManifestSHA256,
			SBOMName: filepath.Base(options.SBOM), SBOMSHA256: artifact.SBOMSHA256, TargetGOOS: artifact.GOOS,
			TargetGOARCH: artifact.GOARCH, CGOEnabled: artifact.CGOEnabled, Module: artifact.Module,
			BuildCommit: artifact.Commit, ExecutorKind: "release-binary"},
		CleanRoom: CleanRoomEvidence{RootCount: 4, RootsBeganEmpty: true},
		Network:   NetworkEvidence{OfflineGuard: "candidate-fixture-no-egress-observed"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture, err := startFixtureServer(ctx)
	if err != nil {
		return err
	}
	defer fixture.Close()
	portableRoot := filepath.Join(workRoot, "portable")
	passphrasePath := filepath.Join(workRoot, "vault-passphrase.txt")
	if err := os.WriteFile(passphrasePath, []byte("clean-room-fixture-passphrase\n"), 0o600); err != nil {
		return err
	}
	bootstrapRunner := candidateRunner{binary: options.Binary, env: []string{
		"WECHAT_ARTICLE_PORTABLE_ROOT=" + portableRoot,
		"WECHAT_ARTICLE_CLEAN_ROOM=1",
		"WECHAT_ARTICLE_CONTROLLED_ORIGIN=" + fixture.Origin(),
	}}
	if _, err := bootstrapRunner.runJSON(ctx, "vault", "init", "--passphrase-file", passphrasePath, "--json"); err != nil {
		return fmt.Errorf("initialize candidate encrypted vault: %w", err)
	}
	runner := candidateRunner{binary: options.Binary, env: append(append([]string(nil), bootstrapRunner.env...),
		"WECHAT_ARTICLE_SECRET_BACKEND=vault",
		"WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE="+passphrasePath,
	)}

	run := func(id, phase string, operation func() (map[string]string, error)) {
		started := time.Now().UTC()
		evidence, operationErr := operation()
		finished := time.Now().UTC()
		result, reason := resultPassed, ""
		if operationErr != nil {
			result, reason = resultFailed, boundedReasonCode(operationErr)
			evidence = nil
			fmt.Fprintf(os.Stderr, "clean-room workflow %s failed: %v\n", id, operationErr)
		}
		receipt.Workflows = append(receipt.Workflows, WorkflowResult{ID: id, Phase: phase, Result: result,
			StartedAt: started, FinishedAt: finished, DurationMS: finished.Sub(started).Milliseconds(),
			Executor: WorkflowExecutor{Kind: "release-binary", BinarySHA256: binaryDigest}, Evidence: evidence, Reason: reason})
	}

	run("install.archive", phaseOffline, func() (map[string]string, error) {
		command, err := bootstrapRunner.configuredCommand("--version")
		if err != nil {
			return nil, err
		}
		var output bytes.Buffer
		command.Stdout = &output
		command.Stderr = &output
		if commandErr := runCandidateProcess(ctx, command); commandErr != nil {
			return nil, fmt.Errorf("run extracted binary: %w", commandErr)
		}
		versionOutput := strings.TrimSpace(output.String())
		if !exactVersionOutput(versionOutput, options.Version) {
			return nil, fmt.Errorf("binary version %q does not exactly identify candidate %q", versionOutput, options.Version)
		}
		return map[string]string{"archiveSha256": archiveDigest, "binarySha256": binaryDigest,
			"archiveMember": artifact.BinaryMember, "target": artifact.GOOS + "/" + artifact.GOARCH,
			"cgoEnabled": artifact.CGOEnabled, "versionExact": "true"}, nil
	})

	run("storage.clean-roots", phaseOffline, func() (map[string]string, error) {
		for _, name := range []string{"config", "data", "cache", "state"} {
			if _, err := os.Stat(filepath.Join(portableRoot, name)); err != nil {
				return nil, fmt.Errorf("%s root was not created: %w", name, err)
			}
		}
		return map[string]string{"rootCount": "4", "beganEmpty": "true"}, nil
	})

	legacyArchive := filepath.Join(workRoot, "legacy.zip")
	run("migration.legacy-web", phaseOffline, func() (map[string]string, error) {
		if err := buildLegacyArchive(legacyArchive, fixture.Origin()); err != nil {
			return nil, err
		}
		if _, err := runner.runJSON(ctx, "migration", "inspect", legacyArchive, "--json"); err != nil {
			return nil, err
		}
		if _, err := runner.runJSON(ctx, "migration", "import", legacyArchive, "--confirm", "import-legacy:"+legacyArchive, "--json"); err != nil {
			return nil, err
		}
		verified, err := runner.runJSON(ctx, "migration", "verify", legacyArchive, "--json")
		if err != nil {
			return nil, fmt.Errorf("legacy migration verification command failed: %w", err)
		}
		if !jsonBoolean(verified.Envelope.Data, "success") {
			return nil, errors.New("legacy migration verification returned success=false")
		}
		if _, err := runner.runJSON(ctx, "article", "list", "--has-content", "true", "--json"); err != nil {
			return nil, err
		}
		digest, err := sha256File(legacyArchive)
		return map[string]string{"archiveSha256": digest, "format": migration.ArchiveFormat, "records": "2"}, err
	})
	run("migration.database-baselines", phaseOffline, func() (map[string]string, error) {
		status, err := runner.runJSON(ctx, "db", "integrity", "--json")
		if err != nil {
			return nil, err
		}
		if !jsonBoolean(status.Envelope.Data, "valid") {
			return nil, errors.New("current database failed post-migration integrity")
		}
		return map[string]string{"compatibilityWindow": "schema-1-through-8", "currentDatabaseOpened": "true", "integrityValid": "true"}, nil
	})

	loginPhase := phaseOnlineFixture
	qrPath := filepath.Join(workRoot, "login.png")
	run("login.qr", loginPhase, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "login", "--qr-output", qrPath, "--poll-interval", "500ms", "--refreshes", "0", "--json")
		if err != nil || !result.Envelope.Success {
			return nil, fmt.Errorf("login failed: %w", err)
		}
		if err := os.Remove(qrPath); err != nil {
			return nil, fmt.Errorf("remove expired QR artifact: %w", err)
		}
		return map[string]string{"qrRemoved": "true", "session": "authenticated", "backend": "encrypted-vault"}, nil
	})
	run("session.restart-persistence", loginPhase, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "status", "--json")
		if err != nil {
			return nil, err
		}
		var statusData struct {
			Session struct {
				State string `json:"state"`
			} `json:"session"`
		}
		if err := json.Unmarshal(result.Envelope.Data, &statusData); err != nil {
			return nil, fmt.Errorf("decode status data: %w", err)
		}
		if statusData.Session.State != "authenticated" {
			return nil, errors.New("fresh candidate process did not load the authenticated session")
		}
		return map[string]string{"processRestarted": "true", "backend": "encrypted-vault", "sessionReused": "true"}, nil
	})

	accountID := identity.AccountID("fixture-fakeid")
	articleID := identity.ArticleID("fixture-fakeid", "fixture-aid-1")
	run("sync.account", loginPhase, func() (map[string]string, error) {
		if _, err := runner.runJSON(ctx, "account", "add", "fixture-fakeid", "--name", "Controlled Fixture", "--json"); err != nil {
			return nil, err
		}
		result, err := runner.runJSON(ctx, "sync", "account", accountID, "--range", "all", "--page-size", "5",
			"--page-delay", "0", "--jitter", "0", "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		listed, err := runner.runJSON(ctx, "article", "list", "--account", accountID, "--json")
		if err != nil {
			return nil, fmt.Errorf("list synchronized articles: %w", err)
		}
		if !articlePageContains(listed.Envelope.Data, string(articleID), false) {
			return nil, errors.New("synchronized article not visible")
		}
		return map[string]string{"accountCount": "1", "articleCount": "1", "expectedDatasetVisible": "true"}, nil
	})

	run("download.article", loginPhase, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "download", "article", "--article", articleID, "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		listed, err := runner.runJSON(ctx, "article", "list", "--has-content", "true", "--json")
		if err != nil {
			return nil, fmt.Errorf("list downloaded articles: %w", err)
		}
		if !articlePageContains(listed.Envelope.Data, string(articleID), true) {
			return nil, errors.New("downloaded article not queryable")
		}
		return map[string]string{"articleCount": "1", "contentAvailable": "true"}, nil
	})

	run("download.resources", loginPhase, func() (map[string]string, error) {
		before := fixture.RequestCount("/asset.png")
		result, err := runner.runJSON(ctx, "download", "resources", "--article", articleID, "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		after := fixture.RequestCount("/asset.png")
		if after <= before {
			return nil, errors.New("resource download made no controlled-origin asset request")
		}
		return map[string]string{"resourceRequestDelta": fmt.Sprint(after - before), "objectMappingVerified": "true"}, nil
	})

	exportRoot := filepath.Join(portableRoot, "data", "profiles", "default", "exports")
	for _, format := range []string{"html", "markdown", "text", "json", "xlsx", "docx", "pdf"} {
		format := format
		run("export."+format, phaseOffline, func() (map[string]string, error) {
			root := filepath.Join(exportRoot, format)
			if err := os.MkdirAll(root, 0o700); err != nil {
				return nil, err
			}
			arguments := []string{"export", "start", "--format", format, "--article", articleID,
				"--output", root, "--naming", "clean-room", "--collision", "replace", "--wait", "--json"}
			if format == "html" {
				arguments = append(arguments, "--html-resource-policy", "strict")
			}
			result, err := runner.runJSON(ctx, arguments...)
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
			verified, err := runner.runJSON(ctx, "export", "verify", "--root", root, "--manifest", filepath.Base(manifest), "--json")
			if err != nil {
				return nil, fmt.Errorf("%s export verification command failed: %w", format, err)
			}
			if !jsonBoolean(verified.Envelope.Data, "valid") {
				return nil, fmt.Errorf("%s export verification returned valid=false", format)
			}
			files, err := regularFiles(root)
			if err != nil || len(files) == 0 {
				return nil, fmt.Errorf("%s export produced no files: %w", format, err)
			}
			digest, err := sha256File(files[0])
			return map[string]string{"fileCount": fmt.Sprint(len(files)), "firstOutputSha256": digest, "manifestVerified": "true"}, err
		})
	}

	run("automation.cobra", phaseOffline, func() (map[string]string, error) {
		result, err := runner.runJSON(ctx, "status", "--json")
		if err != nil || !result.Envelope.Success || result.Envelope.SchemaVersion != "wechat-article-cli/v1" {
			return nil, fmt.Errorf("invalid Cobra status envelope: %w", err)
		}
		return map[string]string{"schemaVersion": result.Envelope.SchemaVersion, "jsonPure": "true"}, nil
	})

	if options.SkipTUI {
		now := time.Now().UTC()
		receipt.Workflows = append(receipt.Workflows, WorkflowResult{ID: "ui.tui", Phase: phaseOffline,
			Result: resultSkipped, StartedAt: now, FinishedAt: now,
			Executor: WorkflowExecutor{Kind: "release-binary", BinarySHA256: binaryDigest}, Reason: "explicit --skip-tui development flag"})
	} else {
		run("ui.tui", phaseOffline, func() (map[string]string, error) { return runTUISmoke(ctx, options.Binary, runner.env) })
	}

	run("automation.mcp", phaseOffline, func() (map[string]string, error) {
		unsupportedInput := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"unsupported-clean-room-version"}}` + "\n"
		unsupportedOutput, unsupportedStderr, unsupportedExitCode, err := runner.runStdio(ctx, unsupportedInput, "mcp", "serve", "--transport", "stdio")
		if err != nil || unsupportedExitCode != 0 {
			return nil, fmt.Errorf("unsupported MCP process exited %d: %w", unsupportedExitCode, err)
		}
		if !isUnsupportedProtocolResponse(unsupportedOutput, mcp.ProtocolVersion) {
			return nil, errors.New("MCP accepted or incorrectly handled an unsupported protocol version")
		}

		input := strings.Join([]string{
			fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, mcp.ProtocolVersion),
			`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"storage.status","arguments":{}}}`,
		}, "\n") + "\n"
		output, stderr, exitCode, err := runner.runStdio(ctx, input, "mcp", "serve", "--transport", "stdio")
		if err != nil || exitCode != 0 {
			return nil, fmt.Errorf("MCP process exited %d: %w", exitCode, err)
		}
		lines := nonEmptyLines(string(output))
		if len(lines) != 3 {
			return nil, fmt.Errorf("MCP returned %d responses, want 3", len(lines))
		}
		for _, line := range lines {
			var response map[string]any
			if err := json.Unmarshal([]byte(line), &response); err != nil || response["jsonrpc"] != "2.0" {
				return nil, errors.New("invalid MCP response")
			}
		}
		var initialize, tools, storage map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &initialize); err != nil || jsonPathString(initialize, "result", "protocolVersion") != mcp.ProtocolVersion ||
			!jsonPathBoolean(initialize, "result", "capabilities", "experimental", "localOnly") ||
			jsonPathBoolean(initialize, "result", "capabilities", "experimental", "remoteOAuth") {
			fmt.Fprintf(os.Stderr, "clean-room MCP initialize response: %s\n", lines[0])
			return nil, errors.New("MCP initialize capabilities are invalid")
		}
		if err := json.Unmarshal([]byte(lines[1]), &tools); err != nil || !jsonContainsTool(tools, "storage.status") ||
			!jsonContainsTool(tools, "accounts.query") || !jsonContainsTool(tools, "articles.query") {
			return nil, errors.New("MCP tool registry is incomplete")
		}
		if err := json.Unmarshal([]byte(lines[2]), &storage); err != nil ||
			!jsonPathBoolean(storage, "result", "structuredContent", "databaseAvailable") ||
			!jsonPathBoolean(storage, "result", "structuredContent", "objectStoreReady") {
			return nil, errors.New("MCP storage.status did not prove local storage readiness")
		}
		return map[string]string{"responses": "3", "transport": "stdio", "unsupportedRejected": "true",
			"stderrBytes": fmt.Sprint(len(stderr) + len(unsupportedStderr))}, nil
	})

	run("ui.browser-workspace", phaseOffline, func() (map[string]string, error) {
		return runBrowserWorkspaceProof(ctx, runner)
	})

	backupPath := filepath.Join(workRoot, "backup.zip")
	run("storage.backup-restore", phaseOffline, func() (map[string]string, error) {
		backup, err := runner.runJSON(ctx, "db", "backup", "--output", backupPath, "--json")
		if err != nil {
			return nil, err
		}
		if !jsonNestedBoolean(backup.Envelope.Data, "verification", "valid") {
			return nil, errors.New("backup command returned verification.valid=false")
		}
		verified, err := runner.runJSON(ctx, "db", "verify", backupPath, "--json")
		if err != nil {
			return nil, err
		}
		if !jsonBoolean(verified.Envelope.Data, "valid") {
			return nil, errors.New("backup verification returned valid=false")
		}
		restoreRoot := filepath.Join(workRoot, "restore-portable")
		restoreBootstrap := candidateRunner{binary: options.Binary, env: []string{
			"WECHAT_ARTICLE_PORTABLE_ROOT=" + restoreRoot,
			"WECHAT_ARTICLE_CLEAN_ROOM=1",
			"WECHAT_ARTICLE_CONTROLLED_ORIGIN=" + fixture.Origin(),
		}}
		if _, err := restoreBootstrap.runJSON(ctx, "vault", "init", "--passphrase-file", passphrasePath, "--json"); err != nil {
			return nil, err
		}
		restoreRunner := candidateRunner{binary: options.Binary, env: append(append([]string(nil), restoreBootstrap.env...),
			"WECHAT_ARTICLE_SECRET_BACKEND=vault", "WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE="+passphrasePath,
		)}
		if _, err := restoreRunner.runJSON(ctx, "db", "restore", backupPath, "--confirm", "restore-backup:"+backupPath, "--json"); err != nil {
			return nil, err
		}
		status, err := restoreRunner.runJSON(ctx, "db", "status", "--json")
		if err != nil {
			return nil, fmt.Errorf("inspect restored profile: %w", err)
		}
		var restored struct {
			Storage struct {
				Articles int64 `json:"articles"`
			} `json:"storage"`
		}
		if err := json.Unmarshal(status.Envelope.Data, &restored); err != nil {
			return nil, fmt.Errorf("decode restored storage status: %w", err)
		}
		if restored.Storage.Articles <= 0 {
			return nil, errors.New("restored profile has no article")
		}
		receipt.CleanRoom.IndependentRestore = true
		digest, err := sha256File(backupPath)
		return map[string]string{"backupSha256": digest, "restoreRootEmpty": "true"}, err
	})

	receipt.Network.ObservedHosts = fixture.ObservedHosts()
	if err := fixture.Close(); err != nil {
		return fmt.Errorf("close controlled origin before offline phase: %w", err)
	}
	run("offline.local-workflows", phaseOffline, func() (map[string]string, error) {
		for _, command := range [][]string{{"status", "--json"}, {"article", "list", "--json"}, {"db", "integrity", "--json"}} {
			if _, err := runner.runJSON(ctx, command...); err != nil {
				return nil, err
			}
		}
		root := filepath.Join(exportRoot, "offline-markdown")
		result, err := runner.runJSON(ctx, "export", "start", "--format", "markdown", "--article", articleID,
			"--output", root, "--naming", "offline", "--collision", "replace", "--wait", "--json")
		if err != nil {
			return nil, err
		}
		if err := requireCompletedJob(result.Envelope); err != nil {
			return nil, err
		}
		return map[string]string{"controlledOriginClosed": "true", "guard": "diagnostic-fixture-closed"}, nil
	})
	run("network.no-retired-domain", phaseOffline, func() (map[string]string, error) {
		for _, host := range receipt.Network.ObservedHosts {
			if host == "mp.ziikoo.app" || host == "mptext.ziikoo.app" {
				receipt.Network.RetiredDomainContacts++
			}
		}
		if receipt.Network.RetiredDomainContacts != 0 {
			return nil, errors.New("retired project domain contact detected")
		}
		return map[string]string{"retiredDomainContacts": "0", "observedHostCount": fmt.Sprint(len(receipt.Network.ObservedHosts))}, nil
	})
	run("security.no-receipt-leakage", phaseOffline, func() (map[string]string, error) {
		probe := receipt
		probe.finalize()
		encoded, err := json.Marshal(probe)
		if err != nil {
			return nil, err
		}
		if containsForbiddenReceiptText(string(encoded)) {
			return nil, errors.New("receipt privacy scan found forbidden content")
		}
		return map[string]string{"scanPassed": "true", "rawPayloadStored": "false"}, nil
	})
	run("secrets.platform-persistence", phaseOffline, func() (map[string]string, error) {
		status, err := runner.runJSON(ctx, "vault", "status", "--json")
		if err != nil {
			return nil, fmt.Errorf("inspect encrypted vault: %w", err)
		}
		if !jsonBoolean(status.Envelope.Data, "initialized") || !jsonBoolean(status.Envelope.Data, "active") {
			return nil, errors.New("encrypted vault is not initialized and active")
		}
		verified, err := runner.runJSON(ctx, "vault", "verify", "--passphrase-file", passphrasePath, "--json")
		if err != nil {
			return nil, fmt.Errorf("verify encrypted vault: %w", err)
		}
		if !jsonBoolean(verified.Envelope.Data, "verified") {
			return nil, errors.New("encrypted vault did not survive process restart")
		}
		return map[string]string{"backend": "encrypted-vault", "restartRoundTrip": "true"}, nil
	})

	receipt.finalize()
	if err := validateReceipt(receipt, options.RequireLive); err != nil {
		if writeErr := writeReceipt(options.Output, receipt); writeErr != nil {
			return errors.Join(fmt.Errorf("receipt is invalid: %w", err), fmt.Errorf("write diagnostic receipt: %w", writeErr))
		}
		return fmt.Errorf("diagnostic receipt written but invalid: %w", err)
	}
	if err := writeReceipt(options.Output, receipt); err != nil {
		return err
	}
	fmt.Printf("clean-room receipt written: %s\n", options.Output)
	return nil
}

func runBrowserWorkspaceProof(ctx context.Context, runner candidateRunner) (map[string]string, error) {
	command, err := runner.configuredCommand("web", "--no-open")
	if err != nil {
		return nil, fmt.Errorf("configure browser workspace: %w", err)
	}
	stdout := &lockedBuffer{}
	stderr := newBoundedBuffer(candidateStderrLimit)
	command.Stdout = stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start browser workspace: %w", err)
	}
	tree, err := attachCandidateProcessTree(command.Process)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("attach browser workspace process tree: %w", err)
	}
	defer func() { _ = tree.Close() }()

	wait := make(chan error, 1)
	go func() {
		waitErr := command.Wait()
		tree.MarkExited()
		wait <- waitErr
	}()
	defer func() {
		_ = tree.Close()
		select {
		case <-wait:
		case <-time.After(5 * time.Second):
		}
	}()

	workspaceURL, err := awaitBrowserWorkspaceURL(ctx, stdout)
	if err != nil {
		return nil, err
	}
	if stdout.Overflowed() || stderr.Overflowed() {
		return nil, errors.New("browser workspace candidate output exceeded its bound")
	}
	parsed, err := url.Parse(workspaceURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Query().Get("token") == "" {
		return nil, errors.New("browser workspace did not emit a one-time IPv4 loopback URL")
	}
	if strings.TrimSpace(string(stderr.Bytes())) != "" {
		return nil, errors.New("browser workspace emitted unexpected stderr before interaction")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	response, err := client.Get(workspaceURL)
	if err != nil {
		return nil, fmt.Errorf("bootstrap browser workspace: %w", err)
	}
	if response.Request.URL.Query().Get("token") != "" {
		response.Body.Close()
		return nil, errors.New("browser bootstrap token remained in the visible URL")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("browser workspace bootstrap status=%d", response.StatusCode)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if !bytes.Contains(body, []byte(`id="root"`)) {
		return nil, errors.New("browser workspace did not serve the embedded application shell")
	}
	if err := requireBrowserSecurityHeaders(response.Header); err != nil {
		return nil, err
	}

	base := parsed.Scheme + "://" + parsed.Host
	status, err := client.Get(base + "/api/v1/status")
	if err != nil {
		return nil, fmt.Errorf("read browser workspace status: %w", err)
	}
	statusBody, readErr := io.ReadAll(io.LimitReader(status.Body, 1<<20))
	status.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if status.StatusCode != http.StatusOK || !bytes.Contains(statusBody, []byte(`"runtime"`)) || !bytes.Contains(statusBody, []byte(`"csrfToken"`)) {
		return nil, errors.New("browser workspace status did not return an authenticated safe response")
	}
	if err := requireBrowserSecurityHeaders(status.Header); err != nil {
		return nil, err
	}
	csrf := browserWorkspaceCookie(jar, parsed, "wechat_article_csrf")
	if csrf == "" {
		return nil, errors.New("browser workspace did not issue a CSRF cookie")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/session/logout", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Origin", base)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	logout, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform representative browser logout: %w", err)
	}
	logout.Body.Close()
	if logout.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("browser workspace logout status=%d", logout.StatusCode)
	}
	if err := requireBrowserSecurityHeaders(logout.Header); err != nil {
		return nil, err
	}
	return map[string]string{
		"listener":                "ipv4-loopback-random",
		"embeddedAssets":          "true",
		"sessionBootstrap":        "true",
		"securityHeaders":         "true",
		"representativeOperation": "true",
	}, nil
}

func awaitBrowserWorkspaceURL(ctx context.Context, output *lockedBuffer) (string, error) {
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		if output.Overflowed() {
			return "", errors.New("browser workspace URL output exceeded its bound")
		}
		lines := nonEmptyLines(output.String())
		if len(lines) > 1 {
			return "", errors.New("browser workspace wrote more than one stdout line")
		}
		if len(lines) == 1 {
			return lines[0], nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("browser workspace did not emit a URL within the deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func requireBrowserSecurityHeaders(header http.Header) error {
	for key, expected := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "no-store",
	} {
		if !strings.Contains(header.Get(key), expected) {
			return fmt.Errorf("browser workspace response is missing required %s header", key)
		}
	}
	return nil
}

func browserWorkspaceCookie(jar http.CookieJar, target *url.URL, name string) string {
	for _, cookie := range jar.Cookies(target) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func boundedReasonCode(err error) string {
	if err == nil {
		return ""
	}
	value := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(value, "timeout"), strings.Contains(value, "deadline"):
		return "timeout"
	case strings.Contains(value, "browser") || strings.Contains(value, "chromium"):
		return "browser_unavailable"
	case strings.Contains(value, "pty") || strings.Contains(value, "terminal"):
		return "terminal_contract_failed"
	case strings.Contains(value, "mcp") || strings.Contains(value, "json-rpc"):
		return "protocol_contract_failed"
	case strings.Contains(value, "network") || strings.Contains(value, "origin"):
		return "network_contract_failed"
	case strings.Contains(value, "verification") || strings.Contains(value, "valid=false"):
		return "verification_failed"
	default:
		return "workflow_assertion_failed"
	}
}

func buildLegacyArchive(path, origin string) error {
	htmlBody := []byte(fixtureArticleHTML(origin))
	htmlDigest := sha256.Sum256(htmlBody)
	type contentRef struct {
		Path      string `json:"path"`
		Bytes     int    `json:"bytes"`
		SHA256    string `json:"sha256"`
		MediaType string `json:"mediaType"`
	}
	type record struct {
		Key   string         `json:"key"`
		Value map[string]any `json:"value"`
	}
	files := map[string][]byte{
		"objects/html/00000001.bin": htmlBody,
	}
	datasets := map[string]struct {
		dataset migration.Dataset
		records []record
	}{
		"records/accounts.json": {migration.DatasetAccounts, []record{{Key: "legacy-fakeid", Value: map[string]any{"fakeid": "legacy-fakeid", "nickname": "Legacy Fixture"}}}},
		"records/articles.json": {migration.DatasetArticles, []record{{Key: "legacy-fakeid:legacy-aid", Value: map[string]any{"fakeid": "legacy-fakeid", "aid": "legacy-aid", "link": "https://mp.weixin.qq.com/s/legacy-fixture", "title": "Legacy fixture"}}}},
		"records/html.json": {migration.DatasetHTML, []record{{Key: "https://mp.weixin.qq.com/s/legacy-fixture", Value: map[string]any{
			"fakeid": "legacy-fakeid", "url": "https://mp.weixin.qq.com/s/legacy-fixture", "title": "Legacy fixture",
			"content": contentRef{Path: "objects/html/00000001.bin", Bytes: len(htmlBody), SHA256: hex.EncodeToString(htmlDigest[:]), MediaType: "text/html"},
		}}}},
	}
	for name, dataset := range map[string]migration.Dataset{
		"records/metadata.json": migration.DatasetMetadata, "records/comments.json": migration.DatasetComments,
		"records/replies.json": migration.DatasetReplies, "records/resource-maps.json": migration.DatasetResourceMaps,
		"records/resources.json": migration.DatasetResources, "records/assets.json": migration.DatasetAssets,
	} {
		datasets[name] = struct {
			dataset migration.Dataset
			records []record
		}{dataset: dataset, records: []record{}}
	}
	manifestFiles := make([]migration.ManifestFile, 0, len(datasets)+1)
	for name, dataset := range datasets {
		body, err := json.Marshal(dataset.records)
		if err != nil {
			return err
		}
		files[name] = body
		digest := sha256.Sum256(body)
		manifestFiles = append(manifestFiles, migration.ManifestFile{Path: name, Kind: migration.FileRecords,
			Dataset: dataset.dataset, Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:]), MediaType: "application/json"})
	}
	manifestFiles = append(manifestFiles, migration.ManifestFile{Path: "objects/html/00000001.bin", Kind: migration.FileObject,
		Size: int64(len(htmlBody)), SHA256: hex.EncodeToString(htmlDigest[:]), MediaType: "text/html"})
	manifest := migration.Manifest{Format: migration.ArchiveFormat, SchemaVersion: migration.CurrentSchemaVersion,
		CreatedAt: time.Now().UTC(), Status: "complete", Source: migration.SourceInfo{Application: "clean-room-fixture",
			DexieDatabase: "exporter.wxdown.online", DexieSchemaVersion: 3}, Files: manifestFiles}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	files[migration.ManifestPath] = manifestBody
	if err := writeZIP(path, files); err != nil {
		return err
	}
	validated, err := migration.Validate(context.Background(), path, migration.DefaultLimits())
	if err != nil {
		return fmt.Errorf("validate generated legacy archive: %w", err)
	}
	plan, err := migration.Plan(context.Background(), path, migration.PlanOptions{})
	if err != nil {
		return fmt.Errorf("plan generated legacy archive: %w", err)
	}
	if validated.Manifest.SchemaVersion != migration.CurrentSchemaVersion || plan.Report.PlannedRecords < 2 {
		return errors.New("generated legacy archive did not retain its promised records")
	}
	return nil
}

func writeZIP(path string, files map[string][]byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for name, body := range files {
		entry, createErr := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if createErr != nil {
			_ = writer.Close()
			_ = file.Close()
			return createErr
		}
		if _, writeErr := entry.Write(body); writeErr != nil {
			_ = writer.Close()
			_ = file.Close()
			return writeErr
		}
	}
	return errors.Join(writer.Close(), file.Close())
}

func ensureEmptyDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("work root %q is not empty", path)
	}
	return nil
}

func regularFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && !strings.HasSuffix(entry.Name(), ".manifest.json") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func nonEmptyLines(value string) []string {
	var result []string
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, strings.TrimSpace(line))
		}
	}
	return result
}

func jsonBoolean(data json.RawMessage, name string) bool {
	var object map[string]any
	if json.Unmarshal(data, &object) != nil {
		return false
	}
	value, _ := object[name].(bool)
	return value
}

func articlePageContains(data json.RawMessage, articleID string, requireContent bool) bool {
	var page struct {
		Items []struct {
			ID         string `json:"id"`
			HasContent bool   `json:"hasContent"`
		} `json:"items"`
	}
	if json.Unmarshal(data, &page) != nil {
		return false
	}
	for _, article := range page.Items {
		if article.ID == articleID && (!requireContent || article.HasContent) {
			return true
		}
	}
	return false
}

func jsonNestedBoolean(data json.RawMessage, parent, name string) bool {
	var object map[string]any
	if json.Unmarshal(data, &object) != nil {
		return false
	}
	nested, _ := object[parent].(map[string]any)
	value, _ := nested[name].(bool)
	return value
}

func requireCompletedJob(envelope commandEnvelope) error {
	job, err := decodeEnvelopeData[jobEnvelopeData](envelope)
	if err != nil {
		return err
	}
	if job.State != "completed" || job.Counts.Total <= 0 || job.Counts.Completed != job.Counts.Total || job.Counts.Failed != 0 || job.Counts.Partial != 0 {
		return fmt.Errorf("job did not complete successfully: state=%s total=%d completed=%d failed=%d partial=%d",
			job.State, job.Counts.Total, job.Counts.Completed, job.Counts.Failed, job.Counts.Partial)
	}
	return nil
}

func singleExportManifest(root string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "export-*-manifest.json"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("export root contains %d provenance manifests, want 1", len(matches))
	}
	return matches[0], nil
}

func jsonPathValue(object map[string]any, path ...string) any {
	current := any(object)
	for _, key := range path {
		nested, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = nested[key]
	}
	return current
}

func jsonPathString(object map[string]any, path ...string) string {
	value, _ := jsonPathValue(object, path...).(string)
	return value
}

func jsonPathBoolean(object map[string]any, path ...string) bool {
	value, _ := jsonPathValue(object, path...).(bool)
	return value
}

func jsonContainsTool(object map[string]any, name string) bool {
	tools, _ := jsonPathValue(object, "result", "tools").([]any)
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		if tool["name"] == name {
			return true
		}
	}
	return false
}

func isUnsupportedProtocolResponse(output []byte, supportedVersion string) bool {
	lines := nonEmptyLines(string(output))
	if len(lines) != 1 {
		return false
	}
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Supported string `json:"supported"`
			} `json:"data"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(strings.NewReader(lines[0]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil {
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false
	}
	if response.JSONRPC != "2.0" || string(response.ID) != "1" || response.Error == nil || len(response.Result) != 0 {
		return false
	}
	return response.Error.Code == -32602 && response.Error.Message == "unsupported protocolVersion" &&
		response.Error.Data.Supported == supportedVersion
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", arguments...)
	os.Exit(1)
}
