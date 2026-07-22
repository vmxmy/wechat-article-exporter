package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

func TestRenderMarkdownFrontMatterAndDefaultHTMLPolicy(t *testing.T) {
	published := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	article := processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion,
		Title:         "Title: \"quoted\"\nnext",
		Author:        "Author",
		Account:       processor.Account{Nickname: "Fixture"},
		CanonicalURL:  "https://mp.weixin.qq.com/s/example",
		Timestamps:    processor.Timestamps{PublishedAt: &published},
		Content:       `<div onclick="alert(1)"><script>alert(2)</script><p><strong>Safe</strong> body</p></div>`,
	}

	data, err := RenderMarkdown("article-md", article, MarkdownOptions{IncludeFrontMatter: true})
	if err != nil {
		t.Fatal(err)
	}
	markdown := string(data)
	if !strings.HasPrefix(markdown, "---\nschemaVersion: \"wechat-article.markdown/v1\"\narticleId: \"article-md\"\n") {
		t.Fatalf("front matter =\n%s", markdown)
	}
	for _, forbidden := range []string{"<script", "onclick=", "alert(1)", "alert(2)", "<div"} {
		if strings.Contains(strings.ToLower(markdown), forbidden) {
			t.Fatalf("default Markdown retained %q:\n%s", forbidden, markdown)
		}
	}
	if !strings.Contains(markdown, "**Safe** body") {
		t.Fatalf("semantic Markdown missing:\n%s", markdown)
	}
}

func TestRenderMarkdownAllowsOnlySanitizedEmbeddedHTML(t *testing.T) {
	article := processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion,
		Title:         "Embedded",
		Account:       processor.Account{Nickname: "Fixture"},
		Content:       `<div class="allowed" onclick="alert(1)"><p>Body</p><iframe src="https://evil.invalid"></iframe></div>`,
	}
	data, err := RenderMarkdown("article-md", article, MarkdownOptions{EmbeddedHTMLPolicy: MarkdownHTMLSanitized})
	if err != nil {
		t.Fatal(err)
	}
	markdown := string(data)
	if !strings.Contains(markdown, `<article id="js_article"`) || !strings.Contains(markdown, `class="allowed"`) || !strings.Contains(markdown, `<p>Body</p>`) {
		t.Fatalf("sanitized embedded HTML missing:\n%s", markdown)
	}
	for _, forbidden := range []string{"onclick=", "alert(1)", "iframe", "evil.invalid"} {
		if strings.Contains(strings.ToLower(markdown), forbidden) {
			t.Fatalf("sanitized Markdown retained %q:\n%s", forbidden, markdown)
		}
	}
	if _, err := RenderMarkdown("article-md", article, MarkdownOptions{EmbeddedHTMLPolicy: "unsafe_raw"}); err == nil {
		t.Fatal("unsupported unsafe HTML policy succeeded")
	}
}

func TestExportMarkdownFileWritesComments(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := ExportMarkdownFile(context.Background(), manager, "article.md", domain.ArticleID("article-md"), processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion, Title: "Markdown",
		Account: processor.Account{Nickname: "Fixture"}, Content: "<p>Body</p>",
	}, MarkdownOptions{IncludeComments: true, Comments: []processor.Comment{{Author: "Reader", Content: "Comment"}}}, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "article.md"))
	if err != nil {
		t.Fatal(err)
	}
	if output.SHA256 == "" || !strings.Contains(string(data), "## Comments") || !strings.Contains(string(data), "Reader") {
		t.Fatalf("Markdown export = %#v\n%s", output, data)
	}
}
