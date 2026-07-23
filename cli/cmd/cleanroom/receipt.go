package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	receiptSchemaVersion = "wechat-article-clean-room-platform/v1"
	modeFixture          = "fixture"
	modeLive             = "live"
	phaseOnlineFixture   = "online-fixture"
	phaseOnlineLive      = "online-live"
	phaseOffline         = "offline"
	resultPassed         = "pass"
	resultFailed         = "fail"
	resultSkipped        = "skip"
	maximumReceiptBytes  = 4 << 20
)

type workflowContract struct {
	ID           string
	LiveEvidence bool
	Phase        string
}

var workflowContracts = []workflowContract{
	{ID: "install.archive", Phase: phaseOffline},
	{ID: "storage.clean-roots", Phase: phaseOffline},
	{ID: "migration.legacy-web", Phase: phaseOffline},
	{ID: "migration.database-baselines", Phase: phaseOffline},
	{ID: "login.qr", LiveEvidence: true},
	{ID: "session.restart-persistence", LiveEvidence: true},
	{ID: "sync.account", LiveEvidence: true},
	{ID: "download.article", LiveEvidence: true},
	{ID: "download.resources", LiveEvidence: true},
	{ID: "export.html", Phase: phaseOffline},
	{ID: "export.markdown", Phase: phaseOffline},
	{ID: "export.text", Phase: phaseOffline},
	{ID: "export.json", Phase: phaseOffline},
	{ID: "export.xlsx", Phase: phaseOffline},
	{ID: "export.docx", Phase: phaseOffline},
	{ID: "export.pdf", Phase: phaseOffline},
	{ID: "automation.cobra", Phase: phaseOffline},
	{ID: "ui.tui", Phase: phaseOffline},
	{ID: "automation.mcp", Phase: phaseOffline},
	{ID: "storage.backup-restore", Phase: phaseOffline},
	{ID: "offline.local-workflows", Phase: phaseOffline},
	{ID: "network.no-retired-domain", Phase: phaseOffline},
	{ID: "security.no-receipt-leakage", Phase: phaseOffline},
	{ID: "secrets.platform-persistence", Phase: phaseOffline},
}

const receiptSetSchemaVersion = "wechat-article-clean-room-release-set/v1"

var requiredTargetTuples = []string{
	"darwin/arm64",
	"darwin/amd64",
	"linux/arm64",
	"linux/amd64",
	"windows/amd64",
}

type Receipt struct {
	SchemaVersion string            `json:"schemaVersion"`
	ReceiptID     string            `json:"receiptId"`
	Mode          string            `json:"mode"`
	StartedAt     time.Time         `json:"startedAt"`
	FinishedAt    time.Time         `json:"finishedAt"`
	Platform      PlatformEvidence  `json:"platform"`
	Source        SourceEvidence    `json:"source"`
	Artifact      ArtifactEvidence  `json:"artifact"`
	CleanRoom     CleanRoomEvidence `json:"cleanRoom"`
	Network       NetworkEvidence   `json:"network"`
	Workflows     []WorkflowResult  `json:"workflows"`
	Summary       ReceiptSummary    `json:"summary"`
}

type PlatformEvidence struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	Native     bool   `json:"native"`
	RunnerOS   string `json:"runnerOs,omitempty"`
	RunnerArch string `json:"runnerArch,omitempty"`
}

type SourceEvidence struct {
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Commit     string `json:"commit"`
	Version    string `json:"version"`
}

type ArtifactEvidence struct {
	ArchiveName            string `json:"archiveName"`
	ArchiveSHA256          string `json:"archiveSha256"`
	ArchiveMember          string `json:"archiveMember"`
	BinaryName             string `json:"binaryName"`
	BinarySHA256           string `json:"binarySha256"`
	BuildInfoSHA256        string `json:"buildInfoSha256"`
	ChecksumManifestSHA256 string `json:"checksumManifestSha256"`
	SBOMName               string `json:"sbomName"`
	SBOMSHA256             string `json:"sbomSha256"`
	TargetGOOS             string `json:"targetGoos"`
	TargetGOARCH           string `json:"targetGoarch"`
	CGOEnabled             string `json:"cgoEnabled"`
	Module                 string `json:"module"`
	BuildCommit            string `json:"buildCommit"`
	ExecutorKind           string `json:"executorKind"`
}

