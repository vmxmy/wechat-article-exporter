package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractReleaseArchiveRejectsTraversalSymlinkAndDuplicate(t *testing.T) {
	root := t.TempDir()
	traversal := filepath.Join(root, "traversal.zip")
	writeTestZIP(t, traversal, []testArchiveMember{{name: "../escape", body: []byte("bad")}})
	if _, err := extractReleaseArchive(traversal, filepath.Join(root, "out-traversal")); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("traversal error = %v", err)
	}

	symlink := filepath.Join(root, "symlink.tar.gz")
	writeTestTarGzip(t, symlink, []testArchiveMember{{name: "candidate/link", typeflag: tar.TypeSymlink, linkname: "/tmp/escape"}})
	if _, err := extractReleaseArchive(symlink, filepath.Join(root, "out-symlink")); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("symlink error = %v", err)
	}

	duplicate := filepath.Join(root, "duplicate.zip")
	writeTestZIP(t, duplicate, []testArchiveMember{{name: "candidate/file", body: []byte("one")}, {name: "candidate/file", body: []byte("two")}})
	if _, err := extractReleaseArchive(duplicate, filepath.Join(root, "out-duplicate")); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestParseBuildInfoAndExactVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build-info.txt")
	body := `candidate: go1.25.0
	path	github.com/wechat-article/wechat-article-exporter/cli/cmd/wechat-article
	mod	github.com/wechat-article/wechat-article-exporter/cli	(devel)
	build	CGO_ENABLED=0
	build	GOARCH=amd64
	build	GOOS=linux
	build	vcs.revision=0123456789012345678901234567890123456789
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata, err := parseBuildInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Module != "github.com/wechat-article/wechat-article-exporter/cli" || metadata.GOOS != "linux" || metadata.GOARCH != "amd64" || metadata.CGOEnabled != "0" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if !exactVersionOutput("2.0.1", "2.0.1") || exactVersionOutput("2.0.10", "2.0.1") {
		t.Fatal("exact version comparison is not exact")
	}
}

type testArchiveMember struct {
	name     string
	body     []byte
	typeflag byte
	linkname string
}

func writeTestZIP(t *testing.T, path string, members []testArchiveMember) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, member := range members {
		entry, err := writer.Create(member.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(member.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestTarGzip(t *testing.T, path string, members []testArchiveMember) {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for _, member := range members {
		typeflag := member.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{Name: member.name, Typeflag: typeflag, Linkname: member.linkname, Mode: 0o600, Size: int64(len(member.body))}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(member.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
