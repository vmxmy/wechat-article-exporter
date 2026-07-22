package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/input"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/mcpclient"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/oauth"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/tui"
	"golang.org/x/oauth2"
)

const (
	DefaultServer = "https://mptext.ziikoo.app"
)

var Version = "2.0.0"

type App struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	store  *config.Store

	server  string
	jsonOut bool
	debug   bool
}

func New(stdin io.Reader, stdout, stderr io.Writer) *App {
	return &App{stdin: stdin, stdout: stdout, stderr: stderr, store: config.NewStore("")}
}

func (a *App) Execute(ctx context.Context, args []string) error {
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetContext(ctx)
	err := root.Execute()
	if err == nil {
		return nil
	}
	var usageError *UsageError
	if errors.As(err, &usageError) {
		return err
	}
	if isCobraUsageError(err) {
		return usage(err.Error())
	}
	return err
}

func (a *App) JSONOutputEnabled() bool { return a.jsonOut }

func (a *App) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "wechat-article",
		Short:         "Remote CLI for WeChat article export",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			if a.debug {
				a.debugf("config=%s", a.store.Path())
			}
		},
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if a.jsonOut {
				return usage("a command is required with --json")
			}
			if !tui.IsInteractive(a.stdin, a.stdout) {
				return command.Help()
			}
			return a.runDashboard(command.Context())
		},
	}
	root.SetIn(a.stdin)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usage(err.Error()) })
	root.PersistentFlags().StringVar(&a.server, "server", "", "MCP server base URL")
	root.PersistentFlags().BoolVar(&a.jsonOut, "json", false, "force machine-readable JSON output")
	root.PersistentFlags().BoolVar(&a.debug, "debug", false, "enable verbose development logging")
	root.AddCommand(
		a.loginCommand(),
		a.logoutCommand(),
		a.statusCommand(),
		a.apiCommand("api"),
		a.apiCommand("mcp"),
		a.articleCommand(),
		a.accountCommand(),
		a.albumCommand(),
	)
	return root
}

func (a *App) loginCommand() *cobra.Command {
	var headless bool
	var noOpen bool
	command := &cobra.Command{
		Use:   "login",
		Short: "Authorize this CLI with the remote MCP server",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if headless && noOpen {
				return usage("--headless and --no-open cannot be used together")
			}
			return a.login(command.Context(), headless, noOpen)
		},
	}
	command.Flags().BoolVar(&headless, "headless", false, "show a callback command for a browserless machine")
	command.Flags().BoolVar(&noOpen, "no-open", false, "print the authorization URL without opening a browser")
	return command
}

func (a *App) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove saved OAuth credentials",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cleared, err := a.store.ClearSession()
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": map[string]any{"server": cleared.Server, "authenticated": false}})
		},
	}
}

func (a *App) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"whoami"},
		Short:   "Show local authentication state",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.printStatus()
		},
	}
}

func (a *App) apiCommand(name string) *cobra.Command {
	command := &cobra.Command{Use: name, Short: "Discover and call the server-advertised MCP tool surface"}
	listName := "list"
	if name == "mcp" {
		listName = "tools"
	}
	command.AddCommand(&cobra.Command{
		Use:   listName,
		Short: "List remote MCP tools",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			client, err := a.authenticatedClient(command.Context())
			if err != nil {
				return err
			}
			defer client.Close()
			tools, err := client.ListTools(command.Context())
			if err != nil {
				return err
			}
			sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
			return a.output(map[string]any{"success": true, "data": map[string]any{"count": len(tools), "tools": tools}})
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "describe <tool>",
		Short: "Describe one MCP tool",
		Args:  exactArgs(1, "api describe requires a tool name"),
		RunE: func(command *cobra.Command, args []string) error {
			client, err := a.authenticatedClient(command.Context())
			if err != nil {
				return err
			}
			defer client.Close()
			tool, err := findTool(command.Context(), client, args[0])
			if err != nil {
				return err
			}
			return a.output(map[string]any{"success": true, "data": tool})
		},
	})
	command.AddCommand(a.callCommand())
	return command
}

