package wechat

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/htmlx"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestSearchAccountsUsesFixturePaginationAndNormalizes(t *testing.T) {
	client, server := discoveryFixtureClient(t, map[string]string{"/cgi-bin/searchbiz": "search-success.json"})
	defer server.Close()
	page, err := client.SearchAccounts(context.Background(), domain.AccountQuery{Keyword: " Fixture ", Offset: 5, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 || page.Offset != 5 || page.Limit != 2 || len(page.Items) != 2 {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0].FakeID != "fixture-account-a" || page.Items[0].ID == "" || page.Items[0].ServiceType != 2 ||
		page.Items[0].AvatarURL == "" {
		t.Fatalf("normalized account = %#v", page.Items[0])
	}
}

func TestSearchAccountsMapsAuthAndMalformedFixtures(t *testing.T) {
	for name, fixture := range map[string]string{"auth": "search-auth-expired.json", "malformed": "search-malformed.json"} {
		t.Run(name, func(t *testing.T) {
			client, server := discoveryFixtureClient(t, map[string]string{"/cgi-bin/searchbiz": fixture})
			defer server.Close()
			_, err := client.SearchAccounts(context.Background(), domain.AccountQuery{Keyword: "fixture"})
			if name == "auth" && !errors.Is(err, ErrDiscoveryAuthentication) {
				t.Fatalf("error = %v", err)
			}
			if name == "malformed" && !errors.Is(err, ErrDiscoveryProtocol) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSearchAccountsEmptyFixtureCompletesWithoutInventingResults(t *testing.T) {
	client, server := discoveryFixtureClient(t, map[string]string{"/cgi-bin/searchbiz": "search-empty.json"})
	defer server.Close()
	page, err := client.SearchAccounts(context.Background(), domain.AccountQuery{Keyword: "missing", Offset: 5, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Items) != 0 || page.Offset != 5 {
		t.Fatalf("page = %#v", page)
	}
}

func TestResolveAccountNameRejectsUnsupportedURLBeforeRequest(t *testing.T) {
	requests := 0
	client := newClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("should not request")
	})}, secrets.NewMemoryStore(), "default", upstreamOrigin)
	for _, rawURL := range []string{"http://mp.weixin.qq.com/s/a", "https://example.com/s/a", "not-a-url"} {
		if _, err := client.ResolveAccountName(context.Background(), rawURL); err == nil {
			t.Fatalf("ResolveAccountName(%q) error = nil", rawURL)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestResolveAccountNameRejectsRedirectOutsideAllowedHosts(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`<span class="wx_follow_nickname">Should Not Be Read</span>`))
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/outside", http.StatusFound)
	}))
	defer redirector.Close()
	client := newClient(redirector.Client(), secrets.NewMemoryStore(), "default", redirector.URL)
	if _, err := client.ResolveAccountName(context.Background(), redirector.URL+"/article"); err == nil {
		t.Fatal("ResolveAccountName(redirect outside allowed hosts) error = nil")
	}
}

// WeChat now renders the follow bar from script, so wx_follow_nickname reaches
// the page only as element ids inside <script> and the class anchor alone
// resolves nothing. Each layout must keep resolving through the anchor chain.
func TestResolveAccountNameAcrossArticleLayouts(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		fixture string
		want    string
		wantErr string
	}{
		{name: "script rendered layout", fixture: "article-account-script-layout.html", want: "示例公众号"},
		{name: "legacy follow bar layout", fixture: "article-account.html", want: "Fixture Account"},
		{name: "anchor carries no name", fixture: "article-account-blank.html", wantErr: "article account name was empty"},
		{name: "no anchor at all", fixture: "article-account-absent.html", wantErr: "article did not expose an account name"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client, server := discoveryFixtureClient(t, map[string]string{"/s/article": testCase.fixture})
			defer server.Close()
			name, err := client.ResolveAccountName(context.Background(), server.URL+"/s/article")
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, testCase.wantErr)
				}
				if !errors.Is(err, ErrDiscoveryProtocol) {
					t.Fatalf("error = %v, want ErrDiscoveryProtocol", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != testCase.want {
				t.Fatalf("name = %q, want %q", name, testCase.want)
			}
		})
	}
}

// The script-rendered layout must not silently fall through to the legacy class
// anchor: that anchor is absent, so a regression there would resolve nothing.
func TestArticleAccountNameIgnoresScriptSideFollowNicknameIDs(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "discovery", "article-account-script-layout.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("js_wx_follow_nickname")) {
		t.Fatal("fixture no longer reproduces script-side wx_follow_nickname ids")
	}
	if bytes.Contains(body, []byte(`class="wx_follow_nickname`)) {
		t.Fatal("fixture must not carry the legacy class anchor")
	}
	document, err := htmlx.Parse(bytes.NewReader(body), htmlx.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	name, anchorName, matched := articleAccountNameChain.Resolve(document)
	if !matched || name != "示例公众号" {
		t.Fatalf("Resolve() = %q, %q, %v", name, anchorName, matched)
	}
	if anchorName != "js_name" {
		t.Fatalf("anchorName = %q, want the primary js_name anchor", anchorName)
	}
}

