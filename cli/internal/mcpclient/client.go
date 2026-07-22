package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Client struct {
	client  *mcp.Client
	session *mcp.ClientSession
}

type Options struct {
	Endpoint     string
	Version      string
	HTTPClient   *http.Client
	OAuthHandler auth.OAuthHandler
}

func Connect(ctx context.Context, options Options) (*Client, error) {
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Hostname() == "" {
		return nil, errors.New("MCP endpoint must be an absolute URL")
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && isLoopback(endpoint.Hostname())) {
		return nil, errors.New("MCP endpoint must use HTTPS; HTTP is allowed only for loopback hosts")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "wechat-article-cli", Version: options.Version}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{},
	})
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint.String(),
		HTTPClient:           options.HTTPClient,
		OAuthHandler:         options.OAuthHandler,
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP client: %w", err)
	}
	return &Client{client: client, session: session}, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	const maxPages = 100
	const maxTools = 10000
	var tools []*mcp.Tool
	var cursor string
	seen := map[string]struct{}{}
	for page := 0; page < maxPages; page++ {
		result, err := c.session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("list MCP tools: %w", err)
		}
		tools = append(tools, result.Tools...)
		if len(tools) > maxTools {
			return nil, fmt.Errorf("list MCP tools: exceeded %d tools", maxTools)
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		if _, exists := seen[result.NextCursor]; exists {
			return nil, fmt.Errorf("list MCP tools: repeated pagination cursor %q", result.NextCursor)
		}
		seen[result.NextCursor] = struct{}{}
		cursor = result.NextCursor
	}
	return nil, fmt.Errorf("list MCP tools: exceeded %d pages", maxPages)
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, fmt.Errorf("call MCP tool %s: %w", name, err)
	}
	return result, nil
}

func (c *Client) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}
