package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type testEvidence struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
}

type evidence struct {
	Commands []string       `json:"commands"`
	Tests    []testEvidence `json:"tests"`
	Fixtures []string       `json:"fixtures"`
}

type entry struct {
	ID                     string   `json:"id"`
	Workflow               string   `json:"workflow"`
	Classification         string   `json:"classification"`
	Spec                   string   `json:"spec"`
	CurrentEntrypoints     []string `json:"currentEntrypoints"`
	Acceptance             string   `json:"acceptance"`
	Evidence               evidence `json:"evidence"`
	Blockers               []string `json:"blockers"`
	IntentionalDifferences []string `json:"intentionalDifferences"`
	Status                 string   `json:"status"`
}

type auditExecution struct {
	Command string `json:"command"`
	Result  string `json:"result"`
	Note    string `json:"note,omitempty"`
}

type currentRunEvidence struct {
	ID       string                   `json:"id"`
	Packages []currentRunPackageTests `json:"packages"`
}

type currentRunPackageTests struct {
	Path  string   `json:"path"`
	Tests []string `json:"tests"`
}

type currentRunReceipt struct {
	ID          string   `json:"id"`
	Packages    []string `json:"packages"`
	Tests       []string `json:"tests"`
	Result      string   `json:"result"`
	TestsPassed int      `json:"testsPassed"`
}

type gateRun struct {
	Invoked  bool
	Passed   bool
	Receipts []currentRunReceipt
	Error    string
}

type commandRunner func(context.Context, string, string, ...string) ([]byte, error)

type matrix struct {
	SchemaVersion int    `json:"schemaVersion"`
	Change        string `json:"change"`
	Audit         struct {
		Task       string           `json:"task"`
		Date       string           `json:"date"`
		Executions []auditExecution `json:"executions"`
		SignOff    struct {
			Status string `json:"status"`
			Note   string `json:"note"`
		} `json:"signOff"`
	} `json:"audit"`
	ReleaseGate struct {
		Name       string               `json:"name"`
		Rule       string               `json:"rule"`
		CurrentRun []currentRunEvidence `json:"currentRun"`
	} `json:"releaseGate"`
	Entries []entry `json:"entries"`
}

type machineEntry struct {
	ID                     string   `json:"id"`
	Workflow               string   `json:"workflow"`
	Classification         string   `json:"classification"`
	Status                 string   `json:"status"`
	Acceptance             string   `json:"acceptance"`
	Evidence               evidence `json:"evidence"`
	Blockers               []string `json:"blockers"`
	IntentionalDifferences []string `json:"intentionalDifferences"`
}

type differenceReport struct {
	ID          string   `json:"id"`
	Differences []string `json:"differences"`
}

type blockingEntry struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	Blockers []string `json:"blockers"`
}

type machineReport struct {
	SchemaVersion int              `json:"schemaVersion"`
	Task          string           `json:"task"`
	AuditDate     string           `json:"auditDate"`
	Change        string           `json:"change"`
	SignOff       any              `json:"signOff"`
	Executions    []auditExecution `json:"executions"`
	ReleaseGate   struct {
		Name            string              `json:"name"`
		Rule            string              `json:"rule"`
		Result          string              `json:"result"`
		Execution       string              `json:"execution"`
		MandatoryPassed int                 `json:"mandatoryPassed"`
		MandatoryTotal  int                 `json:"mandatoryTotal"`
		Blocking        []blockingEntry     `json:"blocking"`
		CurrentRun      []currentRunReceipt `json:"currentRun"`
	} `json:"releaseGate"`
	Summary struct {
		Entries         int            `json:"entries"`
		Classifications map[string]int `json:"classifications"`
		Statuses        map[string]int `json:"statuses"`
	} `json:"summary"`
	MandatoryEntries            []machineEntry     `json:"mandatoryEntries"`
	MigrationEntries            []machineEntry     `json:"migrationEntries"`
	KnownIntentionalDifferences []differenceReport `json:"knownIntentionalDifferences"`
}

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	datePattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	symbolPattern   = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)
	testNamePattern = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
	packagePattern  = regexp.MustCompile(`^\./internal/[a-z0-9]+(?:/[a-z0-9._-]+)*$`)
)

