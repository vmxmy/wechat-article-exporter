package exporter

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteDOCXEmbedsSemanticStructureMediaAndComments(t *testing.T) {
	document := DOCXDocument{
		Title: "离线基线文章", Account: "示例公众号", Author: "示例作者",
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		HTML:        string(readExporterFixture(t, "docx_article.html")),
		Media: []DOCXMedia{{
			Source: "./assets/fixture-image.png", Name: "fixture-image.png", ContentType: "image/png", Data: testPNG,
		}},
		Comments: []DOCXComment{{
			Author: "读者甲", Content: "很有帮助",
			Replies: []DOCXReply{{Author: "示例作者", Content: "谢谢阅读"}},
		}},
	}
	var output bytes.Buffer
	report, err := WriteDOCX(context.Background(), &output, document, DOCXOptions{IncludeComments: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.MediaEmbedded != 1 || report.Comments != 1 || report.Paragraphs == 0 {
		t.Fatalf("report = %#v", report)
	}

	validation, err := ValidateDOCX(bytes.NewReader(output.Bytes()), int64(output.Len()), DOCXValidationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || validation.Media != 1 || validation.Tables != 1 || validation.Hyperlinks != 1 {
		t.Fatalf("validation = %#v", validation)
	}

	documentXML := readZipEntry(t, output.Bytes(), "word/document.xml")
	for _, expected := range []string{
		`w:val="Heading1"`, `w:hyperlink`, `w:numPr`, `w:val="Quote"`, `w:val="CodeBlock"`,
		`<w:tbl>`, `<w:drawing>`, "[Audio]", "[Video]", "Comments", "读者甲", "谢谢阅读", "non-breaking\u00a0space",
	} {
		if !strings.Contains(documentXML, expected) {
			t.Fatalf("document.xml missing %q:\n%s", expected, documentXML)
		}
	}
	if media := readZipEntry(t, output.Bytes(), "word/media/fixture-image.png"); !bytes.Equal([]byte(media), testPNG) {
		t.Fatalf("embedded media changed: %x", []byte(media))
	}
	for _, required := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		if !zipHasEntry(t, output.Bytes(), required) {
			t.Fatalf("DOCX missing %s", required)
		}
	}
	relationships := readZipEntry(t, output.Bytes(), "word/_rels/document.xml.rels")
	for _, expected := range []string{
		`Id="rIdStyles"`, `Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"`, `Target="styles.xml"`,
		`Id="rIdNumbering"`, `Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"`, `Target="numbering.xml"`,
	} {
		if !strings.Contains(relationships, expected) {
			t.Fatalf("document relationships missing %q:\n%s", expected, relationships)
		}
	}

	var repeated bytes.Buffer
	if _, err := WriteDOCX(context.Background(), &repeated, document, DOCXOptions{IncludeComments: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), repeated.Bytes()) {
		t.Fatal("identical DOCX inputs produced different package bytes")
	}
}

func TestWriteAndValidateDOCXEnforceBoundsAndCancellation(t *testing.T) {
	document := DOCXDocument{Title: "Large", HTML: "<p>body</p>", Media: []DOCXMedia{{
		Source: "image.png", Name: "image.png", ContentType: "image/png", Data: bytes.Repeat([]byte{1}, 32),
	}}}
	_, err := WriteDOCX(context.Background(), io.Discard, document, DOCXOptions{MaxMediaBytes: 16})
	if !errors.Is(err, ErrDOCXLimit) {
		t.Fatalf("media limit error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = WriteDOCX(ctx, io.Discard, DOCXDocument{Title: "Cancelled", HTML: "<p>body</p>"}, DOCXOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}

	invalid := []byte("not a zip")
	validation, err := ValidateDOCX(bytes.NewReader(invalid), int64(len(invalid)), DOCXValidationOptions{})
	if err == nil || validation.Valid {
		t.Fatalf("invalid validation=%#v err=%v", validation, err)
	}

	var valid bytes.Buffer
	if _, err := WriteDOCX(context.Background(), &valid, DOCXDocument{Title: "Relationships", HTML: "<p>body</p>"}, DOCXOptions{}); err != nil {
		t.Fatal(err)
	}
	broken := rewriteZipEntry(t, valid.Bytes(), "word/_rels/document.xml.rels", func(value string) string {
		return strings.Replace(value, ` Target="styles.xml"`, ` Target="missing-styles.xml"`, 1)
	})
	validation, err = ValidateDOCX(bytes.NewReader(broken), int64(len(broken)), DOCXValidationOptions{})
	if err == nil || validation.Valid || !strings.Contains(err.Error(), "missing document relationship: styles.xml") {
		t.Fatalf("relationship validation=%#v err=%v", validation, err)
	}
}

func TestGeneratedDOCXOpensInLibreOfficeHeadless(t *testing.T) {
	soffice, err := exec.LookPath("soffice")
	if err != nil {
		t.Skip("LibreOffice soffice is not installed")
	}
	versionContext, cancelVersion := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelVersion()
	versionOutput, err := exec.CommandContext(versionContext, soffice, "--version").CombinedOutput()
	if err != nil {
		t.Skipf("LibreOffice launcher is unavailable: %v: %s", err, strings.TrimSpace(string(versionOutput)))
	}
	directory := t.TempDir()
	docxPath := filepath.Join(directory, "article.docx")
	file, err := os.OpenFile(docxPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := WriteDOCX(context.Background(), file, DOCXDocument{
		Title: "LibreOffice smoke", Account: "Fixture", Author: "Codex",
		HTML:  string(readExporterFixture(t, "docx_article.html")),
		Media: []DOCXMedia{{Source: "./assets/fixture-image.png", Name: "fixture-image.png", ContentType: "image/png", Data: testPNG}},
	}, DOCXOptions{})
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal(errors.Join(writeErr, closeErr))
	}
	profile := filepath.Join(directory, "libreoffice-profile")
	convertContext, cancelConvert := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelConvert()
	command := exec.CommandContext(convertContext, soffice, "-env:UserInstallation=file://"+filepath.ToSlash(profile), "--headless",
		"--convert-to", "pdf", "--outdir", directory, docxPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("LibreOffice conversion failed: %v\n%s", err, output)
	}
	pdfPath := filepath.Join(directory, "article.pdf")
	pdf, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("LibreOffice did not produce PDF: %v\n%s", err, output)
	}
	if len(pdf) <= 1024 || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("LibreOffice PDF is invalid: %d bytes\n%s", len(pdf), output)
	}
}

func zipHasEntry(t *testing.T, data []byte, name string) bool {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if file.Name == name {
			return true
		}
	}
	return false
}

func rewriteZipEntry(t *testing.T, data []byte, name string, rewrite func(string) string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
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
		if file.Name == name {
			contents = []byte(rewrite(string(contents)))
		}
		writer, err := archive.CreateHeader(&zip.FileHeader{Name: file.Name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

var testPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54,
	0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}
