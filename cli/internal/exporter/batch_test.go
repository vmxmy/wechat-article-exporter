package exporter

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

func TestLargeBatchXLSXMemoryAndThroughput(t *testing.T) {
	const rowCount = 25_000
	source := &generatedBatchXLSXSource{remaining: rowCount, content: strings.Repeat("bounded article body ", 24)}

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	var peakHeap atomic.Uint64
	peakHeap.Store(baseline.HeapAlloc)
	stopSampling := make(chan struct{})
	samplingStopped := make(chan struct{})
	go sampleBatchHeap(stopSampling, samplingStopped, &peakHeap)

	started := time.Now()
	report, err := WriteXLSX(context.Background(), io.Discard, source, XLSXOptions{
		IncludeContent: true,
		SheetName:      "Large batch",
	})
	elapsed := time.Since(started)
	close(stopSampling)
	<-samplingStopped
	var final runtime.MemStats
	runtime.ReadMemStats(&final)
	storeBatchPeak(&peakHeap, final.HeapAlloc)
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows != rowCount || report.Columns != len(XLSXColumns(true)) || source.calls != rowCount+1 {
		t.Fatalf("large XLSX report=%#v source calls=%d", report, source.calls)
	}
	throughput := float64(rowCount) / elapsed.Seconds()
	if throughput < 100 {
		t.Fatalf("large XLSX throughput %.1f rows/s is below regression floor (elapsed %s)", throughput, elapsed)
	}
	heapGrowth := uint64(0)
	if peakHeap.Load() > baseline.HeapAlloc {
		heapGrowth = peakHeap.Load() - baseline.HeapAlloc
	}
	if heapGrowth > 128<<20 {
		t.Fatalf("large XLSX peak heap grew %d MiB; expected bounded streaming memory", heapGrowth>>20)
	}
	t.Logf("streamed %d XLSX rows at %.0f rows/s with %d MiB peak heap growth", rowCount, throughput, heapGrowth>>20)
}

func TestLargeBatchCollisionPlanningIsDeterministic(t *testing.T) {
	const articleCount = 1_024
	items := make([]NamingData, articleCount)
	for index := range items {
		title := []string{"Same report", "same report", "SAME REPORT"}[index%3]
		items[index] = NamingData{ArticleID: domain.ArticleID(fmt.Sprintf("article-%04d", index)), Title: title}
	}
	options := NamingOptions{
		Template: "${title}", Extension: ".html", MaximumBytes: 48, Platform: PlatformWindows,
	}
	forward, err := PlanFilenames(options, items)
	if err != nil {
		t.Fatal(err)
	}
	reversedItems := make([]NamingData, len(items))
	for index := range items {
		reversedItems[index] = items[len(items)-1-index]
	}
	reversed, err := PlanFilenames(options, reversedItems)
	if err != nil {
		t.Fatal(err)
	}

	forwardByID := batchPlansByID(forward)
	reversedByID := batchPlansByID(reversed)
	if len(forwardByID) != articleCount || len(reversedByID) != articleCount {
		t.Fatalf("planned names forward=%d reversed=%d", len(forwardByID), len(reversedByID))
	}
	seen := make(map[string]domain.ArticleID, articleCount)
	for articleID, path := range forwardByID {
		if reversedByID[articleID] != path {
			t.Fatalf("article %s changed path from %q to %q after input reorder", articleID, path, reversedByID[articleID])
		}
		key := strings.ToLower(path)
		if previous := seen[key]; previous != "" {
			t.Fatalf("Windows collision for %s and %s at %q", previous, articleID, path)
		}
		seen[key] = articleID
		if len(path) > options.MaximumBytes || strings.ContainsAny(path, `/\\`) {
			t.Fatalf("unsafe or oversized planned path %q", path)
		}
	}
}

