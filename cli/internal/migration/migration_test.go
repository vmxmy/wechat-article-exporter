package migration

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

func TestPlanSupportsRepresentativeDexieVersions(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		t.Run("v"+string(rune('0'+version)), func(t *testing.T) {
			archive := buildFixtureArchive(t, filepath.Join("testdata", "dexie-v"+string(rune('0'+version))), version, nil)
			plan, err := Plan(context.Background(), archive, PlanOptions{})
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if plan.Manifest.Source.DexieVersion != version {
				t.Fatalf("Dexie version = %d, want %d", plan.Manifest.Source.DexieVersion, version)
			}
			if countKind(plan.Records, RecordAccount) == 0 || countKind(plan.Records, RecordArticle) == 0 {
				t.Fatalf("planned records = %#v", plan.Records)
			}
			if version == 1 {
				for _, record := range plan.Records {
					if record.Kind == RecordHTML && record.Data.HTML.FakeID != "account-a" {
						t.Fatalf("v1 HTML fakeid = %q, want URL-derived account-a", record.Data.HTML.FakeID)
					}
				}
			}
		})
	}
}

func TestPlanSupportsWebExporterArchiveShape(t *testing.T) {
	archive := buildWebExporterArchive(t)
	plan, err := Plan(context.Background(), archive, PlanOptions{})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Manifest.Source.SchemaVersion() != 3 {
		t.Fatalf("Dexie version = %d, want 3", plan.Manifest.Source.SchemaVersion())
	}
	if countKind(plan.Records, RecordHTML) != 1 || countKind(plan.Records, RecordResource) != 2 {
		t.Fatalf("planned counts = %#v", plan.Report.Counts)
	}
	if len(plan.Objects) != 3 {
		t.Fatalf("objects = %d, want 3", len(plan.Objects))
	}
	if plan.Report.MissingResources != 1 {
		t.Fatalf("missing resources = %d, want 1", plan.Report.MissingResources)
	}
	html := firstKind(t, plan.Records, RecordHTML)
	if html.Data.HTML.ObjectDigest == "" || html.Data.HTML.MediaType != "text/html" {
		t.Fatalf("HTML = %#v", html.Data.HTML)
	}
}

func TestPlanCoversLegacyArchiveDataAndReconcilesDuplicates(t *testing.T) {
	archive := buildFixtureArchive(t, filepath.Join("testdata", "dexie-v3"), 3, func(files map[string][]byte) {
		var envelope map[string]any
		if err := json.Unmarshal(files["records/articles.json"], &envelope); err != nil {
			t.Fatal(err)
		}
		records := envelope["records"].([]any)
		envelope["records"] = append(records, records[0])
		files["records/articles.json"], _ = json.Marshal(envelope)
	})
	plan, err := Plan(context.Background(), archive, PlanOptions{})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	wantKinds := []RecordKind{RecordAccount, RecordArticle, RecordHTML, RecordMetric, RecordComment, RecordReply, RecordResourceMap, RecordResource}
	for _, kind := range wantKinds {
		if countKind(plan.Records, kind) == 0 {
			t.Errorf("missing planned kind %q", kind)
		}
	}
	if plan.Report.ArchiveDuplicates != 1 {
		t.Fatalf("archive duplicates = %d, want 1", plan.Report.ArchiveDuplicates)
	}
	if len(plan.Objects) != 2 {
		t.Fatalf("objects = %d, want 2 unique objects", len(plan.Objects))
	}
}

func TestPlanAllowsPartialArchiveAndReportsMissingResources(t *testing.T) {
	archive := buildFixtureArchive(t, filepath.Join("testdata", "partial"), 2, nil)
	plan, err := Plan(context.Background(), archive, PlanOptions{})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Report.MissingResources != 1 {
		t.Fatalf("missing resources = %d, want 1", plan.Report.MissingResources)
	}
	if len(plan.Report.Warnings) == 0 {
		t.Fatal("expected partial archive warning")
	}
}

