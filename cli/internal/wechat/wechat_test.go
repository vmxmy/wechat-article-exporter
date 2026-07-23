package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtimeutil"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

func TestBeginLoginCapturesUUIDAndReturnsUpstreamQRImage(t *testing.T) {
	qr := fixturePNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("action") {
		case "startlogin":
			if request.Method != http.MethodPost {
				t.Fatalf("startlogin method = %s", request.Method)
			}
			http.SetCookie(writer, &http.Cookie{Name: "uuid", Value: "sanitized-uuid", Path: "/cgi-bin", HttpOnly: true})
			io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"}}`)
		case "getqrcode":
			if cookie, err := request.Cookie("uuid"); err != nil || cookie.Value != "sanitized-uuid" {
				t.Fatalf("uuid cookie = %#v, %v", cookie, err)
			}
			writer.Header().Set("Content-Type", "image/png")
			writer.Write(qr)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()

	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	flow, err := client.BeginLogin(context.Background(), "fixture-session")
	if err != nil {
		t.Fatal(err)
	}
	if flow.SessionID != "fixture-session" || !bytes.Equal(flow.QRBytes, qr) {
		t.Fatalf("unexpected flow: %#v", flow)
	}
	if _, err := RenderQRImageText(flow.QRBytes); err != nil {
		t.Fatalf("render QR: %v", err)
	}
}

func TestBeginLoginAcceptsCurrentUpstreamJPEGQRImage(t *testing.T) {
	qr := fixtureJPEG(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("action") {
		case "startlogin":
			http.SetCookie(writer, &http.Cookie{Name: "uuid", Value: "sanitized-uuid", Path: "/cgi-bin", HttpOnly: true})
			io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"}}`)
		case "getqrcode":
			writer.Header().Set("Content-Type", "image/jpg")
			writer.Write(qr)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()

	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	flow, err := client.BeginLogin(context.Background(), "fixture-session")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(flow.QRBytes, qr) {
		t.Fatal("JPEG QR bytes changed")
	}
	if _, err := RenderQRImageText(flow.QRBytes); err != nil {
		t.Fatalf("render JPEG QR: %v", err)
	}
}

func TestBeginLoginRestoresUUIDFromCurrentJSONResponse(t *testing.T) {
	qr := fixtureJPEG(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("action") {
		case "startlogin":
			io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"},"uuid":"sanitized-json-uuid"}`)
		case "getqrcode":
			cookie, err := request.Cookie("uuid")
			if err != nil || cookie.Value != "sanitized-json-uuid" {
				t.Fatalf("uuid cookie = %#v, %v", cookie, err)
			}
			writer.Header().Set("Content-Type", "image/jpg")
			writer.Write(qr)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()

	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	if _, err := client.BeginLogin(context.Background(), "fixture-session"); err != nil {
		t.Fatal(err)
	}
}

func TestBeginLoginRejectsMissingUUIDCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		io.WriteString(writer, `{"base_resp":{"ret":0}}`)
	}))
	defer server.Close()
	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	_, err := client.BeginLogin(context.Background(), "fixture")
	if err == nil || !strings.Contains(err.Error(), "uuid cookie") {
		t.Fatalf("error = %v", err)
	}
}

func TestPollLoginStatusFixtures(t *testing.T) {
	tests := []struct {
		status int
		count  int
		want   QRState
	}{
		{0, 0, QRWaiting},
		{1, 1, QRConfirmed},
		{2, 1, QRExpired},
		{3, 1, QRExpired},
		{4, 2, QRScanned},
		{4, 0, QRNoAccount},
		{5, 0, QRUnboundEmail},
		{6, 1, QRScanned},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("status_%d_count_%d", test.status, test.count), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(writer).Encode(map[string]any{
					"base_resp": map[string]any{"ret": 0}, "status": test.status, "acct_size": test.count,
				})
			}))
			defer server.Close()
			client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
			result, err := client.PollLogin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.State != test.want || result.AccountCount != test.count {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestListSwitchableAccountsFromSanitizedPollFixture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		io.WriteString(writer, `{
  "base_resp": {"ret": 0},
  "status": 4,
  "acct_size": 2,
  "accounts": [
    {"id": "account-a", "name": "Account A", "avatarUrl": "https://example.invalid/a"},
    {"id": "account-b", "name": "Account B", "alias": "fixture-b"}
  ]
}`)
	}))
	defer server.Close()
	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	if _, err := client.PollLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	accounts, err := client.ListSwitchableAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[1].ID != "account-b" || accounts[1].Name != "Account B" {
		t.Fatalf("accounts = %#v", accounts)
	}
}

