// Command patrol probes live WeChat article pages for account-name anchor
// drift. It fetches each URL from a local link file, runs every anchor of the
// wechat article account-name chain individually (no chain short-circuit),
// and prints a sanitized hit table: URLs appear only as ordinals, and no URL,
// page content, or extracted value is ever printed.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/htmlx"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

const (
	browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	fetchTimeout     = 30 * time.Second
	allowedRedirect  = "mp.weixin.qq.com"
	maxRedirects     = 10
)

func main() {
	urlsPath := flag.String("urls", "", "local link file: one URL per line, # starts a comment")
	flag.Parse()
	if strings.TrimSpace(*urlsPath) == "" {
		fmt.Fprintln(os.Stderr, "usage: patrol -urls <file>")
		os.Exit(2)
	}
	code, err := run(*urlsPath, newPatrolClient(), os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "patrol: %v\n", err)
		os.Exit(2)
	}
	os.Exit(code)
}

// run probes every URL in the link file and writes the sanitized report.
// It returns exit code 1 when any URL misses the primary (chain-head) anchor,
// 0 when every URL hits it, and a non-nil error only for operational failures
// such as an unreadable link file.
func run(urlsPath string, client *http.Client, out io.Writer) (int, error) {
	urls, err := readURLList(urlsPath)
	if err != nil {
		return 0, err
	}
	if len(urls) == 0 {
		return 0, errors.New("link file carries no URLs")
	}
	chain := wechat.ArticleAccountNameChain()
	if len(chain) == 0 {
		return 0, errors.New("article account-name chain is empty")
	}
	results := make([]urlResult, 0, len(urls))
	for _, target := range urls {
		results = append(results, probe(client, chain, target))
	}
	writeReport(out, chain, results)
	for _, result := range results {
		if !result.hits[0] {
			return 1, nil
		}
	}
	return 0, nil
}

func newPatrolClient() *http.Client {
	return &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			host := strings.ToLower(request.URL.Hostname())
			if host == allowedRedirect || host == strings.ToLower(via[0].URL.Hostname()) {
				return nil
			}
			return errors.New("redirect left the allowed host")
		},
	}
}

func readURLList(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open link file: %w", err)
	}
	defer file.Close()
	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read link file: %w", err)
	}
	return urls, nil
}

type urlResult struct {
	hits []bool
	// note is a sanitized failure category; it must never carry the URL, an
	// upstream error string, or page content.
	note string
}

func probe(client *http.Client, chain htmlx.Chain, target string) urlResult {
	result := urlResult{hits: make([]bool, len(chain))}
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		result.note = "invalid URL"
		return result
	}
	request.Header.Set("User-Agent", browserUserAgent)
	response, err := client.Do(request)
	if err != nil {
		result.note = "fetch failed"
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result.note = fmt.Sprintf("HTTP %d", response.StatusCode)
		return result
	}
	document, err := htmlx.Parse(response.Body, htmlx.DefaultLimits())
	if err != nil {
		result.note = "parse failed"
		return result
	}
	for index, anchor := range chain {
		value, matched := anchor.Extract(document)
		result.hits[index] = matched && value != ""
	}
	return result
}

func writeReport(out io.Writer, chain htmlx.Chain, results []urlResult) {
	fmt.Fprintln(out, "版式巡检报告（单次失败 ≠ 漂移，重跑确认后再回流）")
	writer := tabwriter.NewWriter(out, 2, 0, 2, ' ', 0)
	header := []string{"URL"}
	for _, anchor := range chain {
		header = append(header, anchor.Name)
	}
	fmt.Fprintln(writer, strings.Join(append(header, "备注"), "\t"))
	hitTotals := make([]int, len(chain))
	primaryMisses := 0
	for index, result := range results {
		row := []string{fmt.Sprintf("URL_%d", index+1)}
		for anchorIndex, hit := range result.hits {
			cell := "miss"
			if hit {
				cell = "hit"
				hitTotals[anchorIndex]++
			}
			row = append(row, cell)
		}
		note := result.note
		if note == "" {
			note = "-"
		}
		fmt.Fprintln(writer, strings.Join(append(row, note), "\t"))
		if !result.hits[0] {
			primaryMisses++
		}
	}
	writer.Flush()
	summary := make([]string, 0, len(chain))
	for index, anchor := range chain {
		summary = append(summary, fmt.Sprintf("%s %d/%d", anchor.Name, hitTotals[index], len(results)))
	}
	fmt.Fprintf(out, "汇总: %s\n", strings.Join(summary, " | "))
	fmt.Fprintf(out, "主锚点(%s)未命中 URL 数: %d\n", chain[0].Name, primaryMisses)
}