func TestImportDeduplicatesObjectsAndPreservesLocallyNewerRecords(t *testing.T) {
	archive := buildFixtureArchive(t, filepath.Join("testdata", "dexie-v3"), 3, nil)
	plan, err := Plan(context.Background(), archive, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	article := firstKind(t, plan.Records, RecordArticle)
	resourceObject := plan.Objects[0]
	target := &fakeTarget{state: TargetState{
		Records: map[RecordIdentity]LocalRecord{
			article.Identity(): {UpdatedAt: article.UpdatedAt.Add(time.Hour), Fingerprint: "local-newer"},
		},
		Objects: map[string]LocalObject{
			resourceObject.Digest: {Size: resourceObject.Size},
		},
	}}
	report, err := Import(context.Background(), plan, target, ImportOptions{ConflictPolicy: PreserveNewer})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if report.PreservedLocal != 1 {
		t.Fatalf("preserved local = %d, want 1", report.PreservedLocal)
	}
	if report.ObjectsReused != 1 || report.ObjectsWritten != len(plan.Objects)-1 {
		t.Fatalf("object report = reused %d written %d", report.ObjectsReused, report.ObjectsWritten)
	}
	for _, record := range target.batch.Records {
		if record.Record.Identity() == article.Identity() {
			t.Fatal("locally newer article was included in apply batch")
		}
	}
	if target.objectBytes == 0 {
		t.Fatal("fake target did not receive staged object bytes")
	}
}

func TestLocalTargetPreserveNewerInspectsEveryIdentityShape(t *testing.T) {
	ctx := context.Background()
	archive := buildFixtureArchive(t, filepath.Join("testdata", "dexie-v3"), 3, nil)
	plan, err := Plan(ctx, archive, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	database, err := library.Open(ctx, library.OpenOptions{
		Path: filepath.Join(t.TempDir(), "library.sqlite"), ProfileID: "profile-a", ProfileName: "Profile A",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	objectStore, err := objects.NewFileStore(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	target := LocalTarget{Library: database, Objects: objectStore}

	archiveUpdated := time.Unix(1_700_000_300, 0).UTC()
	localUpdated := archiveUpdated.Add(time.Hour)
	account, err := database.ImportLegacyAccount(ctx, domain.Account{
		FakeID: "account-a", Name: "local account", LastSyncAt: localUpdated,
	})
	if err != nil {
		t.Fatal(err)
	}
	article := domain.Article{
		ID: "article-local", AccountID: account.ID, Aid: "300_1", Title: "local article",
		CanonicalURL: "https://mp.weixin.qq.com/s/v3", UpdatedAt: localUpdated,
	}
	if _, err := database.ImportLegacyArticle(ctx, article); err != nil {
		t.Fatal(err)
	}
	article, err = database.GetArticleByCanonicalURL(ctx, article.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	htmlObject, err := objectStore.Put(ctx, strings.NewReader("<article>local</article>"), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CommitContent(ctx, article.ID, htmlObject, "html", article.CanonicalURL, "valid", "comment-v3", localUpdated); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ImportLegacyMetricSnapshot(ctx, library.MetricSnapshot{
		ArticleID: article.ID, ReadCount: 999, CapturedAt: localUpdated,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CommitCommentPage(ctx, article.ID, library.CommentPageCommit{
		Comments:  []library.CommentRecord{{UpstreamID: "comment-1", Content: "local comment", ReplyTotal: 2}},
		FetchedAt: localUpdated,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CommitReplyPage(ctx, article.ID, "comment-1", library.ReplyPageCommit{
		Replies: []library.ReplyRecord{
			{UpstreamID: "1", Content: "local embedded reply"},
			{UpstreamID: "2", Content: "local full reply"},
		},
		MaxReplyID: 2, FetchedAt: localUpdated,
	}); err != nil {
		t.Fatal(err)
	}
	resourceObject, err := objectStore.Put(ctx, strings.NewReader("local resource"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CommitResource(ctx, article.ID, "https://img.example/shared.png", "legacy", 0, resourceObject); err != nil {
		t.Fatal(err)
	}

	state, err := target.Inspect(ctx, Inventory{Records: recordIdentities(plan.Records)})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range plan.Records {
		local, exists := state.Records[record.Identity()]
		if !exists {
			t.Errorf("Inspect() missed %s", record.Identity().String())
			continue
		}
		if record.Kind != RecordResource && !local.UpdatedAt.After(record.UpdatedAt) {
			t.Errorf("Inspect() %s updatedAt = %s, want after archive %s", record.Identity().String(), local.UpdatedAt, record.UpdatedAt)
		}
	}

	report, err := Import(ctx, plan, target, ImportOptions{ConflictPolicy: PreserveNewer})
	if err != nil {
		t.Fatal(err)
	}
	if report.PreservedLocal != len(plan.Records)-1 {
		t.Fatalf("preserved local = %d, want %d (resource records have no reliable source timestamp)", report.PreservedLocal, len(plan.Records)-1)
	}
	storedArticle, err := database.GetArticleByCanonicalURL(ctx, article.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	if storedArticle.Title != "local article" {
		t.Fatalf("locally newer article was replaced: %#v", storedArticle)
	}
}

func recordIdentities(records []Record) []RecordIdentity {
	identities := make([]RecordIdentity, len(records))
	for index, record := range records {
		identities[index] = record.Identity()
	}
	return identities
}

func TestImportRefusesChangedArchiveAfterPlanning(t *testing.T) {
	archive := buildFixtureArchive(t, filepath.Join("testdata", "partial"), 2, nil)
	plan, err := Plan(context.Background(), archive, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(archive, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("changed")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Import(context.Background(), plan, &fakeTarget{}, ImportOptions{})
	if !errors.Is(err, ErrArchiveChanged) {
		t.Fatalf("Import() error = %v, want ErrArchiveChanged", err)
	}
}

func TestValidateRejectsUnsafeZIPs(t *testing.T) {
	tests := []struct {
		name string
		make func(*testing.T) string
		code ProblemCode
	}{
		{name: "path traversal", code: ProblemUnsafePath, make: func(t *testing.T) string {
			return buildRawArchive(t, []rawEntry{{name: "manifest.json", body: minimalManifest(t, nil)}, {name: "../escape", body: []byte("x")}})
		}},
		{name: "duplicate entry", code: ProblemDuplicateEntry, make: func(t *testing.T) string {
			return buildRawArchive(t, []rawEntry{{name: "manifest.json", body: minimalManifest(t, nil)}, {name: "records/accounts.json", body: []byte("[]")}, {name: "records/accounts.json", body: []byte("[]")}})
		}},
		{name: "checksum mismatch", code: ProblemChecksumMismatch, make: func(t *testing.T) string {
			body := []byte(`{"schemaVersion":1,"records":[]}`)
			manifest := minimalManifest(t, []ManifestFile{{Path: "records/accounts.json", Kind: FileRecords, Dataset: DatasetAccounts, Size: int64(len(body)), SHA256: strings.Repeat("0", 64)}})
			return buildRawArchive(t, []rawEntry{{name: "manifest.json", body: manifest}, {name: "records/accounts.json", body: body}})
		}},
		{name: "oversized entry", code: ProblemEntryTooLarge, make: func(t *testing.T) string {
			body := bytes.Repeat([]byte("x"), 32)
			digest := sha256.Sum256(body)
			manifest := minimalManifest(t, []ManifestFile{{Path: "records/accounts.json", Kind: FileRecords, Dataset: DatasetAccounts, Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}})
			return buildRawArchive(t, []rawEntry{{name: "manifest.json", body: manifest}, {name: "records/accounts.json", body: body}})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			if test.code == ProblemEntryTooLarge {
				limits.MaxEntryBytes = 16
			}
			_, err := Validate(context.Background(), test.make(t), limits)
			var validation *ValidationError
			if !errors.As(err, &validation) || !validation.Has(test.code) {
				t.Fatalf("Validate() error = %v, want code %q", err, test.code)
			}
		})
	}
}

type fakeTarget struct {
	state       TargetState
	batch       ImportBatch
	objectBytes int64
}

func (target *fakeTarget) Inspect(context.Context, Inventory) (TargetState, error) {
	if target.state.Records == nil {
		target.state.Records = map[RecordIdentity]LocalRecord{}
	}
	if target.state.Objects == nil {
		target.state.Objects = map[string]LocalObject{}
	}
	return target.state, nil
}

func (target *fakeTarget) Apply(ctx context.Context, batch ImportBatch) error {
	target.batch = batch
	for _, object := range batch.Objects {
		reader, err := object.Source.Open(ctx)
		if err != nil {
			return err
		}
		count, err := io.Copy(io.Discard, reader)
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if count != object.Size {
			return errors.New("unexpected staged object size")
		}
		target.objectBytes += count
	}
	return nil
}

type rawEntry struct {
	name string
	body []byte
}

func buildRawArchive(t *testing.T, entries []rawEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		part, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func buildFixtureArchive(t *testing.T, root string, dexieVersion int, mutate func(map[string][]byte)) string {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = body
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	objectFiles := map[string][]byte{}
	for name, body := range files {
		if !strings.HasPrefix(name, "objects-src/") {
			continue
		}
		digest := sha256.Sum256(body)
		hexDigest := hex.EncodeToString(digest[:])
		token := "{{object:" + strings.TrimPrefix(name, "objects-src/") + "}}"
		for recordName, recordBody := range files {
			if strings.HasPrefix(recordName, "records/") {
				files[recordName] = bytes.ReplaceAll(recordBody, []byte(token), []byte(hexDigest))
			}
		}
		objectFiles["objects/sha256/"+hexDigest] = body
		delete(files, name)
	}
	for name, body := range objectFiles {
		files[name] = body
	}
	if mutate != nil {
		mutate(files)
	}
	manifestFiles := make([]ManifestFile, 0, len(files))
	counts := map[Dataset]int{}
	for name, body := range files {
		digest := sha256.Sum256(body)
		entry := ManifestFile{Path: name, Size: int64(len(body)), SHA256: hex.EncodeToString(digest[:])}
		if strings.HasPrefix(name, "records/") {
			entry.Kind = FileRecords
			entry.Dataset = Dataset(strings.TrimSuffix(strings.TrimPrefix(name, "records/"), ".json"))
			var envelope struct {
				Records []json.RawMessage `json:"records"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("decode fixture %s: %v", name, err)
			}
			counts[entry.Dataset] = len(envelope.Records)
		} else {
			entry.Kind = FileObject
			entry.MediaType = fixtureMediaType(name, body)
		}
		manifestFiles = append(manifestFiles, entry)
	}
	sort.Slice(manifestFiles, func(i, j int) bool { return manifestFiles[i].Path < manifestFiles[j].Path })
	manifest := Manifest{
		Format: ArchiveFormat, SchemaVersion: CurrentSchemaVersion, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		Source: SourceInfo{Database: "exporter.wxdown.online", DexieVersion: dexieVersion}, Files: manifestFiles, Counts: counts,
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	entries := []rawEntry{{name: ManifestPath, body: manifestBody}}
	for _, entry := range manifestFiles {
		entries = append(entries, rawEntry{name: entry.Path, body: files[entry.Path]})
	}
	return buildRawArchive(t, entries)
}

func buildWebExporterArchive(t *testing.T) string {
	t.Helper()
	htmlBytes := []byte("<html>web exporter</html>")
	resourceBytes := []byte{0, 1, 2, 3}
	assetBytes := []byte("stylesheet")
	type content struct {
		Path      string `json:"path"`
		Bytes     int    `json:"bytes"`
		SHA256    string `json:"sha256"`
		MediaType string `json:"mediaType"`
	}
	type legacyRecord struct {
		Key   string         `json:"key"`
		Value map[string]any `json:"value"`
	}
	digest := func(body []byte) string {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	files := map[string][]byte{
		"objects/html/00000001.bin":      htmlBytes,
		"objects/resources/00000001.bin": resourceBytes,
		"objects/assets/00000001.bin":    assetBytes,
	}
	records := map[string]any{
		"records/accounts.json":      []legacyRecord{{Key: "account-a", Value: map[string]any{"fakeid": "account-a", "nickname": "Web"}}},
		"records/articles.json":      []legacyRecord{{Key: "account-a:1", Value: map[string]any{"fakeid": "account-a", "aid": "1", "link": "https://mp.weixin.qq.com/s/web", "title": "Web"}}},
		"records/html.json":          []legacyRecord{{Key: "https://mp.weixin.qq.com/s/web", Value: map[string]any{"fakeid": "account-a", "url": "https://mp.weixin.qq.com/s/web", "content": content{Path: "objects/html/00000001.bin", Bytes: len(htmlBytes), SHA256: digest(htmlBytes), MediaType: "text/html"}}}},
		"records/metadata.json":      []legacyRecord{{Key: "https://mp.weixin.qq.com/s/web", Value: map[string]any{"fakeid": "account-a", "url": "https://mp.weixin.qq.com/s/web", "readNum": 1}}},
		"records/comments.json":      []legacyRecord{},
		"records/replies.json":       []legacyRecord{},
		"records/resource-maps.json": []legacyRecord{{Key: "https://mp.weixin.qq.com/s/web", Value: map[string]any{"fakeid": "account-a", "url": "https://mp.weixin.qq.com/s/web", "resources": []string{"https://cdn.example/resource", "https://cdn.example/missing"}}}},
		"records/resources.json":     []legacyRecord{{Key: "https://cdn.example/resource", Value: map[string]any{"fakeid": "account-a", "url": "https://cdn.example/resource", "content": content{Path: "objects/resources/00000001.bin", Bytes: len(resourceBytes), SHA256: digest(resourceBytes), MediaType: "application/octet-stream"}}}},
		"records/assets.json":        []legacyRecord{{Key: "https://cdn.example/style.css", Value: map[string]any{"fakeid": "account-a", "url": "https://cdn.example/style.css", "content": content{Path: "objects/assets/00000001.bin", Bytes: len(assetBytes), SHA256: digest(assetBytes), MediaType: "text/css"}}}},
	}
	logical := []struct {
		name   string
		source string
		path   string
	}{
		{"accounts", "info", "records/accounts.json"}, {"articles", "article", "records/articles.json"},
		{"html", "html", "records/html.json"}, {"metadata", "metadata", "records/metadata.json"},
		{"comments", "comment", "records/comments.json"}, {"replies", "comment_reply", "records/replies.json"},
		{"resourceMaps", "resource-map", "records/resource-maps.json"}, {"resources", "resource", "records/resources.json"},
		{"assets", "asset", "records/assets.json"},
	}
	tables := map[string]TableManifest{}
	counts := map[Dataset]int{}
	for _, table := range logical {
		body, err := json.Marshal(records[table.path])
		if err != nil {
			t.Fatal(err)
		}
		files[table.path] = body
		count := len(records[table.path].([]legacyRecord))
		tables[table.name] = TableManifest{SourceTable: table.source, Path: table.path, Records: count}
		if dataset, ok := datasetForLogicalTable(table.name); ok {
			counts[dataset] = count
		}
	}
	manifest := Manifest{Format: ArchiveFormat, SchemaVersion: 1, CreatedAt: time.Unix(1_700_000_000, 0).UTC(), Status: "partial",
		Source: SourceInfo{Application: "wechat-article-exporter-web", DexieDatabase: "exporter.wxdown.online", DexieSchemaVersion: 3},
		Counts: counts, Tables: tables, MissingResources: []MissingResource{{ArticleURL: "https://mp.weixin.qq.com/s/web", ResourceURL: "https://cdn.example/missing", Reason: "missing-resource-record"}}, ChecksumFile: "checksums.json"}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files[ManifestPath] = manifestBody
	checksums := checksumFile{Algorithm: "sha256", Scope: "all archive files except checksums.json"}
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		body := files[name]
		checksums.Files = append(checksums.Files, checksumEntry{Path: name, Bytes: int64(len(body)), SHA256: digest(body)})
	}
	checksumsBody, err := json.Marshal(checksums)
	if err != nil {
		t.Fatal(err)
	}
	files["checksums.json"] = checksumsBody
	entries := make([]rawEntry, 0, len(files))
	for name, body := range files {
		entries = append(entries, rawEntry{name: name, body: body})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return buildRawArchive(t, entries)
}

func minimalManifest(t *testing.T, files []ManifestFile) []byte {
	t.Helper()
	body, err := json.Marshal(Manifest{Format: ArchiveFormat, SchemaVersion: CurrentSchemaVersion,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(), Source: SourceInfo{Database: "exporter.wxdown.online", DexieVersion: 3}, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func countKind(records []Record, kind RecordKind) int {
	count := 0
	for _, record := range records {
		if record.Kind == kind {
			count++
		}
	}
	return count
}

func firstKind(t *testing.T, records []Record, kind RecordKind) Record {
	t.Helper()
	for _, record := range records {
		if record.Kind == kind {
			return record
		}
	}
	t.Fatalf("record kind %q not found", kind)
	return Record{}
}

func fixtureMediaType(name string, body []byte) string {
	if strings.Contains(name, "html") || bytes.HasPrefix(bytes.TrimSpace(body), []byte("<")) {
		return "text/html"
	}
	return "application/octet-stream"
}