type CleanRoomEvidence struct {
	RootCount          int  `json:"rootCount"`
	RootsBeganEmpty    bool `json:"rootsBeganEmpty"`
	IndependentRestore bool `json:"independentRestore"`
}

type NetworkEvidence struct {
	ObservedHosts         []string `json:"observedHosts,omitempty"`
	RetiredDomainContacts int      `json:"retiredDomainContacts"`
	OfflineGuard          string   `json:"offlineGuard"`
}

type WorkflowResult struct {
	ID         string            `json:"id"`
	Phase      string            `json:"phase"`
	Result     string            `json:"result"`
	StartedAt  time.Time         `json:"startedAt"`
	FinishedAt time.Time         `json:"finishedAt"`
	DurationMS int64             `json:"durationMs"`
	Executor   WorkflowExecutor  `json:"executor"`
	Evidence   map[string]string `json:"evidence,omitempty"`
	Reason     string            `json:"reason,omitempty"`
}

type WorkflowExecutor struct {
	Kind         string `json:"kind"`
	BinarySHA256 string `json:"binarySha256"`
}

type ReceiptSummary struct {
	Passed  int  `json:"passed"`
	Failed  int  `json:"failed"`
	Skipped int  `json:"skipped"`
	Valid   bool `json:"valid"`
}

func (receipt *Receipt) finalize() {
	receipt.FinishedAt = time.Now().UTC()
	receipt.Summary = summarizeWorkflows(receipt.Workflows)
}

func summarizeWorkflows(workflows []WorkflowResult) ReceiptSummary {
	summary := ReceiptSummary{}
	for _, workflow := range workflows {
		switch workflow.Result {
		case resultPassed:
			summary.Passed++
		case resultFailed:
			summary.Failed++
		case resultSkipped:
			summary.Skipped++
		}
	}
	summary.Valid = summary.Failed == 0 && summary.Skipped == 0
	return summary
}

func workflowContractByID(id string) (workflowContract, bool) {
	for _, contract := range workflowContracts {
		if contract.ID == id {
			return contract, true
		}
	}
	return workflowContract{}, false
}

