package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

type HTMLAsset struct {
	URL       string `json:"url"`
	MediaType string `json:"mediaType,omitempty"`
	Name      string `json:"name,omitempty"`
	Data      []byte `json:"-"`
}

type HTMLArticleInput struct {
	ArticleID domain.ArticleID
	Directory string
	Article   processor.Article
	Assets    []HTMLAsset
	Comments  []processor.Comment
}

type HTMLOptions struct {
	ResourcePolicy  processor.ResourceRewritePolicy
	IncludeComments bool
	Privacy         processor.CommentPrivacy
}

type HTMLArticleResult struct {
	ArticleID        domain.ArticleID     `json:"articleId"`
	Directory        string               `json:"directory"`
	Outputs          []OutputFile         `json:"outputs"`
	MissingResources []processor.Resource `json:"missingResources,omitempty"`
	Warnings         []Warning            `json:"warnings,omitempty"`
}

type HTMLBatchResult struct {
	Output   OutputFile          `json:"output"`
	Articles []HTMLArticleResult `json:"articles"`
	Warnings []Warning           `json:"warnings,omitempty"`
}

type preparedHTMLArticle struct {
	result HTMLArticleResult
	files  []preparedHTMLFile
}

type preparedHTMLFile struct {
	path string
	data []byte
}

func ExportHTMLArticle(
	ctx context.Context,
	manager *OutputManager,
	input HTMLArticleInput,
	options HTMLOptions,
	policy CollisionPolicy,
) (HTMLArticleResult, error) {
	prepared, err := prepareHTMLArticle(input, options)
	if err != nil {
		return HTMLArticleResult{}, err
	}
	outputs := make([]OutputFile, 0, len(prepared.files))
	for _, file := range prepared.files {
		output, err := manager.WriteFile(ctx, file.path, policy, func(writer io.Writer) error {
			_, err := writer.Write(file.data)
			return err
		})
		if err != nil {
			return HTMLArticleResult{}, err
		}
		output.ArticleID = input.ArticleID
		outputs = append(outputs, output)
	}
	prepared.result.Outputs = outputs
	return prepared.result, nil
}

func ExportHTMLBatchArchive(
	ctx context.Context,
	manager *OutputManager,
	relativePath string,
	inputs []HTMLArticleInput,
	options HTMLOptions,
	policy CollisionPolicy,
) (HTMLBatchResult, error) {
	if len(inputs) == 0 {
		return HTMLBatchResult{}, errors.New("HTML batch requires at least one article")
	}
	prepared := make([]preparedHTMLArticle, 0, len(inputs))
	for _, input := range inputs {
		article, err := prepareHTMLArticle(input, options)
		if err != nil {
			return HTMLBatchResult{}, fmt.Errorf("prepare article %s: %w", input.ArticleID, err)
		}
		prepared = append(prepared, article)
	}
	sort.Slice(prepared, func(i, j int) bool {
		if prepared[i].result.ArticleID == prepared[j].result.ArticleID {
			return prepared[i].result.Directory < prepared[j].result.Directory
		}
		return prepared[i].result.ArticleID < prepared[j].result.ArticleID
	})

	archiveBytes, err := buildHTMLArchive(ctx, prepared)
	if err != nil {
		return HTMLBatchResult{}, err
	}
	output, err := manager.WriteFile(ctx, relativePath, policy, func(writer io.Writer) error {
		_, err := writer.Write(archiveBytes)
		return err
	})
	if err != nil {
		return HTMLBatchResult{}, err
	}
	articles := make([]HTMLArticleResult, len(prepared))
	collector := NewWarningCollector()
	for index, article := range prepared {
		articles[index] = article.result
		for _, warning := range article.result.Warnings {
			collector.Add(warning.Code, warning.Message, warning.ArticleIDs...)
		}
	}
	return HTMLBatchResult{Output: output, Articles: articles, Warnings: collector.Warnings()}, nil
}

func prepareHTMLArticle(input HTMLArticleInput, options HTMLOptions) (preparedHTMLArticle, error) {
	if strings.TrimSpace(string(input.ArticleID)) == "" {
		return preparedHTMLArticle{}, errors.New("HTML article ID is required")
	}
	directory, err := normalizeRelativeOutputPath(input.Directory)
	if err != nil {
		return preparedHTMLArticle{}, err
	}
	if strings.Contains(directory, "/") {
		return preparedHTMLArticle{}, fmt.Errorf("HTML article directory must be one path segment: %w", ErrUnsafePath)
	}
	policy := options.ResourcePolicy
	if policy == "" {
		policy = processor.ResourceRewriteBestEffort
	}
	if policy != processor.ResourceRewriteStrict && policy != processor.ResourceRewriteBestEffort {
		return preparedHTMLArticle{}, fmt.Errorf("unsupported HTML resource policy %q", policy)
	}

	assets, resourceMap, err := prepareHTMLAssets(directory, input.Assets)
	if err != nil {
		return preparedHTMLArticle{}, err
	}
	rendered, err := processor.Render(input.Article, processor.RenderOptions{
		ResourceMap: resourceMap, ResourcePolicy: policy, IncludeComments: options.IncludeComments,
		Comments: input.Comments, Privacy: options.Privacy,
	})
	if err != nil {
		return preparedHTMLArticle{}, err
	}
	warnings := NewWarningCollector()
	for _, missing := range rendered.MissingResources {
		warnings.Add("missing_resource", fmt.Sprintf("%s resource was not available locally: %s", missing.Kind, missing.URL), input.ArticleID)
	}
	files := make([]preparedHTMLFile, 0, len(assets)+1)
	files = append(files, assets...)
	files = append(files, preparedHTMLFile{path: directory + "/index.html", data: []byte(rendered.HTML)})
	return preparedHTMLArticle{
		result: HTMLArticleResult{ArticleID: input.ArticleID, Directory: directory,
			MissingResources: append([]processor.Resource(nil), rendered.MissingResources...), Warnings: warnings.Warnings()},
		files: files,
	}, nil
}

