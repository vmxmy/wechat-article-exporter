package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/exporter"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	runtimeenv "github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
)

type localExportRuntime struct {
	profile domain.ProfileID
	library *library.Database
	objects *objects.FileStore
	store   *library.JobStore
	now     func() time.Time
	browser runtimeenv.BrowserDiscovery
	runner  exporter.ProcessRunner
}

type exportJobItem struct {
	Version   int                        `json:"version"`
	ExportID  domain.ExportID            `json:"exportId"`
	ArticleID domain.ArticleID           `json:"articleId"`
	Format    string                     `json:"format"`
	Output    string                     `json:"output"`
	Options   domain.ExportOptions       `json:"options"`
	Selection exporter.SelectionManifest `json:"selection"`
}

func newLocalExportRuntime(
	runtime *ProfileRuntime,
	clock runtimeenv.Clock,
	browser runtimeenv.BrowserDiscovery,
	runner ...exporter.ProcessRunner,
) *localExportRuntime {
	if runtime == nil || runtime.Library == nil || runtime.Objects == nil || runtime.Jobs == nil {
		return nil
	}
	now := time.Now
	if clock != nil {
		now = clock.Now
	}
	var pdfRunner exporter.ProcessRunner
	if len(runner) > 0 {
		pdfRunner = runner[0]
	}
	return &localExportRuntime{
		profile: runtime.Profile.ID, library: runtime.Library, objects: runtime.Objects,
		store: runtime.Jobs, now: now, browser: browser, runner: pdfRunner,
	}
}

func (runtime *localExportRuntime) Start(ctx context.Context, request domain.ExportRequest) (domain.Job, error) {
	if runtime == nil || runtime.library == nil || runtime.store == nil {
		return domain.Job{}, fmt.Errorf("export runtime: %w", application.ErrUnavailable)
	}
	if strings.TrimSpace(request.OutputRoot) == "" {
		return domain.Job{}, errors.New("export output root is required")
	}
	format := strings.ToLower(strings.TrimSpace(request.Format))
	if !supportedLocalExportFormat(format) {
		return domain.Job{}, fmt.Errorf("unsupported export format %q", request.Format)
	}
	manifest, err := exporter.BuildSelectionManifest(ctx, runtime.library, request, runtime.now())
	if err != nil {
		return domain.Job{}, err
	}
	articles := make([]domain.Article, 0, len(manifest.ArticleIDs))
	for _, articleID := range manifest.ArticleIDs {
		article, err := runtime.library.GetArticle(ctx, articleID)
		if err != nil {
			return domain.Job{}, fmt.Errorf("load selected article %s: %w", articleID, err)
		}
		articles = append(articles, article)
	}
	names, err := planExportNames(format, request.Options, articles)
	if err != nil {
		return domain.Job{}, err
	}
	exportID := domain.ExportID(uuid.NewString())
	items := make([]string, 0, len(articles))
	for index, article := range articles {
		envelope := exportJobItem{
			Version: 1, ExportID: exportID, ArticleID: article.ID, Format: format,
			Output: filepath.Join(request.OutputRoot, names[index].Path), Options: request.Options, Selection: manifest,
		}
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return domain.Job{}, fmt.Errorf("encode export job item: %w", err)
		}
		items = append(items, string(encoded))
	}
	job, err := runtime.store.CreateWithItems(ctx, jobs.Spec{
		Kind: "export", Profile: runtime.profile,
		Payload: map[string]any{"exportId": exportID, "format": format, "outputRoot": request.OutputRoot, "selection": manifest},
	}, items)
	if err != nil {
		return domain.Job{}, err
	}
	if err := runtime.library.UpsertExport(ctx, library.ExportRecord{
		ID: exportID, JobID: job.ID, Format: format, Manifest: manifest, OutputRoot: request.OutputRoot,
		State: string(domain.JobQueued), CreatedAt: runtime.now(),
	}); err != nil {
		_, _ = runtime.store.Cancel(context.Background(), job.ID)
		return domain.Job{}, err
	}
	return job, nil
}

