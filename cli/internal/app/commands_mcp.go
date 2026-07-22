package app

import (
	"strings"

	"github.com/spf13/cobra"
	localmcp "github.com/wechat-article/wechat-article-exporter/cli/internal/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
)

func (a *App) mcpCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "mcp",
		Short: "Run and inspect the local stdio MCP server",
		Long:  "Run the local profile-scoped MCP server. This command does not use remote OAuth or bind a network listener.",
	}
	var transport string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Serve MCP over stdin/stdout",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if strings.ToLower(strings.TrimSpace(transport)) != "stdio" {
				return usage("mcp serve currently requires --transport stdio")
			}
			if a.active == nil || a.active.Core == nil {
				return usage("mcp serve requires an active local profile")
			}
			configuration, _, err := profiles.NewConfigStore(a.active.Profile.Paths.Config).Read()
			if err != nil {
				return err
			}
			adapter := localmcp.New(a.active.Core, localmcp.Options{
				Version: Version,
				Profile: string(a.active.Profile.ID),
				Policy:  configuration.MCP,
			})
			return localmcp.NewServer(adapter).Serve(command.Context(), a.stdin, a.stdout, a.stderr)
		},
	}
	serve.Flags().StringVar(&transport, "transport", "stdio", "MCP transport (stdio only)")
	command.AddCommand(serve)
	return command
}
