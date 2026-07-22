package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestWriteXLSXStreamsStableColumnsAndOptionalContent(t *testing.T) {
	rows := &testXLSXSource{rows: []XLSXRow{
		{
			Account: "示例公众号", ArticleID: "article-1", CanonicalURL: "https://mp.weixin.qq.com/s/one",
			Title: "=formula-safe", CoverURL: "https://example.test/cover.jpg", Digest: "摘要",
			CreatedAt: time.Date(2026, 1, 1, 1, 2, 3, 0, time.UTC), PublishedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			ReadCount: 1200, OldLikeCount: 31, ShareCount: 17, LikeCount: 42, CommentCount: 6,
			Author: "作者", Original: true, MessageType: "graphic", State: "ready", DownloadState: "available",
			Albums: []string{"合集 A", "合集 B"}, Content: "正文内容",
		},
	}}
	var output bytes.Buffer
	report, err := WriteXLSX(context.Background(), &output, rows, XLSXOptions{IncludeContent: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Rows != 1 || report.Columns != len(XLSXColumns(true)) || rows.calls != 2 {
		t.Fatalf("report=%#v calls=%d", report, rows.calls)
	}

	sheet := readZipEntry(t, output.Bytes(), "xl/worksheets/sheet1.xml")
	var approved struct {
		Columns []string `json:"columns"`
	}
	if err := json.Unmarshal(readExporterFixture(t, "xlsx_columns.json"), &approved); err != nil {
		t.Fatal(err)
	}
	for _, column := range approved.Columns {
		if !strings.Contains(sheet, ">"+column+"<") {
			t.Fatalf("worksheet missing stable column %q:\n%s", column, sheet)
		}
	}
	for _, value := range []string{"示例公众号", "article-1", "=formula-safe", "合集 A; 合集 B", "正文内容"} {
		if !strings.Contains(sheet, ">"+value+"<") {
			t.Fatalf("worksheet missing value %q:\n%s", value, sheet)
		}
	}
	if strings.Contains(sheet, "<f>") {
		t.Fatalf("untrusted text was emitted as a formula:\n%s", sheet)
	}

	withoutContent := &testXLSXSource{rows: rows.rows}
	output.Reset()
	if _, err := WriteXLSX(context.Background(), &output, withoutContent, XLSXOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet = readZipEntry(t, output.Bytes(), "xl/worksheets/sheet1.xml")
	if strings.Contains(sheet, ">文章内容<") || strings.Contains(sheet, ">正文内容<") {
		t.Fatalf("optional content column was not omitted:\n%s", sheet)
	}
}

func TestWriteXLSXEnforcesBoundsAndCancellation(t *testing.T) {
	rows := &testXLSXSource{rows: []XLSXRow{{Title: "one"}, {Title: "two"}}}
	_, err := WriteXLSX(context.Background(), io.Discard, rows, XLSXOptions{MaxRows: 1})
	if !errors.Is(err, ErrXLSXLimit) {
		t.Fatalf("row limit error = %v", err)
	}

	rows = &testXLSXSource{rows: []XLSXRow{{Title: strings.Repeat("x", 64)}}}
	_, err = WriteXLSX(context.Background(), io.Discard, rows, XLSXOptions{MaxCellBytes: 16})
	if !errors.Is(err, ErrXLSXLimit) {
		t.Fatalf("cell limit error = %v", err)
	}

	_, err = WriteXLSX(context.Background(), io.Discard, &testXLSXSource{}, XLSXOptions{MaxRows: defaultXLSXMaxRows + 1})
	if !errors.Is(err, ErrXLSXLimit) {
		t.Fatalf("worksheet row capacity error = %v", err)
	}

	_, err = WriteXLSX(context.Background(), io.Discard, &testXLSXSource{}, XLSXOptions{MaxCellBytes: defaultXLSXMaxCellBytes + 1})
	if !errors.Is(err, ErrXLSXLimit) {
		t.Fatalf("worksheet cell capacity error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows = &testXLSXSource{rows: []XLSXRow{{Title: "never"}}}
	_, err = WriteXLSX(ctx, io.Discard, rows, XLSXOptions{})
	if !errors.Is(err, context.Canceled) || rows.calls != 0 {
		t.Fatalf("cancel error=%v calls=%d", err, rows.calls)
	}
}

func TestWriteXLSXUsesDeterministicPackageOrder(t *testing.T) {
	var first bytes.Buffer
	if _, err := WriteXLSX(context.Background(), &first, &testXLSXSource{}, XLSXOptions{}); err != nil {
		t.Fatal(err)
	}
	var second bytes.Buffer
	if _, err := WriteXLSX(context.Background(), &second, &testXLSXSource{}, XLSXOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("identical XLSX inputs produced different package bytes")
	}
}

type testXLSXSource struct {
	rows  []XLSXRow
	index int
	calls int
}

func (source *testXLSXSource) Next(context.Context) (XLSXRow, error) {
	source.calls++
	if source.index >= len(source.rows) {
		return XLSXRow{}, io.EOF
	}
	row := source.rows[source.index]
	source.index++
	return row, nil
}

func readZipEntry(t *testing.T, data []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer opened.Close()
		contents, err := io.ReadAll(opened)
		if err != nil {
			t.Fatal(err)
		}
		return string(contents)
	}
	t.Fatalf("ZIP entry %q not found", name)
	return ""
}
