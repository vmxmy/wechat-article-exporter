// Package legacyremote contains the temporary remote MCP/OAuth compatibility
// surface. New local product behavior must depend on internal/application and
// must not import this package.
package legacyremote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/mcpclient"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/oauth"
	"golang.org/x/oauth2"
)

const DefaultServer = "https://mptext.ziikoo.app"

type Adapter struct {
	store      *config.Store
	version    string
	httpClient *http.Client
}

type Status struct {
	Server        string `json:"server"`
	Authenticated bool   `json:"authenticated"`
	Refreshable   bool   `json:"refreshable"`
	ConfigPath    string `json:"configPath"`
}

type LoginOptions struct {
	Server      string
	RedirectURL string
	FetchCode   func(context.Context, string) (code string, state string, err error)
}

func New(store *config.Store, version string, httpClient *http.Client) *Adapter {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Adapter{store: store, version: version, httpClient: httpClient}
}

func (adapter *Adapter) ResolveServer(override string) (string, error) {
	stored, err := adapter.store.Read()
	if err != nil {
		return "", err
	}
	value := override
	if value == "" {
		value = stored.Server
	}
	if value == "" {
		value = DefaultServer
	}
	return oauth.NormalizeServer(value)
}

func (adapter *Adapter) Status(override string) (Status, error) {
	stored, err := adapter.store.Read()
	if err != nil {
		return Status{}, err
	}
	server, err := adapter.ResolveServer(override)
	if err != nil {
		return Status{}, err
	}
	authenticated := stored.Tokens != nil && stored.Tokens.AccessToken != "" && sameServer(stored.Server, server) && sessionUsable(stored)
	return Status{
		Server:        server,
		Authenticated: authenticated,
		Refreshable:   authenticated && stored.Tokens.RefreshToken != "",
		ConfigPath:    adapter.store.Path(),
	}, nil
}

func (adapter *Adapter) Logout() (config.File, error) { return adapter.store.ClearSession() }

