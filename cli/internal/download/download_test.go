package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
)

type memoryObjects struct {
	values map[string][]byte
}

func newMemoryObjects() *memoryObjects { return &memoryObjects{values: map[string][]byte{}} }

func (store *memoryObjects) Put(_ context.Context, reader io.Reader, mediaType string) (objects.Object, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return objects.Object{}, err
	}
	digestBytes := sha256.Sum256(data)
	digest := hex.EncodeToString(digestBytes[:])
	store.values[digest] = append([]byte(nil), data...)
	return objects.Object{Digest: digest, Size: int64(len(data)), MediaType: mediaType}, nil
}

func (store *memoryObjects) Validate(_ context.Context, digest string) error {
	if _, ok := store.values[digest]; !ok {
		return objects.ErrIntegrity
	}
	return nil
}

type articleMemoryStore struct {
	current   library.ContentVersion
	state     string
	deleted   bool
	incidents []library.DebugIncident
}

func (store *articleMemoryStore) CurrentContent(context.Context, domain.ArticleID, string) (library.ContentVersion, error) {
	if store.current.ID == "" {
		return library.ContentVersion{}, sql.ErrNoRows
	}
	return store.current, nil
}

func (store *articleMemoryStore) CommitContent(_ context.Context, articleID domain.ArticleID, object objects.Object,
	kind, sourceURL, classification, commentID string, capturedAt time.Time) (library.ContentVersion, error) {
	store.current = library.ContentVersion{ID: "content-a", ArticleID: articleID, ObjectDigest: object.Digest, Kind: kind,
		SourceURL: sourceURL, Classification: classification, CommentID: commentID, CapturedAt: capturedAt, Current: true}
	return store.current, nil
}

func (store *articleMemoryStore) MarkArticleState(_ context.Context, _ domain.ArticleID, state string, deleted bool) error {
	store.state, store.deleted = state, deleted
	return nil
}

func (store *articleMemoryStore) RecordDebugIncident(_ context.Context, incident library.DebugIncident) (library.DebugIncident, error) {
	incident.ID = "incident-a"
	store.incidents = append(store.incidents, incident)
	return incident, nil
}

type resourceMemoryStore struct {
	records map[string]library.ResourceRecord
	missing int
}

func (store *resourceMemoryStore) ResourceByURL(_ context.Context, raw string) (library.ResourceRecord, error) {
	record, ok := store.records[raw]
	if !ok {
		return library.ResourceRecord{}, sql.ErrNoRows
	}
	return record, nil
}

func (store *resourceMemoryStore) CommitResource(_ context.Context, _ domain.ArticleID, raw, _ string, _ int,
	object objects.Object) (library.ResourceRecord, error) {
	record := library.ResourceRecord{ID: "resource-a", SourceURL: raw, ObjectDigest: object.Digest,
		MediaType: object.MediaType, Status: "available"}
	store.records[raw] = record
	return record, nil
}

func (store *resourceMemoryStore) MarkResourceMissing(_ context.Context, _ domain.ArticleID, raw, _ string, _ int) error {
	store.missing++
	store.records[raw] = library.ResourceRecord{ID: "missing-a", SourceURL: raw, Status: "missing"}
	return nil
}

type countingClient struct {
	calls       int
	body        string
	code        int
	err         error
	contentType string
	requests    []network.Request
}

func (client *countingClient) Do(_ context.Context, request network.Request) (network.Result, error) {
	client.calls++
	client.requests = append(client.requests, request)
	if client.err != nil {
		return network.Result{}, client.err
	}
	code := client.code
	if code == 0 {
		code = http.StatusOK
	}
	contentType := client.contentType
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	return network.Result{Route: "direct", RequestID: "request-a", Response: &http.Response{
		StatusCode: code, Header: http.Header{"Content-Type": {contentType}},
		Body: io.NopCloser(strings.NewReader(client.body)),
	}}, nil
}

