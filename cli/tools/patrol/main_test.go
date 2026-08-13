package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	scriptLayoutPage = `<html><head><script>
var followBar = document.getElementById("js_wx_follow_nickname");
var nickname = htmlDecode("巡检示例号");
</script></head><body><div id="js_name"> 巡检示例号 </div></body></html>`
	legacyLayoutPage = `<html><body><strong class="wx_follow_nickname">Legacy Account</strong></body></html>`
	anchorlessPage   = `<html><body><p>nothing anchored here</p></body></html>`
)

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	pages := map[string]string{
		"/script":     scriptLayoutPage,
		"/legacy":     legacyLayoutPage,
		"/anchorless": anchorlessPage,
	}
	for path, page := range pages {
		body := page
		mux.HandleFunc(path, func(writer http.ResponseWriter, request *http.Request) {
			if !strings.Contains(request.Header.Get("User-Agent"), "Mozilla/5.0") {
				t.Errorf("User-Agent = %q, want a browser UA", request.Header.Get("User-Agent"))
			}
			_, _ = writer.Write([]byte(body))
		})
	}
	mux.HandleFunc("/redirect", func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, "/script", http.StatusFound)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeLinkFile(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "urls.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runPatrol(t *testing.T, path string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code, err := run(path, newPatrolClient(), &out)
	if err != nil {
		t.Fatal(err)
	}
	return code, out.String()
}

func reportRow(t *testing.T, report, label string) string {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, label+" ") {
			return line
		}
	}
	t.Fatalf("report has no row for %s:\n%s", label, report)
	return ""
}

func cells(row string) []string {
	return regexp.MustCompile(`\s{2,}`).Split(strings.TrimSpace(row), -1)
}

func TestRunAllPrimaryHitsExitsZero(t *testing.T) {
	server := fixtureServer(t)
	path := writeLinkFile(t,
		"# 巡检链接（注释行）",
		server.URL+"/script",
		"",
		server.URL+"/redirect",
	)
	code, report := runPatrol(t, path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", code, report)
	}
	for _, label := range []string{"URL_1", "URL_2"} {
		row := cells(reportRow(t, report, label))
		if len(row) != 5 || row[1] != "hit" || row[2] != "miss" || row[3] != "hit" {
			t.Fatalf("%s row = %q", label, row)
		}
	}
	if strings.Contains(report, "URL_3") {
		t.Fatalf("comment or blank line counted as URL:\n%s", report)
	}
	if !strings.Contains(report, "单次失败 ≠ 漂移") {
		t.Fatalf("report lacks the rerun-before-escalating header:\n%s", report)
	}
	if !strings.Contains(report, "js_name 2/2") {
		t.Fatalf("report lacks the summary line:\n%s", report)
	}
}

func TestRunClassifiesLayoutsAndPrimaryMissExitsOne(t *testing.T) {
	server := fixtureServer(t)
	path := writeLinkFile(t,
		server.URL+"/script",
		server.URL+"/legacy",
		server.URL+"/anchorless",
	)
	code, report := runPatrol(t, path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\n%s", code, report)
	}
	for label, want := range map[string][3]string{
		"URL_1": {"hit", "miss", "hit"},
		"URL_2": {"miss", "hit", "miss"},
		"URL_3": {"miss", "miss", "miss"},
	} {
		row := cells(reportRow(t, report, label))
		if len(row) != 5 || row[1] != want[0] || row[2] != want[1] || row[3] != want[2] {
			t.Fatalf("%s row = %q, want %v", label, row, want)
		}
	}
	if !strings.Contains(report, "主锚点(js_name)未命中 URL 数: 2") {
		t.Fatalf("report lacks primary-miss count:\n%s", report)
	}
}

func TestRunFetchFailureCountsAsPrimaryMissWithSanitizedNote(t *testing.T) {
	server := fixtureServer(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	path := writeLinkFile(t, server.URL+"/script", closedURL+"/gone")
	code, report := runPatrol(t, path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1\n%s", code, report)
	}
	row := cells(reportRow(t, report, "URL_2"))
	if len(row) != 5 || row[1] != "miss" || row[4] != "fetch failed" {
		t.Fatalf("URL_2 row = %q", row)
	}
}

func TestReportNeverPrintsURLsOrExtractedValues(t *testing.T) {
	server := fixtureServer(t)
	path := writeLinkFile(t, server.URL+"/script", server.URL+"/legacy")
	_, report := runPatrol(t, path)
	for _, forbidden := range []string{server.URL, "127.0.0.1", "/script", "/legacy", "巡检示例号", "Legacy Account"} {
		if strings.Contains(report, forbidden) {
			t.Fatalf("report leaks %q:\n%s", forbidden, report)
		}
	}
}