func (runtime *localExportRuntime) Run(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if runtime == nil || runtime.store == nil {
		return domain.Job{}, fmt.Errorf("export runtime: %w", application.ErrUnavailable)
	}
	engine, err := jobs.NewEngine(runtime.store, jobs.EngineOptions{
		Owner: "local-export-worker",
		Metadata: func(jobs.Item) jobs.WorkMetadata {
			return jobs.WorkMetadata{Operation: "export", Host: "local"}
		},
	})
	if err != nil {
		return domain.Job{}, err
	}
	job, runErr := engine.Run(ctx, id, runtime.execute)
	if job.ID == "" {
		return job, runErr
	}
	if updateErr := runtime.library.UpdateExportStateByJob(context.Background(), id, string(job.State), runtime.now()); updateErr != nil {
		if runErr != nil {
			return job, errors.Join(runErr, fmt.Errorf("update export state: %w", updateErr))
		}
		return job, fmt.Errorf("update export state: %w", updateErr)
	}
	return job, runErr
}

func (runtime *localExportRuntime) Recover(ctx context.Context) (int64, error) {
	if runtime == nil || runtime.store == nil {
		return 0, fmt.Errorf("export runtime: %w", application.ErrUnavailable)
	}
	return runtime.store.RecoverStale(ctx)
}

func (runtime *localExportRuntime) execute(ctx context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
	var envelope exportJobItem
	decoder := json.NewDecoder(strings.NewReader(item.Key))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode export job item: %w", err)}
	}
	if envelope.Version != 1 {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode export job item: unsupported version %d", envelope.Version)}
	}
	if envelope.ArticleID == "" {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: errors.New("decode export job item: article ID is required")}
	}
	article, normalized, comments, assets, err := runtime.loadExportArticle(ctx, envelope.ArticleID)
	if err != nil {
		return err
	}
	manager, err := exporter.NewOutputManager(filepath.Dir(envelope.Output))
	if err != nil {
		return err
	}
	policy := exportCollisionPolicy(envelope.Options.CollisionPolicy)
	outputName := filepath.Base(envelope.Output)
	if strings.EqualFold(strings.TrimSpace(envelope.Options.CollisionPolicy), "suffix") {
		outputName, err = availableExportOutputName(manager.Root(), outputName, envelope.ArticleID,
			envelope.Options.MaximumNameBytes)
		if err != nil {
			return err
		}
		policy = exporter.CollisionFail
	}
	includeComments := optionBool(envelope.Options.FormatOptions, "comments", false)
	includeContent := optionBool(envelope.Options.FormatOptions, "content", true)
	includeMetadata := optionBool(envelope.Options.FormatOptions, "metadata", true)
	var output exporter.OutputFile
	var outputs []exporter.OutputFile
	switch envelope.Format {
	case "html":
		result, exportErr := exporter.ExportHTMLArticle(ctx, manager, exporter.HTMLArticleInput{
			ArticleID: article.ID, Directory: strings.TrimSuffix(outputName, filepath.Ext(outputName)), Article: normalized,
			Assets: assets, Comments: comments,
		}, exporter.HTMLOptions{ResourcePolicy: processor.ResourceRewriteBestEffort, IncludeComments: includeComments}, policy)
		if exportErr != nil {
			return exportErr
		}
		for _, candidate := range result.Outputs {
			if strings.HasSuffix(candidate.Path, "/index.html") {
				output = candidate
				break
			}
		}
		outputs = result.Outputs
	case "markdown":
		output, err = exporter.ExportMarkdownFile(ctx, manager, outputName, article.ID, normalized,
			exporter.MarkdownOptions{IncludeFrontMatter: includeMetadata, IncludeComments: includeComments, Comments: comments}, policy)
	case "text":
		output, err = exporter.ExportTextFile(ctx, manager, outputName, article.ID, normalized,
			exporter.TextOptions{IncludeMetadataHeader: includeMetadata, IncludeComments: includeComments, Comments: comments}, policy)
	case "json":
		output, err = exporter.ExportJSONFile(ctx, manager, outputName, exporter.JSONExportInput{
			ArticleID: article.ID, Article: normalized, Comments: comments, ExportedAt: runtime.now(),
		}, exporter.JSONOptions{IncludeContent: includeContent, IncludeMetrics: includeMetadata, IncludeComments: includeComments,
			IncludeReplies: includeComments, IncludeAlbums: includeMetadata}, policy)
	case "xlsx":
		output, err = manager.WriteFile(ctx, outputName, policy, func(writer io.Writer) error {
			_, writeErr := exporter.WriteXLSX(ctx, writer, &singleXLSXSource{row: xlsxRow(article, normalized, includeContent)},
				exporter.XLSXOptions{IncludeContent: includeContent, SheetName: "Articles"})
			return writeErr
		})
	case "docx":
		output, err = manager.WriteFile(ctx, outputName, policy, func(writer io.Writer) error {
			_, writeErr := exporter.WriteDOCX(ctx, writer, docxDocument(normalized, comments, assets),
				exporter.DOCXOptions{IncludeComments: includeComments})
			return writeErr
		})
	case "pdf":
		browserPath := ""
		if runtime.browser != nil {
			browser, browserErr := runtime.browser.FindChromium(ctx)
			if browserErr != nil {
				return browserErr
			}
			browserPath = browser.Path
		}
		rendered, renderErr := processor.Render(normalized, processor.RenderOptions{
			ResourceMap: dataResourceMap(assets), ResourcePolicy: processor.ResourceRewriteBestEffort,
			IncludeComments: includeComments, Comments: comments,
		})
		if renderErr != nil {
			return renderErr
		}
		output, err = manager.WriteFile(ctx, outputName, policy, func(writer io.Writer) error {
			_, writeErr := exporter.RenderPDF(ctx, writer, rendered.HTML, exporter.PDFOptions{
				BrowserPath: browserPath, Runner: runtime.runner,
			})
			return writeErr
		})
	}
	if err != nil {
		return err
	}
	output.ArticleID = article.ID
	if len(outputs) == 0 {
		outputs = []exporter.OutputFile{output}
	}
	for _, exported := range outputs {
		if err := runtime.library.UpsertExportFile(ctx, library.ExportFileRecord{
			ExportID: envelope.ExportID, RelativePath: exported.Path, SizeBytes: exported.Size,
			SHA256: exported.SHA256, MediaType: exportMediaType(envelope.Format, exported.Path),
		}); err != nil {
			return fmt.Errorf("record exported file %s: %w", exported.Path, err)
		}
	}
	if err := checkpoint(map[string]any{"exportId": envelope.ExportID, "output": output}); err != nil {
		return err
	}
	return nil
}