func TestArticleDownloaderSkipsValidCachedContent(t *testing.T) {
	objectsStore := newMemoryObjects()
	object, _ := objectsStore.Put(context.Background(), strings.NewReader("cached"), "text/html")
	store := &articleMemoryStore{current: library.ContentVersion{ID: "cached", ObjectDigest: object.Digest,
		Kind: "html", Classification: string(processor.ClassificationValid), Current: true}}
	client := &countingClient{}
	result, err := (ArticleDownloader{Network: client, Processor: processor.New(), Objects: objectsStore, Store: store}).Download(
		context.Background(), ArticleRequest{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"})
	if err != nil || !result.Cached || client.calls != 0 {
		t.Fatalf("result=%#v calls=%d err=%v", result, client.calls, err)
	}
}

func TestArticleDownloaderForceRefreshBypassesValidCachedContent(t *testing.T) {
	objectsStore := newMemoryObjects()
	object, _ := objectsStore.Put(context.Background(), strings.NewReader("cached"), "text/html")
	store := &articleMemoryStore{current: library.ContentVersion{ID: "cached", ObjectDigest: object.Digest,
		Kind: "html", Classification: string(processor.ClassificationValid), Current: true}}
	client := &countingClient{body: `<html><body><div id="js_article"><div id="js_content">fresh</div></div>` +
		`<script>window.cgiDataNew={title:'Fresh',user_name:'gh_fixture',content_noencode:'fresh'}</script></body></html>`}
	result, err := (ArticleDownloader{Network: client, Processor: processor.New(), Objects: objectsStore, Store: store}).Download(
		context.Background(), ArticleRequest{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a", Force: true})
	if err != nil || result.Cached || client.calls != 1 || store.current.ObjectDigest == object.Digest {
		t.Fatalf("result=%#v calls=%d current=%#v err=%v", result, client.calls, store.current, err)
	}
}

func TestArticleDownloaderPersistsOnlyValidContent(t *testing.T) {
	html := `<html><body><div id="js_article"><div id="js_content">hello</div></div><script>window.cgiDataNew={title:'Title',user_name:'gh_fixture',content_noencode:'hello',comment_id:'comment-a'}</script></body></html>`
	objectsStore := newMemoryObjects()
	store := &articleMemoryStore{}
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	result, err := (ArticleDownloader{
		Network: &countingClient{body: html}, Processor: processor.New(), Objects: objectsStore, Store: store, Now: func() time.Time { return now },
	}).Download(context.Background(), ArticleRequest{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Classification.State != processor.ClassificationValid || store.current.CommentID != "comment-a" || store.current.CapturedAt != now {
		t.Fatalf("result=%#v store=%#v", result, store)
	}
	if !bytes.Equal(objectsStore.values[store.current.ObjectDigest], []byte(html)) {
		t.Fatal("persisted content differs from downloaded response")
	}
}

func TestArticleDownloaderUsesBrowserNavigationHeadersForWeChatArticles(t *testing.T) {
	html := `<html><body><div id="js_article"><div id="js_content">hello</div></div><script>window.cgiDataNew={title:'Title',user_name:'gh_fixture',content_noencode:'hello'}</script></body></html>`
	objectsStore := newMemoryObjects()
	store := &articleMemoryStore{}
	client := &countingClient{body: html}
	_, err := (ArticleDownloader{Network: client, Processor: processor.New(), Objects: objectsStore, Store: store}).Download(
		context.Background(), ArticleRequest{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %#v", client.requests)
	}
	header := client.requests[0].Header
	if header.Get("User-Agent") != browserArticleUserAgent || header.Get("Referer") != "https://mp.weixin.qq.com/" ||
		header.Get("Accept-Language") != "zh-CN,zh;q=0.9,en;q=0.8" || header.Get("Sec-Fetch-Mode") != "navigate" {
		t.Fatalf("article request headers = %#v", header)
	}
}

func TestArticleDownloaderClassifiesAndCapturesRiskControlWithoutValidCommit(t *testing.T) {
	objectsStore := newMemoryObjects()
	store := &articleMemoryStore{}
	body := `<html><body>当前环境异常，请完成验证后继续访问</body></html>`
	result, err := (ArticleDownloader{
		Network: &countingClient{body: body}, Processor: processor.New(), Objects: objectsStore, Store: store,
		DebugCapture: true, DebugTTL: time.Hour,
	}).Download(context.Background(), ArticleRequest{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a?pass_ticket=secret"})
	if !errors.Is(err, ErrUnavailable) || result.Classification.State != processor.ClassificationRiskControl {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if store.current.ID != "" || store.state != string(processor.ClassificationRiskControl) || len(store.incidents) != 1 {
		t.Fatalf("store=%#v", store)
	}
	if strings.Contains(store.incidents[0].Summary, "secret") || store.incidents[0].ObjectDigest == "" {
		t.Fatalf("incident=%#v", store.incidents[0])
	}
}

func TestResourceDownloaderReusesCacheAndRecordsMissing(t *testing.T) {
	objectsStore := newMemoryObjects()
	object, _ := objectsStore.Put(context.Background(), strings.NewReader("image"), "image/png")
	target := "https://mmbiz.qpic.cn/a.png"
	store := &resourceMemoryStore{records: map[string]library.ResourceRecord{
		target: {ID: "resource-a", SourceURL: target, ObjectDigest: object.Digest, Status: "available"},
	}}
	client := &countingClient{}
	result, err := (ResourceDownloader{Network: client, Objects: objectsStore, Store: store}).Download(
		context.Background(), ResourceRequest{ArticleID: "article-a", URL: target, Role: "image"})
	if err != nil || !result.Cached || client.calls != 0 {
		t.Fatalf("result=%#v calls=%d err=%v", result, client.calls, err)
	}

	missingURL := "https://mmbiz.qpic.cn/missing.png"
	client.err = errors.New("timeout")
	_, err = (ResourceDownloader{Network: client, Objects: objectsStore, Store: store}).Download(
		context.Background(), ResourceRequest{ArticleID: "article-a", URL: missingURL, Role: "image"})
	if err == nil || store.missing != 1 || store.records[missingURL].Status != "missing" {
		t.Fatalf("missing=%d record=%#v err=%v", store.missing, store.records[missingURL], err)
	}
}

func TestResourceDownloaderPersistsDetectedMIME(t *testing.T) {
	objectsStore := newMemoryObjects()
	store := &resourceMemoryStore{records: map[string]library.ResourceRecord{}}
	client := &countingClient{body: "\x89PNG\r\n\x1a\nfixture", contentType: "application/octet-stream"}
	result, err := (ResourceDownloader{Network: client, Objects: objectsStore, Store: store}).Download(
		context.Background(), ResourceRequest{ArticleID: "article-a", URL: "https://mmbiz.qpic.cn/a.png", Role: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Resource.ObjectDigest == "" || result.Resource.Status != "available" || result.Resource.MediaType != "image/png" {
		t.Fatalf("result=%#v", result)
	}
}

func TestRejectsUnsafeDownloadURLsBeforeNetwork(t *testing.T) {
	client := &countingClient{}
	_, err := (ArticleDownloader{Network: client}).Download(context.Background(), ArticleRequest{
		ArticleID: "article-a", URL: "http://169.254.169.254/latest/meta-data",
	})
	if err == nil || client.calls != 0 {
		t.Fatalf("calls=%d err=%v", client.calls, err)
	}
}
