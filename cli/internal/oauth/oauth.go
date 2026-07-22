package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
	"golang.org/x/oauth2"
)

type BrowserOptions struct {
	Store         *config.Store
	Server        string
	RedirectURL   string
	HTTPClient    *http.Client
	FetchCode     auth.AuthorizationCodeFetcher
	ClientVersion string
}

type PersistentHandler struct {
	store         *config.Store
	server        string
	redirectURL   string
	httpClient    *http.Client
	fetchCode     auth.AuthorizationCodeFetcher
	clientVersion string

	mu          sync.Mutex
	token       *oauth2.Token
	tokenSource oauth2.TokenSource
	inner       *auth.AuthorizationCodeHandler
}

var _ auth.OAuthHandler = (*PersistentHandler)(nil)

func NewPersistentHandler(options BrowserOptions) (*PersistentHandler, error) {
	if options.Store == nil {
		return nil, errors.New("OAuth config store is required")
	}
	if options.FetchCode == nil {
		return nil, errors.New("OAuth authorization code fetcher is required")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = http.DefaultClient
	}
	server, err := NormalizeServer(options.Server)
	if err != nil {
		return nil, err
	}
	return &PersistentHandler{
		store:         options.Store,
		server:        server,
		redirectURL:   options.RedirectURL,
		httpClient:    options.HTTPClient,
		fetchCode:     options.FetchCode,
		clientVersion: options.ClientVersion,
	}, nil
}

func (h *PersistentHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tokenSource != nil {
		return oauth2.ReuseTokenSource(h.token, &savingTokenSource{source: h.tokenSource, save: h.saveToken}), nil
	}
	stored, err := h.store.Read()
	if err != nil {
		return nil, err
	}
	if !sameServer(stored.Server, h.server) || stored.Tokens == nil || stored.Tokens.AccessToken == "" {
		return nil, nil
	}
	token := toOAuthToken(stored)
	h.token = token
	if stored.TokenEndpoint != "" && stored.ClientInformation != nil && stored.ClientInformation.ClientID != "" {
		cfg := oauth2.Config{
			ClientID:     stored.ClientInformation.ClientID,
			ClientSecret: stored.ClientInformation.ClientSecret,
			Endpoint: oauth2.Endpoint{
				TokenURL:  stored.TokenEndpoint,
				AuthStyle: authStyle(stored.ClientInformation.TokenEndpointAuthMethod),
			},
		}
		refreshContext := context.WithValue(ctx, oauth2.HTTPClient, h.httpClient)
		h.tokenSource = cfg.TokenSource(refreshContext, token)
	} else {
		h.tokenSource = oauth2.StaticTokenSource(token)
	}
	return oauth2.ReuseTokenSource(token, &savingTokenSource{source: h.tokenSource, save: h.saveToken}), nil
}

