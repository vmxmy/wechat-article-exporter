package exporter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

func TestMarshalJSONExportMetadataOnlyOmitsOptionalSections(t *testing.T) {
	document, data, err := MarshalJSONExport(JSONExportInput{
		ArticleID: "article-json",
		Article: processor.Article{
			SchemaVersion: processor.NormalizedArticleSchemaVersion,
			Identity:      processor.Identity{MessageID: "message-1"},
			Title:         "Metadata only",
			Account:       processor.Account{Nickname: "Fixture"},
			CanonicalURL:  "https://mp.weixin.qq.com/s/example",
			Content:       "<p>secret body</p>",
			Albums:        []processor.Album{{ID: "album-1", Title: "Album"}},
			Engagement:    processor.Engagement{Reads: int64Pointer(12)},
		},
		Comments:   []processor.Comment{{Author: "Reader", Content: "secret comment"}},
		ExportedAt: time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	}, JSONOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != JSONExportSchemaVersion || document.Article.ID != domain.ArticleID("article-json") {
		t.Fatalf("document identity = %#v", document)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	article := raw["article"].(map[string]any)
	for _, omitted := range []string{"content", "metrics", "comments", "albums", "provenance"} {
		if _, exists := article[omitted]; exists {
			t.Fatalf("metadata-only JSON contains %q: %s", omitted, data)
		}
	}
	if article["title"] != "Metadata only" || article["canonicalUrl"] != "https://mp.weixin.qq.com/s/example" {
		t.Fatalf("metadata-only identity fields = %#v", article)
	}
}

func TestMarshalJSONExportIncludesOptionalContentMetricsCommentsRepliesAlbumsAndProvenance(t *testing.T) {
	selection := SelectionManifest{
		SchemaVersion: SelectionManifestVersion,
		ID:            "selection-a",
		DigestSHA256:  sha256Hex("selection"),
		Kind:          domain.ExportSelectionExplicitIDs,
		ArticleIDs:    []domain.ArticleID{"article-json"},
		Format:        "json",
		CreatedAt:     time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	}
	provenance := ProvenanceManifest{
		SchemaVersion: ProvenanceManifestVersion, ApplicationVersion: "v1", ExportID: "export-json", Format: "json",
		Status: ExportCompleted, Selection: selection,
		Sources: []SourceArticle{{ArticleID: "article-json", SHA256: sha256Hex("source")}},
		Outputs: []OutputFile{}, StartedAt: time.Date(2026, 7, 22, 0, 0, 1, 0, time.UTC),
		CompletedAt: time.Date(2026, 7, 22, 0, 0, 2, 0, time.UTC),
	}
	input := JSONExportInput{
		ArticleID: "article-json",
		Article: processor.Article{
			SchemaVersion: processor.NormalizedArticleSchemaVersion,
			Title:         "Complete",
			Account:       processor.Account{Nickname: "Fixture"},
			Content:       "<p>Body <strong>bold</strong></p>",
			Albums:        []processor.Album{{ID: "album-1", Title: "Album"}},
			Engagement:    processor.Engagement{Reads: int64Pointer(12), Comments: int64Pointer(1)},
		},
		Comments: []processor.Comment{{ID: "comment-1", Author: "Reader", Content: "Comment",
			Replies: []processor.Reply{{ID: "reply-1", Author: "Author", Content: "Reply"}}}},
		Provenance: &provenance,
		ExportedAt: time.Date(2026, 7, 22, 0, 0, 3, 0, time.UTC),
	}
	document, _, err := MarshalJSONExport(input, JSONOptions{
		IncludeContent: true, IncludeMetrics: true, IncludeComments: true, IncludeReplies: true,
		IncludeAlbums: true, IncludeProvenance: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if document.Article.Content == nil || document.Article.Content.HTML == "" || document.Article.Content.Text == "" || document.Article.Content.Markdown == "" {
		t.Fatalf("content = %#v", document.Article.Content)
	}
	if document.Article.Metrics == nil || document.Article.Metrics.Reads == nil || *document.Article.Metrics.Reads != 12 {
		t.Fatalf("metrics = %#v", document.Article.Metrics)
	}
	if len(document.Article.Comments) != 1 || len(document.Article.Comments[0].Replies) != 1 {
		t.Fatalf("comments = %#v", document.Article.Comments)
	}
	if len(document.Article.Albums) != 1 || document.Article.Provenance == nil || document.Article.Provenance.ExportID != "export-json" {
		t.Fatalf("albums/provenance = %#v / %#v", document.Article.Albums, document.Article.Provenance)
	}

	withoutReplies, _, err := MarshalJSONExport(input, JSONOptions{IncludeComments: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutReplies.Article.Comments) != 1 || len(withoutReplies.Article.Comments[0].Replies) != 0 {
		t.Fatalf("replies were not omitted: %#v", withoutReplies.Article.Comments)
	}
}

func TestExportJSONFileIsDeterministicForExplicitTimestamp(t *testing.T) {
	input := JSONExportInput{
		ArticleID: "article-json",
		Article: processor.Article{SchemaVersion: processor.NormalizedArticleSchemaVersion,
			Title: "JSON", Account: processor.Account{Nickname: "Fixture"}, Content: "<p>Body</p>"},
		ExportedAt: time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	}
	options := JSONOptions{IncludeContent: true}
	firstRoot := t.TempDir()
	firstManager, err := NewOutputManager(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	first, err := ExportJSONFile(context.Background(), firstManager, "article.json", input, options, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot := t.TempDir()
	secondManager, err := NewOutputManager(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportJSONFile(context.Background(), secondManager, "article.json", input, options, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("deterministic outputs differ: %#v != %#v", first, second)
	}
	firstData, err := os.ReadFile(filepath.Join(firstRoot, "article.json"))
	if err != nil {
		t.Fatal(err)
	}
	secondData, err := os.ReadFile(filepath.Join(secondRoot, "article.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstData, secondData) {
		t.Fatal("deterministic JSON bytes differ")
	}
}

func int64Pointer(value int64) *int64 { return &value }
