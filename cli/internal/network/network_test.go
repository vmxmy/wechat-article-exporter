package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fixedResolver map[string][]net.IPAddr

func (resolver fixedResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	return resolver[host], nil
}

type roundTripper func(*http.Request) (*http.Response, error)

func (transport roundTripper) Do(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestDestinationPolicyBlocksIPv4IPv6PrivateLinkLocalAndMetadata(t *testing.T) {
	allowed, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	for name, address := range map[string]string{
		"ipv4-loopback":   "127.0.0.1",
		"ipv4-private":    "10.10.10.10",
		"ipv4-link-local": "169.254.10.20",
		"ipv4-metadata":   "169.254.169.254",
		"ipv6-loopback":   "::1",
		"ipv6-private":    "fd12:3456:789a::1",
		"ipv6-link-local": "fe80::1",
		"ipv6-metadata":   "fd00:ec2::254",
	} {
		t.Run(name, func(t *testing.T) {
			policy := DestinationPolicy{
				AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}},
				Resolver:     fixedResolver{"mp.weixin.qq.com": {{IP: net.ParseIP(address)}}},
			}
			if err := policy.Validate(context.Background(), allowed); !errors.Is(err, ErrDestinationPolicy) {
				t.Fatalf("Validate(%s) error = %v", address, err)
			}
		})
	}
	policy := DestinationPolicy{
		AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}},
		Resolver:     fixedResolver{"mp.weixin.qq.com": {{IP: net.ParseIP("203.0.113.20")}}},
	}
	if err := policy.Validate(context.Background(), allowed); err != nil {
		t.Fatal(err)
	}
	metadata, _ := url.Parse("https://metadata.google.internal/computeMetadata/v1")
	metadataPolicy := DestinationPolicy{
		AllowedHosts: map[string]struct{}{"metadata.google.internal": {}},
		Resolver:     fixedResolver{"metadata.google.internal": {{IP: net.ParseIP("203.0.113.40")}}},
	}
	if err := metadataPolicy.Validate(context.Background(), metadata); !errors.Is(err, ErrDestinationPolicy) {
		t.Fatalf("metadata host validation error = %v", err)
	}
}

func TestSensitiveRoutePolicy(t *testing.T) {
	if err := ValidateRoute(Comments, false, TrustPublicOnly); !errors.Is(err, ErrSensitiveRouteRequired) {
		t.Fatalf("ValidateRoute(untrusted) error = %v", err)
	}
	if err := ValidateRoute(Comments, false, TrustCredential); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRoute(PublicContent, false, TrustPublicOnly); err != nil {
		t.Fatal(err)
	}
}

