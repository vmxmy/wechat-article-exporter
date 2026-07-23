package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	localweb "github.com/wechat-article/wechat-article-exporter/cli/internal/web"
)

func (a *App) webCommand() *cobra.Command {
	var noOpen bool
	command := &cobra.Command{
		Use:   "web",
		Short: "Run the local loopback browser workspace",
		Long:  "Run a browser workspace only on a random 127.0.0.1 IPv4 port. The displayed URL contains a one-time local bootstrap credential.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if a.jsonOut {
				return usage("web does not support --json because stdout is reserved for the local workspace URL")
			}
			if a.core == nil {
				return errors.New("active profile runtime is unavailable")
			}
			if a.active == nil || a.active.Library == nil {
				return errors.New("active export workspace is unavailable")
			}
			exports := application.NewWorkspaceExports(a.core, a.active.Library, application.WorkspaceExportsOptions{
				ConfiguredRoot: webConfiguredExportRoot(a.active.Profile.Paths.Config),
			})
			preview := newWorkspaceExtensions(a).(application.WorkspaceArticlePreviewRenderer)
			maintenance, storageMaintenance := newWebMaintenance(a)
			credentialUploads := newWebCredentialUpload(maintenance)
			diagnosticBundles := newWebDiagnosticBundles(a)
			restore, err := newWebRestore(a)
			if err != nil {
				return err
			}
			accountManifests, err := newWebAccountManifests(a)
			if err != nil {
				return err
			}
			server, err := localweb.New(localweb.Options{
				Application: a.core, Exports: exports, Preview: preview,
				Maintenance: maintenance, CredentialUploads: credentialUploads, StorageMaintenance: storageMaintenance, DiagnosticBundles: diagnosticBundles, Restore: restore, AccountManifests: accountManifests,
			})
			if err != nil {
				return err
			}
			defer server.Close()
			if err := server.Start(); err != nil {
				return err
			}
			workspaceURL := server.URL()
			if workspaceURL == "" {
				return errors.New("local browser workspace did not expose a URL")
			}
			if _, err := fmt.Fprintln(a.stdout, workspaceURL); err != nil {
				return fmt.Errorf("write local browser workspace URL: %w", err)
			}
			if !noOpen {
				open := a.webOpenBrowser
				if open == nil {
					open = a.openBrowser
				}
				if err := open(command.Context(), workspaceURL); err != nil {
					// The URL is already available on stdout; inability to launch a
					// desktop opener must not terminate the local workspace.
					fmt.Fprintln(a.stderr, "could not open a local browser; open the URL printed on stdout manually")
				}
			}
			return ignoreContextCancellation(server.Serve(command.Context()))
		},
	}
	command.Flags().BoolVar(&noOpen, "no-open", false, "do not open the local browser automatically")
	return command
}

// webConfiguredExportRoot binds the config path while the web server is
// created, so later profile restore/reopen work cannot race on App.active.
// The browser receives only an opaque directory token, never this host path.
func webConfiguredExportRoot(configPath string) application.WorkspaceExportRootProvider {
	return func(context.Context) (string, error) {
		configuration, _, err := profiles.NewConfigStore(configPath).Read()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(configuration.Preferences.Export.Root), nil
	}
}

func ignoreContextCancellation(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}
