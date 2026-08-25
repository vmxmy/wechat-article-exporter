package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"sync/atomic"
	"time"
)

const (
	defaultMaxResponseBytes = 32 << 20
	defaultUserAgent        = "wechat-article/2 local-client"
)

// BrowserArticleUserAgent is required on public article surfaces. WeChat
// serves those pages by user agent: the local-client agent is answered with a
// 302 to a ~1 KB stub carrying no article payload, while a desktop browser
// agent receives the full page. Every caller that parses a public article page
// or fetches its resources must send this, or it will parse the stub and
// report the article as structurally broken.
const BrowserArticleUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

type Direct struct {
	HTTP      Doer
	Policy    DestinationPolicy
	UserAgent string
	Now       func() time.Time
	sequence  atomic.Uint64
}

func NewDirect(httpClient *http.Client, policy DestinationPolicy) *Direct {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	clone := *httpClient
	switch transport := clone.Transport.(type) {
	case nil:
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok || defaultTransport == nil {
			defaultTransport = &http.Transport{}
		}
		defaultTransport = defaultTransport.Clone()
		defaultTransport.Proxy = nil
		clone.Transport = defaultTransport
	case *http.Transport:
		directTransport := transport.Clone()
		directTransport.Proxy = nil
		clone.Transport = directTransport
	default:
		// A custom RoundTripper may consult proxy environment variables or hide
		// redirect/dial behavior that cannot be audited here. Direct mode must
		// use a transport whose proxy behavior we can explicitly disable.
		clone.Transport = (&http.Transport{}).Clone()
	}
	if clone.Timeout <= 0 {
		clone.Timeout = 30 * time.Second
	}
	if clone.Jar == nil {
		clone.Jar, _ = cookiejar.New(nil)
	}
	previousRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if err := policy.Validate(request.Context(), request.URL); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &Direct{HTTP: &clone, Policy: policy}
}

func (client *Direct) Name() string { return "direct" }

func (client *Direct) Do(ctx context.Context, request Request) (Result, error) {
	if request.URL == nil {
		return Result{}, errors.New("network request URL is required")
	}
	if err := client.Policy.Validate(ctx, request.URL); err != nil {
		return Result{}, err
	}
	method := request.Method
	if method == "" {
		method = http.MethodGet
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, request.URL.String(), request.Body)
	if err != nil {
		return Result{}, err
	}
	if request.Header == nil {
		httpRequest.Header = make(http.Header)
	} else {
		httpRequest.Header = request.Header.Clone()
	}
	userAgent := client.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	if httpRequest.Header.Get("User-Agent") == "" {
		httpRequest.Header.Set("User-Agent", userAgent)
	}
	requestID := fmt.Sprintf("req-%016x", client.sequence.Add(1))
	httpRequest.Header.Set("X-Request-ID", requestID)
	now := client.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	transport := client.HTTP
	if transport == nil {
		transport = NewDirect(nil, client.Policy).HTTP
	}
	response, err := transport.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("direct request %s: %w", requestID, err)
	}
	maximum := request.MaxResponseBytes
	if maximum <= 0 {
		maximum = defaultMaxResponseBytes
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: maximum + 1, maximum: maximum}
	return Result{Response: response, Route: client.Name(), RequestID: requestID, Duration: now().Sub(started)}, nil
}

type boundedBody struct {
	io.ReadCloser
	remaining int64
	maximum   int64
}

func (client *Direct) DoHTTP(request *http.Request) (*http.Response, error) {
	result, err := client.Do(request.Context(), Request{
		Class: PublicContent, Method: request.Method, URL: request.URL,
		Header: request.Header, Body: request.Body,
	})
	return result.Response, err
}

var _ Route = (*Direct)(nil)

func (body *boundedBody) Read(buffer []byte) (int, error) {
	if body.remaining <= 0 {
		return 0, fmt.Errorf("response exceeded %d bytes", body.maximum)
	}
	if int64(len(buffer)) > body.remaining {
		buffer = buffer[:body.remaining]
	}
	count, err := body.ReadCloser.Read(buffer)
	body.remaining -= int64(count)
	if body.remaining <= 0 && err == nil {
		return count, fmt.Errorf("response exceeded %d bytes", body.maximum)
	}
	return count, err
}