func TestDirectAddsRequestIDAndBoundsResponse(t *testing.T) {
	target, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	direct := &Direct{
		Policy: DestinationPolicy{AllowedHosts: map[string]struct{}{"mp.weixin.qq.com": {}}, Resolver: fixedResolver{
			"mp.weixin.qq.com": {{IP: net.ParseIP("203.0.113.20")}},
		}},
		HTTP: roundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("X-Request-ID") == "" || request.Header.Get("User-Agent") == "" {
				t.Fatalf("headers = %#v", request.Header)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("123456")), Header: make(http.Header)}, nil
		}),
	}
	result, err := direct.Do(context.Background(), Request{Class: PublicContent, URL: target, MaxResponseBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Response.Body.Close()
	if _, err := io.ReadAll(result.Response.Body); err == nil {
		t.Fatal("ReadAll(over limit) error = nil")
	}
}

func TestDirectRevalidatesRedirectDestinations(t *testing.T) {
	policy := DestinationPolicy{
		AllowedHosts: map[string]struct{}{
			"mp.weixin.qq.com": {}, "metadata.google.internal": {},
		},
		AllowSubdomains: true,
		Resolver: fixedResolver{
			"mp.weixin.qq.com":         {{IP: net.ParseIP("203.0.113.20")}},
			"metadata.google.internal": {{IP: net.ParseIP("203.0.113.40")}},
		},
	}
	direct := NewDirect(&http.Client{}, policy)
	httpClient, ok := direct.HTTP.(*http.Client)
	if !ok {
		t.Fatalf("HTTP transport = %T", direct.HTTP)
	}
	if httpClient.Jar == nil || httpClient.Timeout <= 0 {
		t.Fatalf("direct client missing jar or timeout: %#v", httpClient)
	}
	for name, rawURL := range map[string]string{
		"ipv4-loopback":       "http://127.0.0.1/latest/meta-data",
		"ipv6-loopback":       "http://[::1]/latest/meta-data",
		"ipv4-private":        "https://10.0.0.1/latest/meta-data",
		"ipv6-private":        "https://[fd00::1]/latest/meta-data",
		"ipv4-link-local":     "https://169.254.10.20/latest/meta-data",
		"ipv6-link-local":     "https://[fe80::1]/latest/meta-data",
		"cloud-metadata-host": "https://metadata.google.internal/computeMetadata/v1",
	} {
		t.Run(name, func(t *testing.T) {
			redirectURL, _ := url.Parse(rawURL)
			redirectRequest := &http.Request{URL: redirectURL, Header: make(http.Header)}
			if err := httpClient.CheckRedirect(redirectRequest, nil); !errors.Is(err, ErrDestinationPolicy) {
				t.Fatalf("CheckRedirect(%s) error = %v", rawURL, err)
			}
		})
	}
}

func TestURLWrapperPreservesCompatibilityContractAndTrust(t *testing.T) {
	endpoint, _ := url.Parse("https://proxy.example/api")
	target, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	policy := DestinationPolicy{AllowedHosts: map[string]struct{}{"proxy.example": {}}, Resolver: fixedResolver{
		"proxy.example": {{IP: net.ParseIP("203.0.113.30")}},
	}}
	var seen *http.Request
	route := (&URLWrapper{
		Endpoint: endpoint, Trust: TrustPublicOnly, Policy: policy,
		HTTP: roundTripper(func(request *http.Request) (*http.Response, error) {
			seen = request
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
		}),
	}).WithAuthorization("proxy-secret")
	if _, err := route.Do(context.Background(), Request{Class: Comments, URL: target}); !errors.Is(err, ErrSensitiveRouteRequired) {
		t.Fatalf("sensitive wrapper error = %v", err)
	}
	if _, err := route.Do(context.Background(), Request{Class: PublicContent, URL: target, Header: http.Header{"X-Test": {"yes"}}}); err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.URL.Query().Get("url") != target.String() || seen.URL.Query().Get("authorization") != "proxy-secret" || !strings.Contains(seen.URL.Query().Get("headers"), "X-Test") {
		t.Fatalf("wrapper request = %#v", seen)
	}
}

func TestURLWrapperAuthorizationDoesNotLeakThroughErrorOrFormatting(t *testing.T) {
	endpoint, _ := url.Parse("https://proxy.example/api?authorization=url-secret")
	target, _ := url.Parse("https://mp.weixin.qq.com/s/example?pass_ticket=wechat-secret&safe=context")
	policy := DestinationPolicy{AllowedHosts: map[string]struct{}{"proxy.example": {}}, Resolver: fixedResolver{
		"proxy.example": {{IP: net.ParseIP("203.0.113.30")}},
	}}
	route := (&URLWrapper{
		RouteName: "safe", Endpoint: endpoint, Trust: TrustPublicOnly, Policy: policy,
		HTTP: roundTripper(func(request *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial failed: %s", request.URL.String())
		}),
	}).WithAuthorization("proxy-secret")
	_, err := route.Do(context.Background(), Request{Class: PublicContent, URL: target})
	if err == nil {
		t.Fatal("Do() error = nil")
	}
	for _, representation := range []string{err.Error(), fmt.Sprintf("%v", route), fmt.Sprintf("%#v", route)} {
		for _, forbidden := range []string{"proxy-secret", "url-secret", "wechat-secret"} {
			if strings.Contains(representation, forbidden) {
				t.Fatalf("representation leaked %q: %s", forbidden, representation)
			}
		}
	}
}

func TestRouterIsDirectFirstThenExplicitFallback(t *testing.T) {
	var calls []string
	retryable := errors.New("retryable")
	router := Router{
		Retryable: func(err error) bool { return errors.Is(err, retryable) },
		Routes: []Candidate{
			{Client: StaticClient{RouteName: "proxy", Call: func(context.Context, Request) (Result, error) {
				calls = append(calls, "proxy")
				return Result{Route: "proxy"}, nil
			}}, Trust: TrustPublicOnly, Priority: 1, Enabled: true},
			{Client: StaticClient{RouteName: "direct", Call: func(context.Context, Request) (Result, error) {
				calls = append(calls, "direct")
				return Result{}, retryable
			}}, Direct: true, Enabled: true},
		},
	}
	result, err := router.Do(context.Background(), Request{Class: PublicContent})
	if err != nil || result.Route != "proxy" || strings.Join(calls, ",") != "direct,proxy" {
		t.Fatalf("Router.Do() = %#v, %v, calls=%v", result, err, calls)
	}
	calls = nil
	_, err = router.Do(context.Background(), Request{Class: Comments})
	if err == nil || strings.Join(calls, ",") != "direct" {
		t.Fatalf("Router.Do(sensitive) error=%v calls=%v", err, calls)
	}
}

func TestRouterRecoveryProbeWaitsBehindHealthyRoutes(t *testing.T) {
	var calls []string
	router := Router{Routes: []Candidate{
		{Client: StaticClient{RouteName: "recovering", Call: func(context.Context, Request) (Result, error) {
			calls = append(calls, "recovering")
			return Result{Route: "recovering"}, nil
		}}, Trust: TrustPublicOnly, Priority: 1, Enabled: true, ProbeRequired: true, Probe: func(context.Context) error {
			calls = append(calls, "probe")
			return nil
		}, Classes: ClassesMap([]RequestClass{PublicContent})},
		{Client: StaticClient{RouteName: "healthy", Call: func(context.Context, Request) (Result, error) {
			calls = append(calls, "healthy")
			return Result{Route: "healthy"}, nil
		}}, Trust: TrustPublicOnly, Priority: 10, Enabled: true, Classes: ClassesMap([]RequestClass{PublicContent})},
	}}
	result, err := router.Do(context.Background(), Request{Class: PublicContent})
	if err != nil || result.Route != "healthy" || strings.Join(calls, ",") != "healthy" {
		t.Fatalf("Router.Do() = %#v, %v calls=%v", result, err, calls)
	}
}