func (a *App) callCommand() *cobra.Command {
	var options input.Options
	var dryRun bool
	var confirmation string
	command := &cobra.Command{
		Use:   "call <tool>",
		Short: "Call one MCP tool with structured JSON input",
		Args:  exactArgs(1, "api call requires a tool name"),
		RunE: func(command *cobra.Command, args []string) error {
			options.InlineSet = command.Flags().Changed("input")
			options.FileSet = command.Flags().Changed("file")
			arguments, err := input.Load(options, a.stdin)
			if err != nil {
				return usage(err.Error())
			}
			return a.executeTool(command.Context(), args[0], arguments, dryRun, confirmation)
		},
	}
	command.Flags().StringVar(&options.Inline, "input", "", "inline JSON object")
	command.Flags().StringVar(&options.File, "file", "", "read JSON object from a file")
	command.Flags().BoolVar(&options.Stdin, "stdin", false, "read JSON object from stdin")
	command.Flags().BoolVar(&dryRun, "dry-run", false, "print a redacted preview without connecting")
	command.Flags().StringVar(&confirmation, "confirm", "", "exact tool name required for protected operations")
	return command
}

func (a *App) articleCommand() *cobra.Command {
	article := &cobra.Command{Use: "article", Short: "Download and list WeChat articles"}
	var format string
	var dryRun bool
	article.AddCommand(&cobra.Command{
		Use:   "download <url>",
		Short: "Download an article",
		Args:  exactArgs(1, "article download requires <url>"),
		RunE: func(command *cobra.Command, args []string) error {
			return a.executeTool(command.Context(), "download_article", map[string]any{"url": args[0], "format": format}, dryRun, "")
		},
	})
	download := article.Commands()[0]
	download.Flags().StringVar(&format, "format", "markdown", "markdown, text, html, or json")
	download.Flags().BoolVar(&dryRun, "dry-run", false, "print a redacted preview without connecting")

	var keyword string
	var begin, size int
	article.AddCommand(&cobra.Command{
		Use:   "list <fakeid>",
		Short: "List articles for an account",
		Args:  exactArgs(1, "article list requires <fakeid>"),
		RunE: func(command *cobra.Command, args []string) error {
			if begin < 0 || size < 0 {
				return usage("--begin and --size must be non-negative integers")
			}
			arguments := map[string]any{"fakeid": args[0], "begin": begin, "size": size}
			if keyword != "" {
				arguments["keyword"] = keyword
			}
			return a.executeTool(command.Context(), "list_articles", arguments, false, "")
		},
	})
	list := article.Commands()[1]
	list.Flags().StringVar(&keyword, "keyword", "", "filter by title keyword")
	list.Flags().IntVar(&begin, "begin", 0, "pagination offset")
	list.Flags().IntVar(&size, "size", 5, "number of articles")
	return article
}

func (a *App) accountCommand() *cobra.Command {
	account := &cobra.Command{Use: "account", Short: "Search and inspect WeChat accounts"}
	var begin, size int
	search := &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search WeChat accounts",
		Args:  exactArgs(1, "account search requires <keyword>"),
		RunE: func(command *cobra.Command, args []string) error {
			if begin < 0 || size < 0 {
				return usage("--begin and --size must be non-negative integers")
			}
			return a.executeTool(command.Context(), "search_accounts", map[string]any{"keyword": args[0], "begin": begin, "size": size}, false, "")
		},
	}
	search.Flags().IntVar(&begin, "begin", 0, "pagination offset")
	search.Flags().IntVar(&size, "size", 5, "number of accounts")
	account.AddCommand(search)
	account.AddCommand(a.aliasCommand("from-url <url>", "Resolve account information from an article URL", "get_account_by_url", "url", "account from-url requires <url>"))
	account.AddCommand(a.aliasCommand("details <fakeid>", "Get account details", "get_account_details", "fakeid", "account details requires <fakeid>"))
	account.AddCommand(a.aliasCommand("author <fakeid>", "Get account author information", "get_author_info", "fakeid", "account author requires <fakeid>"))
	account.AddCommand(a.aliasCommand("name <url>", "Get account name from an article URL", "get_account_name", "url", "account name requires <url>"))
	return account
}

