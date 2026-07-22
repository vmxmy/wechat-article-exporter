package exporter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiscoverChromiumUsesDeterministicSupportedCandidates(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "chrome")
	if err := os.WriteFile(executable, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	browser, err := DiscoverChromium(ChromiumDiscoveryOptions{AdditionalPaths: []string{filepath.Join(root, "missing"), executable}})
	if err != nil {
		t.Fatal(err)
	}
	if browser.Path != executable || browser.Family != ChromiumFamilyChrome {
		t.Fatalf("browser = %#v", browser)
	}

	_, err = DiscoverChromium(ChromiumDiscoveryOptions{GOOS: "plan9", DisablePATH: true})
	var dependency *BrowserDependencyError
	if err == nil || !errors.As(err, &dependency) || !strings.Contains(err.Error(), "Install Google Chrome, Chromium, Microsoft Edge, or Brave") {
		t.Fatalf("dependency error = %#v", err)
	}

	nonExecutable := filepath.Join(root, "chrome.exe")
	if err := os.WriteFile(nonExecutable, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverChromium(ChromiumDiscoveryOptions{
		GOOS: "windows", AdditionalPaths: []string{nonExecutable}, OnlyAdditional: true, DisablePATH: true,
	}); err != nil {
		t.Fatalf("Windows executable should not require Unix execute bits: %v", err)
	}
	if _, err := DiscoverChromium(ChromiumDiscoveryOptions{
		GOOS: "linux", AdditionalPaths: []string{nonExecutable}, OnlyAdditional: true, DisablePATH: true,
	}); !errors.Is(err, ErrBrowserUnavailable) {
		t.Fatalf("Linux non-executable candidate error = %v", err)
	}
}

func TestRenderPDFUsesOnlyLocalSelfContainedHTML(t *testing.T) {
	runner := &fakePDFRunner{pdf: []byte("%PDF-1.7\nfixture\n%%EOF")}
	temporaryParent := filepath.Join(t.TempDir(), "parent with spaces")
	if err := os.MkdirAll(temporaryParent, 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	report, err := RenderPDF(context.Background(), &output, string(readExporterFixture(t, "pdf_self_contained.html")), PDFOptions{
		BrowserPath: "/fixture/chrome", Timeout: time.Second, Runner: runner, TempDir: temporaryParent,
		PageFormat: "Letter", PrintBackground: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.BrowserPath != "/fixture/chrome" || report.Bytes != int64(output.Len()) || !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
		t.Fatalf("report=%#v output=%q", report, output.Bytes())
	}
	if runner.path != "/fixture/chrome" || !containsArgument(runner.args, "--headless=new") ||
		!containsArgument(runner.args, "--no-pdf-header-footer") || !containsArgumentPrefix(runner.args, "--user-data-dir=") ||
		!containsArgumentPrefix(runner.args, "--print-to-pdf=") {
		t.Fatalf("runner path=%q args=%#v", runner.path, runner.args)
	}
	if !strings.HasPrefix(runner.inputURL, "file://") || !strings.Contains(runner.inputURL, "%20") {
		t.Fatalf("input URL = %q", runner.inputURL)
	}
	for _, expected := range []string{`id="wechat-pdf-print"`, `@page{size:Letter}`, `print-color-adjust:exact!important`} {
		if !strings.Contains(runner.html, expected) {
			t.Fatalf("staged HTML missing %q:\n%s", expected, runner.html)
		}
	}
	data, err := os.ReadFile(runner.inputPath)
	if err == nil || len(data) != 0 {
		t.Fatalf("temporary HTML survived render: err=%v bytes=%d", err, len(data))
	}
}

func TestRenderPDFRejectsRemoteResourcesTimeoutAndCancellation(t *testing.T) {
	runner := &fakePDFRunner{pdf: []byte("%PDF-1.7\n%%EOF")}
	unsafeDocuments := []string{
		`<html><body><img src="https://remote.example/image.png"></body></html>`,
		`<html><body><img src="./image.png"></body></html>`,
		`<html><body><img src="data:image/png;base64,AA" srcset="https://remote.example/image.png 2x"></body></html>`,
		`<html><head><style>@import "https://remote.example/style.css";</style></head><body></body></html>`,
		`<html><head><meta http-equiv="refresh" content="0;url=https://remote.example/"></head><body></body></html>`,
	}
	for _, document := range unsafeDocuments {
		_, err := RenderPDF(context.Background(), io.Discard, document, PDFOptions{
			BrowserPath: "/fixture/chrome", Runner: runner,
		})
		if !errors.Is(err, ErrPDFNotSelfContained) || runner.calls != 0 {
			t.Fatalf("unsafe resource error=%v calls=%d document=%s", err, runner.calls, document)
		}
	}

	_, err := RenderPDF(context.Background(), io.Discard, `<html><body>safe</body></html>`, PDFOptions{
		BrowserPath: "/fixture/chrome", Runner: runner, PageFormat: "Tabloid",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported PDF page format") || runner.calls != 0 {
		t.Fatalf("page format error=%v calls=%d", err, runner.calls)
	}

	blocking := &fakePDFRunner{block: true}
	_, err = RenderPDF(context.Background(), io.Discard, `<html><body>safe</body></html>`, PDFOptions{
		BrowserPath: "/fixture/chrome", Runner: blocking, Timeout: 20 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocking = &fakePDFRunner{block: true}
	_, err = RenderPDF(ctx, io.Discard, `<html><body>safe</body></html>`, PDFOptions{
		BrowserPath: "/fixture/chrome", Runner: blocking,
	})
	if !errors.Is(err, context.Canceled) || blocking.calls != 0 {
		t.Fatalf("cancel error=%v calls=%d", err, blocking.calls)
	}

	invalid := &fakePDFRunner{pdf: []byte("not a pdf")}
	_, err = RenderPDF(context.Background(), io.Discard, `<html><body>safe</body></html>`, PDFOptions{
		BrowserPath: "/fixture/chrome", Runner: invalid,
	})
	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("invalid PDF error = %v", err)
	}
}

func TestRenderPDFValidatesExplicitBrowserPathForCommandRunner(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-chrome")
	_, err := RenderPDF(context.Background(), io.Discard, `<html><body>safe</body></html>`, PDFOptions{BrowserPath: missing})
	var dependency *BrowserDependencyError
	if err == nil || !errors.As(err, &dependency) || len(dependency.Attempted) != 1 || dependency.Attempted[0] != missing {
		t.Fatalf("explicit dependency error = %#v", err)
	}
}

func TestRealChromiumRendersCuratedSelfContainedPDF(t *testing.T) {
	browser, err := DiscoverChromium(ChromiumDiscoveryOptions{})
	if errors.Is(err, ErrBrowserUnavailable) {
		t.Skip("supported Chromium-family browser is not installed")
	}
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	report, err := RenderPDF(context.Background(), &output, string(readExporterFixture(t, "pdf_self_contained.html")), PDFOptions{
		BrowserPath: browser.Path, Timeout: 60 * time.Second, PrintBackground: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.BrowserPath != browser.Path || report.Bytes <= 1024 || !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
		t.Fatalf("report=%#v bytes=%d", report, output.Len())
	}
	if pdfInfo, lookErr := exec.LookPath("pdfinfo"); lookErr == nil {
		path := filepath.Join(t.TempDir(), "rendered.pdf")
		if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		command := exec.Command(pdfInfo, path)
		metadata, err := command.CombinedOutput()
		if err != nil || !bytes.Contains(metadata, []byte("Pages:")) {
			t.Fatalf("pdfinfo error=%v\n%s", err, metadata)
		}
	}
}

type fakePDFRunner struct {
	mu        sync.Mutex
	calls     int
	path      string
	args      []string
	inputURL  string
	inputPath string
	html      string
	pdf       []byte
	block     bool
}

func (runner *fakePDFRunner) Run(ctx context.Context, path string, args []string, _, _ io.Writer) error {
	runner.mu.Lock()
	runner.calls++
	runner.path = path
	runner.args = append([]string(nil), args...)
	for _, argument := range args {
		if strings.HasPrefix(argument, "file://") {
			runner.inputURL = argument
			parsed, err := url.Parse(argument)
			if err != nil {
				runner.mu.Unlock()
				return err
			}
			runner.inputPath = parsed.Path
			contents, err := os.ReadFile(runner.inputPath)
			if err != nil {
				runner.mu.Unlock()
				return err
			}
			runner.html = string(contents)
		}
	}
	runner.mu.Unlock()
	if runner.block {
		<-ctx.Done()
		return ctx.Err()
	}
	output := argumentValue(args, "--print-to-pdf=")
	return os.WriteFile(output, runner.pdf, 0o600)
}

func containsArgument(arguments []string, expected string) bool {
	for _, argument := range arguments {
		if argument == expected {
			return true
		}
	}
	return false
}

func containsArgumentPrefix(arguments []string, expected string) bool {
	return argumentValue(arguments, expected) != ""
}
