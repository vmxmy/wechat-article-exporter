package exporter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	ErrBrowserUnavailable  = errors.New("supported Chromium-family browser is unavailable")
	ErrPDFNotSelfContained = errors.New("PDF HTML is not self-contained")
	ErrInvalidPDF          = errors.New("browser did not produce a valid PDF")
)

type ChromiumFamily string

const (
	ChromiumFamilyChrome   ChromiumFamily = "chrome"
	ChromiumFamilyChromium ChromiumFamily = "chromium"
	ChromiumFamilyEdge     ChromiumFamily = "edge"
	ChromiumFamilyBrave    ChromiumFamily = "brave"
)

type Browser struct {
	Path   string         `json:"path"`
	Family ChromiumFamily `json:"family"`
}

type BrowserDependencyError struct {
	GOOS      string
	Attempted []string
}

func (err *BrowserDependencyError) Error() string {
	return fmt.Sprintf("%v on %s. Install Google Chrome, Chromium, Microsoft Edge, or Brave, or pass an explicit browser path", ErrBrowserUnavailable, err.GOOS)
}

func (err *BrowserDependencyError) Unwrap() error { return ErrBrowserUnavailable }

type ChromiumDiscoveryOptions struct {
	GOOS            string
	AdditionalPaths []string
	OnlyAdditional  bool
	DisablePATH     bool
	LookPath        func(string) (string, error)
	Stat            func(string) (os.FileInfo, error)
}

type ProcessRunner interface {
	Run(context.Context, string, []string, io.Writer, io.Writer) error
}

type PDFOptions struct {
	BrowserPath     string
	Timeout         time.Duration
	TempDir         string
	Runner          ProcessRunner
	MaxHTMLBytes    int
	MaxPDFBytes     int64
	PageFormat      string
	PrintBackground bool
}

type PDFReport struct {
	BrowserPath string `json:"browserPath"`
	Bytes       int64  `json:"bytes"`
	PageFormat  string `json:"pageFormat"`
}

func DiscoverChromium(options ChromiumDiscoveryOptions) (Browser, error) {
	goos := options.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	stat := options.Stat
	if stat == nil {
		stat = os.Stat
	}
	type candidate struct {
		path   string
		family ChromiumFamily
		lookup bool
	}
	candidates := make([]candidate, 0, len(options.AdditionalPaths)+16)
	for _, path := range options.AdditionalPaths {
		candidates = append(candidates, candidate{path: path, family: chromiumFamilyForName(path)})
	}
	if !options.OnlyAdditional {
		for _, item := range platformChromiumCandidates(goos) {
			candidates = append(candidates, item)
		}
		if !options.DisablePATH {
			for _, item := range pathChromiumCandidates(goos) {
				item.lookup = true
				candidates = append(candidates, item)
			}
		}
	}
	attempted := make([]string, 0, len(candidates))
	seen := make(map[string]struct{})
	for _, item := range candidates {
		path := strings.TrimSpace(item.path)
		if path == "" {
			continue
		}
		if item.lookup {
			resolved, err := lookPath(path)
			if err != nil {
				attempted = append(attempted, path)
				continue
			}
			path = resolved
		}
		absolute, err := filepath.Abs(path)
		if err == nil {
			path = absolute
		}
		path = filepath.Clean(path)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		attempted = append(attempted, path)
		info, err := stat(path)
		if err != nil || !info.Mode().IsRegular() || goos != "windows" && info.Mode().Perm()&0o111 == 0 {
			continue
		}
		family := item.family
		if family == "" {
			family = chromiumFamilyForName(path)
		}
		return Browser{Path: path, Family: family}, nil
	}
	return Browser{}, &BrowserDependencyError{GOOS: goos, Attempted: attempted}
}

