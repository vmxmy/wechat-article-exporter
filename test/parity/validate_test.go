package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestGateRequirementsRejectNonPassingAuditAndUnsignedSignOff(t *testing.T) {
	source := minimalMatrixForValidation()
	if err := validateGateRequirements(source); err != nil {
		t.Fatalf("valid gate requirements: %v", err)
	}

	for _, result := range []string{"failed", "not-run"} {
		t.Run(result, func(t *testing.T) {
			candidate := source
			candidate.Audit.Executions = append([]auditExecution(nil), source.Audit.Executions...)
			candidate.Audit.Executions[0].Result = result
			candidate.Audit.Executions[0].Note = "known failure"
			if err := validateGateRequirements(candidate); err == nil || !strings.Contains(err.Error(), "must be passed") {
				t.Fatalf("validateGateRequirements() = %v", err)
			}
		})
	}

	unsigned := source
	unsigned.Audit.SignOff.Status = "pending"
	if err := validateGateRequirements(unsigned); err == nil || !strings.Contains(err.Error(), "signed-off") {
		t.Fatalf("unsigned gate validation = %v", err)
	}
}

func TestValidateRepositoryPathRejectsDirectorySymlinkAndEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evidence.txt"), []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "evidence-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "directory-link")); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}

	if err := validateRepositoryPath(root, "evidence.txt"); err != nil {
		t.Fatalf("regular contained file: %v", err)
	}
	for _, path := range []string{"directory", "evidence-link", "directory-link/outside.txt", "../outside.txt", "sub/../../outside.txt", outside} {
		t.Run(path, func(t *testing.T) {
			if err := validateRepositoryPath(root, path); err == nil {
				t.Fatalf("validateRepositoryPath(%q) accepted unsafe evidence", path)
			}
		})
	}
}

func TestValidateTestRequiresGoTestFileAndTestingSignature(t *testing.T) {
	root := t.TempDir()
	valid := `package fixture
import "testing"
func TestValid(t *testing.T) {}
`
	writeParityFixture(t, root, "valid_test.go", valid)
	writeParityFixture(t, root, "not_test.go.txt", valid)
	writeParityFixture(t, root, "wrong_signature_test.go", `package fixture
func TestWrong() {}
`)
	writeParityFixture(t, root, "method_test.go", `package fixture
import "testing"
type suite struct{}
func (suite) TestMethod(t *testing.T) {}
`)
	writeParityFixture(t, root, "fake_testing_test.go", `package fixture
import testing "example.com/not-testing"
func TestFake(t *testing.T) {}
`)

	if err := validateTest(root, testEvidence{Path: "valid_test.go", Symbol: "TestValid"}); err != nil {
		t.Fatalf("valid Go test evidence: %v", err)
	}
	for _, evidence := range []testEvidence{
		{Path: "not_test.go.txt", Symbol: "TestValid"},
		{Path: "wrong_signature_test.go", Symbol: "TestWrong"},
		{Path: "method_test.go", Symbol: "TestMethod"},
		{Path: "fake_testing_test.go", Symbol: "TestFake"},
	} {
		if err := validateTest(root, evidence); err == nil {
			t.Fatalf("validateTest(%+v) accepted invalid test evidence", evidence)
		}
	}
}

