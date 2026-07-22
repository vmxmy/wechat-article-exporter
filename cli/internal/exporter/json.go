package exporter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

const JSONExportSchemaVersion = "wechat-article.export/v1"

type JSONOptions struct {
	IncludeContent    bool
	IncludeMetrics    bool
	IncludeComments   bool
	IncludeReplies    bool
	IncludeAlbums     bool
	IncludeProvenance bool
	ResourceMap       map[string]string
	ResourcePolicy    processor.ResourceRewritePolicy
	Privacy           processor.CommentPrivacy
}

type JSONExportInput struct {
	ArticleID  domain.ArticleID
	Article    processor.Article
	Comments   []processor.Comment
	Provenance *ProvenanceManifest
	ExportedAt time.Time
}

type JSONExportDocument struct {
	SchemaVersion string            `json:"schemaVersion"`
	ExportedAt    time.Time         `json:"exportedAt"`
	Article       JSONExportArticle `json:"article"`
}

type JSONExportArticle struct {
	ID           domain.ArticleID      `json:"id"`
	SourceSchema string                `json:"sourceSchemaVersion"`
	Identity     processor.Identity    `json:"identity"`
	Title        string                `json:"title"`
	Description  string                `json:"description,omitempty"`
	Author       string                `json:"author,omitempty"`
	Account      processor.Account     `json:"account"`
	CanonicalURL string                `json:"canonicalUrl,omitempty"`
	SourceURL    string                `json:"sourceUrl,omitempty"`
	Timestamps   processor.Timestamps  `json:"timestamps"`
	Message      processor.Message     `json:"message"`
	Payment      processor.Payment     `json:"payment"`
	Media        processor.Media       `json:"media"`
	Copyright    processor.Copyright   `json:"copyright"`
	Language     string                `json:"language,omitempty"`
	IPLocation   processor.IPLocation  `json:"ipLocation"`
	CommentState processor.Comments    `json:"commentState"`
	Content      *JSONExportContent    `json:"content,omitempty"`
	Metrics      *processor.Engagement `json:"metrics,omitempty"`
	Comments     []processor.Comment   `json:"comments,omitempty"`
	Albums       []processor.Album     `json:"albums,omitempty"`
	Provenance   *ProvenanceManifest   `json:"provenance,omitempty"`
}

type JSONExportContent struct {
	HTML             string               `json:"html"`
	Text             string               `json:"text"`
	Markdown         string               `json:"markdown"`
	Resources        []processor.Resource `json:"resources,omitempty"`
	MissingResources []processor.Resource `json:"missingResources,omitempty"`
}

func MarshalJSONExport(input JSONExportInput, options JSONOptions) (JSONExportDocument, []byte, error) {
	if strings.TrimSpace(string(input.ArticleID)) == "" {
		return JSONExportDocument{}, nil, fmt.Errorf("JSON export article ID is required")
	}
	exportedAt := input.ExportedAt
	if exportedAt.IsZero() {
		exportedAt = time.Now()
	}
	exportedAt = exportedAt.UTC()
	document := JSONExportDocument{
		SchemaVersion: JSONExportSchemaVersion,
		ExportedAt:    exportedAt,
		Article: JSONExportArticle{
			ID: input.ArticleID, SourceSchema: input.Article.SchemaVersion, Identity: input.Article.Identity,
			Title: input.Article.Title, Description: input.Article.Description, Author: input.Article.Author,
			Account: input.Article.Account, CanonicalURL: input.Article.CanonicalURL, SourceURL: input.Article.SourceURL,
			Timestamps: input.Article.Timestamps, Message: input.Article.Message, Payment: input.Article.Payment,
			Media: input.Article.Media, Copyright: input.Article.Copyright, Language: input.Article.Language,
			IPLocation: input.Article.IPLocation, CommentState: input.Article.Comments,
		},
	}

	needsRendering := options.IncludeContent || options.IncludeComments
	var rendered processor.RenderedArticle
	if needsRendering {
		resourcePolicy := options.ResourcePolicy
		if resourcePolicy == "" {
			resourcePolicy = processor.ResourceRewriteBestEffort
		}
		var err error
		rendered, err = processor.Render(input.Article, processor.RenderOptions{
			ResourceMap: options.ResourceMap, ResourcePolicy: resourcePolicy,
			IncludeComments: options.IncludeComments, Comments: input.Comments, Privacy: options.Privacy,
		})
		if err != nil {
			return JSONExportDocument{}, nil, err
		}
	}
	if options.IncludeContent {
		document.Article.Content = &JSONExportContent{
			HTML: rendered.HTML, Text: rendered.Text, Markdown: rendered.Markdown,
			Resources:        append([]processor.Resource(nil), rendered.Resources...),
			MissingResources: append([]processor.Resource(nil), rendered.MissingResources...),
		}
	}
	if options.IncludeMetrics {
		metrics := input.Article.Engagement
		document.Article.Metrics = &metrics
	}
	if options.IncludeComments {
		document.Article.Comments = cloneComments(rendered.Comments, options.IncludeReplies)
	}
	if options.IncludeAlbums {
		document.Article.Albums = append([]processor.Album(nil), input.Article.Albums...)
	}
	if options.IncludeProvenance {
		if input.Provenance == nil {
			return JSONExportDocument{}, nil, fmt.Errorf("JSON provenance was requested but not provided")
		}
		if err := validateProvenanceManifest(*input.Provenance); err != nil {
			return JSONExportDocument{}, nil, fmt.Errorf("validate JSON provenance: %w", err)
		}
		provenance := cloneProvenanceManifest(*input.Provenance)
		document.Article.Provenance = &provenance
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return JSONExportDocument{}, nil, fmt.Errorf("encode JSON export: %w", err)
	}
	return document, append(data, '\n'), nil
}

func ExportJSONFile(
	ctx context.Context,
	manager *OutputManager,
	relativePath string,
	input JSONExportInput,
	options JSONOptions,
	policy CollisionPolicy,
) (OutputFile, error) {
	_, data, err := MarshalJSONExport(input, options)
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
	output.ArticleID = input.ArticleID
	return output, nil
}

func cloneComments(comments []processor.Comment, includeReplies bool) []processor.Comment {
	result := make([]processor.Comment, len(comments))
	for index, comment := range comments {
		result[index] = comment
		if includeReplies {
			result[index].Replies = append([]processor.Reply(nil), comment.Replies...)
		} else {
			result[index].Replies = nil
		}
	}
	return result
}

func cloneProvenanceManifest(manifest ProvenanceManifest) ProvenanceManifest {
	data, err := json.Marshal(manifest)
	if err != nil {
		return ProvenanceManifest{}
	}
	var clone ProvenanceManifest
	if err := json.Unmarshal(data, &clone); err != nil {
		return ProvenanceManifest{}
	}
	return clone
}
