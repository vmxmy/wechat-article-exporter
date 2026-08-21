package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestListAlbumArticlesNormalizesForwardReverseAndContinuation(t *testing.T) {
	fixtures := map[string]string{"": "album-forward.json", "10002": "album-continuation.json", "reverse": "album-reverse.json"}
	client, server := albumFixtureClient(t, func(request *http.Request) string {
		if request.URL.Query().Get("is_reverse") == "1" {
			return fixtures["reverse"]
		}
		return fixtures[request.URL.Query().Get("begin_msgid")]
	})
	defer server.Close()
	query := BuildAlbumQuery(AlbumListRequest{FakeID: "fixture-account-a", AlbumID: "fixture-album-a", Order: AlbumReverse,
		BeginMessageID: "10002", BeginItemIndex: "1", Limit: 2})
	if query.Get("action") != "getalbum" || query.Get("__biz") != "fixture-account-a" ||
		query.Get("is_reverse") != "1" || query.Get("begin_msgid") != "10002" || query.Get("count") != "2" {
		t.Fatalf("query = %v", query)
	}
	page, err := client.ListAlbumArticles(context.Background(), AlbumListRequest{
		FakeID: "fixture-account-a", AlbumID: "fixture-album-a", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Album.Name != "Fixture Album" || page.Album.ArticleCount != 3 || page.Completed ||
		len(page.Items) != 2 || page.Next.BeginMessageID != "10002" || !page.Items[1].Paid ||
		page.Items[0].Article.AccountID == "" {
		t.Fatalf("page = %#v", page)
	}
	reverse, err := client.ListAlbumArticles(context.Background(), AlbumListRequest{
		FakeID: "fixture-account-a", AlbumID: "fixture-album-a", Order: AlbumReverse,
	})
	if err != nil || reverse.Completed || len(reverse.Items) != 1 || reverse.Next.BeginMessageID != "10003" {
		t.Fatalf("reverse=%#v error=%v", reverse, err)
	}
}

func TestTraverseAlbumResumesDeduplicatesAndPersistsCheckpoint(t *testing.T) {
	client, server := albumFixtureClient(t, func(request *http.Request) string {
		if request.URL.Query().Get("begin_msgid") == "10002" {
			return "album-continuation.json"
		}
		return "album-forward.json"
	})
	defer server.Close()
	var checkpoints []domain.AlbumCheckpoint
	result, err := client.TraverseAlbum(context.Background(), AlbumTraverseOptions{
		Request: AlbumListRequest{FakeID: "fixture-account-a", AlbumID: "fixture-album-a", Limit: 2},
		Checkpoint: domain.AlbumCheckpoint{BeginMessageID: "10002", BeginItemIndex: "1", SeenKeys: []string{"10002:1"},
			PagesCommitted: 1, ItemsCommitted: 2},
		Sleep: func(context.Context, time.Duration) error { return nil },
		OnPage: func(_ AlbumPage, checkpoint domain.AlbumCheckpoint) error {
			checkpoints = append(checkpoints, checkpoint)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || len(result.Items) != 1 || result.Items[0].Key != "10003:1" ||
		result.Checkpoint.PagesCommitted != 2 || result.Checkpoint.ItemsCommitted != 3 ||
		!reflect.DeepEqual(result.Checkpoint.SeenKeys, []string{"10002:1", "10003:1"}) || len(checkpoints) != 1 {
		t.Fatalf("result=%#v checkpoints=%#v", result, checkpoints)
	}
}

func TestNormalizeAlbumArticlesAllowsOnlyExactControlledOrigin(t *testing.T) {
	origin, err := ParseControlledOrigin("http://127.0.0.1:43125")
	if err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`[{"msgid":"10001","itemidx":"1","title":"Fixture","url":"http://127.0.0.1:43125/s/article"}]`)
	items, _, err := normalizeAlbumArticles("fixture-account", origin, raw)
	if err != nil || len(items) != 1 || items[0].CanonicalURL != "http://127.0.0.1:43125/s/article" {
		t.Fatalf("controlled album items=%#v error=%v", items, err)
	}
	if _, _, err := normalizeAlbumArticles("fixture-account", origin, json.RawMessage(`[{"msgid":"10001","itemidx":"1","title":"Fixture","url":"http://127.0.0.1:43126/s/article"}]`)); err == nil {
		t.Fatal("album normalization accepted a different controlled authority")
	}
}

func TestAlbumFixturesCoverEmptyAuthenticationAndMalformedPayload(t *testing.T) {
	for name, fixture := range map[string]string{
		"empty": "album-empty.json", "auth": "search-auth-expired.json", "malformed": "album-malformed.json",
	} {
		t.Run(name, func(t *testing.T) {
			client, server := albumFixtureClient(t, func(*http.Request) string { return fixture })
			defer server.Close()
			page, err := client.ListAlbumArticles(context.Background(), AlbumListRequest{
				FakeID: "fixture-account-a", AlbumID: "fixture-album-a",
			})
			switch name {
			case "empty":
				if err != nil || !page.Completed || len(page.Items) != 0 {
					t.Fatalf("page=%#v error=%v", page, err)
				}
			case "auth":
				if !errors.Is(err, ErrDiscoveryAuthentication) {
					t.Fatalf("error = %v", err)
				}
			case "malformed":
				if !errors.Is(err, ErrDiscoveryProtocol) {
					t.Fatalf("error = %v", err)
				}
			}
		})
	}
}

// Live album responses carry http:// permalinks. Rejecting them made every
// upstream album item unusable while every fixture, being https, stayed green.
func TestNormalizeAlbumArticlesUpgradesCleartextPermalinks(t *testing.T) {
	raw := json.RawMessage(`[{"msgid":"10001","itemidx":"1","title":"Fixture","url":"http://mp.weixin.qq.com/s?__biz=b&mid=10001&idx=1","create_time":"1750000000"}]`)
	items, _, err := normalizeAlbumArticles("fixture-account-a", nil, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].CanonicalURL != "https://mp.weixin.qq.com/s?__biz=b&mid=10001&idx=1" {
		t.Fatalf("items = %#v", items)
	}
	cleartextElsewhere := json.RawMessage(`[{"msgid":"10001","itemidx":"1","title":"Fixture","url":"http://example.com/s/a"}]`)
	if _, _, err := normalizeAlbumArticles("fixture-account-a", nil, cleartextElsewhere); err == nil {
		t.Fatal("normalizeAlbumArticles accepted a non-WeChat host")
	}
}

func albumFixtureClient(t *testing.T, fixture func(*http.Request) string) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/mp/appmsgalbum" {
			http.NotFound(writer, request)
			return
		}
		contents, err := os.ReadFile(filepath.Join("testdata", "discovery", fixture(request)))
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = writer.Write(contents)
	}))
	store := secrets.NewMemoryStore()
	client := newClient(server.Client(), store, "default", server.URL)
	client.now = func() time.Time { return time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC) }
	if err := client.persistSession(context.Background(), Session{Token: "sanitized-token", ExpiresAt: client.now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return client, server
}