func (a *App) albumCommand() *cobra.Command {
	album := &cobra.Command{Use: "album", Short: "List articles in a WeChat album"}
	var count int
	var beginMsgID, beginItemIndex string
	list := &cobra.Command{
		Use:   "list <fakeid> <albumId>",
		Short: "List album articles",
		Args:  exactArgs(2, "album list requires <fakeid> <albumId>"),
		RunE: func(command *cobra.Command, args []string) error {
			if count < 0 {
				return usage("--count must be a non-negative integer")
			}
			arguments := map[string]any{"fakeid": args[0], "album_id": args[1], "count": count}
			if beginMsgID != "" {
				arguments["begin_msgid"] = beginMsgID
			}
			if beginItemIndex != "" {
				arguments["begin_itemidx"] = beginItemIndex
			}
			return a.executeTool(command.Context(), "list_album", arguments, false, "")
		},
	}
	list.Flags().IntVar(&count, "count", 10, "number of articles")
	list.Flags().StringVar(&beginMsgID, "begin-msgid", "", "pagination message ID")
	list.Flags().StringVar(&beginItemIndex, "begin-itemidx", "", "pagination item index")
	album.AddCommand(list)
	return album
}

func (a *App) aliasCommand(use, short, tool, key, message string) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: exactArgs(1, message),
		RunE: func(command *cobra.Command, args []string) error {
			return a.executeTool(command.Context(), tool, map[string]any{key: args[0]}, false, "")
		},
	}
}

func (a *App) executeTool(ctx context.Context, toolName string, arguments map[string]any, dryRun bool, confirmation string) error {
	if dryRun {
		preview := safety.DryRun(toolName, arguments)
		preview["requiredConfirmation"] = safety.RequiredConfirmation(&mcp.Tool{Name: toolName, InputSchema: map[string]any{"type": "object"}})
		return a.output(preview)
	}
	spinner := a.spinner("Connecting to remote MCP server…")
	client, err := a.authenticatedClient(ctx)
	if err != nil {
		spinner.Stop("MCP connection failed", false)
		return err
	}
	defer client.Close()
	tool, err := findTool(ctx, client, toolName)
	if err != nil {
		spinner.Stop("Tool discovery failed", false)
		return err
	}
	if err := safety.AssertConfirmation(tool, confirmation); err != nil {
		spinner.Stop("Confirmation required", false)
		return err
	}
	result, err := client.CallTool(ctx, tool.Name, arguments)
	if err != nil {
		spinner.Stop("Remote tool call failed", false)
		return err
	}
	spinner.Stop("Remote tool call completed", !result.IsError)
	if err := a.output(result); err != nil {
		return err
	}
	if result.IsError {
		return errors.New("remote MCP tool returned an error")
	}
	return nil
}

func (a *App) authenticatedClient(ctx context.Context) (*mcpclient.Client, error) {
	stored, err := a.store.Read()
	if err != nil {
		return nil, err
	}
	server, err := a.resolveServer(stored.Server)
	if err != nil {
		return nil, err
	}
	if stored.Tokens == nil || stored.Tokens.AccessToken == "" || !sameServer(stored.Server, server) || !sessionUsable(stored) {
		return nil, fmt.Errorf("not logged in to %s; run: wechat-article login --server %s", server, server)
	}
	handler := &savedOAuthHandler{store: a.store, server: server, httpClient: http.DefaultClient}
	return mcpclient.Connect(ctx, mcpclient.Options{Endpoint: oauth.MCPURL(server), Version: Version, OAuthHandler: handler})
}