func TestLoginRefreshesExpiredQRWithinBound(t *testing.T) {
	qr := fixturePNG(t)
	var mu sync.Mutex
	starts := 0
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		action := request.URL.Query().Get("action")
		switch action {
		case "startlogin":
			mu.Lock()
			starts++
			mu.Unlock()
			http.SetCookie(writer, &http.Cookie{Name: "uuid", Value: fmt.Sprintf("fixture-%d", starts), Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0}}`)
		case "getqrcode":
			writer.Write(qr)
		case "ask":
			mu.Lock()
			polls++
			poll := polls
			mu.Unlock()
			status := 2
			if poll > 1 {
				status = 1
			}
			fmt.Fprintf(writer, `{"base_resp":{"ret":0},"status":%d,"acct_size":1}`, status)
		case "login":
			http.SetCookie(writer, &http.Cookie{Name: "bizuin", Value: "fixture", Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?token=fixture"}`)
		default:
			if request.URL.Path == "/cgi-bin/home" {
				io.WriteString(writer, `wx.cgiData.nick_name = "Fixture";`)
				return
			}
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()
	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	qrCount := 0
	session, err := client.Login(context.Background(), LoginOptions{
		PollInterval: time.Millisecond, MaxRefreshes: 1,
		OnQR: func(LoginFlow) error { qrCount++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.State != SessionAuthenticated || starts != 2 || qrCount != 2 {
		t.Fatalf("session=%#v starts=%d qrCount=%d", session, starts, qrCount)
	}
}

func TestLoginCompletesWhenUpstreamReportsScanned(t *testing.T) {
	qr := fixturePNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("action") {
		case "startlogin":
			http.SetCookie(writer, &http.Cookie{Name: "uuid", Value: "fixture-scanned", Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0}}`)
		case "getqrcode":
			writer.Write(qr)
		case "ask":
			io.WriteString(writer, `{"base_resp":{"ret":0},"status":4,"acct_size":1}`)
		case "login":
			http.SetCookie(writer, &http.Cookie{Name: "bizuin", Value: "fixture", Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?token=fixture"}`)
		default:
			if request.URL.Path == "/cgi-bin/home" {
				io.WriteString(writer, `wx.cgiData.nick_name = "Fixture";`)
				return
			}
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()

	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	session, err := client.Login(context.Background(), LoginOptions{PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if session.State != SessionAuthenticated {
		t.Fatalf("session=%#v", session)
	}
}

func TestLoginCancellationStopsPollingWithoutCompletingOrPersistingSession(t *testing.T) {
	qr := fixturePNG(t)
	store := secrets.NewMemoryStore()
	var mu sync.Mutex
	polls := 0
	completions := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Query().Get("action") {
		case "startlogin":
			http.SetCookie(writer, &http.Cookie{Name: "uuid", Value: "fixture-cancel", Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0}}`)
		case "getqrcode":
			writer.Write(qr)
		case "ask":
			mu.Lock()
			polls++
			mu.Unlock()
			io.WriteString(writer, `{"base_resp":{"ret":0},"status":0,"acct_size":0}`)
		case "login":
			mu.Lock()
			completions++
			mu.Unlock()
			io.WriteString(writer, `{"base_resp":{"ret":0}}`)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()

	client := newClient(server.Client(), store, "profile-cancel", server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	statusSeen := make(chan struct{}, 1)
	errChannel := make(chan error, 1)
	go func() {
		_, err := client.Login(ctx, LoginOptions{
			PollInterval: time.Hour,
			OnStatus: func(PollResult) {
				select {
				case statusSeen <- struct{}{}:
				default:
				}
			},
		})
		errChannel <- err
	}()
	select {
	case <-statusSeen:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("login did not reach polling before cancellation")
	}
	select {
	case err := <-errChannel:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled login error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled login did not stop promptly")
	}
	mu.Lock()
	defer mu.Unlock()
	if polls != 1 || completions != 0 {
		t.Fatalf("polls=%d completions=%d", polls, completions)
	}
	if _, err := store.Get(context.Background(), secrets.Ref{Profile: "profile-cancel", Kind: "wechat-session", Name: "current"}); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("cancelled login persisted session: %v", err)
	}
}

func TestPersistedSessionSurvivesClientAndProcessRuntimeRecreation(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	store := secrets.NewMemoryStore()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/cgi-bin/home" || request.URL.Query().Get("token") != "restart-token" {
			t.Fatalf("unexpected restart validation request %s", request.URL)
		}
		cookie, err := request.Cookie("bizuin")
		if err != nil || cookie.Value != "restart-cookie" {
			t.Fatalf("restart cookie = %#v, %v", cookie, err)
		}
		io.WriteString(writer, `wx.cgiData.nick_name = "Restart Account"; wx.cgiData.fakeid = "restart-fakeid";`)
	}))
	defer server.Close()

	first := newClient(server.Client(), store, "profile-restart", server.URL)
	first.now = func() time.Time { return now }
	if err := first.persistSession(context.Background(), Session{
		State: SessionAuthenticated, Token: "restart-token", AccountID: "restart-fakeid", AccountName: "Restart Account",
		CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), Cookies: []Cookie{{
			Name: "bizuin", Value: "restart-cookie", Domain: "127.0.0.1", Path: "/", HostOnly: true,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	// A distinct client with a fresh jar models a newly composed application
	// runtime after process restart. Only the profile secret store is shared.
	second := newClient(server.Client(), store, "profile-restart", server.URL)
	second.now = func() time.Time { return now.Add(time.Minute) }
	status, err := second.SessionStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != SessionAuthenticated || status.AccountID != "restart-fakeid" || status.AccountName != "Restart Account" ||
		status.Token != "" || len(status.Cookies) != 0 || requests != 1 {
		t.Fatalf("recreated runtime status=%#v requests=%d", status, requests)
	}
}

func TestCompleteLoginPersistsTokenCookieAttributesAndIdentity(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	expires := now.Add(48 * time.Hour)
	store := secrets.NewMemoryStore()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/cgi-bin/bizlogin":
			http.SetCookie(writer, &http.Cookie{
				Name: "bizuin", Value: "sanitized-bizuin", Path: "/cgi-bin", Expires: expires,
				HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
			io.WriteString(writer, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?t=home/index&token=sanitized-token"}`)
		case "/cgi-bin/home":
			if request.URL.Query().Get("token") != "sanitized-token" {
				t.Fatalf("token query = %q", request.URL.Query().Get("token"))
			}
			if _, err := request.Cookie("bizuin"); err != nil {
				t.Fatalf("missing authenticated cookie: %v", err)
			}
			io.WriteString(writer, `wx.cgiData.nick_name = "Fixture&nbsp;Account"; wx.cgiData.head_img = "https://example.invalid/avatar"; wx.cgiData.fakeid = "fixture-fakeid";`)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
	}))
	defer server.Close()
	client := newClient(server.Client(), store, "profile-a", server.URL)
	client.now = func() time.Time { return now }

	session, err := client.CompleteLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if session.Token != "sanitized-token" || session.AccountName != "Fixture Account" || session.AccountID != "fixture-fakeid" {
		t.Fatalf("session = %#v", session)
	}
	if !session.ExpiresAt.Equal(expires) || len(session.Cookies) != 1 {
		t.Fatalf("expiry/cookies = %v %#v", session.ExpiresAt, session.Cookies)
	}
	cookie := session.Cookies[0]
	if cookie.Path != "/cgi-bin" || cookie.Domain != "127.0.0.1" || !cookie.HostOnly || !cookie.HTTPOnly || cookie.SameSite != int(http.SameSiteLaxMode) {
		t.Fatalf("cookie attributes = %#v", cookie)
	}
	encoded, err := store.Get(context.Background(), client.sessionRef())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("sanitized-token")) || !bytes.Contains(encoded, []byte("sanitized-bizuin")) {
		t.Fatalf("persisted secret did not contain session material")
	}
	public, err := client.SessionStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if public.Token != "" || public.Cookies != nil || public.State != SessionAuthenticated {
		t.Fatalf("public status leaked secret: %#v", public)
	}
}

func TestCompleteLoginRejectsInvalidRedirectMissingTokenAndCookies(t *testing.T) {
	tests := []struct {
		name      string
		redirect  string
		setCookie bool
		want      string
	}{
		{"invalid-host", "https://evil.invalid/cgi-bin/home?token=x", true, "invalid redirect host"},
		{"missing-token", "/cgi-bin/home?t=home/index", true, "management token"},
		{"missing-cookies", "/cgi-bin/home?token=x", false, "session cookies"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.setCookie {
					http.SetCookie(writer, &http.Cookie{Name: "bizuin", Value: "fixture", Path: "/"})
				}
				json.NewEncoder(writer).Encode(map[string]any{
					"base_resp": map[string]any{"ret": 0}, "redirect_url": test.redirect,
				})
			}))
			defer server.Close()
			client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
			_, err := client.CompleteLogin(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestCompleteLoginRejectsHomeIdentityFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/cgi-bin/bizlogin" {
			http.SetCookie(writer, &http.Cookie{Name: "bizuin", Value: "fixture", Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?token=fixture"}`)
			return
		}
		io.WriteString(writer, `<html>login page</html>`)
	}))
	defer server.Close()
	client := newClient(server.Client(), secrets.NewMemoryStore(), "default", server.URL)
	_, err := client.CompleteLogin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "account identity") {
		t.Fatalf("error = %v", err)
	}
}