func validateReceipt(receipt Receipt, requireLive bool) error {
	var problems []error
	if receipt.SchemaVersion != receiptSchemaVersion {
		problems = append(problems, fmt.Errorf("schemaVersion=%q, want %q", receipt.SchemaVersion, receiptSchemaVersion))
	}
	if strings.TrimSpace(receipt.ReceiptID) == "" {
		problems = append(problems, errors.New("receiptId is required"))
	}
	if receipt.Mode != modeFixture && receipt.Mode != modeLive {
		problems = append(problems, fmt.Errorf("mode=%q, want fixture or live", receipt.Mode))
	}
	if receipt.StartedAt.IsZero() || receipt.FinishedAt.IsZero() || receipt.FinishedAt.Before(receipt.StartedAt) {
		problems = append(problems, errors.New("receipt timestamps are incomplete or out of order"))
	}
	if !receipt.Platform.Native || receipt.Platform.GOOS == "" || receipt.Platform.GOARCH == "" {
		problems = append(problems, errors.New("native platform evidence is required"))
	}
	if receipt.Source.Commit == "" || receipt.Source.Version == "" {
		problems = append(problems, errors.New("source commit and version are required"))
	}
	for label, digest := range map[string]string{
		"archiveSha256":          receipt.Artifact.ArchiveSHA256,
		"binarySha256":           receipt.Artifact.BinarySHA256,
		"buildInfoSha256":        receipt.Artifact.BuildInfoSHA256,
		"checksumManifestSha256": receipt.Artifact.ChecksumManifestSHA256,
		"sbomSha256":             receipt.Artifact.SBOMSHA256,
	} {
		if !validSHA256(digest) {
			problems = append(problems, fmt.Errorf("%s is not a SHA-256 digest", label))
		}
	}
	if receipt.Artifact.ExecutorKind != "release-binary" {
		problems = append(problems, errors.New("artifact executorKind must be release-binary"))
	}
	if receipt.Artifact.ArchiveMember == "" || receipt.Artifact.SBOMName == "" ||
		receipt.Artifact.Module != "github.com/wechat-article/wechat-article-exporter/cli" ||
		receipt.Artifact.CGOEnabled != "0" || receipt.Artifact.TargetGOOS != receipt.Platform.GOOS ||
		receipt.Artifact.TargetGOARCH != receipt.Platform.GOARCH || receipt.Artifact.BuildCommit != receipt.Source.Commit {
		problems = append(problems, errors.New("artifact provenance does not match the source and native target"))
	}
	if requireLive && receipt.Source.Tag != "wechat-article-v"+receipt.Source.Version {
		problems = append(problems, errors.New("stable receipt release tag does not match the exact version"))
	}
	if receipt.CleanRoom.RootCount < 4 || !receipt.CleanRoom.RootsBeganEmpty {
		problems = append(problems, errors.New("clean config/data/cache/state roots were not proven"))
	}
	if !receipt.CleanRoom.IndependentRestore {
		problems = append(problems, errors.New("independent restore was not proven"))
	}
	if receipt.Network.RetiredDomainContacts != 0 {
		problems = append(problems, errors.New("retired project domains were contacted"))
	}
	if strings.TrimSpace(receipt.Network.OfflineGuard) == "" {
		problems = append(problems, errors.New("offline network guard evidence is required"))
	}

	seen := make(map[string]WorkflowResult, len(receipt.Workflows))
	for _, workflow := range receipt.Workflows {
		contract, ok := workflowContractByID(workflow.ID)
		if !ok {
			problems = append(problems, fmt.Errorf("unknown workflow %q", workflow.ID))
		} else if contract.LiveEvidence {
			expectedPhase := phaseOnlineFixture
			if receipt.Mode == modeLive {
				expectedPhase = phaseOnlineLive
			}
			if workflow.Phase != expectedPhase {
				problems = append(problems, fmt.Errorf("workflow %q phase=%q, want %q", workflow.ID, workflow.Phase, expectedPhase))
			}
		} else if workflow.Phase != contract.Phase {
			problems = append(problems, fmt.Errorf("workflow %q phase=%q, want %q", workflow.ID, workflow.Phase, contract.Phase))
		}
		if _, duplicate := seen[workflow.ID]; duplicate {
			problems = append(problems, fmt.Errorf("duplicate workflow %q", workflow.ID))
		}
		seen[workflow.ID] = workflow
		if workflow.Result != resultPassed && workflow.Result != resultFailed && workflow.Result != resultSkipped {
			problems = append(problems, fmt.Errorf("workflow %q has invalid result %q", workflow.ID, workflow.Result))
		}
		if workflow.Executor.Kind != "release-binary" || workflow.Executor.BinarySHA256 != receipt.Artifact.BinarySHA256 {
			problems = append(problems, fmt.Errorf("workflow %q is not bound to the release binary", workflow.ID))
		}
		if workflow.StartedAt.IsZero() || workflow.FinishedAt.IsZero() || workflow.FinishedAt.Before(workflow.StartedAt) {
			problems = append(problems, fmt.Errorf("workflow %q has invalid timestamps", workflow.ID))
		}
		expectedDuration := workflow.FinishedAt.Sub(workflow.StartedAt).Milliseconds()
		if workflow.DurationMS < 0 || workflow.DurationMS != expectedDuration {
			problems = append(problems, fmt.Errorf("workflow %q duration does not match timestamps", workflow.ID))
		}
		if workflow.Result == resultSkipped && strings.TrimSpace(workflow.Reason) == "" {
			problems = append(problems, fmt.Errorf("workflow %q skip reason is required", workflow.ID))
		}
	}
	for _, contract := range workflowContracts {
		workflow, ok := seen[contract.ID]
		if !ok {
			problems = append(problems, fmt.Errorf("required workflow %q is missing", contract.ID))
			continue
		}
		if workflow.Result != resultPassed {
			problems = append(problems, fmt.Errorf("required workflow %q did not pass (%s)", contract.ID, workflow.Result))
		}
		if requireLive && contract.LiveEvidence && (workflow.Phase != phaseOnlineLive || workflow.Result != resultPassed) {
			problems = append(problems, fmt.Errorf("stable receipt requires live controlled-account evidence for %q", contract.ID))
		}
	}
	if receipt.Mode == modeFixture {
		for _, workflow := range receipt.Workflows {
			if workflow.Phase == phaseOnlineLive {
				problems = append(problems, fmt.Errorf("fixture receipt cannot contain live phase for %q", workflow.ID))
			}
		}
	}
	if receipt.Mode == modeLive {
		for _, contract := range workflowContracts {
			if !contract.LiveEvidence {
				continue
			}
			if workflow, ok := seen[contract.ID]; !ok || workflow.Phase != phaseOnlineLive {
				problems = append(problems, fmt.Errorf("live receipt requires live phase for %q", contract.ID))
			}
		}
	}
	if receipt.Summary != summarizeWorkflows(receipt.Workflows) {
		problems = append(problems, errors.New("summary result counts are inconsistent"))
	}
	if receipt.Mode == "fixture" && requireLive {
		problems = append(problems, errors.New("stable receipt cannot use fixture mode"))
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		problems = append(problems, err)
	} else if containsForbiddenReceiptText(string(encoded)) {
		problems = append(problems, errors.New("receipt contains forbidden secret, article-body, or retired-domain text"))
	}
	return errors.Join(problems...)
}

