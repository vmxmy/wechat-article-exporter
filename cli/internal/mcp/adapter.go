package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

const (
	defaultVersion         = "1.0.0"
	DefaultMaxMessageBytes = 1 << 20
	toolSuccessMessage     = "Tool completed successfully; use structuredContent for the result."
	protocolVersion        = "2025-06-18"
	destructiveArgument    = "confirm"
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
	AllowedRoots       []string
	DefaultOutputRoot  string
}

type Adapter struct {
	application       application.Application
	version           string
	profile           string
	policy            profiles.MCPPolicy
	logger            *slog.Logger
	maxMessage        int
	content           ContentReader
	sensitive         SensitiveHandler
	allowSecret       bool
	secretProof       string
	name              string
	allowedRoots      []string
	defaultOutputRoot string
	tools             map[string]ToolDefinition
}

type ToolDefinition struct {
	Tool        *Tool
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
		configuration.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if configuration.Logger == nil {
		configuration.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if configuration.ImplementationName == "" {
		configuration.ImplementationName = "wechat-article-local"
	}
	adapter := &Adapter{
		application:       shared,
		version:           configuration.Version,
		profile:           configuration.Profile,
		policy:            configuration.Policy,
		logger:            configuration.Logger,
		maxMessage:        configuration.MaxMessageBytes,
		content:           configuration.Content,
		sensitive:         configuration.Sensitive,
		allowSecret:       configuration.AllowSensitive,
		secretProof:       configuration.SensitiveConfirm,
		name:              configuration.ImplementationName,
		allowedRoots:      normalizeAllowedRoots(configuration.AllowedRoots),
		defaultOutputRoot: strings.TrimSpace(configuration.DefaultOutputRoot),
	}
	adapter.tools = adapter.buildTools()
	return adapter
}

func (adapter *Adapter) RuntimeStatus(ctx context.Context) (domain.RuntimeStatus, error) {
	return adapter.application.RuntimeStatus(ctx)
}

func (adapter *Adapter) Tools() []*Tool {
	names := make([]string, 0, len(adapter.tools))
	for name := range adapter.tools {
		if adapter.allowed(name, false) == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	tools := make([]*Tool, 0, len(names))
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
	if name == "exports.start" {
		var request domain.ExportRequest
		if err := json.Unmarshal(arguments, &request); err != nil {
			return nil, err
		}
		if bytes.Equal(bytes.TrimSpace(arguments), []byte("null")) {
			return nil, errors.New("exports.start arguments must be a JSON object")
		}
		outputRoot := strings.TrimSpace(request.OutputRoot)
		if outputRoot == "" {
			outputRoot = adapter.defaultOutputRoot
		}
		if outputRoot == "" {
			return nil, errors.New("exports.start requires outputRoot or a configured default export root")
		}
		if outputRoot != "" {
			validated, authorization, err := adapter.validateAllowedPath(outputRoot)
			if err != nil {
				return nil, err
			}
			if definition.handler == nil {
				return nil, application.ErrUnavailable
			}
			if definition.Destructive || definition.Sensitive {
				return nil, errors.New("exports.start policy metadata is invalid")
			}
			request.OutputRoot = validated
			request.OutputAuthorization = authorization
			job, err := adapter.application.StartExport(ctx, request)
			if err != nil {
				return nil, safety.RedactError(err)
			}
			return safety.Redact(jobResult{JobID: job.ID, State: job.State, Kind: job.Kind, Profile: job.Profile}, ""), nil
		}
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

func normalizeAllowedRoots(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		absolute, err := filepath.Abs(strings.TrimSpace(value))
		if err != nil || strings.TrimSpace(value) == "" {
			continue
		}
		absolute = filepath.Clean(absolute)
		if _, ok := seen[absolute]; ok {
			continue
		}
		seen[absolute] = struct{}{}
		result = append(result, absolute)
	}
	sort.Strings(result)
	return result
}

func (adapter *Adapter) validateAllowedPath(value string) (string, *domain.ExportOutputAuthorization, error) {
	if len(adapter.allowedRoots) == 0 {
		return "", nil, errors.New("MCP file output is disabled because no allowed root is configured")
	}
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", nil, fmt.Errorf("resolve MCP output path: %w", err)
	}
	absolute = filepath.Clean(absolute)
	for _, root := range adapter.allowedRoots {
		relative, relErr := filepath.Rel(root, absolute)
		if relErr != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		resolvedRoot, rootErr := evalExistingPath(root)
		resolvedParent, parentErr := evalExistingPath(filepath.Dir(absolute))
		if rootErr == nil && parentErr == nil {
			symlinkRelative, symlinkErr := filepath.Rel(resolvedRoot, resolvedParent)
			if symlinkErr != nil || symlinkRelative == ".." || filepath.IsAbs(symlinkRelative) || strings.HasPrefix(symlinkRelative, ".."+string(filepath.Separator)) {
				continue
			}
		}
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", nil, fmt.Errorf("create MCP allowed output root: %w", err)
		}
		info, err := os.Lstat(root)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", nil, errors.New("MCP allowed output root is unavailable or unsafe")
		}
		identityFile, err := os.Open(root)
		if err != nil {
			return "", nil, fmt.Errorf("open MCP allowed output root identity handle: %w", err)
		}
		device, inode, identityErr := allowedRootIdentityFromFile(identityFile)
		closeErr := identityFile.Close()
		err = errors.Join(identityErr, closeErr)
		if err != nil {
			return "", nil, fmt.Errorf("identify MCP allowed output root: %w", err)
		}
		return absolute, &domain.ExportOutputAuthorization{Root: root, RelativePath: filepath.ToSlash(relative), Device: device, Inode: inode}, nil
	}
	return "", nil, fmt.Errorf("MCP output path %q is outside configured allowed roots", value)
}

