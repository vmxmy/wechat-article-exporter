package download

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestFakeUpstreamArticleCasesCachedSuccessDeletedRestrictedRiskAndProxyFailure(t *testing.T) {
	validHTML := `<html><body><div id="js_article"><div id="js_content">hello</div></div><script>window.cgiDataNew={title:'Title',user_name:'gh_fixture',content_noencode:'hello',comment_id:'comment-a'}</script></body></html>`
	cases := []struct {
		name      string
		body      string
		clientErr error
		cached    bool
		wantState processor.ClassificationState
		wantErr   bool
	}{
		{name: "cached", cached: true, wantState: processor.ClassificationValid},
		{name: "success", body: validHTML, wantState: processor.ClassificationValid},
		{name: "deleted", body: `<html><body>该内容已被作者删除</body></html>`, wantState: processor.ClassificationDeleted, wantErr: true},
		{name: "restricted", body: `<html><body>此内容因违规无法查看</body></html>`, wantState: processor.ClassificationUnavailable, wantErr: true},
		{name: "risk", body: `<html><body>当前环境异常，请完成验证后继续访问</body></html>`, wantState: processor.ClassificationRiskControl, wantErr: true},
		{name: "proxy-failure", clientErr: errors.New("proxy unavailable"), wantErr: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			objectsStore := newMemoryObjects()
			store := &articleMemoryStore{}
			if test.cached {
				object, _ := objectsStore.Put(context.Background(), strings.NewReader("cached"), "text/html")
				store.current = library.ContentVersion{ID: "cached", ObjectDigest: object.Digest,
					Kind: "html", Classification: string(processor.ClassificationValid), Current: true}
			}
			client := &countingClient{body: test.body, err: test.clientErr}
			result, err := (ArticleDownloader{
				Network: client, Processor: processor.New(), Objects: objectsStore, Store: store,
			}).Download(context.Background(), ArticleRequest{ArticleID: "article-a", URL: "https://mp.weixin.qq.com/s/a"})
			if (err != nil) != test.wantErr {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if test.clientErr == nil && result.Classification.State != test.wantState {
				t.Fatalf("classification=%#v", result.Classification)
			}
			if test.cached && (!result.Cached || client.calls != 0) {
				t.Fatalf("cached result=%#v calls=%d", result, client.calls)
			}
		})
	}
}