func TestResolveAccountFromArticleDetailsAndAuthorFixtures(t *testing.T) {
	client, server := discoveryFixtureClient(t, map[string]string{
		"/s/article": "article-account.html", "/cgi-bin/searchbiz": "search-success.json", "/mp/authorinfo": "author-success.json",
	})
	defer server.Close()
	account, err := client.ResolveAccountFromArticle(context.Background(), server.URL+"/s/article")
	if err != nil {
		t.Fatal(err)
	}
	if account.FakeID != "fixture-account-a" {
		t.Fatalf("account = %#v", account)
	}
	details, err := client.AccountDetails(context.Background(), account.FakeID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Author.PrincipalName != "Fixture Organization" || details.Account.Alias != "fixture_alias" {
		t.Fatalf("details = %#v", details)
	}
}

func TestBuildAndParseArticleListFixtures(t *testing.T) {
	query := BuildArticleListQuery("token", ArticleListRequest{FakeID: "fixture-account-a", Keyword: "agent", Offset: 3, Limit: 7})
	if query.Get("sub") != "search" || query.Get("search_field") != "7" || query.Get("begin") != "3" ||
		query.Get("count") != "7" || query.Get("sub_action") != "list_ex" {
		t.Fatalf("query = %v", query)
	}
	client, server := discoveryFixtureClient(t, map[string]string{"/cgi-bin/appmsgpublish": "article-page-one.json"})
	defer server.Close()
	page, err := client.ListArticles(context.Background(), ArticleListRequest{FakeID: "fixture-account-a", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Next != 1 || page.Completed || len(page.Items) != 2 || !page.Items[1].Paid {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0].Aid != "aid-1" || page.Items[0].AccountID == "" || page.Items[0].PublishedAt.IsZero() {
		t.Fatalf("article = %#v", page.Items[0])
	}
}

func TestArticleListUsesTopLevelTotalAndMessagePagination(t *testing.T) {
	client, server := discoveryFixtureClient(t, map[string]string{
		"/cgi-bin/appmsgpublish": "article-page-top-level-total.json",
	})
	defer server.Close()
	page, err := client.ListArticles(context.Background(), ArticleListRequest{
		FakeID: "fixture-account-a", Offset: 1, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Offset != 1 || page.Next != 2 || page.Completed || len(page.Items) != 2 {
		t.Fatalf("page = %#v", page)
	}
	if page.Items[0].ID == page.Items[1].ID {
		t.Fatalf("article IDs collided: %#v", page.Items)
	}
	if domain.ArticleID(identity.ArticleID("fixture-account-a", page.Items[0].Aid)) != page.Items[0].ID {
		t.Fatalf("stable article ID changed with URL: %#v", page.Items[0])
	}
}

func TestDiscoveryRejectsMissingPaginationTotals(t *testing.T) {
	for name, route := range map[string]string{
		"account": "/cgi-bin/searchbiz",
		"article": "/cgi-bin/appmsgpublish",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := "search-missing-total.json"
			if name == "article" {
				fixture = "article-missing-total.json"
			}
			client, server := discoveryFixtureClient(t, map[string]string{route: fixture})
			defer server.Close()
			var err error
			if name == "account" {
				_, err = client.SearchAccounts(context.Background(), domain.AccountQuery{Keyword: "fixture"})
			} else {
				_, err = client.ListArticles(context.Background(), ArticleListRequest{FakeID: "fixture-account-a"})
			}
			if !errors.Is(err, ErrDiscoveryProtocol) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestArticleListEmptyAndMalformedFixtures(t *testing.T) {
	for name, fixture := range map[string]string{"empty": "article-empty.json", "malformed": "article-malformed.json"} {
		t.Run(name, func(t *testing.T) {
			client, server := discoveryFixtureClient(t, map[string]string{"/cgi-bin/appmsgpublish": fixture})
			defer server.Close()
			page, err := client.ListArticles(context.Background(), ArticleListRequest{FakeID: "fixture-account-a"})
			if name == "empty" {
				if err != nil || !page.Completed || len(page.Items) != 0 {
					t.Fatalf("page=%#v error=%v", page, err)
				}
			} else if !errors.Is(err, ErrDiscoveryProtocol) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestArticleListMapsAuthenticationFixture(t *testing.T) {
	client, server := discoveryFixtureClient(t, map[string]string{"/cgi-bin/appmsgpublish": "search-auth-expired.json"})
	defer server.Close()
	_, err := client.ListArticles(context.Background(), ArticleListRequest{FakeID: "fixture-account-a"})
	if !errors.Is(err, ErrDiscoveryAuthentication) {
		t.Fatalf("error = %v", err)
	}
}

func discoveryFixtureClient(t *testing.T, fixtures map[string]string) (*Client, *httptest.Server) {
	t.Helper()
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fixture, ok := fixtures[request.URL.Path]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Path == "/cgi-bin/searchbiz" {
			if request.URL.Query().Get("token") != "sanitized-token" || request.URL.Query().Get("begin") == "" {
				t.Errorf("search query = %v", request.URL.Query())
			}
		}
		contents, err := os.ReadFile(filepath.Join("testdata", "discovery", fixture))
		if err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		_, _ = writer.Write(contents)
		mu.Unlock()
	}))
	store := secrets.NewMemoryStore()
	client := newClient(server.Client(), store, "default", server.URL)
	client.now = func() time.Time { return time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC) }
	parsed, _ := url.Parse(server.URL)
	if err := client.persistSession(context.Background(), Session{
		Token: "sanitized-token", ExpiresAt: client.now().Add(time.Hour),
		Cookies: []Cookie{{Name: "bizuin", Value: "sanitized-cookie", Domain: parsed.Hostname(), Path: "/", HostOnly: true}},
	}); err != nil {
		t.Fatal(err)
	}
	return client, server
}