func RenderPDF(ctx context.Context, destination io.Writer, htmlDocument string, options PDFOptions) (PDFReport, error) {
	if destination == nil {
		return PDFReport{}, errors.New("PDF destination is required")
	}
	if err := ctx.Err(); err != nil {
		return PDFReport{}, err
	}
	if options.MaxHTMLBytes <= 0 {
		options.MaxHTMLBytes = 32 << 20
	}
	if options.MaxPDFBytes <= 0 {
		options.MaxPDFBytes = 512 << 20
	}
	if options.Timeout <= 0 {
		options.Timeout = 60 * time.Second
	}
	if options.PageFormat == "" {
		options.PageFormat = "A4"
	}
	pageFormat, err := normalizePDFPageFormat(options.PageFormat)
	if err != nil {
		return PDFReport{}, err
	}
	options.PageFormat = pageFormat
	if len(htmlDocument) > options.MaxHTMLBytes {
		return PDFReport{}, fmt.Errorf("PDF HTML exceeds %d bytes", options.MaxHTMLBytes)
	}
	if err := validateSelfContainedPDFHTML(htmlDocument); err != nil {
		return PDFReport{}, err
	}
	htmlDocument = injectPDFPrintCSS(htmlDocument, options.PageFormat, options.PrintBackground)
	if len(htmlDocument) > options.MaxHTMLBytes {
		return PDFReport{}, fmt.Errorf("PDF HTML with print styles exceeds %d bytes", options.MaxHTMLBytes)
	}
	browserPath := strings.TrimSpace(options.BrowserPath)
	if browserPath == "" {
		browser, err := DiscoverChromium(ChromiumDiscoveryOptions{})
		if err != nil {
			return PDFReport{}, err
		}
		browserPath = browser.Path
	} else if options.Runner == nil {
		browser, err := DiscoverChromium(ChromiumDiscoveryOptions{
			AdditionalPaths: []string{browserPath},
			OnlyAdditional:  true,
			DisablePATH:     true,
		})
		if err != nil {
			return PDFReport{}, err
		}
		browserPath = browser.Path
	}
	runner := options.Runner
	if runner == nil {
		runner = commandProcessRunner{}
	}
	temporaryRoot, err := os.MkdirTemp(options.TempDir, "wechat-pdf-")
	if err != nil {
		return PDFReport{}, fmt.Errorf("create PDF staging directory: %w", err)
	}
	defer os.RemoveAll(temporaryRoot)
	if err := os.Chmod(temporaryRoot, 0o700); err != nil {
		return PDFReport{}, fmt.Errorf("secure PDF staging directory: %w", err)
	}
	htmlPath := filepath.Join(temporaryRoot, "article.html")
	pdfPath := filepath.Join(temporaryRoot, "article.pdf")
	if err := os.WriteFile(htmlPath, []byte(htmlDocument), 0o600); err != nil {
		return PDFReport{}, fmt.Errorf("write PDF input HTML: %w", err)
	}
	inputURL := fileURL(htmlPath)
	args := []string{
		"--headless=new", "--disable-gpu", "--disable-extensions", "--disable-background-networking",
		"--disable-component-update", "--disable-default-apps", "--disable-sync", "--metrics-recording-only",
		"--no-first-run", "--no-default-browser-check", "--hide-scrollbars", "--run-all-compositor-stages-before-draw",
		"--virtual-time-budget=10000", "--no-pdf-header-footer", "--user-data-dir=" + filepath.Join(temporaryRoot, "profile"),
		"--print-to-pdf=" + pdfPath, inputURL,
	}
	runCtx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	var stderr bytes.Buffer
	if err := runner.Run(runCtx, browserPath, args, io.Discard, &stderr); err != nil {
		if runCtx.Err() != nil {
			return PDFReport{}, runCtx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 512 {
			detail = detail[:512]
		}
		if detail == "" {
			detail = err.Error()
		}
		return PDFReport{}, fmt.Errorf("local Chromium PDF rendering failed: %s", detail)
	}
	file, err := os.Open(pdfPath)
	if err != nil {
		return PDFReport{}, fmt.Errorf("open rendered PDF: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return PDFReport{}, err
	}
	if info.Size() <= 0 || info.Size() > options.MaxPDFBytes {
		return PDFReport{}, fmt.Errorf("rendered PDF size %d is invalid: %w", info.Size(), ErrInvalidPDF)
	}
	header := make([]byte, 5)
	if _, err := io.ReadFull(file, header); err != nil || string(header) != "%PDF-" {
		return PDFReport{}, ErrInvalidPDF
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return PDFReport{}, err
	}
	written, err := copyPDFContext(ctx, destination, file, options.MaxPDFBytes)
	if err != nil {
		return PDFReport{}, err
	}
	return PDFReport{BrowserPath: browserPath, Bytes: written, PageFormat: options.PageFormat}, nil
}

type commandProcessRunner struct{}

func (commandProcessRunner) Run(ctx context.Context, path string, args []string, stdout, stderr io.Writer) error {
	command := exec.Command(path, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Stdin = nil
	configurePDFProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()

	outputPath := argumentValue(args, "--print-to-pdf=")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastCompleteSize int64
	for {
		select {
		case err := <-wait:
			if err == nil {
				return nil
			}
			if completePDFSize(outputPath) > 0 {
				return nil
			}
			return err
		case <-ctx.Done():
			terminatePDFProcess(command)
			<-wait
			return ctx.Err()
		case <-ticker.C:
			completeSize := completePDFSize(outputPath)
			if completeSize <= 0 {
				lastCompleteSize = 0
				continue
			}
			if completeSize != lastCompleteSize {
				lastCompleteSize = completeSize
				continue
			}
			// Some Chromium builds finish --print-to-pdf but keep the browser
			// process alive for background services. Once a complete PDF has
			// remained stable across polls, terminate only this isolated browser
			// process tree and treat the render as successful.
			terminatePDFProcess(command)
			<-wait
			return nil
		}
	}
}

func argumentValue(arguments []string, prefix string) string {
	for _, argument := range arguments {
		if strings.HasPrefix(argument, prefix) {
			return strings.TrimPrefix(argument, prefix)
		}
	}
	return ""
}

func completePDFSize(path string) int64 {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() < 10 {
		return 0
	}
	header := make([]byte, 5)
	if _, err := io.ReadFull(file, header); err != nil || string(header) != "%PDF-" {
		return 0
	}
	tailSize := int64(2048)
	if info.Size() < tailSize {
		tailSize = info.Size()
	}
	if _, err := file.Seek(-tailSize, io.SeekEnd); err != nil {
		return 0
	}
	tail, err := io.ReadAll(io.LimitReader(file, tailSize))
	if err != nil || !bytes.Contains(tail, []byte("%%EOF")) {
		return 0
	}
	return info.Size()
}

func platformChromiumCandidates(goos string) []struct {
	path   string
	family ChromiumFamily
	lookup bool
} {
	type candidate = struct {
		path   string
		family ChromiumFamily
		lookup bool
	}
	switch goos {
	case "darwin":
		return []candidate{
			{path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", family: ChromiumFamilyChrome},
			{path: "/Applications/Chromium.app/Contents/MacOS/Chromium", family: ChromiumFamilyChromium},
			{path: "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", family: ChromiumFamilyEdge},
			{path: "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser", family: ChromiumFamilyBrave},
		}
	case "windows":
		roots := []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")}
		var result []candidate
		for _, root := range roots {
			if root == "" {
				continue
			}
			result = append(result,
				candidate{path: filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"), family: ChromiumFamilyChrome},
				candidate{path: filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"), family: ChromiumFamilyEdge},
				candidate{path: filepath.Join(root, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"), family: ChromiumFamilyBrave},
			)
		}
		return result
	case "linux":
		return []candidate{
			{path: "/usr/bin/google-chrome", family: ChromiumFamilyChrome},
			{path: "/usr/bin/google-chrome-stable", family: ChromiumFamilyChrome},
			{path: "/usr/bin/chromium", family: ChromiumFamilyChromium},
			{path: "/usr/bin/chromium-browser", family: ChromiumFamilyChromium},
			{path: "/usr/bin/microsoft-edge", family: ChromiumFamilyEdge},
			{path: "/usr/bin/brave-browser", family: ChromiumFamilyBrave},
		}
	default:
		return nil
	}
}

func pathChromiumCandidates(goos string) []struct {
	path   string
	family ChromiumFamily
	lookup bool
} {
	type candidate = struct {
		path   string
		family ChromiumFamily
		lookup bool
	}
	if goos == "windows" {
		return []candidate{{path: "chrome.exe", family: ChromiumFamilyChrome}, {path: "msedge.exe", family: ChromiumFamilyEdge}, {path: "brave.exe", family: ChromiumFamilyBrave}}
	}
	return []candidate{
		{path: "google-chrome", family: ChromiumFamilyChrome}, {path: "google-chrome-stable", family: ChromiumFamilyChrome},
		{path: "chromium", family: ChromiumFamilyChromium}, {path: "chromium-browser", family: ChromiumFamilyChromium},
		{path: "microsoft-edge", family: ChromiumFamilyEdge}, {path: "brave-browser", family: ChromiumFamilyBrave},
	}
}

func chromiumFamilyForName(value string) ChromiumFamily {
	lower := strings.ToLower(filepath.Base(value))
	switch {
	case strings.Contains(lower, "edge") || strings.Contains(lower, "msedge"):
		return ChromiumFamilyEdge
	case strings.Contains(lower, "brave"):
		return ChromiumFamilyBrave
	case strings.Contains(lower, "chromium"):
		return ChromiumFamilyChromium
	default:
		return ChromiumFamilyChrome
	}
}

func validateSelfContainedPDFHTML(value string) error {
	lower := strings.ToLower(value)
	for _, executable := range []string{"<script", "<iframe", "<object", "<embed", "javascript:", "srcdoc="} {
		if strings.Contains(lower, executable) {
			return fmt.Errorf("active content %q is not allowed: %w", executable, ErrPDFNotSelfContained)
		}
	}
	if strings.Contains(lower, "@import") {
		return fmt.Errorf("CSS @import is not allowed; inline imported styles: %w", ErrPDFNotSelfContained)
	}
	if hasPDFMetaRefresh(lower) {
		return fmt.Errorf("meta refresh is not allowed: %w", ErrPDFNotSelfContained)
	}
	for _, attribute := range []string{"src", "href", "poster", "action", "formaction", "srcset"} {
		position := 0
		for {
			index := strings.Index(lower[position:], attribute)
			if index < 0 {
				break
			}
			index += position
			beforeOK := index == 0 || !pdfNameChar(lower[index-1])
			after := index + len(attribute)
			afterOK := after >= len(lower) || !pdfNameChar(lower[after])
			if !beforeOK || !afterOK {
				position = after
				continue
			}
			for after < len(value) && pdfSpace(value[after]) {
				after++
			}
			if after >= len(value) || value[after] != '=' {
				position = after
				continue
			}
			after++
			for after < len(value) && pdfSpace(value[after]) {
				after++
			}
			url := ""
			if after < len(value) && (value[after] == '\'' || value[after] == '"') {
				quote := value[after]
				after++
				start := after
				for after < len(value) && value[after] != quote {
					after++
				}
				url = value[start:after]
			} else {
				start := after
				for after < len(value) && !pdfSpace(value[after]) && value[after] != '>' {
					after++
				}
				url = value[start:after]
			}
			if attribute == "srcset" && strings.TrimSpace(url) != "" {
				return fmt.Errorf("srcset is not allowed; inline the selected image in src: %w", ErrPDFNotSelfContained)
			}
			if unsafePDFResourceURL(url) {
				return fmt.Errorf("non-embedded %s resource %q: %w", attribute, url, ErrPDFNotSelfContained)
			}
			position = after
		}
	}
	for _, cssURL := range pdfCSSURLs(value) {
		if unsafePDFResourceURL(cssURL) {
			return fmt.Errorf("non-embedded CSS resource %q: %w", cssURL, ErrPDFNotSelfContained)
		}
	}
	return nil
}

func hasPDFMetaRefresh(lower string) bool {
	position := 0
	for {
		index := strings.Index(lower[position:], "<meta")
		if index < 0 {
			return false
		}
		start := position + index
		end := strings.IndexByte(lower[start:], '>')
		if end < 0 {
			return false
		}
		end += start
		tag := lower[start : end+1]
		if strings.Contains(tag, "http-equiv") && strings.Contains(tag, "refresh") {
			return true
		}
		position = end + 1
	}
}

func unsafePDFResourceURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "data:") {
		return false
	}
	return true
}

func pdfCSSURLs(value string) []string {
	lower := strings.ToLower(value)
	position := 0
	var values []string
	for {
		index := strings.Index(lower[position:], "url(")
		if index < 0 {
			break
		}
		start := position + index + len("url(")
		end := strings.IndexByte(value[start:], ')')
		if end < 0 {
			break
		}
		end += start
		values = append(values, strings.Trim(strings.TrimSpace(value[start:end]), "\"'"))
		position = end + 1
	}
	return values
}

func fileURL(path string) string {
	path = filepath.ToSlash(path)
	if len(path) > 1 && path[1] == ':' {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func normalizePDFPageFormat(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A3":
		return "A3", nil
	case "A4":
		return "A4", nil
	case "A5":
		return "A5", nil
	case "LETTER":
		return "Letter", nil
	case "LEGAL":
		return "Legal", nil
	default:
		return "", fmt.Errorf("unsupported PDF page format %q; use A3, A4, A5, Letter, or Legal", value)
	}
}

func injectPDFPrintCSS(value, pageFormat string, printBackground bool) string {
	var style strings.Builder
	style.WriteString(`<style id="wechat-pdf-print">@page{size:`)
	style.WriteString(pageFormat)
	style.WriteString(`}`)
	if printBackground {
		style.WriteString(`*{-webkit-print-color-adjust:exact!important;print-color-adjust:exact!important}`)
	}
	style.WriteString(`</style>`)

	lower := strings.ToLower(value)
	if index := strings.Index(lower, "</head>"); index >= 0 {
		return value[:index] + style.String() + value[index:]
	}
	return style.String() + value
}

func copyPDFContext(ctx context.Context, destination io.Writer, source io.Reader, maximum int64) (int64, error) {
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			total += int64(read)
			if total > maximum {
				return total, ErrInvalidPDF
			}
			if _, err := destination.Write(buffer[:read]); err != nil {
				return total, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func pdfNameChar(character byte) bool {
	return character == '-' || character == '_' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func pdfSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n' || character == '\f'
}