func availableExportOutputName(
	root, plannedName string,
	articleID domain.ArticleID,
	maximumBytes int,
) (string, error) {
	if maximumBytes == 0 {
		maximumBytes = exporter.DefaultMaximumBytes
	}
	name := plannedName
	for attempt := 0; ; attempt++ {
		candidatePath := filepath.Join(root, name)
		_, err := os.Lstat(candidatePath)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect export destination %s: %w", candidatePath, err)
		}
		suffixID := articleID
		if attempt > 0 {
			suffixID = domain.ArticleID(string(articleID) + "-" + strconv.Itoa(attempt+1))
		}
		name, err = exporter.AddCollisionSuffix(plannedName, suffixID, exporter.NamingOptions{
			MaximumBytes: maximumBytes, Platform: exporter.PlatformPortable,
		})
		if err != nil {
			return "", err
		}
	}
}

func exportMediaType(format, path string) string {
	if format == "html" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".html", ".htm":
			return "text/html"
		case ".css":
			return "text/css"
		}
		return "application/octet-stream"
	}
	return map[string]string{
		"markdown": "text/markdown", "text": "text/plain", "json": "application/json",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"pdf":  "application/pdf",
	}[format]
}

func (runtime *localExportRuntime) loadExportArticle(
	ctx context.Context,
	articleID domain.ArticleID,
) (domain.Article, processor.Article, []processor.Comment, []exporter.HTMLAsset, error) {
	article, err := runtime.library.GetArticle(ctx, articleID)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	content, err := runtime.library.CurrentContent(ctx, articleID, "html")
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, fmt.Errorf("article %s has no downloaded HTML content: %w", articleID, err)
	}
	reader, _, err := runtime.objects.Open(ctx, content.ObjectDigest)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	parsed, processErr := processor.New().Process(ctx, reader)
	closeErr := reader.Close()
	if processErr != nil || closeErr != nil || parsed.Article == nil {
		return domain.Article{}, processor.Article{}, nil, nil, errors.Join(processErr, closeErr)
	}
	comments, err := runtime.loadComments(ctx, articleID)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	assets, err := runtime.loadAssets(ctx, articleID)
	if err != nil {
		return domain.Article{}, processor.Article{}, nil, nil, err
	}
	return article, *parsed.Article, comments, assets, nil
}

