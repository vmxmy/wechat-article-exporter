package exporter

import (
	"os"
	"path/filepath"
	"testing"
)

func readExporterFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