const (
	currentRunTimeout     = 20 * time.Minute
	currentPackageTimeout = 4 * time.Minute
	maximumCurrentRunJobs = 32
	maximumTestsPerJob    = 64
)

func main() {
	var gate bool
	var writeReport bool
	flag.BoolVar(&gate, "gate", false, "fail when a mandatory parity entry is not passed")
	flag.BoolVar(&writeReport, "write-report", false, "rewrite the machine-readable and Markdown reports")
	flag.Parse()
	if gate && writeReport {
		fatalIf(errors.New("--gate and --write-report cannot be combined"))
	}

	repositoryRoot, err := findRepositoryRoot()
	fatalIf(err)
	matrixPath := filepath.Join(repositoryRoot, "test", "parity", "matrix.json")
	body, err := os.ReadFile(matrixPath)
	fatalIf(err)
	var source matrix
	fatalIf(json.Unmarshal(body, &source))
	fatalIf(validateMatrix(repositoryRoot, source))
	gateResult := gateRun{}
	if gate {
		fatalIf(validateGateRequirements(source))
		gateResult.Invoked = true
		ctx, cancel := context.WithTimeout(context.Background(), currentRunTimeout)
		receipts, executionErr := executeCurrentRunEvidence(ctx, repositoryRoot, source.ReleaseGate.CurrentRun, runCommand)
		cancel()
		gateResult.Receipts = receipts
		gateResult.Passed = executionErr == nil
		if executionErr != nil {
			gateResult.Error = executionErr.Error()
		}
	}

	report := buildReport(source, gateResult)
	machineBody, err := json.MarshalIndent(report, "", "  ")
	fatalIf(err)
	machineBody = append(machineBody, '\n')
	markdownBody := []byte(renderMarkdown(report))
	reportPath := filepath.Join(repositoryRoot, "test", "parity", "report.json")
	markdownPath := filepath.Join(repositoryRoot, "docs", "release", "parity-report.md")
	if writeReport {
		fatalIf(writeFileAtomically(reportPath, machineBody))
		fatalIf(writeFileAtomically(markdownPath, markdownBody))
	} else if !gate {
		fatalIf(assertCurrent(reportPath, machineBody))
		fatalIf(assertCurrent(markdownPath, markdownBody))
	} else {
		staticReport := buildReport(source, gateRun{})
		staticBody, marshalErr := json.MarshalIndent(staticReport, "", "  ")
		fatalIf(marshalErr)
		staticBody = append(staticBody, '\n')
		fatalIf(assertCurrent(reportPath, staticBody))
		fatalIf(assertCurrent(markdownPath, []byte(renderMarkdown(staticReport))))
	}
	if gate && !gateResult.Passed {
		fatalIf(errors.New(gateResult.Error))
	}

	if gate && len(report.ReleaseGate.Blocking) > 0 {
		fmt.Fprintf(os.Stderr, "parity gate blocked: %d mandatory workflows are not passed\n", len(report.ReleaseGate.Blocking))
		for _, item := range report.ReleaseGate.Blocking {
			fmt.Fprintf(os.Stderr, "- %s: %s — %s\n", item.ID, item.Status, strings.Join(item.Blockers, "; "))
		}
		os.Exit(1)
	}
	result := map[string]any{
		"valid":           true,
		"reportCurrent":   true,
		"entries":         report.Summary.Entries,
		"classifications": report.Summary.Classifications,
		"statuses":        report.Summary.Statuses,
		"mandatoryPassed": report.ReleaseGate.MandatoryPassed,
		"mandatoryTotal":  report.ReleaseGate.MandatoryTotal,
		"gateEligible":    len(report.ReleaseGate.Blocking) == 0,
		"gateExecuted":    gate,
		"currentRun":      gateResult.Receipts,
	}
	output, err := json.MarshalIndent(result, "", "  ")
	fatalIf(err)
	fmt.Println(string(output))
}

func findRepositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "test", "parity", "matrix.json")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("repository root containing test/parity/matrix.json was not found")
		}
		directory = parent
	}
}

