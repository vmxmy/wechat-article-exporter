package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

func TestRenderTextProducesUTF8WithOptionalMetadataHeader(t *testing.T) {
	published := time.Date(2026, 7, 22, 3, 4, 5, 0, time.FixedZone("CST", 8*60*60))
	article := processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion,
		Identity:      processor.Identity{MessageID: "message-7", AppMessage: "aid-9"},
		Title:         "中文标题",
		Author:        "作者",
		Account:       processor.Account{Nickname: "公众号"},
		CanonicalURL:  "https://mp.weixin.qq.com/s/example",
		Content:       "<p>正文内容</p>",
		Timestamps:    processor.Timestamps{PublishedAt: &published},
	}

	data, err := RenderText(domain.ArticleID("article-text"), article, TextOptions{IncludeMetadataHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) || strings.HasPrefix(string(data), "\ufeff") {
		t.Fatalf("text is not plain UTF-8: %q", data)
	}
	wantPrefix := "Schema-Version: wechat-article.text/v1\nArticle-ID: article-text\nTitle: 中文标题\nAccount: 公众号\nAuthor: 作者\nPublished-At: 2026-07-21T19:04:05Z\nCanonical-URL: https://mp.weixin.qq.com/s/example\nMessage-ID: message-7\nApp-Message-ID: aid-9\n---\n"
	if !strings.HasPrefix(string(data), wantPrefix) || !strings.Contains(string(data), "正文内容") {
		t.Fatalf("text output =\n%s", data)
	}

	withoutHeader, err := RenderText("article-text", article, TextOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withoutHeader), "Schema-Version:") {
		t.Fatalf("unexpected metadata header:\n%s", withoutHeader)
	}
}

func TestExportTextFileUsesAtomicOutputAndComments(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	output, err := ExportTextFile(context.Background(), manager, "article.txt", "article-text", processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion,
		Title:         "Text",
		Account:       processor.Account{Nickname: "Fixture"},
		Content:       "<p>Body</p>",
	}, TextOptions{IncludeComments: true, Comments: []processor.Comment{{Author: "Reader", Content: "Comment"}}}, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "article.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if output.SHA256 == "" || !strings.Contains(string(data), "Reader") || !strings.Contains(string(data), "Comment") {
		t.Fatalf("text export = %#v\n%s", output, data)
	}
}
