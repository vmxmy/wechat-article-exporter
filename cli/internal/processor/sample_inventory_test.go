package processor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type sampleInventory struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Entries       []sampleInventoryEntry `json:"entries"`
}

type sampleInventoryEntry struct {
	Path        string `json:"path"`
	Disposition string `json:"disposition"`
	Golden      string `json:"golden,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ReviewedAt  string `json:"reviewedAt,omitempty"`
}

func TestTrackedSamplesHaveGoldenOrReviewedExclusion(t *testing.T) {
	repositoryRoot := sampleRepositoryRoot(t)
	corpus := sampleHTMLOnDisk(t, filepath.Join(repositoryRoot, "samples"))

	data, err := os.ReadFile(filepath.Join(repositoryRoot, "samples", "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory sampleInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.SchemaVersion != 1 {
		t.Fatalf("inventory schemaVersion = %d, want 1", inventory.SchemaVersion)
	}

	entries := make(map[string]sampleInventoryEntry, len(inventory.Entries))
	for _, entry := range inventory.Entries {
		path := filepath.ToSlash(filepath.Clean(entry.Path))
		if path == "." || filepath.IsAbs(entry.Path) || strings.HasPrefix(path, "../") || path != entry.Path || !strings.HasSuffix(path, ".html") {
			t.Errorf("invalid inventory path %q", entry.Path)
			continue
		}
		if _, exists := entries[path]; exists {
			t.Errorf("duplicate inventory path %q", path)
			continue
		}
		entries[path] = entry
		switch entry.Disposition {
		case "golden":
			if strings.TrimSpace(entry.Golden) == "" {
				t.Errorf("golden sample %q has no golden evidence", path)
				continue
			}
			verifySampleGoldenEvidence(t, repositoryRoot, entry)
		case "reviewed-exclusion":
			if strings.TrimSpace(entry.Reason) == "" {
				t.Errorf("reviewed exclusion %q has no reason", path)
			}
			if _, err := time.Parse("2006-01-02", entry.ReviewedAt); err != nil {
				t.Errorf("reviewed exclusion %q reviewedAt = %q: %v", path, entry.ReviewedAt, err)
			}
		default:
			t.Errorf("sample %q disposition = %q", path, entry.Disposition)
		}
	}

	inventoried := make([]string, 0, len(entries))
	for path := range entries {
		inventoried = append(inventoried, path)
	}
	sort.Strings(inventoried)
	if !reflect.DeepEqual(inventoried, corpus) {
		t.Fatalf("sample inventory does not match committed HTML corpus\ncorpus: %v\ninventory: %v", corpus, inventoried)
	}
}

func sampleHTMLOnDisk(t *testing.T, samplesRoot string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(samplesRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".html") {
			return nil
		}
		relative, err := filepath.Rel(samplesRoot, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatalf("walk sample HTML: %v", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("sample corpus contains no HTML")
	}
	return paths
}

func verifySampleGoldenEvidence(t *testing.T, repositoryRoot string, entry sampleInventoryEntry) {
	t.Helper()
	goldenPath := filepath.Clean(filepath.Join(repositoryRoot, filepath.FromSlash(entry.Golden)))
	if !strings.HasPrefix(goldenPath, repositoryRoot+string(filepath.Separator)) {
		t.Errorf("golden evidence for %q escapes repository: %q", entry.Path, entry.Golden)
		return
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Errorf("read golden evidence for %q: %v", entry.Path, err)
		return
	}
	var cases []struct {
		Path []string `json:"path"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Errorf("parse golden evidence for %q: %v", entry.Path, err)
		return
	}
	for _, goldenCase := range cases {
		if strings.Join(goldenCase.Path, "/") == entry.Path {
			return
		}
	}
	t.Errorf("golden evidence %q has no case for %q", entry.Golden, entry.Path)
}

func sampleRepositoryRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Dir(samplePath(t))
	if filepath.Base(root) == "samples" {
		return filepath.Dir(root)
	}
	return root
}
