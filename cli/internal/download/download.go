package download

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

var ErrUnavailable = errors.New("download response is not a valid article")

type ArticleStore interface {
	CurrentContent(context.Context, domain.ArticleID, string) (library.ContentVersion, error)
	CommitContent(context.Context, domain.ArticleID, objects.Object, string, string, string, string, time.Time) (library.ContentVersion, error)
	MarkArticleState(context.Context, domain.ArticleID, string, bool) error
	RecordDebugIncident(context.Context, library.DebugIncident) (library.DebugIncident, error)
}

// ArticleIdentityRepair is an optional post-parse storage capability used for
// provisional single-article ingestion. Download persistence remains usable
// with narrower stores, while the local SQLite library repairs real account
// identity and canonical article IDs when normalized payload data is present.
type ArticleIdentityRepair interface {
	RepairSingleArticle(context.Context, library.SingleArticleRepair) (domain.Article, error)
}

type ResourceStore interface {
	ResourceByURL(context.Context, string) (library.ResourceRecord, error)
	CommitResource(context.Context, domain.ArticleID, string, string, int, objects.Object) (library.ResourceRecord, error)
	MarkResourceMissing(context.Context, domain.ArticleID, string, string, int) error
}

type ObjectStore interface {
	Put(context.Context, io.Reader, string) (objects.Object, error)
	Validate(context.Context, string) error
}

type Processor interface {
	Process(context.Context, io.Reader) (processor.Result, error)
}

type ArticleDownloader struct {
	Network      network.Client
	Processor    Processor
	Objects      ObjectStore
	Store        ArticleStore
	Now          func() time.Time
	DebugCapture bool
	DebugTTL     time.Duration
}

type ArticleRequest struct {
	ArticleID domain.ArticleID
	URL       string
	Force     bool
}

type ArticleResult struct {
	Cached         bool                     `json:"cached"`
	Classification processor.Classification `json:"classification"`
	Content        library.ContentVersion   `json:"content,omitempty"`
	Route          string                   `json:"route,omitempty"`
	RequestID      string                   `json:"requestId,omitempty"`
}

const browserArticleUserAgent = network.BrowserArticleUserAgent

func browserArticleHeaders(target *url.URL) http.Header {
	header := make(http.Header)
	if target == nil || !strings.EqualFold(target.Hostname(), "mp.weixin.qq.com") {
		return header
	}
	header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	header.Set("Cache-Control", "max-age=0")
	header.Set("Referer", "https://mp.weixin.qq.com/")
	header.Set("Sec-Fetch-Dest", "document")
	header.Set("Sec-Fetch-Mode", "navigate")
	header.Set("Sec-Fetch-Site", "same-origin")
	header.Set("Upgrade-Insecure-Requests", "1")
	header.Set("User-Agent", browserArticleUserAgent)
	return header
}

