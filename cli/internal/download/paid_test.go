package download

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestPaidContentDownloaderUsesSensitiveRouteClass(t *testing.T) {
	source := &fakeCredentialArticleSource{response: wechat.ContentResponse{Body: []byte("paid body"), Route: "trusted"}}
	result, err := (PaidContentDownloader{
		Credentials: &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a"}, record: downloadCredential()}, Source: source,
	}).Fetch(context.Background(), PaidContentRequest{AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/paid"})
	if err != nil || source.request.Class != network.PaidContent || string(result.Body) != "paid body" || result.CredentialID != "credential-a" {
		t.Fatalf("result=%#v request=%#v err=%v", result, source.request, err)
	}
}

func TestPaidContentDownloaderDoesNotFallBackToPublicRequestWithoutCredential(t *testing.T) {
	source := &fakeCredentialArticleSource{}
	_, err := (PaidContentDownloader{Credentials: &fixedCredentialLoader{err: credentials.ErrCredentialMissing}, Source: source}).Fetch(
		context.Background(), PaidContentRequest{AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/paid"})
	if !errors.Is(err, credentials.ErrCredentialMissing) || source.calls != 0 {
		t.Fatalf("calls=%d err=%v", source.calls, err)
	}
}

func TestPaidArticleDownloaderValidatesAndCommitsSensitiveResponse(t *testing.T) {
	html := `<html><body><div id="js_article"><div id="js_content">paid</div></div><script>window.cgiDataNew={title:'Paid',user_name:'gh_fixture',content_noencode:'paid',comment_id:'comment-paid',is_pay_subscribe:1}</script></body></html>`
	source := &fakeCredentialArticleSource{response: wechat.ContentResponse{
		Body: []byte(html), MediaType: "text/html", Route: "trusted", RequestID: "request-paid",
	}}
	objectsStore := newMemoryObjects()
	store := &articleMemoryStore{}
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	result, err := (PaidArticleDownloader{
		Fetcher: PaidContentDownloader{
			Credentials: &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a"}, record: downloadCredential()},
			Source:      source,
		},
		Processor: processor.New(), Objects: objectsStore, Store: store, Now: func() time.Time { return now },
	}).Download(context.Background(), PaidContentJobRequest{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/paid"})
	if err != nil {
		t.Fatal(err)
	}
	if source.request.Class != network.PaidContent || result.Classification.State != processor.ClassificationValid ||
		result.CredentialID != "credential-a" || result.Route != "trusted" || store.current.CommentID != "comment-paid" || !store.current.CapturedAt.Equal(now) {
		t.Fatalf("result=%#v store=%#v request=%#v", result, store, source.request)
	}
}
