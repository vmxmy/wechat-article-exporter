package oauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
)

const maxRegistrationResponseBytes = 1 << 20

type registrationRoundTripper struct {
	base        http.RoundTripper
	store       *config.Store
	server      string
	redirectURL string
}

func NewRegistrationHTTPClient(base *http.Client, store *config.Store, server, redirectURL string) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	roundTripper := base.Transport
	if roundTripper == nil {
		roundTripper = http.DefaultTransport
	}
	clone.Transport = &registrationRoundTripper{
		base: roundTripper, store: store, server: server, redirectURL: redirectURL,
	}
	return &clone
}

func (r *registrationRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := r.base.RoundTrip(request)
	if err != nil || request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/register") || response.StatusCode/100 != 2 {
		return response, err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxRegistrationResponseBytes+1))
	response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read OAuth response: %w", readErr)
	}
	if len(body) > maxRegistrationResponseBytes {
		return nil, fmt.Errorf("OAuth response exceeds %d bytes", maxRegistrationResponseBytes)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	var registration oauthex.ClientRegistrationResponse
	if json.Unmarshal(body, &registration) != nil || registration.ClientID == "" {
		return response, nil
	}
	if err := MergeDynamicRegistration(r.store, r.server, r.redirectURL, &registration, ""); err != nil {
		return nil, fmt.Errorf("save dynamic client registration: %w", err)
	}
	return response, nil
}
