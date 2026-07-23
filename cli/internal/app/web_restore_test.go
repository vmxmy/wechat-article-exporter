package app

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

func TestWebRestoreFileStagingKeepsArchivePrivateAndCleansUp(t *testing.T) {
	instance, _, _ := newTestApp(t)
	backend := applicationUploadFileStaging{app: instance}
	object, err := backend.Stage(context.Background(), strings.NewReader("restore archive"), -1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(object.Reference, "web-restore") {
		t.Fatalf("staged archive path = %q", object.Reference)
	}
	info, err := os.Stat(object.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("staged archive mode = %o, want owner-only", info.Mode().Perm())
	}
	reader, err := backend.Open(context.Background(), object)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "restore archive" {
		t.Fatalf("staged contents = %q", contents)
	}
	if err := backend.Delete(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(object.Reference); !os.IsNotExist(err) {
		t.Fatalf("staged archive remained after cleanup: %v", err)
	}
}

var _ application.UploadStagingBackend = applicationUploadFileStaging{}