func (h *PersistentHandler) Authorize(ctx context.Context, request *http.Request, response *http.Response) error {
	stored, err := h.store.Read()
	if err != nil {
		return err
	}
	metadata, err := discoverMetadata(ctx, request.URL.String(), response, h.httpClient)
	if err != nil {
		return err
	}
	handlerConfig := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              h.redirectURL,
		AuthorizationCodeFetcher: h.fetchCode,
		Client:                   h.httpClient,
	}
	clientInformation := stored.ClientInformation
	clientSecretValid := clientInformation != nil &&
		(clientInformation.ClientSecretExpiresAt == 0 || time.Now().Unix() < clientInformation.ClientSecretExpiresAt)
	redirectRegistered := clientInformation != nil && slices.Contains(clientInformation.RedirectURIs, h.redirectURL)
	if sameServer(stored.Server, h.server) && clientInformation != nil && clientInformation.ClientID != "" && clientSecretValid && redirectRegistered {
		handlerConfig.PreregisteredClient = clientCredentials(clientInformation)
	} else {
		handlerConfig.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs:            []string{h.redirectURL},
				TokenEndpointAuthMethod: "none",
				GrantTypes:              []string{"authorization_code", "refresh_token"},
				ResponseTypes:           []string{"code"},
				ClientName:              "WeChat Article Exporter CLI",
				Scope:                   "wechat.read",
				SoftwareID:              "wechat-article-cli",
				SoftwareVersion:         h.clientVersion,
			},
		}
	}
	inner, err := auth.NewAuthorizationCodeHandler(handlerConfig)
	if err != nil {
		return err
	}
	if err := inner.Authorize(ctx, request, response); err != nil {
		return err
	}
	tokenSource, err := inner.TokenSource(ctx)
	if err != nil {
		return err
	}
	if tokenSource == nil {
		return errors.New("OAuth authorization completed without a token source")
	}
	token, err := tokenSource.Token()
	if err != nil {
		return err
	}

	h.mu.Lock()
	h.inner = inner
	h.token = token
	h.tokenSource = tokenSource
	h.mu.Unlock()
	if err := h.saveToken(token); err != nil {
		return err
	}
	if metadata != nil {
		if err := h.store.Update(func(value *config.File) error {
			value.Server = h.server
			value.TokenEndpoint = metadata.TokenEndpoint
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func discoverMetadata(ctx context.Context, resourceURL string, response *http.Response, httpClient *http.Client) (*oauthex.AuthServerMeta, error) {
	challenges, _ := oauthex.ParseWWWAuthenticate(response.Header.Values("WWW-Authenticate"))
	resourceMetadataURL := ""
	for _, challenge := range challenges {
		if value := challenge.Params["resource_metadata"]; value != "" {
			resourceMetadataURL = value
			break
		}
	}
	authorizationServer := ""
	if resourceMetadataURL != "" {
		metadata, metadataErr := oauthex.GetProtectedResourceMetadata(ctx, resourceMetadataURL, resourceURL, httpClient)
		if metadataErr != nil {
			return nil, fmt.Errorf("get protected resource metadata: %w", metadataErr)
		}
		if metadata == nil || len(metadata.AuthorizationServers) == 0 {
			return nil, errors.New("protected resource metadata does not advertise an authorization server")
		}
		authorizationServer = metadata.AuthorizationServers[0]
	}
	if authorizationServer == "" {
		parsed, parseErr := url.Parse(resourceURL)
		if parseErr != nil {
			return nil, parseErr
		}
		parsed.Path = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		authorizationServer = strings.TrimSuffix(parsed.String(), "/")
	}
	metadata, err := auth.GetAuthServerMetadata(ctx, authorizationServer, httpClient)
	if err != nil {
		return nil, err
	}
	if metadata == nil {
		metadata = &oauthex.AuthServerMeta{
			Issuer:                authorizationServer,
			AuthorizationEndpoint: strings.TrimSuffix(authorizationServer, "/") + "/authorize",
			TokenEndpoint:         strings.TrimSuffix(authorizationServer, "/") + "/token",
			RegistrationEndpoint:  strings.TrimSuffix(authorizationServer, "/") + "/register",
		}
	}
	return metadata, nil
}

func (h *PersistentHandler) saveToken(token *oauth2.Token) error {
	if token == nil {
		return nil
	}
	return h.store.Update(func(value *config.File) error {
		if !sameServer(value.Server, h.server) {
			*value = config.File{Server: h.server}
		}
		value.Server = h.server
		value.Tokens = fromOAuthToken(token)
		value.TokenSavedAt = time.Now().UnixMilli()
		return nil
	})
}

func clientCredentials(info *config.ClientInformation) *oauthex.ClientCredentials {
	credentials := &oauthex.ClientCredentials{ClientID: info.ClientID}
	if info.ClientSecret != "" {
		credentials.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: info.ClientSecret}
	}
	return credentials
}

func authStyle(method string) oauth2.AuthStyle {
	switch method {
	case "client_secret_basic":
		return oauth2.AuthStyleInHeader
	case "client_secret_post", "none":
		return oauth2.AuthStyleInParams
	default:
		return oauth2.AuthStyleAutoDetect
	}
}

func toOAuthToken(value config.File) *oauth2.Token {
	token := &oauth2.Token{
		AccessToken:  value.Tokens.AccessToken,
		TokenType:    value.Tokens.TokenType,
		RefreshToken: value.Tokens.RefreshToken,
	}
	if expiry := value.TokenExpiry(); !expiry.IsZero() {
		token.Expiry = expiry
	}
	return token
}

func fromOAuthToken(token *oauth2.Token) *config.Tokens {
	expiresIn := int64(0)
	if !token.Expiry.IsZero() {
		expiresIn = int64(time.Until(token.Expiry).Seconds())
		if expiresIn < 0 {
			expiresIn = 0
		}
	}
	scope, _ := token.Extra("scope").(string)
	return &config.Tokens{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		ExpiresIn:    expiresIn,
		RefreshToken: token.RefreshToken,
		Scope:        scope,
	}
}

type savingTokenSource struct {
	source oauth2.TokenSource
	save   func(*oauth2.Token) error
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	token, err := s.source.Token()
	if err != nil {
		return nil, err
	}
	if err := s.save(token); err != nil {
		return nil, err
	}
	return token, nil
}

func NormalizeServer(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("server URL must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("server URL must use HTTP(S) and must not contain credentials")
	}
	if parsed.User != nil {
		return "", errors.New("server URL must use HTTP(S) and must not contain credentials")
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" {
		return "", errors.New("server URL must use HTTPS; HTTP is allowed only for loopback hosts")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	if parsed.Path == "/mcp" {
		parsed.Path = ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func MCPURL(server string) string {
	return strings.TrimSuffix(server, "/") + "/mcp"
}

func sameServer(left, right string) bool {
	if left == "" || right == "" {
		return left == right
	}
	normalizedLeft, leftErr := NormalizeServer(left)
	normalizedRight, rightErr := NormalizeServer(right)
	return leftErr == nil && rightErr == nil && normalizedLeft == normalizedRight
}

func MergeDynamicRegistration(store *config.Store, server string, redirectURL string, response *oauthex.ClientRegistrationResponse, tokenEndpoint string) error {
	if response == nil {
		return fmt.Errorf("dynamic client registration returned no client information")
	}
	return store.Update(func(value *config.File) error {
		if !sameServer(value.Server, server) {
			*value = config.File{Server: server}
		}
		value.Server = server
		value.ClientInformation = &config.ClientInformation{
			ClientID:                response.ClientID,
			ClientSecret:            response.ClientSecret,
			ClientIDIssuedAt:        unixOrZero(response.ClientIDIssuedAt),
			ClientSecretExpiresAt:   unixOrZero(response.ClientSecretExpiresAt),
			RedirectURIs:            append([]string(nil), response.RedirectURIs...),
			TokenEndpointAuthMethod: response.TokenEndpointAuthMethod,
			GrantTypes:              append([]string(nil), response.GrantTypes...),
			ResponseTypes:           append([]string(nil), response.ResponseTypes...),
			ClientName:              response.ClientName,
			Scope:                   response.Scope,
		}
		if len(value.ClientInformation.RedirectURIs) == 0 {
			value.ClientInformation.RedirectURIs = []string{redirectURL}
		}
		if tokenEndpoint != "" {
			value.TokenEndpoint = tokenEndpoint
		}
		return nil
	})
}

func unixOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}