func TestBatchInterruptionPreservesDestinationAndResumesRemaining(t *testing.T) {
	const (
		committedBeforeInterruption = 20
		totalArticles               = 60
	)
	root := t.TempDir()
	manager, err := NewOutputManager(root)
	if err != nil {
		t.Fatal(err)
	}
	committed := make(map[string]OutputFile, committedBeforeInterruption)
	for index := 0; index < committedBeforeInterruption; index++ {
		path, payload := batchOutputFixture(index)
		output, err := manager.WriteFile(context.Background(), path, CollisionFail, batchWriteString(payload))
		if err != nil {
			t.Fatal(err)
		}
		committed[path] = output
	}
	firstPath, firstPayload := batchOutputFixture(0)
	if _, err := manager.WriteFile(context.Background(), firstPath, CollisionFail, batchWriteString(firstPayload)); !errors.Is(err, ErrDestinationExists) {
		t.Fatalf("fail collision error = %v", err)
	}

	interruptedPath, interruptedPayload := batchOutputFixture(committedBeforeInterruption)
	previousDestination := []byte("previous complete destination")
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(interruptedPath)), previousDestination, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = manager.WriteFile(ctx, interruptedPath, CollisionReplace, func(writer io.Writer) error {
		if _, err := io.WriteString(writer, strings.Repeat("staged-but-uncommitted", 1_024)); err != nil {
			return err
		}
		cancel()
		_, err := io.WriteString(writer, interruptedPayload)
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted write error = %v", err)
	}
	unchanged, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(interruptedPath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(previousDestination) {
		t.Fatalf("interruption changed destination to %q", unchanged)
	}
	assertNoBatchStagingFiles(t, root)

	for index := 0; index < totalArticles; index++ {
		path, payload := batchOutputFixture(index)
		policy := CollisionFail
		wantStatus := OutputWritten
		writerCalls := 0
		switch {
		case index < committedBeforeInterruption:
			policy = CollisionSkip
			wantStatus = OutputSkipped
		case index == committedBeforeInterruption:
			policy = CollisionReplace
			wantStatus = OutputReplaced
		}
		output, err := manager.WriteFile(context.Background(), path, policy, func(writer io.Writer) error {
			writerCalls++
			_, err := io.WriteString(writer, payload)
			return err
		})
		if err != nil {
			t.Fatalf("resume article %d: %v", index, err)
		}
		if output.Status != wantStatus || output.SHA256 != digestBytes([]byte(payload)) {
			t.Fatalf("resume article %d output=%#v want status=%s", index, output, wantStatus)
		}
		if index < committedBeforeInterruption && writerCalls != 0 {
			t.Fatalf("resume rewrote committed article %d", index)
		}
		if previous, exists := committed[path]; exists && previous.SHA256 != output.SHA256 {
			t.Fatalf("resume changed committed checksum for %s", path)
		}
	}
	for index := 0; index < totalArticles; index++ {
		path, payload := batchOutputFixture(index)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != payload {
			t.Fatalf("resumed output %s = %q, want %q", path, data, payload)
		}
	}
	assertNoBatchStagingFiles(t, root)
}

func TestLargeHTMLBatchStrictAndBestEffortMissingResources(t *testing.T) {
	const articleCount = 180
	inputs := make([]HTMLArticleInput, articleCount)
	expectedMissing := make(map[domain.ArticleID]string)
	for index := range inputs {
		articleID := domain.ArticleID(fmt.Sprintf("article-%04d", index))
		resourceURL := fmt.Sprintf("https://cdn.example.test/batch/%04d.png", index)
		inputs[index] = HTMLArticleInput{
			ArticleID: articleID,
			Directory: string(articleID),
			Article: processor.Article{
				SchemaVersion: processor.NormalizedArticleSchemaVersion,
				Title:         fmt.Sprintf("Batch article %04d", index),
				Account:       processor.Account{Nickname: "Batch fixture"},
				Content:       `<p>bounded batch body</p><img src="` + resourceURL + `" alt="fixture">`,
			},
		}
		if index%9 == 4 {
			expectedMissing[articleID] = resourceURL
		} else {
			inputs[index].Assets = []HTMLAsset{{URL: resourceURL, Name: "resource", MediaType: "image/png", Data: regressionPNG}}
		}
	}

	strictRoot := t.TempDir()
	strictManager, err := NewOutputManager(strictRoot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExportHTMLBatchArchive(context.Background(), strictManager, "strict.zip", inputs,
		HTMLOptions{ResourcePolicy: processor.ResourceRewriteStrict}, CollisionFail)
	var missingError *processor.ResourceRewriteError
	if err == nil || !errors.As(err, &missingError) || len(missingError.Missing) != 1 {
		t.Fatalf("strict missing-resource error = %#v", err)
	}
	if _, err := os.Stat(filepath.Join(strictRoot, "strict.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("strict batch published an archive: %v", err)
	}
	assertNoBatchStagingFiles(t, strictRoot)

	bestEffortRoot := t.TempDir()
	bestEffortManager, err := NewOutputManager(bestEffortRoot)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := ExportHTMLBatchArchive(context.Background(), bestEffortManager, "best-effort.zip", inputs,
		HTMLOptions{ResourcePolicy: processor.ResourceRewriteBestEffort}, CollisionFail)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	throughput := float64(articleCount) / elapsed.Seconds()
	if throughput < 5 {
		t.Fatalf("HTML batch throughput %.1f articles/s is below regression floor (elapsed %s)", throughput, elapsed)
	}
	if len(result.Articles) != articleCount || len(result.Warnings) != len(expectedMissing) || result.Output.Status != OutputWritten {
		t.Fatalf("best-effort batch articles=%d warnings=%d output=%#v", len(result.Articles), len(result.Warnings), result.Output)
	}
	foundMissing := make(map[domain.ArticleID]string, len(expectedMissing))
	for _, article := range result.Articles {
		if len(article.MissingResources) == 0 {
			continue
		}
		if len(article.MissingResources) != 1 || len(article.Warnings) != 1 {
			t.Fatalf("article %s missing resources=%#v warnings=%#v", article.ArticleID, article.MissingResources, article.Warnings)
		}
		foundMissing[article.ArticleID] = article.MissingResources[0].URL
	}
	if len(foundMissing) != len(expectedMissing) {
		t.Fatalf("best-effort missing articles=%d, want %d", len(foundMissing), len(expectedMissing))
	}
	for articleID, resourceURL := range expectedMissing {
		if foundMissing[articleID] != resourceURL {
			t.Fatalf("article %s missing URL=%q, want %q", articleID, foundMissing[articleID], resourceURL)
		}
	}
	archive, err := zip.OpenReader(filepath.Join(bestEffortRoot, "best-effort.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	wantEntries := articleCount*2 - len(expectedMissing)
	if len(archive.File) != wantEntries {
		t.Fatalf("best-effort ZIP entries=%d, want %d", len(archive.File), wantEntries)
	}
	missingHTML := batchReadZipEntry(t, archive.File, "article-0004/index.html")
	if !strings.Contains(missingHTML, expectedMissing["article-0004"]) {
		t.Fatalf("best-effort missing-resource HTML lost unresolved URL:\n%s", missingHTML)
	}
	localHTML := batchReadZipEntry(t, archive.File, "article-0000/index.html")
	if strings.Contains(localHTML, "https://cdn.example.test/batch/0000.png") || !strings.Contains(localHTML, `src="./assets/resource.png"`) {
		t.Fatalf("best-effort local-resource HTML was not self-contained:\n%s", localHTML)
	}
	t.Logf("packaged %d HTML articles at %.0f articles/s with %d missing-resource warnings", articleCount, throughput, len(result.Warnings))
}

type generatedBatchXLSXSource struct {
	remaining int
	calls     int
	content   string
}

func (source *generatedBatchXLSXSource) Next(ctx context.Context) (XLSXRow, error) {
	source.calls++
	if err := ctx.Err(); err != nil {
		return XLSXRow{}, err
	}
	if source.remaining == 0 {
		return XLSXRow{}, io.EOF
	}
	index := source.calls - 1
	source.remaining--
	return XLSXRow{
		Account: "Large batch fixture", ArticleID: fmt.Sprintf("article-%06d", index),
		CanonicalURL: fmt.Sprintf("https://mp.weixin.qq.com/s/batch-%06d", index),
		Title:        fmt.Sprintf("Article %06d", index), Digest: "bounded streaming row", Author: "Fixture",
		PublishedAt: time.Date(2026, 1, 1, 0, 0, index%60, 0, time.UTC),
		ReadCount:   int64(index), Original: index%2 == 0, MessageType: "graphic", State: "ready",
		DownloadState: "available", Albums: []string{"Large batch"}, Content: source.content,
	}, nil
}

func sampleBatchHeap(stop <-chan struct{}, stopped chan<- struct{}, peak *atomic.Uint64) {
	defer close(stopped)
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			storeBatchPeak(peak, stats.HeapAlloc)
		}
	}
}

func storeBatchPeak(peak *atomic.Uint64, value uint64) {
	for current := peak.Load(); value > current; current = peak.Load() {
		if peak.CompareAndSwap(current, value) {
			return
		}
	}
}

func batchPlansByID(plans []PlannedName) map[domain.ArticleID]string {
	result := make(map[domain.ArticleID]string, len(plans))
	for _, plan := range plans {
		result[plan.ArticleID] = plan.Path
	}
	return result
}

func batchOutputFixture(index int) (string, string) {
	return fmt.Sprintf("articles/article-%03d.txt", index), fmt.Sprintf("article %03d complete payload\n", index)
}

func batchWriteString(value string) func(io.Writer) error {
	return func(writer io.Writer) error {
		_, err := io.WriteString(writer, value)
		return err
	}
}

func assertNoBatchStagingFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if isTemporaryOutputName(entry.Name()) {
			t.Fatalf("abandoned staging output %q", entry.Name())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func batchReadZipEntry(t *testing.T, files []*zip.File, name string) string {
	t.Helper()
	for _, file := range files {
		if file.Name != name {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(opened)
		closeErr := opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return string(data)
	}
	t.Fatalf("ZIP entry %q not found", name)
	return ""
}
