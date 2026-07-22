package exporter

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

const MarkdownExportSchemaVersion = "wechat-article.markdown/v1"

type MarkdownHTMLPolicy string

const (
	MarkdownHTMLStrip     MarkdownHTMLPolicy = "strip"
	MarkdownHTMLSanitized MarkdownHTMLPolicy = "sanitized"
)

type MarkdownOptions struct {
	IncludeFrontMatter bool
	EmbeddedHTMLPolicy MarkdownHTMLPolicy
	ResourceMap        map[string]string
	ResourcePolicy     processor.ResourceRewritePolicy
	IncludeComments    bool
	Comments           []processor.Comment
	Privacy            processor.CommentPrivacy
}

func RenderMarkdown(articleID domain.ArticleID, article processor.Article, options MarkdownOptions) ([]byte, error) {
	if strings.TrimSpace(string(articleID)) == "" {
		return nil, fmt.Errorf("Markdown export article ID is required")
	}
	htmlPolicy := options.EmbeddedHTMLPolicy
	if htmlPolicy == "" {
		htmlPolicy = MarkdownHTMLStrip
	}
	if htmlPolicy != MarkdownHTMLStrip && htmlPolicy != MarkdownHTMLSanitized {
		return nil, fmt.Errorf("unsupported Markdown embedded HTML policy %q", htmlPolicy)
	}
	resourcePolicy := options.ResourcePolicy
	if resourcePolicy == "" {
		resourcePolicy = processor.ResourceRewriteBestEffort
	}
	rendered, err := processor.Render(article, processor.RenderOptions{
		ResourceMap: options.ResourceMap, ResourcePolicy: resourcePolicy,
		IncludeComments: options.IncludeComments, Comments: options.Comments, Privacy: options.Privacy,
	})
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	if options.IncludeFrontMatter {
		writeMarkdownFrontMatter(&builder, articleID, article)
	}
	if htmlPolicy == MarkdownHTMLSanitized {
		builder.WriteString(rendered.HTML)
		if !strings.HasSuffix(rendered.HTML, "\n") {
			builder.WriteByte('\n')
		}
	} else {
		builder.WriteString(rendered.Markdown)
	}
	return []byte(builder.String()), nil
}

func ExportMarkdownFile(
	ctx context.Context,
	manager *OutputManager,
	relativePath string,
	articleID domain.ArticleID,
	article processor.Article,
	options MarkdownOptions,
	policy CollisionPolicy,
) (OutputFile, error) {
	data, err := RenderMarkdown(articleID, article, options)
	if err != nil {
		return OutputFile{}, err
	}
	output, err := manager.WriteFile(ctx, relativePath, policy, func(writer io.Writer) error {
		_, err := writer.Write(data)
		return err
	})
	if err != nil {
		return OutputFile{}, err
	}
	output.ArticleID = articleID
	return output, nil
}

func writeMarkdownFrontMatter(builder *strings.Builder, articleID domain.ArticleID, article processor.Article) {
	builder.WriteString("---\n")
	writeYAMLString(builder, "schemaVersion", MarkdownExportSchemaVersion)
	writeYAMLString(builder, "articleId", string(articleID))
	writeYAMLString(builder, "title", article.Title)
	writeYAMLString(builder, "account", firstTextValue(article.Account.Nickname, article.Account.Alias, article.Account.Username))
	writeYAMLString(builder, "author", article.Author)
	writeYAMLString(builder, "publishedAt", formattedTime(article.Timestamps.PublishedAt))
	writeYAMLString(builder, "canonicalUrl", article.CanonicalURL)
	writeYAMLString(builder, "messageId", article.Identity.MessageID)
	writeYAMLString(builder, "appMessageId", article.Identity.AppMessage)
	builder.WriteString("---\n")
}

func writeYAMLString(builder *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	builder.WriteString(key)
	builder.WriteString(": ")
	builder.WriteString(strconv.Quote(strings.ReplaceAll(value, "\x00", "")))
	builder.WriteByte('\n')
}