func (runtime *localExportRuntime) loadComments(ctx context.Context, articleID domain.ArticleID) ([]processor.Comment, error) {
	records, err := runtime.library.CommentsForArticle(ctx, articleID)
	if err != nil {
		return nil, err
	}
	comments := make([]processor.Comment, 0, len(records))
	for _, record := range records {
		created := record.CreatedAt
		comment := processor.Comment{ID: record.UpstreamID, Author: record.AuthorName, Content: record.Content, Likes: int64(record.LikeCount)}
		if !created.IsZero() {
			comment.CreatedAt = &created
		}
		for _, reply := range record.EmbeddedReplies {
			replyCreated := reply.CreatedAt
			value := processor.Reply{ID: reply.UpstreamID, Author: reply.AuthorName, Content: reply.Content, Likes: int64(reply.LikeCount)}
			if !replyCreated.IsZero() {
				value.CreatedAt = &replyCreated
			}
			comment.Replies = append(comment.Replies, value)
		}
		comments = append(comments, comment)
	}
	return comments, nil
}

func (runtime *localExportRuntime) loadAssets(ctx context.Context, articleID domain.ArticleID) ([]exporter.HTMLAsset, error) {
	mappings, err := runtime.library.ListArticleResources(ctx, articleID)
	if err != nil {
		return nil, err
	}
	assets := make([]exporter.HTMLAsset, 0, len(mappings))
	for _, mapping := range mappings {
		record, err := runtime.library.ResourceByURL(ctx, mapping.OriginalURL)
		if err != nil || record.ObjectDigest == "" || record.Status != "available" {
			continue
		}
		reader, _, err := runtime.objects.Open(ctx, record.ObjectDigest)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.Join(readErr, closeErr)
		}
		assets = append(assets, exporter.HTMLAsset{
			URL: mapping.OriginalURL, Name: filepath.Base(strings.SplitN(mapping.OriginalURL, "?", 2)[0]),
			MediaType: record.MediaType, Data: data,
		})
	}
	return assets, nil
}

func planExportNames(format string, options domain.ExportOptions, articles []domain.Article) ([]exporter.PlannedName, error) {
	extension := map[string]string{
		"html": "", "markdown": ".md", "text": ".txt", "json": ".json",
		"xlsx": ".xlsx", "docx": ".docx", "pdf": ".pdf",
	}[format]
	items := make([]exporter.NamingData, len(articles))
	for index, article := range articles {
		items[index] = exporter.NamingData{ArticleID: article.ID, AccountID: article.AccountID, Title: article.Title,
			AID: article.Aid, Author: article.Author, PublishedAt: article.PublishedAt, Index: index + 1}
	}
	template := strings.NewReplacer(
		"{title}", "${title}", "{published}", "${YYYY}-${MM}-${DD}", "{author}", "${author}",
		"{articleId}", "${articleId}", "{aid}", "${aid}", "{index}", "${index}",
	).Replace(options.NamingTemplate)
	return exporter.PlanFilenames(exporter.NamingOptions{
		Template: template, Extension: extension, MaximumBytes: options.MaximumNameBytes, Platform: exporter.PlatformPortable,
	}, items)
}

