package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

type proxyManager interface {
	Add(context.Context, network.AddProxyRequest) (network.RouteConfig, error)
	List(context.Context) ([]network.RouteConfig, error)
	Remove(context.Context, string) (network.RouteConfig, error)
	Enable(context.Context, string) (network.RouteConfig, error)
	Disable(context.Context, string) (network.RouteConfig, error)
	Test(context.Context, string) (network.ProbeResult, error)
}

func (a *App) proxyCommand() *cobra.Command {
	proxy := &cobra.Command{Use: "proxy", Short: "Manage explicit local network proxy routes"}

	var endpoint string
	var authorization string
	var trustValue string
	var classesValue string
	var priority int
	var confirmation string
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a URL-wrapper proxy route",
		Args:  exactArgs(1, "proxy add requires <name>"),
		RunE: func(command *cobra.Command, args []string) error {
			manager, err := a.proxyManager()
			if err != nil {
				return err
			}
			trust, err := network.ParseTrustLevel(trustValue)
			if err != nil {
				return usage(err.Error())
			}
			classes, err := parseProxyClasses(classesValue)
			if err != nil {
				return usage(err.Error())
			}
			classes, err = network.NormalizeRequestClasses(classes)
			if err != nil {
				return usage(err.Error())
			}
			if priority < 1 || priority > 10000 {
				return usage("--priority must be between 1 and 10000")
			}
			if trust == network.TrustCredential {
				required := network.CredentialTrustConfirmation(args[0])
				if confirmation != required {
					return usage(fmt.Sprintf(
						"credential-trusted proxy %q can receive secrets [%s] for request classes [%s] at destination %s; retry with --confirm %s",
						args[0], strings.Join(network.CredentialSecretsForClasses(classes), ", "), proxyClassNames(classes),
						safety.RedactURL(endpoint), required,
					))
				}
			}
			route, err := manager.Add(command.Context(), network.AddProxyRequest{
				Name: args[0], Endpoint: endpoint, Authorization: authorization,
				Trust: trust, Classes: classes, Priority: priority,
			})
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": route})
		},
	}
	add.Flags().StringVar(&endpoint, "endpoint", "", "URL-wrapper endpoint (HTTPS or approved loopback HTTP)")
	add.Flags().StringVar(&authorization, "authorization", "", "optional proxy authorization stored in the profile secret store")
	add.Flags().StringVar(&trustValue, "trust", string(network.TrustPublicOnly), "public-only or credential-trusted")
	add.Flags().StringVar(&classesValue, "classes", string(network.PublicContent)+","+string(network.PublicResource), "comma-separated eligible request classes")
	add.Flags().IntVar(&priority, "priority", 100, "route priority; lower values are preferred")
	add.Flags().StringVar(&confirmation, "confirm", "", "exact confirmation required for credential-trusted routes")
	_ = add.MarkFlagRequired("endpoint")
	proxy.AddCommand(add)

	proxy.AddCommand(&cobra.Command{
		Use: "list", Short: "List proxy routes and health", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			manager, err := a.proxyManager()
			if err != nil {
				return err
			}
			routes, err := manager.List(command.Context())
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{"routes": routes, "count": len(routes)}})
		},
	})

	proxy.AddCommand(a.proxyOperationCommand("remove <name-or-id>", "Remove a proxy route and its authorization secret", "proxy remove", func(ctx context.Context, manager proxyManager, id string) (any, error) {
		return manager.Remove(ctx, id)
	}))
	proxy.AddCommand(a.proxyOperationCommand("enable <name-or-id>", "Enable a proxy route", "proxy enable", func(ctx context.Context, manager proxyManager, id string) (any, error) {
		return manager.Enable(ctx, id)
	}))
	proxy.AddCommand(a.proxyOperationCommand("disable <name-or-id>", "Disable a proxy route", "proxy disable", func(ctx context.Context, manager proxyManager, id string) (any, error) {
		return manager.Disable(ctx, id)
	}))
	proxy.AddCommand(a.proxyOperationCommand("test <name-or-id>", "Run a credential-free route health probe", "proxy test", func(ctx context.Context, manager proxyManager, id string) (any, error) {
		return manager.Test(ctx, id)
	}))
	return proxy
}

func (a *App) proxyOperationCommand(
	use string,
	short string,
	errorLabel string,
	operation func(context.Context, proxyManager, string) (any, error),
) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, errorLabel+" requires <name-or-id>"),
		RunE: func(command *cobra.Command, args []string) error {
			manager, err := a.proxyManager()
			if err != nil {
				return err
			}
			result, err := operation(command.Context(), manager, args[0])
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": result})
		},
	}
}

func (a *App) proxyManager() (proxyManager, error) {
	if a.proxy != nil {
		return a.proxy, nil
	}
	if a.active == nil || a.active.Network == nil {
		return nil, errors.New("active profile network service is unavailable")
	}
	return a.active.Network, nil
}

func parseProxyClasses(value string) ([]network.RequestClass, error) {
	parts := strings.Split(value, ",")
	classes := make([]network.RequestClass, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		classes = append(classes, network.RequestClass(part))
	}
	if len(classes) == 0 {
		return nil, errors.New("--classes must include at least one request class")
	}
	return classes, nil
}

func proxyClassNames(classes []network.RequestClass) string {
	names := make([]string, len(classes))
	for index, class := range classes {
		names[index] = string(class)
	}
	return strings.Join(names, ", ")
}
