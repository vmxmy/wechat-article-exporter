package download

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestMetadataDownloaderPreflightsCredentialAndPersistsProvenance(t *testing.T) {
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	loader := &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a", AccountID: "account-a"}, record: downloadCredential()}
	source := &fakeCredentialArticleSource{response: wechat.ContentResponse{
		Body:  []byte(`{"user_info":{"appmsg_bar_data":{"read_num":1200,"old_like_count":31,"share_count":17,"like_count":42,"comment_count":6}}}`),
		Route: "trusted", RequestID: "request-a",
	}}
	store := &metadataMemoryStore{}
	result, err := (MetadataDownloader{Credentials: loader, Source: source, Store: store, Now: func() time.Time { return now }}).Download(
		context.Background(), MetadataRequest{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/a"})
	if err != nil {
		t.Fatal(err)
	}
	if source.request.Class != network.EngagementMetrics || result.Route != "trusted" || result.RequestID != "request-a" {
		t.Fatalf("result=%#v request=%#v", result, source.request)
	}
	if store.snapshot.CredentialID != "credential-a" || !store.snapshot.CapturedAt.Equal(now) || store.snapshot.ReadCount != 1200 || store.snapshot.CommentCount != 6 {
		t.Fatalf("snapshot=%#v", store.snapshot)
	}
}

func TestMetadataDownloaderRejectsMissingCredentialBeforeNetwork(t *testing.T) {
	loader := &fixedCredentialLoader{err: credentials.ErrCredentialMissing}
	source := &fakeCredentialArticleSource{}
	_, err := (MetadataDownloader{Credentials: loader, Source: source, Store: &metadataMemoryStore{}}).Download(
		context.Background(), MetadataRequest{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/a"})
	if !errors.Is(err, credentials.ErrCredentialMissing) || source.calls != 0 {
		t.Fatalf("calls=%d err=%v", source.calls, err)
	}
}

func TestMetadataDownloaderMarksExpiredCredentialInvalid(t *testing.T) {
	loader := &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a", AccountID: "account-a"}, record: downloadCredential()}
	source := &fakeCredentialArticleSource{err: credentials.ErrCredentialExpired}
	store := &metadataMemoryStore{}
	_, err := (MetadataDownloader{Credentials: loader, Source: source, Store: store}).Download(
		context.Background(), MetadataRequest{ArticleID: "article-a", AccountID: "account-a", URL: "https://mp.weixin.qq.com/s/a"})
	if !errors.Is(err, credentials.ErrCredentialExpired) || store.status != credentials.StatusInvalid {
		t.Fatalf("status=%q err=%v", store.status, err)
	}
}

type fixedCredentialLoader struct {
	metadata credentials.Metadata
	record   credentials.Record
	err      error
}

func (loader *fixedCredentialLoader) LoadForAccount(context.Context, domain.AccountID) (credentials.Metadata, credentials.Record, error) {
	return loader.metadata, loader.record, loader.err
}

type fakeCredentialArticleSource struct {
	request  wechat.CredentialArticleRequest
	response wechat.ContentResponse
	err      error
	calls    int
}

func (source *fakeCredentialArticleSource) FetchArticle(_ context.Context, request wechat.CredentialArticleRequest) (wechat.ContentResponse, error) {
	source.calls++
	source.request = request
	return source.response, source.err
}

type metadataMemoryStore struct {
	snapshot library.MetricSnapshot
	status   credentials.Status
}

func (store *metadataMemoryStore) CommitMetricSnapshot(_ context.Context, snapshot library.MetricSnapshot) (library.MetricSnapshot, error) {
	snapshot.ID = "metric-a"
	store.snapshot = snapshot
	return snapshot, nil
}

func (store *metadataMemoryStore) UpdateCredentialStatus(_ context.Context, _ string, status credentials.Status, validatedAt time.Time) (credentials.Metadata, error) {
	store.status = status
	return credentials.Metadata{ID: "credential-a", Status: status, ValidatedAt: validatedAt}, nil
}

func downloadCredential() credentials.Record {
	return credentials.Record{Biz: "fixture-biz", UIN: "1", Key: "key", PassTicket: "ticket", WapSID2: "sid", AppMsgToken: "token"}
}