func (a *App) login(ctx context.Context, headless, noOpen bool) error {
	stored, err := a.store.Read()
	if err != nil {
		return err
	}
	server, err := a.resolveServer(stored.Server)
	if err != nil {
		return err
	}
	callback, err := newCallbackServer(5 * time.Minute)
	if err != nil {
		return err
	}
	defer callback.Close()

	httpClient := oauth.NewRegistrationHTTPClient(http.DefaultClient, a.store, server, callback.RedirectURL)
	handler, err := oauth.NewPersistentHandler(oauth.BrowserOptions{
		Store: a.store, Server: server, RedirectURL: callback.RedirectURL, HTTPClient: httpClient, ClientVersion: Version,
		FetchCode: func(flowContext context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			authorizationURL, parseErr := url.Parse(args.URL)
			if parseErr != nil {
				return nil, parseErr
			}
			if headless {
				query := authorizationURL.Query()
				query.Set("headless", "1")
				authorizationURL.RawQuery = query.Encode()
			}
			fmt.Fprintf(a.stderr, "Open this authorization URL:\n%s\n", authorizationURL.String())
			if !noOpen && !headless {
				_ = openBrowser(authorizationURL.String())
			}
			result, waitErr := callback.Wait(flowContext, authorizationURL.Query().Get("state"))
			if waitErr != nil {
				return nil, waitErr
			}
			return &auth.AuthorizationResult{Code: result.Code, State: result.State}, nil
		},
	})
	if err != nil {
		return err
	}
	spinner := a.spinner("Waiting for OAuth authorization…")
	client, err := mcpclient.Connect(ctx, mcpclient.Options{Endpoint: oauth.MCPURL(server), Version: Version, HTTPClient: httpClient, OAuthHandler: handler})
	if err != nil {
		spinner.Stop("OAuth authorization failed", false)
		return err
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil {
		spinner.Stop("OAuth verification failed", false)
		return err
	}
	spinner.Stop("OAuth authorization completed", true)
	return a.output(map[string]any{"success": true, "data": map[string]any{"server": server, "authenticated": true, "toolCount": len(tools)}})
}

func (a *App) printStatus() error {
	stored, err := a.store.Read()
	if err != nil {
		return err
	}
	server, err := a.resolveServer(stored.Server)
	if err != nil {
		return err
	}
	authenticated := stored.Tokens != nil && stored.Tokens.AccessToken != "" && sameServer(stored.Server, server) && sessionUsable(stored)
	data := map[string]any{
		"server":        server,
		"authenticated": authenticated,
		"refreshable":   authenticated && stored.Tokens.RefreshToken != "",
		"configPath":    a.store.Path(),
	}
	if a.jsonOut || !tui.IsInteractive(a.stdin, a.stdout) {
		return a.output(map[string]any{"success": true, "data": data})
	}
	detail := fmt.Sprintf("server: %s\nconfig: %s", server, a.store.Path())
	tui.RenderStatus(a.stdout, tui.Status{Label: statusLabel(authenticated), Detail: detail, Success: authenticated})
	return nil
}

func (a *App) runDashboard(ctx context.Context) error {
	choice, err := tui.Choose(ctx, a.stdin, a.stdout, "WeChat Article CLI", []tui.MenuItem{
		{Title: "查看登录状态", Description: "本地 OAuth 凭据与远端地址", Value: "status"},
		{Title: "登录 / 重新授权", Description: "在浏览器完成 OAuth 2.1 授权", Value: "login"},
		{Title: "查看远端工具", Description: "动态读取 MCP server 的工具列表", Value: "tools"},
		{Title: "退出登录", Description: "清除本地 token，保留 server 地址", Value: "logout"},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	switch choice {
	case "status":
		return a.printStatus()
	case "login":
		return a.login(ctx, false, false)
	case "tools":
		client, err := a.authenticatedClient(ctx)
		if err != nil {
			return err
		}
		defer client.Close()
		tools, err := client.ListTools(ctx)
		if err != nil {
			return err
		}
		return a.output(map[string]any{"success": true, "data": map[string]any{"count": len(tools), "tools": tools}})
	case "logout":
		_, err := a.store.ClearSession()
		return err
	default:
		return nil
	}
}

func (a *App) resolveServer(saved string) (string, error) {
	value := a.server
	if value == "" {
		value = saved
	}
	if value == "" {
		value = DefaultServer
	}
	server, err := oauth.NormalizeServer(value)
	if err != nil {
		return "", usage(err.Error())
	}
	return server, nil
}

func (a *App) spinner(label string) *tui.Spinner {
	if a.jsonOut || !tui.IsInteractive(a.stdin, a.stderr) {
		return nil
	}
	return tui.StartSpinner(a.stderr, label)
}

func (a *App) output(value any) error {
	if !a.jsonOut && tui.IsInteractive(a.stdin, a.stdout) {
		if result, ok := value.(*mcp.CallToolResult); ok {
			if renderToolResult(a.stdout, result) {
				return nil
			}
		}
	}
	encoder := json.NewEncoder(a.stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func renderToolResult(output io.Writer, result *mcp.CallToolResult) bool {
	if result == nil || len(result.Content) == 0 {
		return false
	}
	texts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			return false
		}
		texts = append(texts, text.Text)
	}
	for _, text := range texts {
		fmt.Fprintln(output, text)
	}
	return true
}

func isCobraUsageError(err error) bool {
	message := err.Error()
	return strings.HasPrefix(message, "unknown command") ||
		strings.HasPrefix(message, "unknown flag") ||
		strings.Contains(message, "accepts ") ||
		strings.Contains(message, "requires at least") ||
		strings.Contains(message, "requires no arguments")
}

func findTool(ctx context.Context, client *mcpclient.Client, name string) (*mcp.Tool, error) {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, tool := range tools {
		if tool.Name == name {
			return tool, nil
		}
	}
	return nil, fmt.Errorf("MCP tool not found: %s", name)
}

func exactArgs(count int, message string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != count {
			return usage(message)
		}
		return nil
	}
}

func statusLabel(authenticated bool) string {
	if authenticated {
		return "Authenticated"
	}
	return "Not authenticated"
}

func sameServer(left, right string) bool {
	if left == "" {
		return false
	}
	normalizedLeft, leftErr := oauth.NormalizeServer(left)
	normalizedRight, rightErr := oauth.NormalizeServer(right)
	return leftErr == nil && rightErr == nil && normalizedLeft == normalizedRight
}

func sessionUsable(stored config.File) bool {
	expiry := stored.TokenExpiry()
	if expiry.IsZero() || time.Now().Before(expiry) {
		return true
	}
	return stored.Tokens != nil && stored.Tokens.RefreshToken != "" && stored.TokenEndpoint != "" &&
		stored.ClientInformation != nil && stored.ClientInformation.ClientID != ""
}

type savedOAuthHandler struct {
	store      *config.Store
	server     string
	httpClient *http.Client
}

func (h *savedOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	stored, err := h.store.Read()
	if err != nil {
		return nil, err
	}
	if stored.Tokens == nil || stored.Tokens.AccessToken == "" || !sameServer(stored.Server, h.server) {
		return nil, nil
	}
	token := &oauth2.Token{
		AccessToken: stored.Tokens.AccessToken, TokenType: stored.Tokens.TokenType, RefreshToken: stored.Tokens.RefreshToken,
	}
	if expiry := stored.TokenExpiry(); !expiry.IsZero() {
		token.Expiry = expiry
	}
	if stored.TokenEndpoint == "" || stored.ClientInformation == nil || stored.ClientInformation.ClientID == "" {
		return oauth2.StaticTokenSource(token), nil
	}
	config := oauth2.Config{
		ClientID: stored.ClientInformation.ClientID, ClientSecret: stored.ClientInformation.ClientSecret,
		Endpoint: oauth2.Endpoint{TokenURL: stored.TokenEndpoint, AuthStyle: tokenAuthStyle(stored.ClientInformation.TokenEndpointAuthMethod)},
	}
	refreshContext := context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)
	return &persistentTokenSource{source: config.TokenSource(refreshContext, token), store: h.store, server: h.server}, nil
}

func (h *savedOAuthHandler) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	response.Body.Close()
	return fmt.Errorf("saved OAuth credentials were rejected; run: wechat-article login --server %s", h.server)
}

type persistentTokenSource struct {
	source oauth2.TokenSource
	store  *config.Store
	server string
}

func (s *persistentTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	err = s.store.Update(func(value *config.File) error {
		value.Server = s.server
		value.Tokens = &config.Tokens{AccessToken: token.AccessToken, TokenType: token.TokenType, RefreshToken: token.RefreshToken}
		if !token.Expiry.IsZero() {
			value.Tokens.ExpiresIn = max(0, int64(time.Until(token.Expiry).Seconds()))
		}
		if scope, ok := token.Extra("scope").(string); ok {
			value.Tokens.Scope = scope
		}
		value.TokenSavedAt = time.Now().UnixMilli()
		return nil
	})
	return token, err
}

func tokenAuthStyle(method string) oauth2.AuthStyle {
	switch method {
	case "client_secret_basic":
		return oauth2.AuthStyleInHeader
	case "client_secret_post", "none":
		return oauth2.AuthStyleInParams
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

func (a *App) debugf(format string, args ...any) {
	if a.debug {
		fmt.Fprintf(a.stderr, "debug: "+format+"\n", args...)
	}
}
