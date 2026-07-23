package corpus

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

const protocolVerificationCommand = "cd cli && go test -count=1 ./internal/corpus"

var protocolCaseFiles = []string{
	"case.json",
	"request.sanitized.json",
	"response.sanitized.json",
	"expected.normalized.json",
}

type protocolCoverage struct {
	SchemaVersion int `json:"schemaVersion"`
	Verification  struct {
		Command       string   `json:"command"`
		RequiredFiles []string `json:"requiredFiles"`
	} `json:"verification"`
	Domains map[string]struct {
		Status   string   `json:"status"`
		Required []string `json:"required"`
	} `json:"domains"`
}

type protocolCase struct {
	SchemaVersion  int      `json:"schemaVersion"`
	Domain         string   `json:"domain"`
	Name           string   `json:"name"`
	Operation      string   `json:"operation"`
	Classification string   `json:"classification"`
	Provenance     string   `json:"provenance"`
	CapturedAt     string   `json:"capturedAt"`
	Sanitization   []string `json:"sanitization"`
}

func TestProtocolCorpusIsCompleteAndSanitized(t *testing.T) {
	root := protocolCorpusRoot(t)
	var coverage protocolCoverage
	readProtocolJSON(t, filepath.Join(root, "coverage.json"), &coverage)
	if coverage.SchemaVersion != 1 {
		t.Fatalf("coverage schemaVersion = %d, want 1", coverage.SchemaVersion)
	}
	if coverage.Verification.Command != protocolVerificationCommand {
		t.Fatalf("coverage verification command = %q, want %q", coverage.Verification.Command, protocolVerificationCommand)
	}
	if !reflect.DeepEqual(coverage.Verification.RequiredFiles, protocolCaseFiles) {
		t.Fatalf("coverage requiredFiles = %v, want %v", coverage.Verification.RequiredFiles, protocolCaseFiles)
	}

	discoveredDomains := directoryNames(t, root)
	expectedDomains := sortedMapKeys(coverage.Domains)
	if !reflect.DeepEqual(discoveredDomains, expectedDomains) {
		t.Fatalf("protocol domains = %v, coverage domains = %v", discoveredDomains, expectedDomains)
	}

	for _, domain := range expectedDomains {
		domainCoverage := coverage.Domains[domain]
		if domainCoverage.Status != "captured" {
			t.Errorf("domain %s status = %q, want captured", domain, domainCoverage.Status)
		}
		if len(domainCoverage.Required) == 0 {
			t.Errorf("domain %s has no required cases", domain)
			continue
		}
		discoveredCases := directoryNames(t, filepath.Join(root, domain))
		requiredCases := append([]string(nil), domainCoverage.Required...)
		sort.Strings(requiredCases)
		if !reflect.DeepEqual(discoveredCases, requiredCases) {
			t.Errorf("domain %s cases = %v, required = %v", domain, discoveredCases, requiredCases)
			continue
		}
		for _, name := range requiredCases {
			t.Run(domain+"/"+name, func(t *testing.T) {
				verifyProtocolCase(t, root, domain, name)
			})
		}
	}
}

func verifyProtocolCase(t *testing.T, root, domain, name string) {
	t.Helper()
	caseRoot := filepath.Join(root, domain, name)
	entries, err := os.ReadDir(caseRoot)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	expectedFiles := append([]string(nil), protocolCaseFiles...)
	sort.Strings(expectedFiles)
	if !reflect.DeepEqual(files, expectedFiles) {
		t.Fatalf("case files = %v, want %v", files, expectedFiles)
	}

	var metadata protocolCase
	readProtocolJSON(t, filepath.Join(caseRoot, "case.json"), &metadata)
	if metadata.SchemaVersion != 1 || metadata.Domain != domain || metadata.Name != name {
		t.Fatalf("case identity = schema %d domain %q name %q", metadata.SchemaVersion, metadata.Domain, metadata.Name)
	}
	if strings.TrimSpace(metadata.Operation) == "" || strings.TrimSpace(metadata.Classification) == "" {
		t.Fatalf("case operation/classification must be non-empty: %#v", metadata)
	}
	if metadata.Provenance != "sanitized-synthetic-shape-derived-from-current-web-implementation" {
		t.Fatalf("case provenance = %q", metadata.Provenance)
	}
	if _, err := time.Parse(time.RFC3339Nano, metadata.CapturedAt); err != nil {
		t.Fatalf("case capturedAt = %q: %v", metadata.CapturedAt, err)
	}
	for _, required := range []string{"tokens replaced", "cookies omitted", "private identifiers pseudonymized", "article body synthetic"} {
		if !containsString(metadata.Sanitization, required) {
			t.Errorf("case sanitization is missing %q", required)
		}
	}

	for _, name := range protocolCaseFiles[1:] {
		var document any
		readProtocolJSON(t, filepath.Join(caseRoot, name), &document)
		if _, ok := document.(map[string]any); !ok {
			t.Errorf("%s must contain a JSON object", name)
		}
		verifySensitiveValuesAreRedacted(t, name, document)
	}
}

func verifySensitiveValuesAreRedacted(t *testing.T, path string, value any) {
	t.Helper()
	sensitive := map[string]struct{}{
		"access_token": {}, "appmsg_token": {}, "authorization": {}, "cookie": {},
		"pass_ticket": {}, "password": {}, "refresh_token": {}, "secret": {}, "token": {},
	}
	var walk func(string, any)
	walk = func(current string, currentValue any) {
		switch typed := currentValue.(type) {
		case map[string]any:
			for key, child := range typed {
				childPath := current + "." + key
				if _, ok := sensitive[strings.ToLower(key)]; ok {
					text, stringValue := child.(string)
					if !stringValue || text != "<redacted>" {
						t.Errorf("%s contains non-redacted sensitive field %s", path, childPath)
					}
				}
				walk(childPath, child)
			}
		case []any:
			for index, child := range typed {
				walk(fmt.Sprintf("%s[%d]", current, index), child)
			}
		}
	}
	walk("$", value)
}

func protocolCorpusRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate protocol corpus test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "test", "fixtures", "protocol"))
}

func directoryNames(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func readProtocolJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
