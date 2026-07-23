package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateReceiptAcceptsCompleteFixtureReceipt(t *testing.T) {
	receipt := validTestReceipt()
	if err := validateReceipt(receipt, false); err != nil {
		t.Fatalf("validateReceipt: %v", err)
	}
}

func TestValidateReceiptRejectsInvalidWorkflowRegistryAndSummary(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Receipt)
		want string
	}{
		{name: "missing", edit: func(receipt *Receipt) { receipt.Workflows = receipt.Workflows[1:]; receipt.finalize() }, want: "is missing"},
		{name: "duplicate", edit: func(receipt *Receipt) {
			receipt.Workflows = append(receipt.Workflows, receipt.Workflows[0])
			receipt.finalize()
		}, want: "duplicate workflow"},
		{name: "unknown", edit: func(receipt *Receipt) { receipt.Workflows[0].ID = "unknown.workflow"; receipt.finalize() }, want: "unknown workflow"},
		{name: "skip", edit: func(receipt *Receipt) {
			receipt.Workflows[0].Result = resultSkipped
			receipt.Workflows[0].Reason = "runner_unavailable"
			receipt.finalize()
		}, want: "did not pass"},
		{name: "bad digest", edit: func(receipt *Receipt) { receipt.Artifact.BinarySHA256 = "ABC" }, want: "binarySha256"},
		{name: "retired domain", edit: func(receipt *Receipt) { receipt.Network.RetiredDomainContacts = 1 }, want: "retired project domains"},
		{name: "bad duration", edit: func(receipt *Receipt) { receipt.Workflows[0].DurationMS++ }, want: "duration does not match"},
		{name: "outside receipt interval", edit: func(receipt *Receipt) {
			receipt.Workflows[0].StartedAt = receipt.StartedAt.Add(-time.Second)
			receipt.Workflows[0].FinishedAt = receipt.StartedAt
			receipt.Workflows[0].DurationMS = 1000
		}, want: "outside the receipt interval"},
		{name: "bad summary", edit: func(receipt *Receipt) { receipt.Summary.Passed-- }, want: "summary result counts"},
		{name: "wrong phase", edit: func(receipt *Receipt) { receipt.Workflows[0].Phase = phaseOnlineFixture }, want: "phase="},
		{name: "unknown evidence", edit: func(receipt *Receipt) { receipt.Workflows[0].Evidence["unexpected"] = "true" }, want: "evidence is invalid"},
		{name: "missing evidence", edit: func(receipt *Receipt) { delete(receipt.Workflows[0].Evidence, "archiveSha256") }, want: "evidence is invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			receipt := validTestReceipt()
			test.edit(&receipt)
			err := validateReceipt(receipt, false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateReceipt error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestReadReceiptRejectsOversizedAndMultipleDocuments(t *testing.T) {
	root := t.TempDir()
	oversized := filepath.Join(root, "oversized.json")
	if err := os.WriteFile(oversized, make([]byte, maximumReceiptBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReceipt(oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized receipt error = %v", err)
	}
	multiple := filepath.Join(root, "multiple.json")
	if err := os.WriteFile(multiple, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReceipt(multiple); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("multiple receipt error = %v", err)
	}
}

func TestForbiddenReceiptScannerRejectsSensitiveEvidence(t *testing.T) {
	for _, value := range []string{
		`{"host":"mp.ziikoo.app"}`,
		`{"token":"secret"}`,
		`{"path":"/Users/example/private"}`,
		`{"url":"https://example.invalid/path?token=value"}`,
	} {
		if !containsForbiddenReceiptText(value) {
			t.Fatalf("scanner accepted %s", value)
		}
	}
}

func TestValidateReceiptRejectsFixtureLiveSubstitutionAndLeakage(t *testing.T) {
	receipt := validTestReceipt()
	receipt.Workflows[workflowIndex(receipt, "login.qr")].Phase = phaseOnlineLive
	if err := validateReceipt(receipt, false); err == nil || !strings.Contains(err.Error(), "fixture receipt cannot contain live phase") {
		t.Fatalf("fixture-live error = %v", err)
	}

	receipt = validTestReceipt()
	if err := validateReceipt(receipt, true); err == nil || !strings.Contains(err.Error(), "stable receipt requires live") {
		t.Fatalf("require-live error = %v", err)
	}

	receipt = validTestReceipt()
	receipt.Workflows[0].Evidence["payload"] = "<html>fixture payload</html>"
	if !containsForbiddenReceiptText("<html>fixture payload</html>") {
		t.Fatal("forbidden-text scanner did not detect HTML")
	}
	encoded, marshalErr := json.Marshal(receipt)
	if marshalErr != nil || !containsForbiddenReceiptText(string(encoded)) {
		t.Fatalf("encoded receipt scanner mismatch: err=%v body=%s", marshalErr, encoded)
	}
	if err := validateReceipt(receipt, false); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("leakage error = %v", err)
	}
}

func TestValidateReceiptRequiresLiveRunnerIdentity(t *testing.T) {
	receipt := validTestReceipt()
	receipt.Mode = modeLive
	receipt.Source.Repository = "vmxmy/wechat-article-exporter"
	receipt.Platform.RunnerOS = "Linux"
	receipt.Platform.RunnerArch = "X64"
	for index, workflow := range receipt.Workflows {
		contract, _ := workflowContractByID(workflow.ID)
		if contract.LiveEvidence {
			receipt.Workflows[index].Phase = phaseOnlineLive
		}
	}
	if err := validateReceipt(receipt, true); err != nil {
		t.Fatalf("valid live runner identity: %v", err)
	}
	receipt.Platform.RunnerArch = "ARM64"
	if err := validateReceipt(receipt, true); err == nil || !strings.Contains(err.Error(), "runner identity") {
		t.Fatalf("mismatched runner identity error = %v", err)
	}
}

func validTestReceipt() Receipt {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("clean-room-test"))
	sha := hex.EncodeToString(digest[:])
	receipt := Receipt{
		SchemaVersion: receiptSchemaVersion,
		ReceiptID:     "fixture-receipt",
		Mode:          modeFixture,
		StartedAt:     now,
		Platform:      PlatformEvidence{GOOS: "linux", GOARCH: "amd64", Native: true},
		Source:        SourceEvidence{Tag: "wechat-article-v2.0.1", Commit: "0123456789012345678901234567890123456789", Version: "2.0.1"},
		Artifact: ArtifactEvidence{ArchiveName: "candidate.tar.gz", ArchiveSHA256: sha, ArchiveMember: "candidate/wechat-article",
			BinaryName: "wechat-article", BinarySHA256: sha, BuildInfoSHA256: sha, ChecksumManifestSHA256: sha,
			SBOMName: "candidate.sbom.cdx.json", SBOMSHA256: sha, TargetGOOS: "linux", TargetGOARCH: "amd64",
			CGOEnabled: "0", Module: "github.com/wechat-article/wechat-article-exporter/cli",
			BuildCommit: "0123456789012345678901234567890123456789", ExecutorKind: "release-binary"},
		CleanRoom: CleanRoomEvidence{RootCount: 4, RootsBeganEmpty: true, IndependentRestore: true},
		Network:   NetworkEvidence{OfflineGuard: "deny-all", RetiredDomainContacts: 0},
	}
	for index, contract := range workflowContracts {
		start := now.Add(time.Duration(index) * time.Second)
		phase := contract.Phase
		if contract.LiveEvidence {
			phase = phaseOnlineFixture
		}
		receipt.Workflows = append(receipt.Workflows, WorkflowResult{
			ID: contract.ID, Phase: phase, Result: resultPassed, StartedAt: start, FinishedAt: start.Add(time.Millisecond),
			DurationMS: 1, Executor: WorkflowExecutor{Kind: "release-binary", BinarySHA256: sha}, Evidence: testWorkflowEvidence(contract, sha),
		})
	}
	receipt.finalize()
	return receipt
}

func testWorkflowEvidence(contract workflowContract, sha string) map[string]string {
	values := make(map[string]string, len(contract.Evidence))
	for name, constraint := range contract.Evidence {
		if constraint.Equals != "" {
			values[name] = constraint.Equals
			continue
		}
		switch constraint.Rule {
		case evidenceBoolean:
			values[name] = "true"
		case evidenceCount:
			if constraint.Minimum > 0 {
				values[name] = fmt.Sprint(constraint.Minimum)
			} else {
				values[name] = "0"
			}
		case evidenceDigest:
			values[name] = sha
		case evidenceText:
			values[name] = "verified"
		}
	}
	return values
}

func workflowIndex(receipt Receipt, id string) int {
	for index, workflow := range receipt.Workflows {
		if workflow.ID == id {
			return index
		}
	}
	return -1
}