func writeReceipt(path string, receipt Receipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".clean-room-receipt-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(receipt); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func readReceipt(path string) (Receipt, error) {
	body, err := readBoundedRegularFile(path, maximumReceiptBytes)
	if err != nil {
		return Receipt{}, err
	}
	return decodeReceipt(body)
}

func decodeReceipt(body []byte) (Receipt, error) {
	var receipt Receipt
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return Receipt{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Receipt{}, errors.New("receipt contains multiple JSON values")
		}
		return Receipt{}, err
	}
	return receipt, nil
}

type ReceiptSet struct {
	SchemaVersion string                `json:"schemaVersion"`
	Release       ReceiptSetRelease     `json:"release"`
	Receipts      []ReceiptSetReference `json:"receipts"`
	Summary       ReceiptSetSummary     `json:"summary"`
}

type ReceiptSetRelease struct {
	Repository string `json:"repository,omitempty"`
	Commit     string `json:"commit"`
	Version    string `json:"version"`
}

type ReceiptSetReference struct {
	Target        string `json:"target"`
	ReceiptPath   string `json:"receiptPath"`
	ReceiptSHA256 string `json:"receiptSha256"`
	ArchiveSHA256 string `json:"archiveSha256"`
}

type ReceiptSetSummary struct {
	RequiredTargets int    `json:"requiredTargets"`
	PassedTargets   int    `json:"passedTargets"`
	MissingTargets  int    `json:"missingTargets"`
	GateStatus      string `json:"gateStatus"`
}