func TestValidateCurrentRunEvidenceRequiresExactMandatoryCoverageAndSafePackages(t *testing.T) {
	source := minimalMatrixForValidation()
	source.Entries = []entry{
		{ID: "feature.one", Classification: "mandatory-parity", Status: "passed",
			Evidence: evidence{Tests: []testEvidence{{Path: "cli/internal/app/one_test.go", Symbol: "TestOne"}}}},
		{ID: "feature.two", Classification: "mandatory-parity", Status: "passed",
			Evidence: evidence{Tests: []testEvidence{{Path: "cli/internal/library/two_test.go", Symbol: "TestTwo"},
				{Path: "cli/internal/library/three_test.go", Symbol: "TestThree"}}}},
		{ID: "migration.one", Classification: "migration-only", Status: "passed"},
	}
	source.ReleaseGate.CurrentRun = []currentRunEvidence{
		{ID: "feature.one", Packages: []currentRunPackageTests{{Path: "./internal/app", Tests: []string{"TestOne"}}}},
		{ID: "feature.two", Packages: []currentRunPackageTests{{Path: "./internal/library", Tests: []string{"TestTwo", "TestThree"}}}},
	}
	if err := validateCurrentRunEvidence(source); err != nil {
		t.Fatalf("valid current-run evidence: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*matrix)
	}{
		{name: "missing mandatory", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun = value.ReleaseGate.CurrentRun[:1] }},
		{name: "unknown id", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[1].ID = "unknown" }},
		{name: "package escape", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[0].Packages[0].Path = "../outside" }},
		{name: "shell metacharacter", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[0].Packages[0].Path = "./internal/app;touch" }},
		{name: "invalid test", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[0].Packages[0].Tests[0] = "TestOne|TestTwo" }},
		{name: "wrong capability test", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[0].Packages[0].Tests[0] = "TestTwo" }},
		{name: "wrong capability package", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[0].Packages[0].Path = "./internal/library" }},
		{name: "empty tests", mutate: func(value *matrix) { value.ReleaseGate.CurrentRun[0].Packages[0].Tests = nil }},
		{name: "partial declared coverage", mutate: func(value *matrix) {
			value.ReleaseGate.CurrentRun[1].Packages[0].Tests = []string{"TestTwo"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneCurrentRunMatrix(source)
			test.mutate(&candidate)
			if err := validateCurrentRunEvidence(candidate); err == nil {
				t.Fatal("unsafe current-run evidence accepted")
			}
		})
	}
}