func supportedLocalExportFormat(format string) bool {
	switch format {
	case "html", "markdown", "text", "json", "xlsx", "docx", "pdf":
		return true
	default:
		return false
	}
}

func exportCollisionPolicy(value string) exporter.CollisionPolicy {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "skip":
		return exporter.CollisionSkip
	case "replace":
		return exporter.CollisionReplace
	default:
		return exporter.CollisionFail
	}
}

func optionBool(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key]
	if !ok {
		return fallback
	}
	result, ok := value.(bool)
	if !ok {
		return fallback
	}
	return result
}

type singleXLSXSource struct {
	row  exporter.XLSXRow
	done bool
}

func (source *singleXLSXSource) Next(context.Context) (exporter.XLSXRow, error) {
	if source.done {
		return exporter.XLSXRow{}, io.EOF
	}
	source.done = true
	return source.row, nil
}

func xlsxRow(article domain.Article, normalized processor.Article, includeContent bool) exporter.XLSXRow {
	row := exporter.XLSXRow{
		Account: normalized.Account.Nickname, ArticleID: string(article.ID), CanonicalURL: article.CanonicalURL,
		Title: article.Title, CoverURL: article.CoverURL, Digest: article.Digest, PublishedAt: article.PublishedAt,
		ReadCount: int64(article.ReadCount), OldLikeCount: int64(article.OldLikeCount), ShareCount: int64(article.ShareCount),
		LikeCount: int64(article.LikeCount), CommentCount: int64(article.CommentCount), Author: article.Author,
		Original: article.Original, MessageType: strconv.Itoa(article.MessageType), State: article.State,
		DownloadState: map[bool]string{true: "available", false: "missing"}[article.HasContent],
	}
	for _, album := range article.Albums {
		row.Albums = append(row.Albums, album.Name)
	}
	if includeContent {
		if rendered, err := processor.Render(normalized, processor.RenderOptions{ResourcePolicy: processor.ResourceRewriteBestEffort}); err == nil {
			row.Content = rendered.Text
		}
	}
	return row
}

func docxDocument(article processor.Article, comments []processor.Comment, assets []exporter.HTMLAsset) exporter.DOCXDocument {
	document := exporter.DOCXDocument{Title: article.Title, Account: article.Account.Nickname, Author: article.Author, HTML: article.Content}
	if article.Timestamps.PublishedAt != nil {
		document.PublishedAt = *article.Timestamps.PublishedAt
	}
	for _, asset := range assets {
		name := filepath.Base(asset.Name)
		if name == "." || name == "" {
			name = filepath.Base(strings.SplitN(asset.URL, "?", 2)[0])
		}
		document.Media = append(document.Media, exporter.DOCXMedia{
			Source: asset.URL, Name: name, ContentType: asset.MediaType, Data: asset.Data,
		})
	}
	for _, comment := range comments {
		value := exporter.DOCXComment{Author: comment.Author, Content: comment.Content}
		if comment.CreatedAt != nil {
			value.CreatedAt = *comment.CreatedAt
		}
		for _, reply := range comment.Replies {
			replyValue := exporter.DOCXReply{Author: reply.Author, Content: reply.Content}
			if reply.CreatedAt != nil {
				replyValue.CreatedAt = *reply.CreatedAt
			}
			value.Replies = append(value.Replies, replyValue)
		}
		document.Comments = append(document.Comments, value)
	}
	return document
}

func dataResourceMap(assets []exporter.HTMLAsset) map[string]string {
	mapping := make(map[string]string, len(assets))
	for _, asset := range assets {
		mapping[asset.URL] = "data:" + asset.MediaType + ";base64," + base64String(asset.Data)
	}
	return mapping
}

func base64String(data []byte) string {
	var output bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &output)
	_, _ = encoder.Write(data)
	_ = encoder.Close()
	return output.String()
}

var _ application.ExportJobs = (*localExportRuntime)(nil)