func TestSessionStatusExpiredAndNetworkUnknown(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	store := secrets.NewMemoryStore()
	client := newClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}, store, "default", "http://127.0.0.1")
	client.now = func() time.Time { return now }
	if err := client.persistSession(context.Background(), Session{Token: "secret", ExpiresAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	status, err := client.SessionStatus(context.Background())
	if err != nil || status.State != SessionExpired || status.Validation != "expired" {
		t.Fatalf("expired status = %#v, %v", status, err)
	}
	if err := client.persistSession(context.Background(), Session{Token: "secret", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	status, err = client.SessionStatus(context.Background())
	if err != nil || status.State != SessionNetworkUnknown || status.Validation != "network_unknown" {
		t.Fatalf("network status = %#v, %v", status, err)
	}
}

func TestPersistentCookiesPreserveDomainPathExpiryAndSelection(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	store := secrets.NewMemoryStore()
	client := newClient(&http.Client{}, store, "default", "https://mp.weixin.qq.com")
	client.now = func() time.Time { return now }
	jarNow := time.Now()
	cookies := []Cookie{
		{Name: "root", Value: "one", Domain: "mp.weixin.qq.com", Path: "/", HostOnly: true, Expires: jarNow.Add(time.Hour), Secure: true},
		{Name: "cgi", Value: "two", Domain: ".weixin.qq.com", Path: "/cgi-bin", Expires: jarNow.Add(2 * time.Hour), Secure: true},
		{Name: "expired", Value: "three", Domain: "mp.weixin.qq.com", Path: "/", HostOnly: true, Expires: now.Add(-time.Hour)},
	}
	client.restoreCookies(cookies)
	homeURL, _ := url.Parse("https://mp.weixin.qq.com/cgi-bin/home")
	got := client.http.Jar.Cookies(homeURL)
	names := map[string]string{}
	for _, cookie := range got {
		names[cookie.Name] = cookie.Value
	}
	if !reflect.DeepEqual(names, map[string]string{"root": "one", "cgi": "two"}) {
		t.Fatalf("cookies = %#v", names)
	}
	rootURL, _ := url.Parse("https://mp.weixin.qq.com/")
	for _, cookie := range client.http.Jar.Cookies(rootURL) {
		if cookie.Name == "cgi" {
			t.Fatalf("path-scoped cookie leaked to root")
		}
	}
}

func TestAccountSwitchFixture(t *testing.T) {
	store := secrets.NewMemoryStore()
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		switch request.URL.Path {
		case "/cgi-bin/bizlogin":
			body, _ := io.ReadAll(request.Body)
			if !strings.Contains(string(body), "action=switch_account") || !strings.Contains(string(body), "acct_id=account-b") {
				t.Fatalf("switch payload = %q", body)
			}
			http.SetCookie(writer, &http.Cookie{Name: "bizuin", Value: "account-b-cookie", Path: "/"})
			io.WriteString(writer, `{"base_resp":{"ret":0},"redirect_url":"/cgi-bin/home?token=new-token"}`)
		case "/cgi-bin/home":
			io.WriteString(writer, `wx.cgiData.nick_name = "Account B"; wx.cgiData.fakeid = "account-b";`)
		}
	}))
	defer server.Close()
	client := newClient(server.Client(), store, "default", server.URL)
	if err := client.persistSession(context.Background(), Session{Token: "old-token", Cookies: []Cookie{{Name: "bizuin", Value: "old", Domain: "127.0.0.1", Path: "/", HostOnly: true}}}); err != nil {
		t.Fatal(err)
	}
	status, err := client.SwitchAccount(context.Background(), "account-b")
	if err != nil {
		t.Fatal(err)
	}
	if status.AccountID != "account-b" || status.AccountName != "Account B" || status.Token != "" {
		t.Fatalf("status = %#v", status)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestLogoutDeletesLocalSecretWhenUpstreamFails(t *testing.T) {
	store := secrets.NewMemoryStore()
	client := newClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("logout failed")
	})}, store, "default", "http://127.0.0.1")
	if err := client.persistSession(context.Background(), Session{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), client.sessionRef()); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("secret still present: %v", err)
	}
}