func TestFakeUpstreamMetadataCommentsPaidAndCredentialExpiry(t *testing.T) {
	route := &scriptedRoute{articleBodies: map[string]string{
		"/s/metrics": `{"user_info":{"appmsg_bar_data":{"read_num":1200,"old_like_count":31,"share_count":17,"like_count":42,"comment_count":6}}}`,
		"/s/paid":    `<html><body><div id="js_content">paid body</div></body></html>`,
		"/s/expired": `<html><body><form action="/cgi-bin/bizlogin">login</form></body></html>`,
	}, commentBodies: map[string][]string{
		"getcomment": {
			`{"base_resp":{"ret":0},"continue_flag":true,"buffer":"buffer-2","elected_comment":[{"content_id":"comment-1","nick_name":"甲","content":"one","reply_new":{"reply_total_cnt":1,"max_reply_id":0,"reply_list":[]}}]}`,
			`{"base_resp":{"ret":0},"continue_flag":false,"buffer":"","elected_comment":[{"content_id":"comment-1","nick_name":"甲","content":"one"},{"content_id":"comment-2","nick_name":"乙","content":"two"}]}`,
		},
		"getcommentreply": {
			`{"base_resp":{"ret":-1},"err_msg":"temporary upstream failure"}`,
			`{"base_resp":{"ret":0},"content_id":"comment-1","reply_list":{"max_reply_id":1,"reply_list":[{"reply_id":1,"nick_name":"作者","content":"resumed"}]}}`,
		},
	}}
	endpoint := wechat.ContentEndpoint{Network: route}
	loader := &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a", AccountID: "account-a"}, record: downloadCredential()}

	metricStore := &metadataMemoryStore{}
	metadata, err := (MetadataDownloader{Credentials: loader, Source: endpoint, Store: metricStore}).Download(
		context.Background(), MetadataRequest{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/metrics"})
	if err != nil || metadata.Snapshot.ReadCount != 1200 || metadata.Snapshot.CredentialID != "credential-a" {
		t.Fatalf("metadata=%#v err=%v", metadata, err)
	}
	paid, err := (PaidContentDownloader{Credentials: loader, Source: endpoint}).Fetch(
		context.Background(), PaidContentRequest{AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/paid"})
	if err != nil || !strings.Contains(string(paid.Body), "paid body") || paid.CredentialID != "credential-a" {
		t.Fatalf("paid=%#v err=%v", paid, err)
	}

	commentStore := newCommentMemoryStore()
	comments := CommentsDownloader{Credentials: loader, Source: endpoint, Store: commentStore, MaxRetries: 1}
	request := CommentsRequest{ArticleID: "article-a", AccountID: "account-a", BusinessID: "fixture-biz",
		AppMessageID: 10001, ItemIndex: 1, CommentID: "stream"}
	first, err := comments.Download(context.Background(), request)
	if err == nil || !first.Partial || first.PagesCommitted != 2 || first.CommentsStored != 2 || first.ReplyThreadsFailed != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := comments.Download(context.Background(), request)
	if err != nil || second.Partial || second.PagesCommitted != 0 || second.ReplyThreadsCompleted != 1 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	_, err = (MetadataDownloader{Credentials: loader, Source: endpoint, Store: metricStore}).Download(
		context.Background(), MetadataRequest{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/expired"})
	if !errors.Is(err, credentials.ErrCredentialExpired) || metricStore.status != credentials.StatusInvalid {
		t.Fatalf("credential expiry status=%q err=%v", metricStore.status, err)
	}
	for _, request := range route.requests {
		if request.Class == network.Comments {
			for _, secret := range []string{loader.record.Key, loader.record.PassTicket, loader.record.WapSID2} {
				if strings.Contains(request.URL.String(), secret) {
					t.Fatalf("comment URL leaked %q: %s", secret, request.URL)
				}
			}
		}
	}
}

type scriptedRoute struct {
	mu            sync.Mutex
	articleBodies map[string]string
	commentBodies map[string][]string
	requests      []network.Request
}

func (route *scriptedRoute) Do(_ context.Context, request network.Request) (network.Result, error) {
	route.mu.Lock()
	defer route.mu.Unlock()
	route.requests = append(route.requests, request)
	body := ""
	if request.URL.Path == "/mp/appmsg_comment" {
		action := request.URL.Query().Get("action")
		queue := route.commentBodies[action]
		if len(queue) == 0 {
			return network.Result{}, errors.New("fake upstream exhausted")
		}
		body = queue[0]
		route.commentBodies[action] = queue[1:]
	} else {
		body = route.articleBodies[request.URL.Path]
	}
	return network.Result{Route: "trusted-fake", RequestID: "fake-request", Response: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)),
	}}, nil
}

var _ network.Client = (*scriptedRoute)(nil)
var _ CredentialArticleSource = wechat.ContentEndpoint{}
var _ CommentSource = wechat.ContentEndpoint{}

func TestPaidRouteRejectsUntrustedProxyInFakeUpstreamSetup(t *testing.T) {
	target, _ := url.Parse("https://mp.weixin.qq.com/s/paid")
	proxyCalls := 0
	router := network.Router{Routes: []network.Candidate{{
		Client: network.StaticClient{RouteName: "untrusted", Call: func(context.Context, network.Request) (network.Result, error) {
			proxyCalls++
			return network.Result{}, nil
		}},
		Trust: network.TrustPublicOnly, Enabled: true, Classes: network.ClassesMap([]network.RequestClass{network.PaidContent}),
	}}}
	_, err := router.Do(context.Background(), network.Request{Class: network.PaidContent, URL: target})
	if !errors.Is(err, network.ErrSensitiveRouteRequired) || proxyCalls != 0 {
		t.Fatalf("proxyCalls=%d err=%v", proxyCalls, err)
	}
}

// Keep compile-time coverage for the storage seam used by the fake-upstream
// acceptance path without expanding into application/job orchestration.
var _ interface {
	CommitContent(context.Context, domain.ArticleID, objects.Object, string, string, string, string, time.Time) (library.ContentVersion, error)
} = (*articleMemoryStore)(nil)
