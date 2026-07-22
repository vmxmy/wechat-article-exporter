package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type URLWrapper struct {
	RouteName     string
	Endpoint      *url.URL
	Trust         TrustLevel
	HTTP          Doer
	Policy        DestinationPolicy
	Now           func() time.Time
	authorization string
}

func (route *URLWrapper) WithAuthorization(authorization string) *URLWrapper {
	clone := *route
	clone.authorization = authorization
	return &clone
}

func (route URLWrapper) String() string {
	endpoint := ""
	if route.Endpoint != nil {
		endpoint = route.Endpoint.Scheme + "://" + route.Endpoint.Host + route.Endpoint.EscapedPath()
	}
	return fmt.Sprintf("URLWrapper{name:%q, endpoint:%q, trust:%q, authorizationConfigured:%t}",
		route.Name(), endpoint, route.Trust, route.authorization != "")
}

func (route URLWrapper) GoString() string { return route.String() }

func (route *URLWrapper) Name() string {
	if route.RouteName != "" {
		return route.RouteName
	}
	return "url-wrapper"
}

func (route *URLWrapper) Do(ctx context.Context, request Request) (Result, error) {
	if route.Endpoint == nil {
		return Result{}, errors.New("URL-wrapper endpoint is required")
	}
	if request.Class != RouteProbe {
		if err := ValidateRoute(request.Class, false, route.Trust); err != nil {
			return Result{}, err
		}
	}
	if err := route.Policy.Validate(ctx, route.Endpoint); err != nil {
		return Result{}, fmt.Errorf("validate URL-wrapper endpoint: %w", err)
	}
	if request.URL == nil {
		return Result{}, errors.New("wrapped target URL is required")
	}
	headers, err := json.Marshal(request.Header)
	if err != nil {
		return Result{}, fmt.Errorf("encode wrapped headers: %w", err)
	}
	wrapperURL := *route.Endpoint
	query := wrapperURL.Query()
	query.Set("url", request.URL.String())
	query.Set("headers", string(headers))
	if route.authorization != "" {
		query.Set("authorization", route.authorization)
	}
	wrapperURL.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, wrapperURL.String(), nil)
	if err != nil {
		return Result{}, err
	}
	httpClient := route.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := route.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return Result{}, fmt.Errorf("URL-wrapper request: %w", sanitizeNetworkError(err, &wrapperURL))
	}
	maximum := request.MaxResponseBytes
	if maximum <= 0 {
		maximum = defaultMaxResponseBytes
	}
	response.Body = &boundedBody{ReadCloser: response.Body, remaining: maximum + 1, maximum: maximum}
	return Result{Response: response, Route: route.Name(), Duration: now().Sub(started)}, nil
}

func (route *URLWrapper) DoHTTP(request *http.Request) (*http.Response, error) {
	result, err := route.Do(request.Context(), Request{
		Class: PublicContent, Method: request.Method, URL: request.URL,
		Header: request.Header, Body: request.Body,
	})
	return result.Response, err
}

var _ RoutedClient = (*URLWrapper)(nil)
