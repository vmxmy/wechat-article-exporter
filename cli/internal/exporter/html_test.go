package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

func TestExportHTMLArticleWritesSelfContainedResourcesAndComments(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	input := HTMLArticleInput{
		ArticleID: "article-html",
		Directory: "article-html",
		Article: processor.Article{
			SchemaVersion: processor.NormalizedArticleSchemaVersion,
			Title:         "Offline article",
			Account:       processor.Account{Nickname: "Fixture Account"},
			CanonicalURL:  "https://mp.weixin.qq.com/s/source-link-is-metadata-only",
			Content: `<style>.hero{background:url("https://cdn.example.test/background.png")}</style>
<p class="hero"><img src="https://cdn.example.test/photo.jpg" alt="Photo"></p>
<audio src="https://cdn.example.test/audio.mp3"></audio>`,
		},
		Assets: []HTMLAsset{
			{URL: "https://cdn.example.test/photo.jpg", MediaType: "image/jpeg", Data: []byte("photo")},
			{URL: "https://cdn.example.test/audio.mp3", MediaType: "audio/mpeg", Data: []byte("audio")},
			{URL: "https://cdn.example.test/background.png", MediaType: "image/png", Data: []byte("background")},
		},
		Comments: []processor.Comment{{Author: "Reader", Content: "Saved offline"}},
	}

	result, err := ExportHTMLArticle(context.Background(), manager, input, HTMLOptions{
		ResourcePolicy:  processor.ResourceRewriteStrict,
		IncludeComments: true,
	}, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MissingResources) != 0 || len(result.Outputs) != 4 {
		t.Fatalf("result = %#v", result)
	}
	htmlBytes, err := os.ReadFile(filepath.Join(root, "article-html", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	for _, remote := range []string{"https://cdn.example.test/photo.jpg", "https://cdn.example.test/audio.mp3", "https://cdn.example.test/background.png"} {
		if strings.Contains(html, remote) {
			t.Fatalf("HTML retained remote resource %q:\n%s", remote, html)
		}
	}
	for _, required := range []string{"./assets/", "Reader", "Saved offline", `id="js_article"`, `id="js_content"`} {
		if !strings.Contains(html, required) {
			t.Fatalf("HTML missing %q:\n%s", required, html)
		}
	}
	for _, output := range result.Outputs {
		if output.SHA256 == "" || output.Size == 0 {
			t.Fatalf("output lacks checksum: %#v", output)
		}
	}
}

func TestExportHTMLArticleStrictAllowsRemoteMetadataLinkButNotRemoteResources(t *testing.T) {
	manager, err := NewOutputManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportHTMLArticle(context.Background(), manager, HTMLArticleInput{
		ArticleID: "article-metadata", Directory: "article-metadata",
		Article: processor.Article{SchemaVersion: processor.NormalizedArticleSchemaVersion, Title: "Metadata",
			Account: processor.Account{Nickname: "Fixture"}, CanonicalURL: "https://mp.weixin.qq.com/s/example", Content: "<p>Body</p>"},
	}, HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict}, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
}

func TestExportHTMLArticleStrictMissingResourcePublishesNothing(t *testing.T) {
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	input := HTMLArticleInput{
		ArticleID: "article-missing",
		Directory: "article-missing",
		Article: processor.Article{
			SchemaVersion: processor.NormalizedArticleSchemaVersion,
			Title:         "Missing",
			Account:       processor.Account{Nickname: "Fixture"},
			Content:       `<img src="https://cdn.example.test/missing.jpg">`,
		},
	}

	_, err = ExportHTMLArticle(context.Background(), manager, input,
		HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict}, CollisionFail)
	var missing *processor.ResourceRewriteError
	if err == nil || !errors.As(err, &missing) || len(missing.Missing) != 1 {
		t.Fatalf("strict error = %#v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "article-missing", "index.html")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("strict export published index.html: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("strict export published paths: %#v", entries)
	}
}

func TestExportHTMLArticleRejectsEmptyLocalAssetData(t *testing.T) {
	manager, err := NewOutputManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportHTMLArticle(context.Background(), manager, HTMLArticleInput{
		ArticleID: "article-empty", Directory: "article-empty",
		Article: processor.Article{SchemaVersion: processor.NormalizedArticleSchemaVersion, Title: "Empty",
			Account: processor.Account{Nickname: "Fixture"}, Content: `<img src="https://cdn.example.test/empty.jpg">`},
		Assets: []HTMLAsset{{URL: "https://cdn.example.test/empty.jpg", MediaType: "image/jpeg"}},
	}, HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict}, CollisionFail)
	if err == nil || !strings.Contains(err.Error(), "no local data") {
		t.Fatalf("empty asset error = %v", err)
	}
}

func TestExportHTMLArticleBestEffortReportsMissingResources(t *testing.T) {
	manager, err := NewOutputManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExportHTMLArticle(context.Background(), manager, HTMLArticleInput{
		ArticleID: "article-best-effort",
		Directory: "article-best-effort",
		Article: processor.Article{SchemaVersion: processor.NormalizedArticleSchemaVersion, Title: "Best effort",
			Account: processor.Account{Nickname: "Fixture"}, Content: `<img src="https://cdn.example.test/missing.jpg">`},
	}, HTMLOptions{ResourcePolicy: processor.ResourceRewriteBestEffort}, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MissingResources) != 1 || len(result.Warnings) != 1 || result.Outputs[len(result.Outputs)-1].Path != "article-best-effort/index.html" {
		t.Fatalf("best-effort result = %#v", result)
	}
}

func TestExportHTMLBatchArchiveIsDeterministicAndPortable(t *testing.T) {
	article := processor.Article{
		SchemaVersion: processor.NormalizedArticleSchemaVersion,
		Title:         "Archive",
		Account:       processor.Account{Nickname: "Fixture"},
		Content:       `<img src="https://cdn.example.test/photo.jpg">`,
	}
	inputs := []HTMLArticleInput{
		{ArticleID: "article-b", Directory: "02-second", Article: article,
			Assets: []HTMLAsset{{URL: "https://cdn.example.test/photo.jpg", MediaType: "image/jpeg", Data: []byte("photo")}}},
		{ArticleID: "article-a", Directory: "01-first", Article: article,
			Assets: []HTMLAsset{{URL: "https://cdn.example.test/photo.jpg", MediaType: "image/jpeg", Data: []byte("photo")}}},
	}
	options := HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict}
	firstRoot := t.TempDir()
	firstManager, err := NewOutputManager(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportHTMLBatchArchive(context.Background(), firstManager, "batch.zip", inputs, options, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot := t.TempDir()
	secondManager, err := NewOutputManager(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportHTMLBatchArchive(context.Background(), secondManager, "batch.zip", inputs, options, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if first.Output.SHA256 != second.Output.SHA256 {
		t.Fatalf("archive checksum changed: %s != %s", first.Output.SHA256, second.Output.SHA256)
	}
	firstBytes, err := os.ReadFile(filepath.Join(firstRoot, "batch.zip"))
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(secondRoot, "batch.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("deterministic batch archives differ")
	}
	reader, err := zip.NewReader(bytes.NewReader(firstBytes), int64(len(firstBytes)))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
		if strings.Contains(file.Name, `\`) || strings.HasPrefix(file.Name, "/") || strings.Contains(file.Name, "../") {
			t.Fatalf("unsafe ZIP path %q", file.Name)
		}
		if strings.HasSuffix(file.Name, "index.html") {
			stream, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(stream)
			stream.Close()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "https://cdn.example.test/photo.jpg") {
				t.Fatalf("archive HTML retained remote resource: %s", data)
			}
		}
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(names, sorted) {
		t.Fatalf("ZIP entries are not sorted: %#v", names)
	}
	if len(first.Articles) != 2 || first.Articles[0].ArticleID != domain.ArticleID("article-a") {
		t.Fatalf("batch result = %#v", first)
	}
}

func TestExportHTMLBatchArchiveRejectsTraversal(t *testing.T) {
	manager, err := NewOutputManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportHTMLBatchArchive(context.Background(), manager, "batch.zip", []HTMLArticleInput{{
		ArticleID: "article-a", Directory: "../escape",
		Article: processor.Article{SchemaVersion: processor.NormalizedArticleSchemaVersion,
			Title: "Unsafe", Account: processor.Account{Nickname: "Fixture"}, Content: "<p>body</p>"},
	}}, HTMLOptions{}, CollisionFail)
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("traversal error = %v", err)
	}
}