func TestWriteAndRenderQRImage(t *testing.T) {
	pngBytes := fixturePNG(t)
	path := filepath.Join(t.TempDir(), "nested", "login.png")
	if err := WriteQRImage(path, pngBytes); err != nil {
		t.Fatal(err)
	}
	runtimeutil.AssertPrivatePermissions(t, path, 0o600)
	text, err := RenderQRImageText(pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.ContainsAny(text, "█▀▄") {
		t.Fatalf("rendered QR lacks blocks: %q", text)
	}
}

func TestRenderQRImageTextDownsamplesRasterModulesAndCropsQuietZone(t *testing.T) {
	const modules, scale, quiet = 29, 4, 3
	size := (modules + quiet*2) * scale
	imageValue := image.NewGray(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			imageValue.SetGray(x, y, color.Gray{Y: 255})
		}
	}
	for moduleY := 0; moduleY < modules; moduleY++ {
		for moduleX := 0; moduleX < modules; moduleX++ {
			if (moduleX+moduleY)%3 != 0 && moduleX != 0 && moduleY != 0 {
				continue
			}
			for y := 0; y < scale; y++ {
				for x := 0; x < scale; x++ {
					imageValue.SetGray((moduleX+quiet)*scale+x, (moduleY+quiet)*scale+y, color.Gray{Y: 0})
				}
			}
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, imageValue); err != nil {
		t.Fatal(err)
	}
	text, err := RenderQRImageText(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != (modules+1)/2 {
		t.Fatalf("QR terminal row count = %d, want %d", len(lines), (modules+1)/2)
	}
	for _, line := range lines {
		if got := len([]rune(line)); got != modules {
			t.Fatalf("QR terminal width = %d, want %d; line=%q", got, modules, line)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func fixturePNG(t *testing.T) []byte {
	t.Helper()
	imageValue := image.NewGray(image.Rect(0, 0, 29, 29))
	for y := 0; y < 29; y++ {
		for x := 0; x < 29; x++ {
			value := uint8(255)
			if x == 0 || y == 0 || x == 28 || y == 28 || (x+y)%3 == 0 {
				value = 0
			}
			imageValue.SetGray(x, y, color.Gray{Y: value})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, imageValue); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func fixtureJPEG(t *testing.T) []byte {
	t.Helper()
	imageValue := image.NewGray(image.Rect(0, 0, 29, 29))
	for y := 0; y < 29; y++ {
		for x := 0; x < 29; x++ {
			value := uint8(255)
			if x == 0 || y == 0 || x == 28 || y == 28 || (x+y)%3 == 0 {
				value = 0
			}
			imageValue.SetGray(x, y, color.Gray{Y: value})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, imageValue, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
