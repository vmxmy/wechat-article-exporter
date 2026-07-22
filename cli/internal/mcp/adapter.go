package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

const (
	defaultVersion      = "1.0.0"
	defaultMaxMessage   = 1 << 20
	protocolVersion     = "2025-06-18"
	destructiveArgument = "confirm"
)

var (
	ErrToolNotFound         = errors.New("MCP tool is not available")
	ErrToolDenied           = errors.New("MCP tool is denied by profile policy")
	ErrReadOnly             = errors.New("MCP tool is unavailable in read-only mode")
	ErrConfirmationRequired = errors.New("MCP tool requires exact confirmation")
	ErrSensitiveOperation   = errors.New("MCP sensitive operation is not enabled")
)

type ContentReader interface {
	ReadContent(context.Context, domain.ArticleID, string) (any, error)
}

type SensitiveHandler interface {
	InvokeSensitive(context.Context, string, json.RawMessage) (any, error)
}

type Options struct {
	Version            string
	Profile            string
	Policy             profiles.MCPPolicy
	MaxMessageBytes    int
	Logger             *slog.Logger
	Content            ContentReader
	Sensitive          SensitiveHandler
	AllowSensitive     bool
	SensitiveConfirm   string
	ImplementationName string
}

type Adapter struct {
	application application.Application
	version     string
	profile     string
	policy      profiles.MCPPolicy
	logger      *slog.Logger
	maxMessage  int
	content     ContentReader
	sensitive   SensitiveHandler
	allowSecret bool
	secretProof string
	name        string
	tools       map[string]ToolDefinition
}

type ToolDefinition struct {
	Tool        *sdk.Tool
	Mutating    bool
	Destructive bool
	Sensitive   bool
	handler     func(context.Context, json.RawMessage) (any, error)
}

type jobResult struct {
	JobID   domain.JobID     `json:"jobId"`
	State   domain.JobState  `json:"state"`
	Kind    string           `json:"kind"`
	Profile domain.ProfileID `json:"profile,omitempty"`
}

type contentResult struct {
	ArticleID domain.ArticleID `json:"articleId"`
	Kind      string           `json:"kind"`
	Content   any              `json:"content"`
}