func (adapter *Adapter) Login(ctx context.Context, options LoginOptions) (int, error) {
	server, err := adapter.ResolveServer(options.Server)
	if err != nil {
		return 0, err
	}
	if options.RedirectURL == "" || options.FetchCode == nil {
		return 0, errors.New("legacy login requires redirect URL and authorization callback")
	}
	httpClient := oauth.NewRegistrationHTTPClient(adapter.httpClient, adapter.store, server, options.RedirectURL)
	handler, err := oauth.NewPersistentHandler(oauth.BrowserOptions{
		Store:         adapter.store,
		Server:        server,
		RedirectURL:   options.RedirectURL,
		HTTPClient:    httpClient,
		ClientVersion: adapter.version,
		FetchCode: func(flowContext context.Context, arguments *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			code, state, fetchErr := options.FetchCode(flowContext, arguments.URL)
			if fetchErr != nil {
				return nil, fetchErr
			}
			return &auth.AuthorizationResult{Code: code, State: state}, nil
		},
	})
	if err != nil {
		return 0, err
	}
	client, err := mcpclient.Connect(ctx, mcpclient.Options{
		Endpoint:     oauth.MCPURL(server),
		Version:      adapter.version,
		HTTPClient:   httpClient,
		OAuthHandler: handler,
	})
	if err != nil {
		return 0, err
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil {
		return 0, err
	}
	return len(tools), nil
}

func (adapter *Adapter) Connect(ctx context.Context, override string) (*mcpclient.Client, error) {
	stored, err := adapter.store.Read()
	if err != nil {
		return nil, err
	}
	server, err := adapter.ResolveServer(override)
	if err != nil {
		return nil, err
	}
	if stored.Tokens == nil || stored.Tokens.AccessToken == "" || !sameServer(stored.Server, server) || !sessionUsable(stored) {
		return nil, fmt.Errorf("not logged in to legacy remote %s; run: wechat-article legacy login --server %s", server, server)
	}
	handler := &savedOAuthHandler{store: adapter.store, server: server, httpClient: adapter.httpClient}
	return mcpclient.Connect(ctx, mcpclient.Options{
		Endpoint:     oauth.MCPURL(server),
		Version:      adapter.version,
		HTTPClient:   adapter.httpClient,
		OAuthHandler: handler,
	})
}

func (adapter *Adapter) ListTools(ctx context.Context, override string) ([]*mcp.Tool, error) {
	client, err := adapter.Connect(ctx, override)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(tools, func(left, right int) bool { return tools[left].Name < tools[right].Name })
	return tools, nil
}

func (adapter *Adapter) FindTool(ctx context.Context, override, name string) (*mcp.Tool, error) {
	tools, err := adapter.ListTools(ctx, override)
	if err != nil {
		return nil, err
	}
	for _, tool := range tools {
		if tool.Name == name {
			return tool, nil
		}
	}
	return nil, fmt.Errorf("legacy remote MCP tool not found: %s", name)
}

func (adapter *Adapter) CallTool(ctx context.Context, override, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	client, err := adapter.Connect(ctx, override)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return client.CallTool(ctx, name, arguments)
}

func (adapter *Adapter) InvokeTool(
	ctx context.Context,
	override string,
	name string,
	arguments map[string]any,
	validate func(*mcp.Tool) error,
) (*mcp.CallToolResult, error) {
	client, err := adapter.Connect(ctx, override)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	var selected *mcp.Tool
	for _, tool := range tools {
		if tool.Name == name {
			selected = tool
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("legacy remote MCP tool not found: %s", name)
	}
	if validate != nil {
		if err := validate(selected); err != nil {
			return nil, err
		}
	}
	return client.CallTool(ctx, selected.Name, arguments)
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

func (handler *savedOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	stored, err := handler.store.Read()
	if err != nil {
		return nil, err
	}
	if stored.Tokens == nil || stored.Tokens.AccessToken == "" || !sameServer(stored.Server, handler.server) {
		return nil, nil
	}
	token := &oauth2.Token{
		AccessToken:  stored.Tokens.AccessToken,
		TokenType:    stored.Tokens.TokenType,
		RefreshToken: stored.Tokens.RefreshToken,
	}
	if expiry := stored.TokenExpiry(); !expiry.IsZero() {
		token.Expiry = expiry
	}
	if stored.TokenEndpoint == "" || stored.ClientInformation == nil || stored.ClientInformation.ClientID == "" {
		return oauth2.StaticTokenSource(token), nil
	}
	configuration := oauth2.Config{
		ClientID:     stored.ClientInformation.ClientID,
		ClientSecret: stored.ClientInformation.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL:  stored.TokenEndpoint,
			AuthStyle: tokenAuthStyle(stored.ClientInformation.TokenEndpointAuthMethod),
		},
	}
	refreshContext := context.WithValue(ctx, oauth2.HTTPClient, handler.httpClient)
	return &persistentTokenSource{
		source: configuration.TokenSource(refreshContext, token),
		store:  handler.store,
		server: handler.server,
	}, nil
}

func (handler *savedOAuthHandler) Authorize(_ context.Context, _ *http.Request, response *http.Response) error {
	response.Body.Close()
	return fmt.Errorf("saved legacy OAuth credentials were rejected; run: wechat-article legacy login --server %s", handler.server)
}

type persistentTokenSource struct {
	source oauth2.TokenSource
	store  *config.Store
	server string
}

func (source *persistentTokenSource) Token() (*oauth2.Token, error) {
	token, err := source.source.Token()
	if err != nil {
		return nil, err
	}
	err = source.store.Update(func(value *config.File) error {
		value.Server = source.server
		value.Tokens = &config.Tokens{
			AccessToken:  token.AccessToken,
			TokenType:    token.TokenType,
			RefreshToken: token.RefreshToken,
		}
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