func TestExecuteCurrentRunEvidenceUsesFixedGoArgumentsAndRequiresPassingTests(t *testing.T) {
	source := minimalMatrixForValidation()
	source.ReleaseGate.CurrentRun = []currentRunEvidence{
		{ID: "feature.one", Packages: []currentRunPackageTests{{Path: "./internal/app", Tests: []string{"TestOne", "TestTwo"}}}},
	}
	var calls [][]string
	runner := func(_ context.Context, directory, executable string, arguments ...string) ([]byte, error) {
		if directory != filepath.Join("repo", "cli") || executable != "go" {
			t.Fatalf("runner directory/executable = %q %q", directory, executable)
		}
		calls = append(calls, append([]string(nil), arguments...))
		return []byte(`{"Action":"run","Package":"github.com/example/repo/internal/app","Test":"TestOne"}
{"Action":"run","Package":"github.com/example/repo/internal/app","Test":"TestOne/subtest"}
{"Action":"pass","Package":"github.com/example/repo/internal/app","Test":"TestOne/subtest"}
{"Action":"pass","Package":"github.com/example/repo/internal/app","Test":"TestOne"}
{"Action":"run","Package":"github.com/example/repo/internal/app","Test":"TestTwo"}
{"Action":"pass","Package":"github.com/example/repo/internal/app","Test":"TestTwo"}
`), nil
	}
	receipts, err := executeCurrentRunEvidence(context.Background(), "repo", source.ReleaseGate.CurrentRun, runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != `test -json -count=1 -run ^(TestOne|TestTwo)$ ./internal/app` {
		t.Fatalf("go invocation = %#v", calls)
	}
	if _, err := regexp.Compile(exactTestPattern([]string{"TestOne", "TestTwo"})); err != nil {
		t.Fatalf("exact test pattern is not valid Go regexp: %v", err)
	}
	if len(receipts) != 1 || receipts[0].Result != "passed" || receipts[0].TestsPassed != 2 {
		t.Fatalf("receipts = %#v", receipts)
	}

	multiPackage := []currentRunEvidence{{ID: "feature.multi", Packages: []currentRunPackageTests{
		{Path: "./internal/app", Tests: []string{"TestApp"}},
		{Path: "./internal/library", Tests: []string{"TestLibrary"}},
	}}}
	calls = nil
	multiRunner := func(_ context.Context, _, _ string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), arguments...))
		packagePath := arguments[len(arguments)-1]
		if packagePath == "./internal/app" {
			return []byte(`{"Action":"run","Package":"github.com/example/repo/internal/app","Test":"TestApp"}
{"Action":"pass","Package":"github.com/example/repo/internal/app","Test":"TestApp"}
`), nil
		}
		return []byte(`{"Action":"run","Package":"github.com/example/repo/internal/library","Test":"TestLibrary"}
{"Action":"pass","Package":"github.com/example/repo/internal/library","Test":"TestLibrary"}
`), nil
	}
	receipts, err = executeCurrentRunEvidence(context.Background(), "repo", multiPackage, multiRunner)
	if err != nil || len(calls) != 2 || receipts[0].TestsPassed != 2 {
		t.Fatalf("multi-package calls=%#v receipts=%#v err=%v", calls, receipts, err)
	}

	failingRunner := func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"Action":"run","Package":"github.com/example/repo/internal/app","Test":"TestOne"}
{"Action":"fail","Package":"github.com/example/repo/internal/app","Test":"TestOne"}
`), context.DeadlineExceeded
	}
	if _, err := executeCurrentRunEvidence(context.Background(), "repo", source.ReleaseGate.CurrentRun, failingRunner); err == nil {
		t.Fatal("failed current-run evidence was accepted")
	}

	skippedRunner := func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte(`{"Action":"run","Package":"github.com/example/repo/internal/app","Test":"TestOne"}
{"Action":"skip","Package":"github.com/example/repo/internal/app","Test":"TestOne"}
`), nil
	}
	if _, err := executeCurrentRunEvidence(context.Background(), "repo", source.ReleaseGate.CurrentRun, skippedRunner); err == nil {
		t.Fatal("skipped current-run evidence was accepted")
	}
}

func TestBuildReportExecutionReflectsInvocationAndResult(t *testing.T) {
	source := minimalMatrixForValidation()
	source.Entries = []entry{{ID: "feature.one", Classification: "mandatory-parity", Status: "passed"}}
	staticReport := buildReport(source, gateRun{})
	if staticReport.ReleaseGate.Execution != "not-invoked" || staticReport.ReleaseGate.Result != "not-run" {
		t.Fatalf("static gate report = %#v", staticReport.ReleaseGate)
	}

	passed := buildReport(source, gateRun{Invoked: true, Passed: true, Receipts: []currentRunReceipt{{ID: "feature.one", Result: "passed"}}})
	if passed.ReleaseGate.Execution != "executed" || passed.ReleaseGate.Result != "passed" {
		t.Fatalf("passed gate report = %#v", passed.ReleaseGate)
	}

	failed := buildReport(source, gateRun{Invoked: true, Passed: false})
	if failed.ReleaseGate.Execution != "executed" || failed.ReleaseGate.Result != "blocked" {
		t.Fatalf("failed gate report = %#v", failed.ReleaseGate)
	}
}

func minimalMatrixForValidation() matrix {
	var source matrix
	source.SchemaVersion = 2
	source.Change = "replace-web-with-local-go-cli"
	source.Audit.Task = "16.7"
	source.Audit.Date = "2026-07-22"
	source.Audit.Executions = []auditExecution{{Command: "go test ./...", Result: "passed"}}
	source.Audit.SignOff.Status = "signed-off"
	source.Audit.SignOff.Note = "reviewed"
	source.ReleaseGate.Name = "test-gate"
	source.ReleaseGate.Rule = "all evidence passes"
	return source
}

func cloneCurrentRunMatrix(source matrix) matrix {
	clone := source
	clone.ReleaseGate.CurrentRun = make([]currentRunEvidence, len(source.ReleaseGate.CurrentRun))
	for index, evidence := range source.ReleaseGate.CurrentRun {
		packages := make([]currentRunPackageTests, len(evidence.Packages))
		for packageIndex, packageEvidence := range evidence.Packages {
			packages[packageIndex] = currentRunPackageTests{Path: packageEvidence.Path, Tests: append([]string(nil), packageEvidence.Tests...)}
		}
		clone.ReleaseGate.CurrentRun[index] = currentRunEvidence{ID: evidence.ID, Packages: packages}
	}
	return clone
}

func TestCurrentRunTimeoutIsBounded(t *testing.T) {
	if currentRunTimeout <= 0 || currentRunTimeout > 20*time.Minute || currentPackageTimeout <= 0 || currentPackageTimeout > 5*time.Minute {
		t.Fatalf("currentRunTimeout = %v", currentRunTimeout)
	}
}

func writeParityFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
