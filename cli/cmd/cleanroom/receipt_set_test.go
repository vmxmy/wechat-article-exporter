package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReceiptSetRequiresAllFiveLiveTargets(t *testing.T) {
	root := t.TempDir()
	set := ReceiptSet{
		SchemaVersion: receiptSetSchemaVersion,
		Release: ReceiptSetRelease{Repository: "vmxmy/wechat-article-exporter", Tag: "wechat-article-v2.0.1",
			Commit: "0123456789012345678901234567890123456789", Version: "2.0.1", ChecksumManifestSHA256: validTestReceipt().Artifact.ChecksumManifestSHA256},
		Summary: ReceiptSetSummary{RequiredTargets: 5, PassedTargets: 5, GateStatus: "pass"},
	}
	for _, target := range requiredTargetTuples {
		receipt := validTestReceipt()
		receipt.Mode = modeLive
		receipt.Source.Repository = set.Release.Repository
		receipt.Source.Tag = "wechat-article-v" + set.Release.Version
		receipt.Source.Commit = set.Release.Commit
		receipt.Source.Version = set.Release.Version
		parts := strings.Split(target, "/")
		receipt.Platform.GOOS, receipt.Platform.GOARCH = parts[0], parts[1]
		receipt.Platform.RunnerOS, receipt.Platform.RunnerArch = parts[0], parts[1]
		receipt.Artifact.TargetGOOS, receipt.Artifact.TargetGOARCH = parts[0], parts[1]
		for index := range receipt.Workflows {
			contract, ok := workflowContractByID(receipt.Workflows[index].ID)
			if ok && contract.LiveEvidence {
				receipt.Workflows[index].Phase = phaseOnlineLive
			}
		}
		receipt.finalize()
		body, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, strings.ReplaceAll(target, "/", "-")+".json")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		digest, err := sha256File(path)
		if err != nil {
			t.Fatal(err)
		}
		set.Receipts = append(set.Receipts, ReceiptSetReference{
			Target: target, ReceiptPath: filepath.Base(path), ReceiptSHA256: digest, ArchiveSHA256: receipt.Artifact.ArchiveSHA256,
		})
	}
	if err := validateReceiptSet(set, root); err != nil {
		t.Fatalf("validateReceiptSet: %v", err)
	}

	set.Receipts = set.Receipts[1:]
	set.Summary.PassedTargets = 4
	set.Summary.MissingTargets = 1
	set.Summary.GateStatus = "incomplete"
	if err := validateReceiptSet(set, root); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing-target error = %v", err)
	}
}

func TestValidateReceiptSetRejectsUnsafeReferencePaths(t *testing.T) {
	set, root := completeTestReceiptSet(t)
	set.Receipts[0].ReceiptPath = "../escape.json"
	if err := validateReceiptSet(set, root); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("traversal error = %v", err)
	}

	set, root = completeTestReceiptSet(t)
	target := filepath.Join(root, set.Receipts[0].ReceiptPath)
	real := filepath.Join(root, "real.json")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateReceiptSet(set, root); err == nil || !strings.Contains(err.Error(), "path is unsafe") {
		t.Fatalf("symlink error = %v", err)
	}
}

func TestValidateReceiptSetRejectsInternalSymlinkReference(t *testing.T) {
	set, root := completeTestReceiptSet(t)
	target := filepath.Join(root, set.Receipts[0].ReceiptPath)
	real := filepath.Join(root, "real.json")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(real), target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := validateReceiptSet(set, root); err == nil || !strings.Contains(err.Error(), "path is unsafe") {
		t.Fatalf("internal symlink error = %v", err)
	}
}

func completeTestReceiptSet(t *testing.T) (ReceiptSet, string) {
	t.Helper()
	root := t.TempDir()
	set := ReceiptSet{
		SchemaVersion: receiptSetSchemaVersion,
		Release: ReceiptSetRelease{Repository: "vmxmy/wechat-article-exporter", Tag: "wechat-article-v2.0.1",
			Commit: "0123456789012345678901234567890123456789", Version: "2.0.1", ChecksumManifestSHA256: validTestReceipt().Artifact.ChecksumManifestSHA256},
		Summary: ReceiptSetSummary{RequiredTargets: 5, PassedTargets: 5, GateStatus: "pass"},
	}
	for _, target := range requiredTargetTuples {
		receipt := validTestReceipt()
		receipt.Mode = modeLive
		receipt.Source = SourceEvidence{Repository: set.Release.Repository, Tag: "wechat-article-v" + set.Release.Version, Commit: set.Release.Commit, Version: set.Release.Version}
		parts := strings.Split(target, "/")
		receipt.Platform.GOOS, receipt.Platform.GOARCH = parts[0], parts[1]
		receipt.Platform.RunnerOS, receipt.Platform.RunnerArch = parts[0], parts[1]
		receipt.Artifact.TargetGOOS, receipt.Artifact.TargetGOARCH = parts[0], parts[1]
		for index, workflow := range receipt.Workflows {
			contract, _ := workflowContractByID(workflow.ID)
			if contract.LiveEvidence {
				receipt.Workflows[index].Phase = phaseOnlineLive
			}
		}
		receipt.finalize()
		body, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, strings.ReplaceAll(target, "/", "-")+".json")
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		set.Receipts = append(set.Receipts, ReceiptSetReference{
			Target: target, ReceiptPath: filepath.Base(path), ReceiptSHA256: sha256Bytes(body), ArchiveSHA256: receipt.Artifact.ArchiveSHA256,
		})
	}
	return set, root
}
