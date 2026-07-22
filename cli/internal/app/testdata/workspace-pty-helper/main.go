package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/app"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func main() {
	root := strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_PTY_ROOT"))
	if root == "" {
		fmt.Fprintln(os.Stderr, "PTY helper portable root is empty")
		os.Exit(2)
	}
	application, err := app.NewWithDependencies(context.Background(), os.Stdin, os.Stdout, os.Stderr, app.Dependencies{
		PathOptions: profiles.PathOptions{Portable: true, PortableRoot: root},
		Secrets:     secrets.NewMemoryStore(),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	application.ForceWorkspaceForTesting()
	if err := application.Execute(context.Background(), nil); err != nil {
		_ = application.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(app.ExitCode(err))
	}
	if err := application.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
