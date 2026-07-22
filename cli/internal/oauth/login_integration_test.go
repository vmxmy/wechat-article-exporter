package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/config"
)

func TestPersistentHandlerUsesDynamicRegistrationAndSavesRefreshableSession(t *testing.T) {
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, oauthex.ProtectedResourceMetadata{
			Resource:             server.URL + "/mcp",
			AuthorizationServers: []string{server.URL},
			ScopesSupported:      []string{"wechat.read"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, oauthex.AuthServerMeta{
			Issuer:                            server.URL,
			AuthorizationEndpoint:             server.URL + "/authorize",
			TokenEndpoint:                     server.URL + "/token",
			RegistrationEndpoint:              server.URL + "/register",
			ResponseTypesSupported:            []string{"code"},
			GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
			TokenEndpointAuthMethodsSupported: []string{"none"},
			CodeChallengeMethodsSupported:     []string{"S256"},
		})
	})
	mux.HandleFunc("/register", func(writer http.ResponseWriter, request *http.Request) {
		var metadata oauthex.ClientRegistrationMetadata
		if err := json.NewDecoder(request.Body).Decode(&metadata); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusCreated)
		payload := map[string]any{
			"client_id":                  "dynamic-client",
			"redirect_uris":              metadata.RedirectURIs,
			"token_endpoint_auth_method": metadata.TokenEndpointAuthMethod,
			"grant_types":                metadata.GrantTypes,
			"response_types":             metadata.ResponseTypes,
			"client_name":                metadata.ClientName,
			"scope":                      metadata.Scope,
		}
		writeJSON(writer, payload)
	})
	mux.HandleFunc("/token", func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Form.Get("client_id") != "dynamic-client" || request.Form.Get("code") != "test-code" {
			http.Error(writer, "bad token request", http.StatusBadRequest)
			return
		}
		writeJSON(writer, map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"token_type":    "bearer",
			"expires_in":    3600,
			"scope":         "wechat.read",
		})
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	store := config.NewStore(filepath.Join(t.TempDir(), "cli.json"))
	redirectURL := "http://127.0.0.1:43210/callback"
	httpClient := NewRegistrationHTTPClient(server.Client(), store, server.URL, redirectURL)
	handler, err := NewPersistentHandler(BrowserOptions{
		Store: store, Server: server.URL, RedirectURL: redirectURL, HTTPClient: httpClient, ClientVersion: "test",
		FetchCode: func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			authorizationURL, err := url.Parse(args.URL)
			if err != nil {
				return nil, err
			}
			if authorizationURL.Query().Get("client_id") != "dynamic-client" {
				return nil, fmt.Errorf("client_id = %q", authorizationURL.Query().Get("client_id"))
			}
			return &auth.AuthorizationResult{Code: "test-code", State: authorizationURL.Query().Get("state")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp", strings.NewReader("{}"))
	response := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header: http.Header{
			"WWW-Authenticate": {fmt.Sprintf(`Bearer resource_metadata=%q, scope="wechat.read"`, server.URL+"/.well-known/oauth-protected-resource")},
		},
		Body: http.NoBody,
	}
	if err := handler.Authorize(context.Background(), request, response); err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	stored, err := store.Read()
	if err != nil {
		t.Fatal(err)
	}
	if stored.Tokens == nil || stored.Tokens.AccessToken != "access-token" || stored.Tokens.RefreshToken != "refresh-token" {
		t.Fatalf("stored tokens = %#v", stored.Tokens)
	}
	if stored.ClientInformation == nil || stored.ClientInformation.ClientID != "dynamic-client" {
		t.Fatalf("stored client = %#v", stored.ClientInformation)
	}
	if stored.TokenEndpoint != server.URL+"/token" {
		t.Fatalf("TokenEndpoint = %q", stored.TokenEndpoint)
	}
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}