func validateMatrix(repositoryRoot string, source matrix) error {
	if source.SchemaVersion != 2 {
		return fmt.Errorf("schemaVersion must be 2, got %d", source.SchemaVersion)
	}
	if source.Change != "replace-web-with-local-go-cli" {
		return fmt.Errorf("unexpected change %q", source.Change)
	}
	if source.Audit.Task != "16.7" || !datePattern.MatchString(source.Audit.Date) {
		return errors.New("audit task/date is invalid")
	}
	if _, err := time.Parse("2006-01-02", source.Audit.Date); err != nil {
		return errors.New("audit task/date is invalid")
	}
	if len(source.Audit.Executions) == 0 || strings.TrimSpace(source.Audit.SignOff.Note) == "" {
		return errors.New("audit executions and sign-off note are required")
	}
	if strings.TrimSpace(source.ReleaseGate.Name) == "" || strings.TrimSpace(source.ReleaseGate.Rule) == "" {
		return errors.New("release gate name and rule are required")
	}
	if err := validateCurrentRunEvidence(source); err != nil {
		return err
	}
	if len(source.Entries) == 0 {
		return errors.New("parity entries are required")
	}
	classifications := stringSet("mandatory-parity", "intentional-retirement", "migration-only", "dev-only")
	statuses := stringSet("not-implemented", "partial", "passed", "retirement-approved")
	ids := map[string]struct{}{}
	for _, execution := range source.Audit.Executions {
		if strings.TrimSpace(execution.Command) == "" || !stringIn(execution.Result, "passed", "failed", "not-run") {
			return fmt.Errorf("invalid audit execution: %+v", execution)
		}
		if execution.Result != "passed" && strings.TrimSpace(execution.Note) == "" {
			return fmt.Errorf("%s: non-passing execution needs a note", execution.Command)
		}
	}
	for _, item := range source.Entries {
		if !idPattern.MatchString(item.ID) {
			return fmt.Errorf("invalid parity id %q", item.ID)
		}
		if _, exists := ids[item.ID]; exists {
			return fmt.Errorf("duplicate parity id %q", item.ID)
		}
		ids[item.ID] = struct{}{}
		if _, ok := classifications[item.Classification]; !ok {
			return fmt.Errorf("%s: invalid classification %q", item.ID, item.Classification)
		}
		if _, ok := statuses[item.Status]; !ok {
			return fmt.Errorf("%s: invalid status %q", item.ID, item.Status)
		}
		if strings.TrimSpace(item.Workflow) == "" || !strings.Contains(item.Spec, " / ") || strings.TrimSpace(item.Acceptance) == "" {
			return fmt.Errorf("%s: workflow, capability requirement, and acceptance are required", item.ID)
		}
		if len(item.CurrentEntrypoints) == 0 {
			return fmt.Errorf("%s: current entrypoint is required", item.ID)
		}
		for _, values := range []struct {
			name  string
			items []string
		}{{"currentEntrypoints", item.CurrentEntrypoints}, {"evidence.commands", item.Evidence.Commands}, {"evidence.fixtures", item.Evidence.Fixtures}, {"blockers", item.Blockers}, {"intentionalDifferences", item.IntentionalDifferences}} {
			if err := uniqueNonEmpty(values.items); err != nil {
				return fmt.Errorf("%s: %s: %w", item.ID, values.name, err)
			}
		}
		for _, relativePath := range append(append([]string{}, item.CurrentEntrypoints...), item.Evidence.Fixtures...) {
			if err := validateRepositoryPath(repositoryRoot, relativePath); err != nil {
				return fmt.Errorf("%s: %w", item.ID, err)
			}
		}
		testKeys := map[string]struct{}{}
		for _, test := range item.Evidence.Tests {
			if !symbolPattern.MatchString(test.Symbol) {
				return fmt.Errorf("%s: invalid test symbol %q", item.ID, test.Symbol)
			}
			key := test.Path + "#" + test.Symbol
			if _, exists := testKeys[key]; exists {
				return fmt.Errorf("%s: duplicate evidence test %s", item.ID, key)
			}
			testKeys[key] = struct{}{}
			if err := validateTest(repositoryRoot, test); err != nil {
				return fmt.Errorf("%s: %w", item.ID, err)
			}
		}
		if (item.Classification == "mandatory-parity" || item.Classification == "migration-only") && item.Status == "retirement-approved" {
			return fmt.Errorf("%s: retained workflow cannot be retirement-approved", item.ID)
		}
		if item.Status == "passed" && (len(item.Evidence.Commands) == 0 || len(item.Evidence.Tests) == 0 || len(item.Blockers) != 0) {
			return fmt.Errorf("%s: passed entry needs commands/tests and no blockers", item.ID)
		}
		if (item.Status == "partial" || item.Status == "not-implemented") && len(item.Blockers) == 0 {
			return fmt.Errorf("%s: incomplete entry needs a blocker", item.ID)
		}
		if item.Status == "retirement-approved" {
			if !stringIn(item.Classification, "intentional-retirement", "dev-only") || len(item.IntentionalDifferences) == 0 {
				return fmt.Errorf("%s: retirement approval requires a retired/dev-only classification and documented difference", item.ID)
			}
		}
	}
	return nil
}

