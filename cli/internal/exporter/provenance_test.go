package exporter

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestWarningCollectorAggregatesAndSortsDeterministically(t *testing.T) {
	collector := NewWarningCollector()
	collector.Add("missing_resource", "image unavailable", "article-b")
	collector.Add("render_fallback", "unsupported element", "article-c")
	collector.Add("missing_resource", "image unavailable", "article-a")
	collector.Add("missing_resource", "image unavailable", "article-b")
	warnings := collector.Warnings()
	want := []Warning{
		{Code: "missing_resource", Message: "image unavailable", ArticleIDs: []domain.ArticleID{"article-a", "article-b"}},
		{Code: "render_fallback", Message: "unsupported element", ArticleIDs: []domain.ArticleID{"article-c"}},
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings = %#v, want %#v", warnings, want)
	}
}

func TestWriteAndVerifyProvenanceDetectsChangedAndMissingOutputs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.WriteFile(ctx, "first.txt", CollisionFail, writeString("first"))
	if err != nil {
		t.Fatal(err)
	}
	first.ArticleID = "article-a"
	second, err := manager.WriteFile(ctx, "second.txt", CollisionFail, writeString("second"))
	if err != nil {
		t.Fatal(err)
	}
	second.ArticleID = "article-b"
	selection, err := BuildSelectionManifest(ctx, nil, domain.ExportRequest{Format: "text",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-a", "article-b"}}}, time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewProvenanceBuilder("v1.2.3", "export-1", "text", selection,
		time.Date(2026, 7, 22, 1, 0, 1, 0, time.UTC))
	if err := builder.AddSource(SourceArticle{ArticleID: "article-a", SHA256: sha256Hex("source-a")}); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddSource(SourceArticle{ArticleID: "article-b", SHA256: sha256Hex("source-b")}); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddOutput(first); err != nil {
		t.Fatal(err)
	}
	if err := builder.AddOutput(second); err != nil {
		t.Fatal(err)
	}
	builder.AddMissingResource(MissingResource{ArticleID: "article-b", Resource: "cover.jpg", Reason: "not downloaded"})
	builder.Warn("missing_resource", "cover image unavailable", "article-b")
	manifest, err := builder.Complete(ExportCompleted, time.Date(2026, 7, 22, 1, 0, 2, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	manifestOutput, err := WriteProvenanceManifest(ctx, manager, "export-manifest.json", manifest, CollisionFail)
	if err != nil {
		t.Fatal(err)
	}
	if manifestOutput.SHA256 == "" || manifestOutput.Size == 0 {
		t.Fatalf("manifest output = %#v", manifestOutput)
	}

	verified, err := VerifyProvenanceManifest(ctx, root, "export-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Valid || verified.VerifiedOutputs != 2 || len(verified.Issues) != 0 {
		t.Fatalf("verification = %#v", verified)
	}

	if err := os.WriteFile(filepath.Join(root, "first.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "second.txt")); err != nil {
		t.Fatal(err)
	}
	changed, err := VerifyProvenanceManifest(ctx, root, "export-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if changed.Valid || !reflect.DeepEqual(changed.AffectedArticleIDs, []domain.ArticleID{"article-a", "article-b"}) {
		t.Fatalf("changed verification = %#v", changed)
	}
	if len(changed.Issues) != 3 {
		t.Fatalf("verification issues = %#v", changed.Issues)
	}
}

func TestProvenanceRejectsOutputArticlesOutsideSelection(t *testing.T) {
	selection, err := BuildSelectionManifest(context.Background(), nil, domain.ExportRequest{Format: "text",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-a"}}}, time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	builder := NewProvenanceBuilder("v1", "export-a", "text", selection, time.Now())
	if err := builder.AddOutput(OutputFile{ArticleID: "article-x", Path: "x.txt", Size: 1, SHA256: sha256Hex("x"), Status: OutputWritten}); err == nil {
		t.Fatal("builder accepted an output outside the selection")
	}
	manifest := ProvenanceManifest{
		SchemaVersion: ProvenanceManifestVersion, ApplicationVersion: "v1", ExportID: "export-a", Format: "text",
		Status: ExportCompleted, Selection: selection,
		Sources:   []SourceArticle{{ArticleID: "article-a", SHA256: sha256Hex("source")}},
		Outputs:   []OutputFile{{ArticleIDs: []domain.ArticleID{"article-a", "article-x"}, Path: "x.txt", Size: 1, SHA256: sha256Hex("x"), Status: OutputWritten}},
		StartedAt: time.Now().Add(-time.Second), CompletedAt: time.Now(),
	}
	if err := validateProvenanceManifest(manifest); err == nil {
		t.Fatal("manifest accepted an output outside the selection")
	}
}

func TestVerifyProvenanceRejectsUnsafeManifestOutputPath(t *testing.T) {
	root := t.TempDir()
	started := time.Unix(1_700_000_000, 0).UTC()
	selection, err := BuildSelectionManifest(context.Background(), nil, domain.ExportRequest{Format: "text",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-a"}}}, started.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	manifest := ProvenanceManifest{
		SchemaVersion:      ProvenanceManifestVersion,
		ApplicationVersion: "test",
		ExportID:           "export-unsafe",
		Format:             "text",
		Status:             ExportCompleted,
		Selection:          selection,
		Sources:            []SourceArticle{{ArticleID: "article-a", SHA256: sha256Hex("source")}},
		Outputs:            []OutputFile{{ArticleID: "article-a", Path: "../outside.txt", Size: 1, SHA256: sha256Hex("x"), Status: OutputWritten}},
		StartedAt:          started, CompletedAt: started.Add(time.Second),
	}
	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyProvenanceManifest(context.Background(), root, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || len(report.Issues) != 2 || report.Issues[0].Kind != VerificationUnsafePath ||
		report.Issues[1].Kind != VerificationInvalidManifest ||
		!reflect.DeepEqual(report.AffectedArticleIDs, []domain.ArticleID{"article-a"}) {
		t.Fatalf("verification = %#v", report)
	}
}

func TestVerifyUnsafeBatchPathReportsEveryAffectedArticle(t *testing.T) {
	root := t.TempDir()
	started := time.Unix(1_700_000_000, 0).UTC()
	selection, err := BuildSelectionManifest(context.Background(), nil, domain.ExportRequest{Format: "html",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-a", "article-b"}}}, started.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	manifest := ProvenanceManifest{
		SchemaVersion: ProvenanceManifestVersion, ApplicationVersion: "test", ExportID: "export-unsafe-batch", Format: "html",
		Status: ExportCompleted, Selection: selection,
		Sources: []SourceArticle{{ArticleID: "article-a", SHA256: sha256Hex("source-a")}, {ArticleID: "article-b", SHA256: sha256Hex("source-b")}},
		Outputs: []OutputFile{{ArticleIDs: []domain.ArticleID{"article-a", "article-b"}, Path: "../batch.zip",
			Size: 1, SHA256: sha256Hex("batch"), Status: OutputWritten}},
		StartedAt: started, CompletedAt: started.Add(time.Second),
	}
	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyProvenanceManifest(context.Background(), root, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !reflect.DeepEqual(report.AffectedArticleIDs, []domain.ArticleID{"article-a", "article-b"}) {
		t.Fatalf("unsafe batch verification=%#v", report)
	}
}

func TestVerifyBatchOutputReportsEveryAffectedArticle(t *testing.T) {
	root := t.TempDir()
	started := time.Unix(1_700_000_000, 0).UTC()
	selection, err := BuildSelectionManifest(context.Background(), nil, domain.ExportRequest{Format: "html",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-a", "article-b"}}}, started.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	manifest := ProvenanceManifest{
		SchemaVersion:      ProvenanceManifestVersion,
		ApplicationVersion: "test",
		ExportID:           "export-batch",
		Format:             "html",
		Status:             ExportCompleted,
		Selection:          selection,
		Sources: []SourceArticle{
			{ArticleID: "article-a", SHA256: sha256Hex("source-a")},
			{ArticleID: "article-b", SHA256: sha256Hex("source-b")},
		},
		Outputs: []OutputFile{{ArticleIDs: []domain.ArticleID{"article-a", "article-b"}, Path: "batch.zip",
			Size: 3, SHA256: sha256Hex("zip"), Status: OutputWritten}},
		StartedAt: started, CompletedAt: started.Add(time.Second),
	}
	data, err := marshalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "batch.zip"), []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := VerifyProvenanceManifest(context.Background(), root, "manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !reflect.DeepEqual(report.AffectedArticleIDs, []domain.ArticleID{"article-a", "article-b"}) {
		t.Fatalf("batch verification=%#v", report)
	}
}