func prepareHTMLAssets(directory string, assets []HTMLAsset) ([]preparedHTMLFile, map[string]string, error) {
	type candidate struct {
		asset HTMLAsset
		path  string
	}
	candidates := make([]candidate, 0, len(assets))
	seenURLs := make(map[string]struct{}, len(assets))
	for index, asset := range assets {
		url := strings.TrimSpace(asset.URL)
		if url == "" {
			return nil, nil, fmt.Errorf("HTML asset %d has no URL", index)
		}
		if _, exists := seenURLs[url]; exists {
			return nil, nil, fmt.Errorf("duplicate HTML asset URL %q", url)
		}
		seenURLs[url] = struct{}{}
		if len(asset.Data) == 0 {
			return nil, nil, fmt.Errorf("HTML asset %q has no local data", url)
		}
		name, err := htmlAssetName(asset)
		if err != nil {
			return nil, nil, fmt.Errorf("HTML asset %q: %w", url, err)
		}
		candidates = append(candidates, candidate{asset: asset, path: directory + "/assets/" + name})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].path == candidates[j].path {
			return candidates[i].asset.URL < candidates[j].asset.URL
		}
		return candidates[i].path < candidates[j].path
	})
	used := make(map[string]string, len(candidates))
	files := make([]preparedHTMLFile, 0, len(candidates))
	mapping := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		path := candidate.path
		if existingURL := used[path]; existingURL != "" && existingURL != candidate.asset.URL {
			extension := filepath.Ext(path)
			base := strings.TrimSuffix(path, extension)
			digest := sha256.Sum256([]byte(candidate.asset.URL))
			path = base + "--" + hex.EncodeToString(digest[:5]) + extension
		}
		used[path] = candidate.asset.URL
		files = append(files, preparedHTMLFile{path: path, data: append([]byte(nil), candidate.asset.Data...)})
		relative := "./" + strings.TrimPrefix(path, directory+"/")
		mapping[candidate.asset.URL] = relative
	}
	return files, mapping, nil
}

func htmlAssetName(asset HTMLAsset) (string, error) {
	extension := ""
	if values, err := mime.ExtensionsByType(strings.TrimSpace(asset.MediaType)); err == nil && len(values) > 0 {
		sort.Strings(values)
		extension = values[0]
	}
	if extension == "" {
		extension = filepath.Ext(strings.SplitN(asset.URL, "?", 2)[0])
	}
	if len(extension) > 12 || strings.ContainsAny(extension, `/\\`) {
		extension = ""
	}
	base := strings.TrimSpace(asset.Name)
	if base == "" {
		digest := sha256.Sum256([]byte(asset.URL))
		base = hex.EncodeToString(digest[:16])
	}
	name, err := RenderFilename(NamingOptions{Template: base, Extension: extension, MaximumBytes: 120, Platform: PlatformPortable},
		NamingData{ArticleID: domain.ArticleID(asset.URL), Title: base})
	if err != nil {
		return "", err
	}
	return name, nil
}

func buildHTMLArchive(ctx context.Context, articles []preparedHTMLArticle) ([]byte, error) {
	files := make([]preparedHTMLFile, 0)
	seen := make(map[string]struct{})
	for _, article := range articles {
		for _, file := range article.files {
			if _, exists := seen[file.path]; exists {
				return nil, fmt.Errorf("duplicate HTML archive path %q", file.path)
			}
			seen[file.path] = struct{}{}
			files = append(files, file)
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range files {
		select {
		case <-ctx.Done():
			_ = writer.Close()
			return nil, ctx.Err()
		default:
		}
		header := &zip.FileHeader{Name: filepath.ToSlash(file.path), Method: zip.Deflate}
		header.SetMode(0o600)
		header.Modified = time.Unix(0, 0).UTC()
		destination, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create HTML archive entry %q: %w", file.path, err)
		}
		if _, err := destination.Write(file.data); err != nil {
			return nil, fmt.Errorf("write HTML archive entry %q: %w", file.path, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close HTML batch archive: %w", err)
	}
	return buffer.Bytes(), nil
}
