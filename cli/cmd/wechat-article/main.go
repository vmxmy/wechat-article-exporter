package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/app"
)

func main() {
	application := app.New(os.Stdin, os.Stdout, os.Stderr)
	defer application.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := application.Execute(ctx, os.Args[1:]); err != nil {
		if application.JSONOutputEnabled() || app.JSONRequested(os.Args[1:]) {
			_ = app.WriteErrorJSON(os.Stdout, err)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(app.ExitCode(err))
	}
}
