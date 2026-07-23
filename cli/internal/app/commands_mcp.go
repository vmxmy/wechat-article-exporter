package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	localmcp "github.com/wechat-article/wechat-article-exporter/cli/internal/mcp"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
)

const localMCPContentLimit = localmcp.DefaultMaxMessageBytes / 2

type contextReadCloser struct {
	ctx    context.Context
	reader io.ReadCloser
}

func (reader contextReadCloser) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func (reader contextReadCloser) Close() error { return reader.reader.Close() }

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
				Version:           Version,
				Profile:           string(a.active.Profile.ID),
				Policy:            configuration.MCP,
				Content:           localMCPContentReader{runtime: a.active},
				AllowedRoots:      mcpAllowedRoots(a.active, configuration),
				DefaultOutputRoot: mcpDefaultOutputRoot(a.active, configuration),
			})
			return localmcp.NewServer(adapter).Serve(command.Context(), a.stdin, a.stdout, a.stderr)
		},
	}
	serve.Flags().StringVar(&transport, "transport", "stdio", "MCP transport (stdio only)")
	command.AddCommand(serve)
	return command
}

func mcpAllowedRoots(active *ProfileRuntime, configuration profiles.ProfileConfig) []string {
	if active == nil {
		return nil
	}
	values := []string{filepath.Join(active.Profile.Paths.Data, "exports")}
	if root := strings.TrimSpace(configuration.Preferences.Export.Root); root != "" {
		values = append(values, root)
	}
	values = append(values, configuration.MCP.AllowedOutputRoots...)
	return values
}

type localMCPContentReader struct {
	runtime   *ProfileRuntime
	afterOpen func()
}

func (reader localMCPContentReader) ReadContent(ctx context.Context, articleID domain.ArticleID, kind string) (any, error) {
	if reader.runtime == nil || reader.runtime.Library == nil || reader.runtime.Objects == nil {
		return nil, errors.New("local content reader is unavailable")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "markdown"
	}
	content, err := reader.runtime.Library.CurrentContent(ctx, articleID, "html")
	if err != nil {
		return nil, err
	}
	objectReader, object, err := reader.runtime.Objects.Open(ctx, content.ObjectDigest)
	if err != nil {
		return nil, err
	}
	defer objectReader.Close()
	if reader.afterOpen != nil {
		reader.afterOpen()
	}
	stream := contextReadCloser{ctx: ctx, reader: objectReader}
	hash := sha256.New()
	tee := io.TeeReader(stream, hash)
	verify := func() error {
		actual := hex.EncodeToString(hash.Sum(nil))
		if actual != content.ObjectDigest {
			return fmt.Errorf("object %s produced digest %s: %w", content.ObjectDigest, actual, objects.ErrIntegrity)
		}
		return nil
	}
	switch kind {
	case "html":
		body, err := io.ReadAll(io.LimitReader(tee, localMCPContentLimit+1))
		if err != nil {
			return nil, err
		}
		if len(body) > localMCPContentLimit {
			return nil, errors.New("local article content exceeds MCP response limit")
		}
		if int64(len(body)) != object.Size {
			return nil, fmt.Errorf("object %s size changed while reading: %w", content.ObjectDigest, objects.ErrIntegrity)
		}
		if err := verify(); err != nil {
			return nil, err
		}
		return map[string]any{"mediaType": content.MediaType, "sha256": object.Digest, "body": string(body)}, nil
	case "text", "markdown", "json", "normalized":
		parsed, err := processor.New().Process(ctx, tee)
		if err != nil {
			return nil, err
		}
		if err := verify(); err != nil {
			return nil, err
		}
		if parsed.Article == nil {
			return nil, errors.New("local article content did not produce a normalized article")
		}
		if kind == "json" || kind == "normalized" {
			return boundedMCPContent(parsed.Article, kind)
		}
		rendered, err := processor.Render(*parsed.Article, processor.RenderOptions{})
		if err != nil {
			return nil, err
		}
		value := rendered.Markdown
		if kind == "text" {
			value = rendered.Text
		}
		if len(value) > localMCPContentLimit {
			return nil, fmt.Errorf("rendered %s content exceeds MCP response limit", kind)
		}
		digest := sha256.Sum256([]byte(value))
		mediaType := "text/markdown"
		if kind == "text" {
			mediaType = "text/plain"
		}
		return boundedMCPContent(map[string]any{"mediaType": mediaType, "sha256": hex.EncodeToString(digest[:]), "body": value}, kind)
	default:
		return nil, fmt.Errorf("content kind must be html, markdown, text, json, or normalized")
	}
}

func boundedMCPContent(value any, kind string) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	// Reserve space for contentResult, CallToolResult, and JSON-RPC framing.
	if len(encoded) > localMCPContentLimit {
		return nil, fmt.Errorf("rendered %s content exceeds MCP response limit", kind)
	}
	return value, nil
}

func mcpDefaultOutputRoot(active *ProfileRuntime, configuration profiles.ProfileConfig) string {
	if root := strings.TrimSpace(configuration.Preferences.Export.Root); root != "" {
		return root
	}
	if active == nil {
		return ""
	}
	return active.Profile.Paths.Data + "/exports"
}
