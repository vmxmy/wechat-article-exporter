package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/app"
)

func main() {
	application := app.New(os.Stdin, os.Stdout, os.Stderr)
	if err := application.Execute(context.Background(), os.Args[1:]); err != nil {
		if application.JSONOutputEnabled() || app.JSONRequested(os.Args[1:]) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"success": false,
				"error":   map[string]any{"message": err.Error(), "exitCode": app.ExitCode(err)},
			})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(app.ExitCode(err))
	}
}