func New(shared application.Application, options ...Options) *Adapter {
	configuration := Options{}
	if len(options) > 0 {
		configuration = options[0]
	}
	if configuration.Version == "" {
		configuration.Version = defaultVersion
	}
	if configuration.MaxMessageBytes <= 0 {
		configuration.MaxMessageBytes = defaultMaxMessage
	}
	if configuration.Logger == nil {
		configuration.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if configuration.ImplementationName == "" {
		configuration.ImplementationName = "wechat-article-local"
	}
	adapter := &Adapter{
		application: shared,
		version:     configuration.Version,
		profile:     configuration.Profile,
		policy:      configuration.Policy,
		logger:      configuration.Logger,
		maxMessage:  configuration.MaxMessageBytes,
		content:     configuration.Content,
		sensitive:   configuration.Sensitive,
		allowSecret: configuration.AllowSensitive,
		secretProof: configuration.SensitiveConfirm,
		name:        configuration.ImplementationName,
	}
	adapter.tools = adapter.buildTools()
	return adapter
}

func (adapter *Adapter) RuntimeStatus(ctx context.Context) (domain.RuntimeStatus, error) {
	return adapter.application.RuntimeStatus(ctx)
}

func (adapter *Adapter) Tools() []*sdk.Tool {
	names := make([]string, 0, len(adapter.tools))
	for name := range adapter.tools {
		if adapter.allowed(name, false) == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	tools := make([]*sdk.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, adapter.tools[name].Tool)
	}
	return tools
}

func (adapter *Adapter) Call(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
	definition, ok := adapter.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	if err := adapter.allowed(name, true); err != nil {
		return nil, err
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if !json.Valid(arguments) {
		return nil, errors.New("tool arguments must be valid JSON")
	}
	if definition.Destructive {
		var envelope struct {
			Confirmation string `json:"confirm"`
		}
		if err := json.Unmarshal(arguments, &envelope); err != nil {
			return nil, err
		}
		required := DestructiveConfirmation(name)
		if envelope.Confirmation != required {
			return nil, fmt.Errorf("%w: retry with %q=%q", ErrConfirmationRequired, destructiveArgument, required)
		}
	}
	if definition.Sensitive {
		var envelope struct {
			Confirmation string `json:"confirmSensitive"`
		}
		if !adapter.allowSecret || adapter.sensitive == nil {
			return nil, fmt.Errorf("%w: %s", ErrSensitiveOperation, name)
		}
		if err := json.Unmarshal(arguments, &envelope); err != nil {
			return nil, err
		}
		if adapter.secretProof == "" || envelope.Confirmation != adapter.secretProof {
			return nil, fmt.Errorf("%w: exact confirmSensitive value required", ErrConfirmationRequired)
		}
	}
	result, err := definition.handler(ctx, arguments)
	if err != nil {
		return nil, safety.RedactError(err)
	}
	return safety.Redact(result, ""), nil
}

func DestructiveConfirmation(name string) string { return "confirm:" + name }

func (adapter *Adapter) allowed(name string, executing bool) error {
	definition, ok := adapter.tools[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	if matchesPolicy(adapter.policy.Deny, name) {
		return fmt.Errorf("%w: %s", ErrToolDenied, name)
	}
	if len(adapter.policy.Allow) > 0 && !matchesPolicy(adapter.policy.Allow, name) {
		return fmt.Errorf("%w: %s", ErrToolDenied, name)
	}
	if executing && adapter.policy.ReadOnly && definition.Mutating {
		return fmt.Errorf("%w: %s", ErrReadOnly, name)
	}
	return nil
}

func matchesPolicy(values []string, name string) bool {
	for _, value := range values {
		if value == name || value == "*" {
			return true
		}
	}
	return false
}

func (adapter *Adapter) buildTools() map[string]ToolDefinition {
	tools := make(map[string]ToolDefinition)
	add := func(name, description string, input, output map[string]any, readOnly, destructive, sensitive bool,
		handler func(context.Context, json.RawMessage) (any, error),
	) {
		openWorld := false
		destructiveHint := destructive
		tools[name] = ToolDefinition{
			Tool: &sdk.Tool{
				Name: name, Description: description, InputSchema: input, OutputSchema: output,
				Annotations: &sdk.ToolAnnotations{
					ReadOnlyHint: readOnly, DestructiveHint: &destructiveHint, OpenWorldHint: &openWorld,
					IdempotentHint: readOnly,
				},
			},
			Mutating: !readOnly, Destructive: destructive, Sensitive: sensitive, handler: handler,
		}
	}

	add("accounts.search", "Search WeChat accounts through the shared local application.", accountQuerySchema(), pageSchema("account"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var query domain.AccountQuery
			return decodeAndCall(raw, &query, func() (any, error) { return adapter.application.SearchAccounts(ctx, query) })
		})
	add("accounts.query", "Query locally stored accounts.", accountQuerySchema(), pageSchema("account"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var query domain.AccountQuery
			return decodeAndCall(raw, &query, func() (any, error) { return adapter.application.QueryAccounts(ctx, query) })
		})
	add("accounts.resolve", "Resolve an account from an article URL.", objectSchema(map[string]any{
		"url": stringProperty("WeChat article URL"),
	}, "url"), objectOutput("account"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var input struct {
				URL string `json:"url"`
			}
			return decodeAndCall(raw, &input, func() (any, error) { return adapter.application.ResolveAccountFromArticle(ctx, input.URL) })
		})
	add("articles.query", "Query locally stored articles with stable filters and pagination.", articleQuerySchema(), pageSchema("article"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var query domain.ArticleQuery
			return decodeAndCall(raw, &query, func() (any, error) { return adapter.application.QueryArticles(ctx, query) })
		})
	add("albums.query", "Query locally stored albums.", albumQuerySchema(), pageSchema("album"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var query domain.AlbumQuery
			return decodeAndCall(raw, &query, func() (any, error) { return adapter.application.QueryAlbums(ctx, query) })
		})
	add("sync.account", "Create a persistent account synchronization job and return immediately.", syncSchema(), jobOutputSchema(), false, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var request domain.SynchronizeAccountRequest
			return decodeJob(raw, &request, func() (domain.Job, error) { return adapter.application.SynchronizeAccount(ctx, request) })
		})
	add("sync.album", "Create a persistent album synchronization job through the shared application job seam.", albumSyncSchema(), jobOutputSchema(), false, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var input struct {
				AccountID domain.AccountID `json:"accountId"`
				AlbumID   domain.AlbumID   `json:"albumId"`
			}
			return decodeJob(raw, &input, func() (domain.Job, error) {
				return adapter.application.SynchronizeAccount(ctx, domain.SynchronizeAccountRequest{AccountID: input.AccountID, Incremental: true})
			})
		})
	add("downloads.start", "Create a persistent article download job and return its job identifier.", downloadSchema(), jobOutputSchema(), false, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var request domain.DownloadRequest
			return decodeJob(raw, &request, func() (domain.Job, error) { return adapter.application.StartDownload(ctx, request) })
		})
	addJobAlias("metadata.start", "Create a persistent metadata download job.", adapter, tools)
	addJobAlias("comments.start", "Create a persistent comments download job.", adapter, tools)
	add("content.get", "Read bounded local article content without contacting remote MCP services.", contentSchema(), objectOutput("content"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			if adapter.content == nil {
				return nil, application.ErrUnavailable
			}
			var input struct {
				ArticleID domain.ArticleID `json:"articleId"`
				Kind      string           `json:"kind"`
			}
			return decodeAndCall(raw, &input, func() (any, error) {
				content, err := adapter.content.ReadContent(ctx, input.ArticleID, input.Kind)
				return contentResult{ArticleID: input.ArticleID, Kind: input.Kind, Content: content}, err
			})
		})
	add("exports.start", "Create a persistent local export job and return its identifier.", exportSchema(), jobOutputSchema(), false, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var request domain.ExportRequest
			return decodeJob(raw, &request, func() (domain.Job, error) { return adapter.application.StartExport(ctx, request) })
		})
	add("jobs.get", "Read one persistent job status.", objectSchema(map[string]any{"jobId": stringProperty("Persistent job identifier")}, "jobId"), objectOutput("job"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var input struct {
				JobID domain.JobID `json:"jobId"`
			}
			return decodeAndCall(raw, &input, func() (any, error) { return adapter.application.GetJob(ctx, input.JobID) })
		})
	add("jobs.query", "Query persistent local jobs.", jobQuerySchema(), pageSchema("job"), true, false, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var query domain.JobQuery
			return decodeAndCall(raw, &query, func() (any, error) { return adapter.application.QueryJobs(ctx, query) })
		})
	add("jobs.cancel", "Cancel one persistent job. Exact confirmation is required.", confirmationSchema(map[string]any{
		"jobId": stringProperty("Persistent job identifier"),
	}, "jobId"), objectOutput("job"), false, true, false,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var input struct {
				JobID domain.JobID `json:"jobId"`
			}
			return decodeAndCall(raw, &input, func() (any, error) { return adapter.application.CancelJob(ctx, input.JobID) })
		})
	add("storage.status", "Read local database and object-store status.", emptySchema(), objectOutput("storage"), true, false, false,
		func(ctx context.Context, _ json.RawMessage) (any, error) {
			return adapter.application.StorageStatus(ctx)
		})
	add("runtime.status", "Read local runtime and selected-profile status.", emptySchema(), objectOutput("runtime"), true, false, false,
		func(ctx context.Context, _ json.RawMessage) (any, error) {
			return adapter.application.RuntimeStatus(ctx)
		})
	add("credentials.invoke", "Invoke an explicitly enabled sensitive local credential operation.", sensitiveSchema(), objectOutput("result"), false, false, true,
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var input struct {
				Operation string `json:"operation"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			return adapter.sensitive.InvokeSensitive(ctx, input.Operation, raw)
		})
	return tools
}

func addJobAlias(name, description string, adapter *Adapter, tools map[string]ToolDefinition) {
	openWorld := false
	destructive := false
	tools[name] = ToolDefinition{
		Tool: &sdk.Tool{
			Name: name, Description: description, InputSchema: downloadSchema(), OutputSchema: jobOutputSchema(),
			Annotations: &sdk.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &openWorld},
		},
		Mutating: true,
		handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var request domain.DownloadRequest
			if strings.HasPrefix(name, "metadata.") {
				request.Kind = "metadata"
			} else if strings.HasPrefix(name, "comments.") {
				request.Kind = "comments"
			}
			if err := json.Unmarshal(raw, &request); err != nil {
				return nil, err
			}
			return decodeJob(json.RawMessage(`{}`), &struct{}{}, func() (domain.Job, error) {
				return adapter.application.StartDownload(ctx, request)
			})
		},
	}
}

func decodeAndCall(raw json.RawMessage, target any, call func() (any, error)) (any, error) {
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, err
	}
	return call()
}

func decodeJob(raw json.RawMessage, target any, call func() (domain.Job, error)) (any, error) {
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, err
	}
	job, err := call()
	if err != nil {
		return nil, err
	}
	return jobResult{JobID: job.ID, State: job.State, Kind: job.Kind, Profile: job.Profile}, nil
}

func toolResult(value any, err error) *sdk.CallToolResult {
	if err != nil {
		redacted := safety.RedactError(err)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: redacted.Error()}}, IsError: true,
			StructuredContent: map[string]any{"error": redacted.Error()}}
	}
	redacted := safety.Redact(value, "")
	encoded, marshalErr := json.Marshal(redacted)
	if marshalErr != nil {
		return toolResult(nil, marshalErr)
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(encoded)}}, StructuredContent: redacted}
}

func (adapter *Adapter) implementation() *sdk.Implementation {
	return &sdk.Implementation{Name: adapter.name, Version: adapter.version, Title: "WeChat Article Local MCP"}
}

func (adapter *Adapter) capabilities() *sdk.ServerCapabilities {
	return &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{ListChanged: false}, Experimental: map[string]any{
		"localOnly": true, "profile": adapter.profile, "remoteOAuth": false,
	}}
}

func normalizeToolName(name string) string { return strings.TrimSpace(name) }