func validateReceiptSet(receiptSet ReceiptSet, baseDirectory string) error {
	var problems []error
	if receiptSet.SchemaVersion != receiptSetSchemaVersion {
		problems = append(problems, fmt.Errorf("schemaVersion=%q, want %q", receiptSet.SchemaVersion, receiptSetSchemaVersion))
	}
	if receiptSet.Release.Commit == "" || receiptSet.Release.Version == "" {
		problems = append(problems, errors.New("release commit and version are required"))
	}
	required := make(map[string]struct{}, len(requiredTargetTuples))
	for _, target := range requiredTargetTuples {
		required[target] = struct{}{}
	}
	seen := make(map[string]struct{}, len(receiptSet.Receipts))
	passed := 0
	for _, reference := range receiptSet.Receipts {
		if _, ok := required[reference.Target]; !ok {
			problems = append(problems, fmt.Errorf("unsupported target %q", reference.Target))
		}
		if _, duplicate := seen[reference.Target]; duplicate {
			problems = append(problems, fmt.Errorf("duplicate target %q", reference.Target))
		}
		seen[reference.Target] = struct{}{}
		if !validSHA256(reference.ReceiptSHA256) || !validSHA256(reference.ArchiveSHA256) {
			problems = append(problems, fmt.Errorf("target %q contains an invalid digest", reference.Target))
			continue
		}
		path, err := receiptReferencePath(baseDirectory, reference.ReceiptPath)
		if err != nil {
			problems = append(problems, fmt.Errorf("target %q receipt path is unsafe", reference.Target))
			continue
		}
		body, err := readBoundedRegularFile(path, maximumReceiptBytes)
		digest := sha256Bytes(body)
		if err != nil || digest != reference.ReceiptSHA256 {
			problems = append(problems, fmt.Errorf("target %q receipt digest mismatch", reference.Target))
			continue
		}
		receipt, err := decodeReceipt(body)
		if err != nil || validateReceipt(receipt, true) != nil {
			problems = append(problems, fmt.Errorf("target %q platform receipt is invalid", reference.Target))
			continue
		}
		actualTarget := receipt.Platform.GOOS + "/" + receipt.Platform.GOARCH
		if actualTarget != reference.Target || receipt.Source.Commit != receiptSet.Release.Commit ||
			receipt.Source.Version != receiptSet.Release.Version || receipt.Source.Repository != receiptSet.Release.Repository ||
			receipt.Artifact.ArchiveSHA256 != reference.ArchiveSHA256 {
			problems = append(problems, fmt.Errorf("target %q release identity mismatch", reference.Target))
			continue
		}
		passed++
	}
	missing := len(requiredTargetTuples) - len(seen)
	if missing != 0 {
		problems = append(problems, fmt.Errorf("release set is missing %d required target(s)", missing))
	}
	if receiptSet.Summary.RequiredTargets != len(requiredTargetTuples) || receiptSet.Summary.PassedTargets != passed ||
		receiptSet.Summary.MissingTargets != missing || receiptSet.Summary.GateStatus != "pass" || passed != len(requiredTargetTuples) {
		problems = append(problems, errors.New("release-set summary is inconsistent or not passing"))
	}
	return errors.Join(problems...)
}

func readReceiptSet(path string) (ReceiptSet, error) {
	body, err := readBoundedRegularFile(path, maximumReceiptBytes)
	if err != nil {
		return ReceiptSet{}, err
	}
	var receiptSet ReceiptSet
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receiptSet); err != nil {
		return ReceiptSet{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ReceiptSet{}, errors.New("receipt set contains multiple JSON values")
		}
		return ReceiptSet{}, err
	}
	return receiptSet, nil
}

func receiptReferencePath(baseDirectory, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" || filepath.IsAbs(reference) || filepath.Clean(reference) != reference {
		return "", errors.New("receipt path must be a clean relative path")
	}
	cleanBase, err := filepath.Abs(baseDirectory)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filepath.Join(cleanBase, reference))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(cleanBase, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("receipt path escapes the receipt-set directory")
	}
	return resolved, nil
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("receipt must not be a symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("receipt must be a regular non-symlink file")
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("receipt exceeds %d bytes", limit)
	}
	return body, nil
}

func sha256Bytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsForbiddenReceiptText(value string) bool {
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"mp.ziikoo.app", "mptext.ziikoo.app", "pass_ticket", "wap_sid2", "appmsg_token",
		`"authorization"`, `"cookie"`, `"cookies"`, `"set-cookie"`, `"token"`, `"sessionvalue"`,
		`"qrbytes"`, `"qrdata"`, `"articlebody"`, `"articlehtml"`, "authorization:", "set-cookie", "cookie:",
		"window.cgidata", "window.cgidatanew", "<html", "<body", `\u003chtml`, `\u003cbody`,
	} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	for _, marker := range []string{`"path":"/`, `"path":"\\`, `file:///`, `c:\\users\\`, `/users/`, `/home/`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, queryKey := range []string{"token=", "key=", "pass_ticket=", "auth=", "code="} {
		if strings.Contains(lower, "?"+queryKey) || strings.Contains(lower, "&"+queryKey) {
			return true
		}
	}
	return false
}