func (downloader ArticleDownloader) Download(ctx context.Context, request ArticleRequest) (ArticleResult, error) {
	if request.ArticleID == "" {
		return ArticleResult{}, errors.New("article ID is required")
	}
	target, err := parseRemoteURL(request.URL)
	if err != nil {
		return ArticleResult{}, err
	}
	if !request.Force && downloader.Store != nil {
		cached, cacheErr := downloader.Store.CurrentContent(ctx, request.ArticleID, "html")
		if cacheErr == nil && cached.Classification == string(processor.ClassificationValid) {
			if downloader.Objects == nil || downloader.Objects.Validate(ctx, cached.ObjectDigest) == nil {
				return ArticleResult{Cached: true, Classification: processor.Classification{State: processor.ClassificationValid}, Content: cached}, nil
			}
		}
	}
	if downloader.Network == nil || downloader.Processor == nil || downloader.Objects == nil || downloader.Store == nil {
		return ArticleResult{}, errors.New("article downloader dependencies are incomplete")
	}
	networkResult, err := downloader.Network.Do(ctx, network.Request{
		Class: network.PublicContent, Method: http.MethodGet, URL: target, Header: browserArticleHeaders(target), MaxResponseBytes: 32 << 20,
	})
	if err != nil {
		return ArticleResult{}, fmt.Errorf("download article: %w", err)
	}
	if networkResult.Response == nil || networkResult.Response.Body == nil {
		return ArticleResult{}, errors.New("download article returned no response body")
	}
	defer networkResult.Response.Body.Close()
	if networkResult.Response.StatusCode < 200 || networkResult.Response.StatusCode >= 300 {
		return ArticleResult{}, fmt.Errorf("download article returned HTTP %d", networkResult.Response.StatusCode)
	}
	mediaType := responseMediaType(networkResult.Response, "text/html; charset=utf-8")
	body, err := io.ReadAll(io.LimitReader(networkResult.Response.Body, (32<<20)+1))
	if err != nil {
		return ArticleResult{}, fmt.Errorf("buffer article response: %w", err)
	}
	if len(body) > 32<<20 {
		return ArticleResult{}, errors.New("article response exceeded 33554432 bytes")
	}
	parsed, parseErr := downloader.Processor.Process(ctx, bytes.NewReader(body))
	if parseErr != nil || parsed.Classification.State != processor.ClassificationValid || parsed.Article == nil {
		if markErr := downloader.markClassification(ctx, request.ArticleID, parsed.Classification); markErr != nil {
			return ArticleResult{}, errors.Join(parseErr, markErr)
		}
		if downloader.DebugCapture {
			if _, debugErr := downloader.captureDebug(ctx, request, parsed.Classification, networkResult, bytes.NewReader(body), mediaType); debugErr != nil {
				return ArticleResult{}, errors.Join(parseErr, debugErr)
			}
		}
		if parseErr != nil {
			return ArticleResult{Classification: parsed.Classification, Route: networkResult.Route, RequestID: networkResult.RequestID}, parseErr
		}
		return ArticleResult{Classification: parsed.Classification, Route: networkResult.Route, RequestID: networkResult.RequestID}, ErrUnavailable
	}
	object, err := downloader.Objects.Put(ctx, bytes.NewReader(body), mediaType)
	if err != nil {
		return ArticleResult{}, fmt.Errorf("persist article object: %w", err)
	}
	capturedAt := downloader.now()
	content, err := downloader.Store.CommitContent(ctx, request.ArticleID, object, "html", target.String(),
		string(parsed.Classification.State), parsed.Article.Comments.ID, capturedAt)
	if err != nil {
		return ArticleResult{}, err
	}
	if repairer, ok := downloader.Store.(ArticleIdentityRepair); ok {
		identity := firstNonBlank(parsed.Article.Account.BusinessID, parsed.Article.Identity.BusinessID,
			parsed.Article.Account.Username)
		if strings.TrimSpace(identity) != "" {
			aid := parsed.Article.Identity.AppMessage
			if aid == "" {
				aid = parsed.Article.Identity.MessageID
			}
			repaired, repairErr := repairer.RepairSingleArticle(ctx, library.SingleArticleRepair{
				ArticleID: request.ArticleID, RealFakeID: identity,
				AccountName: parsed.Article.Account.Nickname, Aid: aid, Title: parsed.Article.Title, Author: parsed.Article.Author,
			})
			if repairErr != nil {
				return ArticleResult{}, fmt.Errorf("repair downloaded article identity: %w", repairErr)
			}
			if repaired.ID != "" && repaired.ID != content.ArticleID {
				content, err = repairerCurrentContent(ctx, downloader.Store, repaired.ID)
				if err != nil {
					return ArticleResult{}, fmt.Errorf("load repaired article content: %w", err)
				}
			}
		}
	}
	return ArticleResult{Classification: parsed.Classification, Content: content,
		Route: networkResult.Route, RequestID: networkResult.RequestID}, nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func repairerCurrentContent(ctx context.Context, store ArticleStore, articleID domain.ArticleID) (library.ContentVersion, error) {
	return store.CurrentContent(ctx, articleID, "html")
}

func (downloader ArticleDownloader) markClassification(ctx context.Context, articleID domain.ArticleID, classification processor.Classification) error {
	if classification.State == "" || downloader.Store == nil {
		return nil
	}
	deleted := classification.State == processor.ClassificationDeleted
	return downloader.Store.MarkArticleState(ctx, articleID, string(classification.State), deleted)
}

func (downloader ArticleDownloader) captureDebug(
	ctx context.Context,
	request ArticleRequest,
	classification processor.Classification,
	networkResult network.Result,
	reader io.Reader,
	mediaType string,
) (library.DebugIncident, error) {
	debugObject, err := downloader.Objects.Put(ctx, reader, mediaType)
	if err != nil {
		return library.DebugIncident{}, fmt.Errorf("persist debug response: %w", err)
	}
	now := downloader.now()
	expires := time.Time{}
	if downloader.DebugTTL > 0 {
		expires = now.Add(downloader.DebugTTL)
	}
	summary := safety.RedactText(fmt.Sprintf("article=%s url=%s classification=%s reason=%s",
		request.ArticleID, safety.RedactURL(request.URL), classification.State, classification.Reason))
	return downloader.Store.RecordDebugIncident(ctx, library.DebugIncident{
		Operation: "article_download", Classification: string(classification.State), RequestID: networkResult.RequestID,
		ObjectDigest: debugObject.Digest, Summary: summary, CreatedAt: now, ExpiresAt: expires,
	})
}

func (downloader ArticleDownloader) now() time.Time {
	if downloader.Now != nil {
		return downloader.Now()
	}
	return time.Now()
}

type ResourceDownloader struct {
	Network network.Client
	Objects ObjectStore
	Store   ResourceStore
}

type ResourceRequest struct {
	ArticleID domain.ArticleID
	URL       string
	Role      string
	Ordinal   int
	Force     bool
}

type ResourceResult struct {
	Cached    bool                   `json:"cached"`
	Missing   bool                   `json:"missing"`
	Resource  library.ResourceRecord `json:"resource"`
	Route     string                 `json:"route,omitempty"`
	RequestID string                 `json:"requestId,omitempty"`
}

func (downloader ResourceDownloader) Download(ctx context.Context, request ResourceRequest) (ResourceResult, error) {
	if request.ArticleID == "" {
		return ResourceResult{}, errors.New("article ID is required")
	}
	target, err := parseRemoteURL(request.URL)
	if err != nil {
		return ResourceResult{}, err
	}
	if downloader.Store == nil || downloader.Objects == nil || downloader.Network == nil {
		return ResourceResult{}, errors.New("resource downloader dependencies are incomplete")
	}
	if !request.Force {
		cached, cacheErr := downloader.Store.ResourceByURL(ctx, target.String())
		if cacheErr == nil && cached.Status == "available" && cached.ObjectDigest != "" && downloader.Objects.Validate(ctx, cached.ObjectDigest) == nil {
			return ResourceResult{Cached: true, Resource: cached}, nil
		}
	}
	result, err := downloader.Network.Do(ctx, network.Request{
		Class: network.PublicResource, Method: http.MethodGet, URL: target, MaxResponseBytes: 128 << 20,
	})
	if err != nil {
		_ = downloader.Store.MarkResourceMissing(ctx, request.ArticleID, target.String(), request.Role, request.Ordinal)
		return ResourceResult{Missing: true}, fmt.Errorf("download resource: %w", err)
	}
	if result.Response == nil || result.Response.Body == nil {
		_ = downloader.Store.MarkResourceMissing(ctx, request.ArticleID, target.String(), request.Role, request.Ordinal)
		return ResourceResult{Missing: true}, errors.New("download resource returned no response body")
	}
	defer result.Response.Body.Close()
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		_ = downloader.Store.MarkResourceMissing(ctx, request.ArticleID, target.String(), request.Role, request.Ordinal)
		return ResourceResult{Missing: true, Route: result.Route, RequestID: result.RequestID},
			fmt.Errorf("download resource returned HTTP %d", result.Response.StatusCode)
	}
	buffered := bufio.NewReader(result.Response.Body)
	mediaType := detectMediaType(buffered, result.Response.Header.Get("Content-Type"), target.Path)
	object, err := downloader.Objects.Put(ctx, buffered, mediaType)
	if err != nil {
		return ResourceResult{}, fmt.Errorf("persist resource object: %w", err)
	}
	record, err := downloader.Store.CommitResource(ctx, request.ArticleID, target.String(), request.Role, request.Ordinal, object)
	if err != nil {
		return ResourceResult{}, err
	}
	return ResourceResult{Resource: record, Route: result.Route, RequestID: result.RequestID}, nil
}

func parseRemoteURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("download URL must be an absolute URL without user information")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback(parsed.Hostname())) {
		return nil, errors.New("download URL must use HTTPS or approved loopback HTTP")
	}
	parsed.Fragment = ""
	return parsed, nil
}

func isLoopback(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func responseMediaType(response *http.Response, fallback string) string {
	contentType := response.Header.Get("Content-Type")
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil && parsed != "" {
		return parsed
	}
	return fallback
}

func mediaTypeFromPath(path string) string {
	if detected := mime.TypeByExtension(filepath.Ext(path)); detected != "" {
		if parsed, _, err := mime.ParseMediaType(detected); err == nil {
			return parsed
		}
	}
	return "application/octet-stream"
}

func detectMediaType(reader *bufio.Reader, header, path string) string {
	headerType := ""
	if parsed, _, err := mime.ParseMediaType(header); err == nil {
		headerType = parsed
	}
	pathType := mediaTypeFromPath(path)
	peek, _ := reader.Peek(512)
	detected := ""
	if len(peek) > 0 {
		detected = http.DetectContentType(peek)
		if parsed, _, err := mime.ParseMediaType(detected); err == nil {
			detected = parsed
		}
	}
	for _, candidate := range []string{headerType, detected, pathType} {
		if candidate != "" && candidate != "application/octet-stream" && candidate != "text/plain" {
			return candidate
		}
	}
	if pathType != "application/octet-stream" {
		return pathType
	}
	if detected != "" {
		return detected
	}
	if headerType != "" {
		return headerType
	}
	return "application/octet-stream"
}
