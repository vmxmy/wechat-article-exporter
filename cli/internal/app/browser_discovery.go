package app

import (
	"context"
	"os/exec"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	runtimeenv "github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
)

type localChromiumDiscovery struct{}

func (localChromiumDiscovery) FindChromium(ctx context.Context) (runtimeenv.Browser, error) {
	browser, err := exporter.DiscoverChromium(exporter.ChromiumDiscoveryOptions{})
	if err != nil {
		return runtimeenv.Browser{}, err
	}
	version := ""
	if output, commandErr := exec.CommandContext(ctx, browser.Path, "--version").CombinedOutput(); commandErr == nil {
		version = strings.TrimSpace(string(output))
	}
	return runtimeenv.Browser{Path: browser.Path, Version: version}, nil
}
