package wechat

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestParseControlledOriginAcceptsOnlyPlainLoopbackOrigins(t *testing.T) {
	for _, origin := range []string{
		"http://127.0.0.1:43125",
		"http://[::1]:43125",
		" http://127.0.0.1:43125 ",
	} {
		t.Run(origin, func(t *testing.T) {
			parsed, err := ParseControlledOrigin(origin)
			if err != nil {
				t.Fatalf("ParseControlledOrigin(%q): %v", origin, err)
			}
			if parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
				t.Fatalf("parsed origin = %#v", parsed)
			}
			if _, err := NewClientForControlledOrigin(http.DefaultClient, secrets.NewMemoryStore(), "fixture", parsed); err != nil {
				t.Fatalf("NewClientForControlledOrigin(%q): %v", origin, err)
			}
		})
	}

	for _, origin := range []string{
		"https://127.0.0.1:43125",
		"http://example.com",
		"http://localhost:43125",
		"http://10.0.0.1",
		"http://127.0.0.1",
		"http://user@127.0.0.1:43125",
		"http://127.0.0.1:43125/path",
		"http://127.0.0.1:43125?query=1",
		"http://127.0.0.1:43125#fragment",
		"http://",
	} {
		t.Run("reject_"+origin, func(t *testing.T) {
			if _, err := ParseControlledOrigin(origin); err == nil {
				t.Fatalf("ParseControlledOrigin(%q) error = nil", origin)
			}
		})
	}
}

func TestControlledOriginClientBlocksCrossOriginRequestsAndRedirects(t *testing.T) {
	origin, err := ParseControlledOrigin("http://127.0.0.1:43125")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClientForControlledOrigin(http.DefaultClient, secrets.NewMemoryStore(), "fixture", origin)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:43126/escaped", nil)
	if _, err := client.http.Do(request); err == nil || !strings.Contains(err.Error(), "changed origin") {
		t.Fatalf("cross-origin transport error = %v", err)
	}
	request, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, origin.String()+"/redirect", nil)
	redirectRequest, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:43126/escaped", nil)
	if err := client.http.CheckRedirect(redirectRequest, []*http.Request{request}); err == nil || !strings.Contains(err.Error(), "changed origin") {
		t.Fatalf("redirect error = %v", err)
	}

	bad := &url.URL{Scheme: "http", Host: "127.0.0.1:43125", User: url.User("fixture")}
	if matchesControlledAuthority(bad, origin) {
		t.Fatal("userinfo-bearing target matched controlled authority")
	}
}

func TestControlledOriginClientRejectsUnauditableTransport(t *testing.T) {
	origin, err := ParseControlledOrigin("http://127.0.0.1:43125")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	})
	if _, err := NewClientForControlledOrigin(&http.Client{Transport: transport}, secrets.NewMemoryStore(), "fixture", origin); err == nil || !strings.Contains(err.Error(), "auditable") {
		t.Fatalf("NewClientForControlledOrigin error = %v", err)
	}
}

func TestControlledOriginClientIgnoresRegisteredProtocolHandlers(t *testing.T) {
	origin, err := ParseControlledOrigin("http://127.0.0.1:43125")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.RegisterProtocol("http", roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	}))
	client, err := NewClientForControlledOrigin(&http.Client{Transport: transport}, secrets.NewMemoryStore(), "fixture", origin)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, origin.String()+"/probe", nil)
	_, _ = client.http.Do(request)
	if called {
		t.Fatal("registered protocol handler was retained by controlled transport")
	}
}

func TestNormalizeDiscoveredArticleControlledOriginDoesNotRelaxProductionPolicy(t *testing.T) {
	item := articleItem{Aid: "fixture-aid", Title: "Fixture", Link: "http://127.0.0.1:43125/s/article"}
	if _, err := normalizeDiscoveredArticle("fixture-account", item); err == nil {
		t.Fatal("production normalization accepted loopback HTTP")
	}
	origin, err := ParseControlledOrigin("http://127.0.0.1:43125")
	if err != nil {
		t.Fatal(err)
	}
	originClient, err := NewClientForControlledOrigin(http.DefaultClient, secrets.NewMemoryStore(), "fixture", origin)
	if err != nil {
		t.Fatal(err)
	}
	article, err := normalizeDiscoveredArticleForOrigin("fixture-account", item, originClient.baseURL)
	if err != nil {
		t.Fatalf("controlled normalization: %v", err)
	}
	if article.CanonicalURL != item.Link {
		t.Fatalf("article URL = %q, want %q", article.CanonicalURL, item.Link)
	}
	item.Link = "http://localhost:43125/s/article"
	if _, err := normalizeDiscoveredArticleForOrigin("fixture-account", item, originClient.baseURL); err == nil {
		t.Fatal("controlled normalization accepted a different loopback authority")
	}
}