func validateGateRequirements(source matrix) error {
	if source.Audit.SignOff.Status != "signed-off" {
		return fmt.Errorf("audit signOff.status must be signed-off, got %q", source.Audit.SignOff.Status)
	}
	for _, execution := range source.Audit.Executions {
		if execution.Result != "passed" {
			return fmt.Errorf("audit execution %q must be passed for --gate, got %q", execution.Command, execution.Result)
		}
	}
	return nil
}

func validateCurrentRunEvidence(source matrix) error {
	mandatory := map[string]entry{}
	for _, item := range source.Entries {
		if item.Classification == "mandatory-parity" {
			mandatory[item.ID] = item
		}
	}
	if len(mandatory) == 0 {
		return errors.New("mandatory parity entries are required before current-run evidence")
	}
	if len(source.ReleaseGate.CurrentRun) == 0 || len(source.ReleaseGate.CurrentRun) > maximumCurrentRunJobs {
		return fmt.Errorf("releaseGate.currentRun must contain 1-%d jobs", maximumCurrentRunJobs)
	}
	seen := map[string]struct{}{}
	for _, current := range source.ReleaseGate.CurrentRun {
		item, ok := mandatory[current.ID]
		if !ok {
			return fmt.Errorf("current-run evidence references non-mandatory id %q", current.ID)
		}
		if _, duplicate := seen[current.ID]; duplicate {
			return fmt.Errorf("duplicate current-run evidence id %q", current.ID)
		}
		seen[current.ID] = struct{}{}
		if len(current.Packages) == 0 || len(current.Packages) > 8 {
			return fmt.Errorf("%s: current-run packages must contain 1-8 entries", current.ID)
		}
		packagePaths := make([]string, 0, len(current.Packages))
		for _, packageEvidence := range current.Packages {
			packagePath := packageEvidence.Path
			cleanPackage := "./" + filepath.ToSlash(filepath.Clean(strings.TrimPrefix(packagePath, "./")))
			if !packagePattern.MatchString(packagePath) || cleanPackage != packagePath {
				return fmt.Errorf("%s: unsafe current-run package %q", current.ID, packagePath)
			}
			packagePaths = append(packagePaths, packagePath)
			if len(packageEvidence.Tests) == 0 || len(packageEvidence.Tests) > maximumTestsPerJob {
				return fmt.Errorf("%s: current-run tests for %s must contain 1-%d entries", current.ID, packagePath, maximumTestsPerJob)
			}
			for _, testName := range packageEvidence.Tests {
				if !testNamePattern.MatchString(testName) {
					return fmt.Errorf("%s: unsafe current-run test %q", current.ID, testName)
				}
			}
			if err := uniqueNonEmpty(packageEvidence.Tests); err != nil {
				return fmt.Errorf("%s: tests for %s: %w", current.ID, packagePath, err)
			}
		}
		if err := uniqueNonEmpty(packagePaths); err != nil {
			return fmt.Errorf("%s: packages: %w", current.ID, err)
		}
		allowed := make(map[string]map[string]struct{}, len(item.Evidence.Tests))
		for _, test := range item.Evidence.Tests {
			packagePath, err := packageForEvidencePath(test.Path)
			if err != nil {
				return fmt.Errorf("%s: evidence test %s#%s: %w", current.ID, test.Path, test.Symbol, err)
			}
			if allowed[packagePath] == nil {
				allowed[packagePath] = map[string]struct{}{}
			}
			allowed[packagePath][test.Symbol] = struct{}{}
		}
		selected := make(map[string]map[string]struct{}, len(current.Packages))
		for _, packageEvidence := range current.Packages {
			packagePath := packageEvidence.Path
			selected[packagePath] = make(map[string]struct{}, len(packageEvidence.Tests))
			if _, exists := allowed[packagePath]; !exists {
				return fmt.Errorf("%s: current-run package %s has no evidence tests for this capability", current.ID, packagePath)
			}
			for _, testName := range packageEvidence.Tests {
				if _, exists := allowed[packagePath][testName]; !exists {
					return fmt.Errorf("%s: current-run test %s is not declared by this capability's evidence in package %s", current.ID, testName, packagePath)
				}
				selected[packagePath][testName] = struct{}{}
			}
		}
		for packagePath, tests := range allowed {
			if len(selected[packagePath]) != len(tests) {
				return fmt.Errorf("%s: current-run evidence for %s must cover all %d declared tests", current.ID, packagePath, len(tests))
			}
			for testName := range tests {
				if _, exists := selected[packagePath][testName]; !exists {
					return fmt.Errorf("%s: current-run evidence is missing declared test %s in package %s", current.ID, testName, packagePath)
				}
			}
		}
	}
	if len(seen) != len(mandatory) {
		var missing []string
		for id := range mandatory {
			if _, ok := seen[id]; !ok {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		return fmt.Errorf("current-run evidence is missing mandatory ids: %s", strings.Join(missing, ", "))
	}
	return nil
}

func packageForEvidencePath(path string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(clean, "cli/internal/") || !strings.HasSuffix(clean, "_test.go") {
		return "", fmt.Errorf("path is not a CLI internal Go test file")
	}
	directory := filepath.ToSlash(filepath.Dir(strings.TrimPrefix(clean, "cli/")))
	packagePath := "./" + directory
	if !packagePattern.MatchString(packagePath) {
		return "", fmt.Errorf("derived package %q is unsafe", packagePath)
	}
	return packagePath, nil
}

func executeCurrentRunEvidence(
	ctx context.Context,
	repositoryRoot string,
	evidence []currentRunEvidence,
	runner commandRunner,
) ([]currentRunReceipt, error) {
	if runner == nil {
		return nil, errors.New("current-run command runner is required")
	}
	cliRoot := filepath.Join(repositoryRoot, "cli")
	receipts := make([]currentRunReceipt, 0, len(evidence))
	for _, current := range evidence {
		var tests []string
		var packages []string
		packageTests := make(map[string][]string, len(current.Packages))
		for _, packageEvidence := range current.Packages {
			packages = append(packages, packageEvidence.Path)
			packageTests[packageEvidence.Path] = append([]string(nil), packageEvidence.Tests...)
			tests = append(tests, packageEvidence.Tests...)
		}
		sort.Strings(tests)
		sort.Strings(packages)
		passed := 0
		var executionErr error
		for _, packagePath := range packages {
			testsForPackage := append([]string(nil), packageTests[packagePath]...)
			sort.Strings(testsForPackage)
			arguments := []string{"test", "-json", "-count=1", "-run", exactTestPattern(testsForPackage), packagePath}
			packageCtx, cancel := context.WithTimeout(ctx, currentPackageTimeout)
			output, err := runner(packageCtx, cliRoot, "go", arguments...)
			cancel()
			packagePassed, receiptErr := parseCurrentRunOutput(output, packagePath, testsForPackage)
			passed += packagePassed
			if err != nil || receiptErr != nil {
				executionErr = errors.Join(executionErr, err, receiptErr)
				break
			}
		}
		receipt := currentRunReceipt{ID: current.ID, Packages: packages, Tests: tests, TestsPassed: passed, Result: "passed"}
		if executionErr != nil {
			receipt.Result = "failed"
			receipts = append(receipts, receipt)
			return receipts, fmt.Errorf("current-run evidence %s failed: %w", current.ID, executionErr)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, nil
}

func exactTestPattern(tests []string) string {
	escaped := make([]string, len(tests))
	for index, testName := range tests {
		escaped[index] = regexp.QuoteMeta(testName)
	}
	return "^(" + strings.Join(escaped, "|") + ")$"
}

func parseCurrentRunOutput(output []byte, expectedPackage string, expected []string) (int, error) {
	passed := map[string]bool{}
	run := map[string]bool{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var event struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
		}
		if err := decoder.Decode(&event); err != nil {
			return countExpectedPasses(expected, run, passed), fmt.Errorf("decode go test JSON: %w", err)
		}
		if event.Test == "" {
			continue
		}
		if event.Package == "" {
			return countExpectedPasses(expected, run, passed), errors.New("go test JSON event is missing Package")
		}
		if !packageMatchesCurrentRun(expectedPackage, event.Package) {
			return countExpectedPasses(expected, run, passed), fmt.Errorf("unexpected package %q for %s", event.Package, expectedPackage)
		}
		rootTest := strings.SplitN(event.Test, "/", 2)[0]
		if !stringIn(rootTest, expected...) {
			continue
		}
		switch event.Action {
		case "run":
			if event.Test == rootTest {
				run[rootTest] = true
			}
		case "pass":
			if event.Test == rootTest {
				passed[rootTest] = true
			}
		case "fail", "skip":
			return countExpectedPasses(expected, run, passed), fmt.Errorf("test %s action=%s", event.Test, event.Action)
		}
	}
	for _, testName := range expected {
		if !run[testName] || !passed[testName] {
			return countExpectedPasses(expected, run, passed), fmt.Errorf("test %s did not run and pass", testName)
		}
	}
	return countExpectedPasses(expected, run, passed), nil
}

func packageMatchesCurrentRun(expected, actual string) bool {
	expected = strings.TrimPrefix(filepath.ToSlash(expected), "./")
	actual = filepath.ToSlash(actual)
	return actual == expected || strings.HasSuffix(actual, "/"+expected)
}

func countExpectedPasses(expected []string, run, passed map[string]bool) int {
	count := 0
	for _, testName := range expected {
		if run[testName] && passed[testName] {
			count++
		}
	}
	return count
}

func runCommand(ctx context.Context, directory, executable string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s %s: %w\n%s", executable, strings.Join(arguments, " "), err, tailOutput(output, 8192))
	}
	return output, nil
}

func tailOutput(output []byte, maximum int) string {
	if len(output) <= maximum {
		return string(output)
	}
	return string(output[len(output)-maximum:])
}

func validateRepositoryPath(repositoryRoot, relativePath string) error {
	if strings.TrimSpace(relativePath) == "" || filepath.IsAbs(relativePath) {
		return fmt.Errorf("unsafe repository path %q", relativePath)
	}
	root, err := filepath.Abs(filepath.Clean(repositoryRoot))
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	target, err := filepath.Abs(filepath.Clean(filepath.Join(root, filepath.FromSlash(relativePath))))
	if err != nil {
		return fmt.Errorf("resolve repository path %q: %w", relativePath, err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe repository path %q", relativePath)
	}
	if err := rejectSymlinkComponents(root, relative); err != nil {
		return fmt.Errorf("unsafe repository path %q: %w", relativePath, err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("missing %s", relativePath)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repository evidence path %q is a symlink", relativePath)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("repository evidence path %q is not a regular file", relativePath)
	}
	return nil
}

func rejectSymlinkComponents(root, relative string) error {
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("component %q is a symlink", component)
		}
	}
	return nil
}

func validateTest(repositoryRoot string, test testEvidence) error {
	if !strings.HasSuffix(filepath.ToSlash(test.Path), "_test.go") {
		return fmt.Errorf("test evidence path %s must end in _test.go", test.Path)
	}
	if err := validateRepositoryPath(repositoryRoot, test.Path); err != nil {
		return err
	}
	path := filepath.Join(repositoryRoot, filepath.FromSlash(test.Path))
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse test evidence %s: %w", test.Path, err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Name.Name != test.Symbol {
			continue
		}
		if validTestingSignature(function.Type) && importsStandardTesting(parsed) {
			return nil
		}
		return fmt.Errorf("test symbol %s in %s must have signature func %s(t *testing.T)", test.Symbol, test.Path, test.Symbol)
	}
	return fmt.Errorf("test symbol %s not found in %s", test.Symbol, test.Path)
}

