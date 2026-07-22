package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

type regressionFixture struct {
	ArticleID    domain.ArticleID    `json:"articleId"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Author       string              `json:"author"`
	Account      string              `json:"account"`
	CanonicalURL string              `json:"canonicalUrl"`
	PublishedAt  time.Time           `json:"publishedAt"`
	Content      string              `json:"content"`
	Comments     []processor.Comment `json:"comments"`
}

type regressionGolden struct {
	TextSHA256     string `json:"textSha256"`
	MarkdownSHA256 string `json:"markdownSha256"`
	HTMLSHA256     string `json:"htmlSha256"`
	JSONSHA256     string `json:"jsonSha256"`
	XLSXSHA256     string `json:"xlsxSha256"`
	DOCXSHA256     string `json:"docxSha256"`
	PDFSHA256      string `json:"pdfSha256"`
	HTML           struct {
		Required  []string `json:"required"`
		Forbidden []string `json:"forbidden"`
	} `json:"html"`
	PDF struct {
		RequiredText []string `json:"requiredText"`
		RequiredArgs []string `json:"requiredArgs"`
		Forbidden    []string `json:"forbidden"`
	} `json:"pdf"`
}

func TestFormatSpecificGoldenAndStructuralRegression(t *testing.T) {
	fixture := loadRegressionFixture(t)
	golden := loadRegressionGolden(t)
	article := fixtureArticle(fixture)
	comments := fixture.Comments

	text, err := RenderText(fixture.ArticleID, article, TextOptions{
		IncludeMetadataHeader: true, IncludeComments: true, Comments: comments,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenDigest(t, "text", text, golden.TextSHA256)
	if !strings.Contains(string(text), "Schema-Version: "+TextExportSchemaVersion) || !strings.Contains(string(text), "读者甲") {
		t.Fatalf("text regression lost metadata/comments:\n%s", text)
	}

	markdown, err := RenderMarkdown(fixture.ArticleID, article, MarkdownOptions{
		IncludeFrontMatter: true, IncludeComments: true, Comments: comments,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenDigest(t, "markdown", markdown, golden.MarkdownSHA256)
	if !strings.Contains(string(markdown), "schemaVersion: \""+MarkdownExportSchemaVersion+"\"") ||
		!strings.Contains(string(markdown), "## Comments") || strings.Contains(string(markdown), "<script") {
		t.Fatalf("Markdown regression mismatch:\n%s", markdown)
	}

	htmlPrepared, err := prepareHTMLArticle(HTMLArticleInput{
		ArticleID: fixture.ArticleID, Directory: "article-regression", Article: article,
		Assets:   []HTMLAsset{{URL: "https://cdn.example.test/regression.png", Name: "regression.png", MediaType: "image/png", Data: regressionPNG}},
		Comments: comments,
	}, HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict, IncludeComments: true})
	if err != nil {
		t.Fatal(err)
	}
	html := preparedFileData(t, htmlPrepared.files, "article-regression/index.html")
	assertGoldenDigest(t, "html", html, golden.HTMLSHA256)
	assertContainsAll(t, "HTML", string(html), golden.HTML.Required)
	assertContainsNone(t, "HTML", string(html), golden.HTML.Forbidden)

	_, jsonBytes, err := MarshalJSONExport(JSONExportInput{
		ArticleID: fixture.ArticleID, Article: article, Comments: comments,
		ExportedAt: time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC),
	}, JSONOptions{IncludeContent: true, IncludeMetrics: true, IncludeComments: true, IncludeReplies: true, IncludeAlbums: true})
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenDigest(t, "json", jsonBytes, golden.JSONSHA256)
	var jsonDocument JSONExportDocument
	if err := json.Unmarshal(jsonBytes, &jsonDocument); err != nil {
		t.Fatal(err)
	}
	if jsonDocument.SchemaVersion != JSONExportSchemaVersion || jsonDocument.Article.Content == nil ||
		len(jsonDocument.Article.Comments) != 1 || len(jsonDocument.Article.Comments[0].Replies) != 1 {
		t.Fatalf("JSON structural regression: %#v", jsonDocument)
	}

	xlsxBytes := renderRegressionXLSX(t, fixture)
	assertStructuralDigest(t, "xlsx", canonicalZipDigest(t, xlsxBytes), golden.XLSXSHA256)
	assertZipEntries(t, xlsxBytes, []string{
		"[Content_Types].xml", "_rels/.rels", "docProps/app.xml", "docProps/core.xml",
		"xl/_rels/workbook.xml.rels", "xl/workbook.xml", "xl/worksheets/sheet1.xml",
	})
	xlsxSheet := readZipEntry(t, xlsxBytes, "xl/worksheets/sheet1.xml")
	assertContainsAll(t, "XLSX", xlsxSheet, []string{fixture.Title, fixture.Account, fixture.Author, "article-regression", "正文包含"})

	docxBytes := renderRegressionDOCX(t, fixture)
	assertStructuralDigest(t, "docx", canonicalZipDigest(t, docxBytes), golden.DOCXSHA256)
	validation, err := ValidateDOCX(bytes.NewReader(docxBytes), int64(len(docxBytes)), DOCXValidationOptions{})
	if err != nil || !validation.Valid || validation.Tables != 1 || validation.Hyperlinks != 1 || validation.Media != 1 {
		t.Fatalf("DOCX validation=%#v err=%v", validation, err)
	}
	documentXML := readZipEntry(t, docxBytes, "word/document.xml")
	assertContainsAll(t, "DOCX", documentXML, []string{"离线导出回归文章", "核心结论", "Comments", "读者甲", "谢谢阅读。", "<w:tbl>", "<w:drawing>"})
}

func TestCuratedHTMLPDFVisualAndStructuralRegression(t *testing.T) {
	fixture := loadRegressionFixture(t)
	golden := loadRegressionGolden(t)
	prepared, err := prepareHTMLArticle(HTMLArticleInput{
		ArticleID: fixture.ArticleID, Directory: "article-regression", Article: fixtureArticle(fixture),
		Assets:   []HTMLAsset{{URL: "https://cdn.example.test/regression.png", Name: "regression.png", MediaType: "image/png", Data: regressionPNG}},
		Comments: fixture.Comments,
	}, HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict, IncludeComments: true})
	if err != nil {
		t.Fatal(err)
	}
	html := string(preparedFileData(t, prepared.files, "article-regression/index.html"))
	assertContainsAll(t, "HTML visual baseline", html, golden.HTML.Required)
	assertContainsNone(t, "HTML visual baseline", html, golden.HTML.Forbidden)
	// PDF rendering is deliberately stricter than portable HTML export: it may
	// not trigger any remote navigation. Replace ordinary external hyperlinks
	// with inert text while retaining the curated article structure.
	pdfHTML := strings.ReplaceAll(html, `href="https://example.com/reference"`, `href="#external-reference"`)
	pdfHTML = strings.ReplaceAll(pdfHTML, `src="./assets/regression.png.png"`,
		`src="data:image/png;base64,`+base64.StdEncoding.EncodeToString(regressionPNG)+`"`)

	runner := &deterministicRegressionPDFRunner{golden: golden}
	var output bytes.Buffer
	report, err := RenderPDF(context.Background(), &output, pdfHTML, PDFOptions{
		BrowserPath: "/fixture/chromium", Runner: runner, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenDigest(t, "pdf", output.Bytes(), golden.PDFSHA256)
	if report.Bytes != int64(output.Len()) || report.PageFormat != "A4" || runner.calls != 1 {
		t.Fatalf("PDF report=%#v calls=%d", report, runner.calls)
	}
	assertContainsAll(t, "PDF captured HTML", runner.inputHTML, golden.PDF.RequiredText)
	assertContainsNone(t, "PDF captured HTML", runner.inputHTML, golden.PDF.Forbidden)
	for _, argument := range golden.PDF.RequiredArgs {
		if !containsArgument(runner.args, argument) {
			t.Fatalf("PDF invocation missing %q: %#v", argument, runner.args)
		}
	}
}

func loadRegressionFixture(t *testing.T) regressionFixture {
	t.Helper()
	var fixture regressionFixture
	if err := json.Unmarshal(readExporterFixture(t, "regression_article.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func loadRegressionGolden(t *testing.T) regressionGolden {
	t.Helper()
	var golden regressionGolden
	if err := json.Unmarshal(readExporterFixture(t, "regression_golden.json"), &golden); err != nil {
		t.Fatal(err)
	}
	return golden
}

func fixtureArticle(fixture regressionFixture) processor.Article {
	reads := int64(1200)
	comments := int64(len(fixture.Comments))
	return processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion,
		Identity:      processor.Identity{MessageID: "message-regression", AppMessage: "aid-regression", Index: 1},
		Title:         fixture.Title,
		Description:   fixture.Description,
		Author:        fixture.Author,
		Account:       processor.Account{Nickname: fixture.Account, Alias: "fixture"},
		CanonicalURL:  fixture.CanonicalURL,
		Content:       fixture.Content,
		Timestamps:    processor.Timestamps{PublishedAt: &fixture.PublishedAt},
		Message:       processor.Message{Type: processor.MessageTypeGraphic},
		Media:         processor.Media{Images: []processor.Image{{URL: "https://cdn.example.test/regression.png", Caption: "回归图"}}},
		Albums:        []processor.Album{{ID: "album-regression", Title: "回归合集"}},
		Comments:      processor.Comments{Enabled: true, SelectedCount: &comments},
		Engagement:    processor.Engagement{Reads: &reads, Comments: &comments},
		Language:      "zh_CN",
	}
}

func renderRegressionXLSX(t *testing.T, fixture regressionFixture) []byte {
	t.Helper()
	source := &regressionXLSXSource{rows: []XLSXRow{{
		Account: fixture.Account, ArticleID: string(fixture.ArticleID), CanonicalURL: fixture.CanonicalURL,
		Title: fixture.Title, Digest: fixture.Description, PublishedAt: fixture.PublishedAt, ReadCount: 1200,
		Author: fixture.Author, Original: true, MessageType: "graphic", State: "ready", DownloadState: "available",
		Albums: []string{"回归合集"}, Content: "正文包含 粗体、斜体、参考链接和本地图片。",
	}}}
	var output bytes.Buffer
	if _, err := WriteXLSX(context.Background(), &output, source, XLSXOptions{IncludeContent: true, SheetName: "回归"}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func renderRegressionDOCX(t *testing.T, fixture regressionFixture) []byte {
	t.Helper()
	var output bytes.Buffer
	comments := make([]DOCXComment, len(fixture.Comments))
	for index, comment := range fixture.Comments {
		comments[index] = DOCXComment{Author: comment.Author, Content: comment.Content}
		for _, reply := range comment.Replies {
			comments[index].Replies = append(comments[index].Replies, DOCXReply{Author: reply.Author, Content: reply.Content})
		}
	}
	if _, err := WriteDOCX(context.Background(), &output, DOCXDocument{
		Title: fixture.Title, Account: fixture.Account, Author: fixture.Author, PublishedAt: fixture.PublishedAt,
		HTML:     strings.ReplaceAll(fixture.Content, "https://cdn.example.test/regression.png", "./assets/regression.png"),
		Media:    []DOCXMedia{{Source: "./assets/regression.png", Name: "regression.png", ContentType: "image/png", Data: regressionPNG}},
		Comments: comments,
	}, DOCXOptions{IncludeComments: true}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func preparedFileData(t *testing.T, files []preparedHTMLFile, path string) []byte {
	t.Helper()
	for _, file := range files {
		if file.path == path {
			return file.data
		}
	}
	t.Fatalf("prepared file %q not found", path)
	return nil
}

func assertGoldenDigest(t *testing.T, name string, data []byte, expected string) {
	t.Helper()
	actual := digestBytes(data)
	if expected == "GENERATE" {
		t.Fatalf("%s golden digest must be set to %s", name, actual)
	}
	if actual != expected {
		t.Fatalf("%s golden mismatch: got %s, want %s", name, actual, expected)
	}
}

func assertStructuralDigest(t *testing.T, name, actual, expected string) {
	t.Helper()
	if expected == "STRUCTURAL" {
		return
	}
	if actual != expected {
		t.Fatalf("%s structural golden mismatch: got %s, want %s", name, actual, expected)
	}
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func canonicalZipDigest(t *testing.T, data []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	type entry struct {
		name string
		data []byte
	}
	entries := make([]entry, 0, len(reader.File))
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry{name: file.Name, data: contents})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	hash := sha256.New()
	for _, entry := range entries {
		_, _ = io.WriteString(hash, entry.name)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(entry.data)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func assertZipEntries(t *testing.T, data []byte, expected []string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(reader.File))
	for index, file := range reader.File {
		names[index] = file.Name
	}
	sort.Strings(names)
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("ZIP entries=%#v, want %#v", names, want)
	}
}

func assertContainsAll(t *testing.T, label, value string, required []string) {
	t.Helper()
	for _, item := range required {
		if !strings.Contains(value, item) {
			t.Fatalf("%s missing %q:\n%s", label, item, value)
		}
	}
}

func assertContainsNone(t *testing.T, label, value string, forbidden []string) {
	t.Helper()
	lower := strings.ToLower(value)
	for _, item := range forbidden {
		if strings.Contains(lower, strings.ToLower(item)) {
			t.Fatalf("%s contains forbidden %q:\n%s", label, item, value)
		}
	}
}

type regressionXLSXSource struct {
	rows  []XLSXRow
	index int
}

func (source *regressionXLSXSource) Next(context.Context) (XLSXRow, error) {
	if source.index >= len(source.rows) {
		return XLSXRow{}, io.EOF
	}
	row := source.rows[source.index]
	source.index++
	return row, nil
}

type deterministicRegressionPDFRunner struct {
	mu        sync.Mutex
	golden    regressionGolden
	calls     int
	args      []string
	inputHTML string
}

func (runner *deterministicRegressionPDFRunner) Run(ctx context.Context, _ string, args []string, _, _ io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	inputPath := ""
	for _, argument := range args {
		if strings.HasPrefix(argument, "file://") {
			path, err := fileURLPath(argument)
			if err != nil {
				return err
			}
			inputPath = path
		}
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	runner.mu.Lock()
	runner.calls++
	runner.args = append([]string(nil), args...)
	runner.inputHTML = string(data)
	runner.mu.Unlock()
	if err := validateSelfContainedPDFHTML(string(data)); err != nil {
		return err
	}
	assertStringsPresent := func(values []string) bool {
		for _, value := range values {
			if !strings.Contains(string(data), value) {
				return false
			}
		}
		return true
	}
	if !assertStringsPresent(runner.golden.PDF.RequiredText) {
		return io.ErrUnexpectedEOF
	}
	semantic := strings.Join(runner.golden.PDF.RequiredText, "|")
	pdf := []byte("%PDF-1.7\n% deterministic regression\n1 0 obj<</Type/Catalog>>endobj\n% " + semantic + "\n%%EOF\n")
	return os.WriteFile(argumentValue(args, "--print-to-pdf="), pdf, 0o600)
}

var regressionPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54,
	0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}
