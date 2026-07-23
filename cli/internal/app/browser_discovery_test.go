package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestRuntimeManagerDefaultsToLocalChromiumDiscovery(t *testing.T) {
	root := t.TempDir()
	manager := newRuntimeManager("test", profiles.Paths{ConfigRoot: filepath.Join(root, "config")}, Dependencies{Secrets: secrets.NewMemoryStore()})
	if manager.browser == nil {
		t.Fatal("default browser discovery is nil")
	}
	if manager.browserExplicit {
		t.Fatal("default Chromium discovery must not replace the platform browser preview launcher")
	}
	_, _ = manager.browser.FindChromium(context.Background())
}
