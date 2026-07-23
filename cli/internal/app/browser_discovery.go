package app

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

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
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	boundedOutput := boundedBrowserVersionWriter{buffer: &output, remaining: 4 << 10}
	command := exec.CommandContext(versionCtx, browser.Path, "--version")
	command.Stdout = &boundedOutput
	command.Stderr = &boundedOutput
	command.WaitDelay = time.Second
	if commandErr := command.Run(); commandErr == nil {
		version = strings.TrimSpace(output.String())
	}
	return runtimeenv.Browser{Path: browser.Path, Version: version}, nil
}

type boundedBrowserVersionWriter struct {
	buffer    *bytes.Buffer
	remaining int
}

func (writer *boundedBrowserVersionWriter) Write(value []byte) (int, error) {
	original := len(value)
	if writer.remaining <= 0 {
		return original, nil
	}
	if len(value) > writer.remaining {
		value = value[:writer.remaining]
	}
	written, err := writer.buffer.Write(value)
	writer.remaining -= written
	if err != nil {
		return written, err
	}
	return original, nil
}
