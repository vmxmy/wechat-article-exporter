package wechat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
)

type contentNetwork struct {
	requests []network.Request
	bodies   []string
	codes    []int
	err      error
}

func (client *contentNetwork) Do(_ context.Context, request network.Request) (network.Result, error) {
	client.requests = append(client.requests, request)
	if client.err != nil {
		return network.Result{}, client.err
	}
	index := len(client.requests) - 1
	body := ""
	if index < len(client.bodies) {
		body = client.bodies[index]
	}
	code := http.StatusOK
	if index < len(client.codes) && client.codes[index] != 0 {
		code = client.codes[index]
	}
	return network.Result{Route: "fixture-route", RequestID: "fixture-request", Response: &http.Response{
		StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)),
	}}, nil
}

func TestContentEndpointUsesSensitiveClassesWithoutLeakingArticleCredentialIntoURL(t *testing.T) {
	client := &contentNetwork{bodies: []string{contentFixture(t, "engagement-success.json")}}
	endpoint := ContentEndpoint{Network: client}
	response, err := endpoint.FetchArticle(context.Background(), CredentialArticleRequest{
		URL: "https://mp.weixin.qq.com/s/fixture", Credential: contentCredential(), Class: network.EngagementMetrics,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Route != "fixture-route" || len(client.requests) != 1 || client.requests[0].Class != network.EngagementMetrics {
		t.Fatalf("response=%#v requests=%#v", response, client.requests)
	}
	request := client.requests[0]
	if request.Header.Get("Cookie") == "" {
		t.Fatal("credential cookie was not attached")
	}
	for _, secret := range []string{"fixture-key", "fixture-ticket", "fixture-token", "fixture-sid"} {
		if strings.Contains(request.URL.String(), secret) {
			t.Fatalf("article URL leaked %q: %s", secret, request.URL)
		}
	}
	metrics, err := DecodeEngagement(response.Body)
	if err != nil || metrics.ReadCount != 1200 || metrics.OldLikeCount != 31 || metrics.ShareCount != 17 || metrics.LikeCount != 42 || metrics.CommentCount != 6 {
		t.Fatalf("metrics=%#v err=%v", metrics, err)
	}
}

func TestContentEndpointBuildsCommentContinuationAndReplyRequests(t *testing.T) {
	client := &contentNetwork{bodies: []string{
		contentFixture(t, "comments-page-one.json"), contentFixture(t, "replies-page.json"),
	}}
	base, _ := url.Parse("http://127.0.0.1")
	endpoint := ContentEndpoint{Network: client, BaseURL: base}
	page, provenance, err := endpoint.FetchComments(context.Background(), CommentPageRequest{
		BusinessID: "fixture-biz", AppMessageID: 10001, ItemIndex: 1, CommentID: "fixture-stream",
		Buffer: "fixture-buffer-1", Credential: contentCredential(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Continue || page.Buffer != "fixture-buffer-2" || len(page.Comments) != 1 || page.Comments[0].ReplyTotal != 2 {
		t.Fatalf("page=%#v", page)
	}
	if provenance.Route != "fixture-route" || client.requests[0].Class != network.Comments || client.requests[0].URL.Query().Get("buffer") != "fixture-buffer-1" {
		t.Fatalf("provenance=%#v request=%#v", provenance, client.requests[0])
	}
	for _, secret := range []string{"fixture-key", "fixture-ticket", "fixture-token", "fixture-sid", "90001"} {
		if strings.Contains(client.requests[0].URL.String(), secret) {
			t.Fatalf("comment URL leaked credential %q: %s", secret, client.requests[0].URL)
		}
	}
	if client.requests[0].Header.Get("X-WeChat-Key") != "fixture-key" || client.requests[0].Header.Get("Cookie") == "" {
		t.Fatalf("comment credential headers=%#v", client.requests[0].Header)
	}
	replies, _, err := endpoint.FetchReplies(context.Background(), ReplyPageRequest{
		BusinessID: "fixture-biz", AppMessageID: 10001, ItemIndex: 1, CommentID: "fixture-stream",
		ContentID: "fixture-comment-001", MaxReplyID: 1, Credential: contentCredential(),
	})
	if err != nil || replies.MaxReplyID != 2 || len(replies.Replies) != 1 || replies.Replies[0].LikeCount != 3 {
		t.Fatalf("replies=%#v err=%v", replies, err)
	}
	if client.requests[1].URL.Query().Get("max_reply_id") != "1" {
		t.Fatalf("reply request=%s", client.requests[1].URL)
	}
	for _, secret := range []string{"fixture-key", "fixture-ticket", "fixture-token", "fixture-sid", "90001"} {
		if strings.Contains(client.requests[1].URL.String(), secret) {
			t.Fatalf("reply URL leaked credential %q: %s", secret, client.requests[1].URL)
		}
	}
}

func TestContentEndpointMapsCredentialExpiryBeforeParsing(t *testing.T) {
	client := &contentNetwork{bodies: []string{contentFixture(t, "credential-expired.html")}}
	endpoint := ContentEndpoint{Network: client}
	_, err := endpoint.FetchArticle(context.Background(), CredentialArticleRequest{
		URL: "https://mp.weixin.qq.com/s/fixture", Credential: contentCredential(), Class: network.PaidContent,
	})
	if !errors.Is(err, credentials.ErrCredentialExpired) {
		t.Fatalf("error=%v", err)
	}
}

func contentCredential() credentials.Record {
	return credentials.Record{
		Biz: "fixture-biz", UIN: "90001", Key: "fixture-key", PassTicket: "fixture-ticket",
		WapSID2: "fixture-sid", AppMsgToken: "fixture-token", Cookie: "fixture_cookie=one",
	}
}

func contentFixture(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", "content", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