func evalExistingPath(value string) (string, error) {
	candidate := filepath.Clean(value)
	for {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", err
		}
		candidate = parent
	}
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
			Tool: &Tool{
				Name: name, Description: description, InputSchema: input, OutputSchema: output,
				Annotations: &ToolAnnotations{
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
				albumApplication, ok := adapter.application.(interface {
					SynchronizeAlbum(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error)
				})
				if !ok {
					return domain.Job{}, fmt.Errorf("album synchronization: %w", application.ErrUnavailable)
				}
				return albumApplication.SynchronizeAlbum(ctx, input.AccountID, input.AlbumID)
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
		Tool: &Tool{
			Name: name, Description: description, InputSchema: downloadSchema(), OutputSchema: jobOutputSchema(),
			Annotations: &ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &openWorld},
		},
		Mutating: true,
		handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var request domain.DownloadRequest
			if err := json.Unmarshal(raw, &request); err != nil {
				return nil, err
			}
			if strings.HasPrefix(name, "metadata.") {
				request.Kind = "metadata"
			} else if strings.HasPrefix(name, "comments.") {
				request.Kind = "comments"
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

func toolResult(value any, err error) *CallToolResult {
	if err != nil {
		redacted := safety.RedactError(err)
		return &CallToolResult{Content: []TextContent{{Type: "text", Text: redacted.Error()}}, IsError: true,
			StructuredContent: map[string]any{"error": redacted.Error()}}
	}
	redacted := safety.Redact(value, "")
	if _, marshalErr := json.Marshal(redacted); marshalErr != nil {
		return toolResult(nil, marshalErr)
	}
	return &CallToolResult{Content: []TextContent{{Type: "text", Text: toolSuccessMessage}}, StructuredContent: redacted}
}

func (adapter *Adapter) implementation() *Implementation {
	return &Implementation{Name: adapter.name, Version: adapter.version, Title: "WeChat Article Local MCP"}
}

func (adapter *Adapter) capabilities() *ServerCapabilities {
	return &ServerCapabilities{Tools: &ToolCapabilities{ListChanged: false}, Experimental: map[string]any{
		"localOnly": true, "profile": adapter.profile, "remoteOAuth": false,
	}}
}

func normalizeToolName(name string) string { return strings.TrimSpace(name) }
