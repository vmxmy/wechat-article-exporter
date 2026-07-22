package exporter

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

const TextExportSchemaVersion = "wechat-article.text/v1"

type TextOptions struct {
	IncludeMetadataHeader bool
	IncludeComments       bool
	Comments              []processor.Comment
	Privacy               processor.CommentPrivacy
}

func RenderText(articleID domain.ArticleID, article processor.Article, options TextOptions) ([]byte, error) {
	if strings.TrimSpace(string(articleID)) == "" {
		return nil, fmt.Errorf("text export article ID is required")
	}
	rendered, err := processor.Render(article, processor.RenderOptions{
		ResourcePolicy:  processor.ResourceRewriteBestEffort,
		IncludeComments: options.IncludeComments,
		Comments:        options.Comments,
		Privacy:         options.Privacy,
	})
	if err != nil {
		return nil, err
	}
	var builder strings.Builder
	if options.IncludeMetadataHeader {
		writeTextMetadataHeader(&builder, articleID, article)
	}
	builder.WriteString(rendered.Text)
	return []byte(builder.String()), nil
}

func ExportTextFile(
	ctx context.Context,
	manager *OutputManager,
	relativePath string,
	articleID domain.ArticleID,
	article processor.Article,
	options TextOptions,
	policy CollisionPolicy,
) (OutputFile, error) {
	data, err := RenderText(articleID, article, options)
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

func writeTextMetadataHeader(builder *strings.Builder, articleID domain.ArticleID, article processor.Article) {
	fields := [][2]string{
		{"Schema-Version", TextExportSchemaVersion},
		{"Article-ID", string(articleID)},
		{"Title", article.Title},
		{"Account", firstTextValue(article.Account.Nickname, article.Account.Alias, article.Account.Username)},
		{"Author", article.Author},
		{"Published-At", formattedTime(article.Timestamps.PublishedAt)},
		{"Canonical-URL", article.CanonicalURL},
		{"Message-ID", article.Identity.MessageID},
		{"App-Message-ID", article.Identity.AppMessage},
	}
	for _, field := range fields {
		if field[1] == "" {
			continue
		}
		builder.WriteString(field[0])
		builder.WriteString(": ")
		builder.WriteString(singleLineMetadata(field[1]))
		builder.WriteByte('\n')
	}
	builder.WriteString("---\n")
}

func singleLineMetadata(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\x00", "")), " ")
}

func formattedTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func firstTextValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