func importsStandardTesting(file *ast.File) bool {
	for _, imported := range file.Imports {
		if imported.Path == nil || imported.Path.Value != `"testing"` {
			continue
		}
		return imported.Name == nil || imported.Name.Name == "testing"
	}
	return false
}

func validTestingSignature(function *ast.FuncType) bool {
	if function == nil || function.TypeParams != nil || function.Results != nil && len(function.Results.List) != 0 ||
		function.Params == nil || len(function.Params.List) != 1 || len(function.Params.List[0].Names) != 1 {
		return false
	}
	pointer, ok := function.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "T" {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "testing"
}

func buildReport(source matrix, gate gateRun) machineReport {
	var report machineReport
	report.SchemaVersion = 2
	report.Task = source.Audit.Task
	report.AuditDate = source.Audit.Date
	report.Change = source.Change
	report.SignOff = source.Audit.SignOff
	report.Executions = source.Audit.Executions
	report.ReleaseGate.Name = source.ReleaseGate.Name
	report.ReleaseGate.Rule = source.ReleaseGate.Rule
	report.ReleaseGate.CurrentRun = gate.Receipts
	if report.ReleaseGate.CurrentRun == nil {
		report.ReleaseGate.CurrentRun = []currentRunReceipt{}
	}
	report.Summary.Entries = len(source.Entries)
	report.Summary.Classifications = map[string]int{"mandatory-parity": 0, "intentional-retirement": 0, "migration-only": 0, "dev-only": 0}
	report.Summary.Statuses = map[string]int{"not-implemented": 0, "partial": 0, "passed": 0, "retirement-approved": 0}
	for _, item := range source.Entries {
		report.Summary.Classifications[item.Classification]++
		report.Summary.Statuses[item.Status]++
		converted := machineEntry{ID: item.ID, Workflow: item.Workflow, Classification: item.Classification, Status: item.Status,
			Acceptance: item.Acceptance, Evidence: item.Evidence, Blockers: item.Blockers, IntentionalDifferences: item.IntentionalDifferences}
		switch item.Classification {
		case "mandatory-parity":
			report.MandatoryEntries = append(report.MandatoryEntries, converted)
			if item.Status != "passed" {
				report.ReleaseGate.Blocking = append(report.ReleaseGate.Blocking, blockingEntry{ID: item.ID, Status: item.Status, Blockers: item.Blockers})
			}
		case "migration-only":
			report.MigrationEntries = append(report.MigrationEntries, converted)
		}
		if len(item.IntentionalDifferences) > 0 {
			report.KnownIntentionalDifferences = append(report.KnownIntentionalDifferences, differenceReport{ID: item.ID, Differences: item.IntentionalDifferences})
		}
	}
	report.ReleaseGate.MandatoryTotal = len(report.MandatoryEntries)
	report.ReleaseGate.MandatoryPassed = report.ReleaseGate.MandatoryTotal - len(report.ReleaseGate.Blocking)
	if !gate.Invoked {
		report.ReleaseGate.Result = "not-run"
		report.ReleaseGate.Execution = "not-invoked"
	} else if len(report.ReleaseGate.Blocking) == 0 && gate.Passed {
		report.ReleaseGate.Result = "passed"
		report.ReleaseGate.Execution = "executed"
	} else {
		report.ReleaseGate.Result = "blocked"
		report.ReleaseGate.Execution = "executed"
	}
	if report.ReleaseGate.Blocking == nil {
		report.ReleaseGate.Blocking = []blockingEntry{}
	}
	return report
}

func renderMarkdown(report machineReport) string {
	var output strings.Builder
	fmt.Fprintln(&output, "# 16.7 Mandatory Parity Audit")
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "- Audit date: %s\n", report.AuditDate)
	fmt.Fprintf(&output, "- Change: `%s`\n", report.Change)
	signOff, _ := json.Marshal(report.SignOff)
	var signOffValue struct{ Status, Note string }
	_ = json.Unmarshal(signOff, &signOffValue)
	fmt.Fprintf(&output, "- Sign-off: **%s** — %s\n", signOffValue.Status, signOffValue.Note)
	fmt.Fprintf(&output, "- Release gate: **%s** (%d/%d mandatory entries passed)\n", report.ReleaseGate.Result, report.ReleaseGate.MandatoryPassed, report.ReleaseGate.MandatoryTotal)
	fmt.Fprintf(&output, "- Gate execution: **%s**. Generated reports record no invocation; `--gate` emits current-run receipts after executing the configured Go tests.\n", report.ReleaseGate.Execution)
	fmt.Fprintln(&output, "- Historical note: Web/Nitro/remote MCP code was present when task 16.7 was signed. The current entrypoint inventory below reflects the post-retirement Go-only repository; removed implementations remain reproducible from the immutable Web-capable archive.")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "This report is generated from `test/parity/matrix.json`. `go run ./test/parity/validate.go --gate` verifies the matrix, current entrypoints, every referenced test/fixture, `test/parity/report.json`, and this Markdown file without restoring Node or Web dependencies.")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "## Executed verification")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "The entries below are historical audit executions recorded at sign-off; they are not current-run receipts.")
	fmt.Fprintln(&output)
	for _, execution := range report.Executions {
		note := ""
		if execution.Note != "" {
			note = " — " + execution.Note
		}
		fmt.Fprintf(&output, "- `%s`: **%s**%s\n", execution.Command, execution.Result, note)
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "## Mandatory matrix")
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "| ID | Status | Test evidence | Fixtures | Blocker |")
	fmt.Fprintln(&output, "| --- | --- | ---: | ---: | --- |")
	for _, item := range report.MandatoryEntries {
		blocker := strings.Join(item.Blockers, "; ")
		if blocker == "" {
			blocker = "—"
		}
		fmt.Fprintf(&output, "| `%s` | %s | %d | %d | %s |\n", item.ID, item.Status, len(item.Evidence.Tests), len(item.Evidence.Fixtures), markdownCell(blocker))
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "## Reproduction evidence")
	fmt.Fprintln(&output)
	for _, item := range report.MandatoryEntries {
		fmt.Fprintf(&output, "### %s — %s\n\n", item.ID, item.Status)
		for _, command := range item.Evidence.Commands {
			fmt.Fprintf(&output, "- Command: `%s`\n", command)
		}
		for _, test := range item.Evidence.Tests {
			fmt.Fprintf(&output, "- Test: `%s#%s`\n", test.Path, test.Symbol)
		}
		for _, fixture := range item.Evidence.Fixtures {
			fmt.Fprintf(&output, "- Fixture: `%s`\n", fixture)
		}
		for _, blocker := range item.Blockers {
			fmt.Fprintf(&output, "- Blocker: %s\n", blocker)
		}
		fmt.Fprintln(&output)
	}
	fmt.Fprintln(&output, "## Migration-only entries")
	fmt.Fprintln(&output)
	for _, item := range report.MigrationEntries {
		fmt.Fprintf(&output, "- `%s`: **%s**\n", item.ID, item.Status)
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "## Known intentional differences")
	fmt.Fprintln(&output)
	for _, item := range report.KnownIntentionalDifferences {
		for _, difference := range item.Differences {
			fmt.Fprintf(&output, "- `%s`: %s\n", item.ID, difference)
		}
	}
	fmt.Fprintln(&output)
	fmt.Fprintln(&output, "## Sign-off decision")
	fmt.Fprintln(&output)
	fmt.Fprintf(&output, "Signed off: %s\n", signOffValue.Note)
	return output.String()
}

func uniqueNonEmpty(values []string) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return errors.New("empty value is not allowed")
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate value %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func stringIn(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func markdownCell(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", `\|`), "\n", "<br>")
}

func writeFileAtomically(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("report destination %s is not a regular file", filepath.ToSlash(path))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".parity-report-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return fmt.Errorf("report destination %s changed type", filepath.ToSlash(path))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = directory.Sync()
	return errors.Join(err, directory.Close())
}

func assertCurrent(path string, expected []byte) error {
	actual, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("%s is stale; run go run ./test/parity/validate.go --write-report", filepath.ToSlash(path))
	}
	return nil
}

func fatalIf(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
