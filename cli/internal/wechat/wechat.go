package wechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

const (
	upstreamOrigin           = "https://mp.weixin.qq.com"
	defaultSessionLifetime   = 4 * 24 * time.Hour
	defaultLoginPollInterval = 2 * time.Second
)

var (
	ErrLoginExpired     = errors.New("WeChat login QR code expired")
	ErrSessionExpired   = errors.New("WeChat session expired; run login again")
	ErrAccountSwitching = errors.New("WeChat did not expose switchable account identities for this session")
)

type SessionState string

const (
	SessionMissing        SessionState = "missing"
	SessionAuthenticated  SessionState = "authenticated"
	SessionExpired        SessionState = "expired"
	SessionNetworkUnknown SessionState = "network_unknown"
)

type QRState string

const (
	QRWaiting      QRState = "waiting"
	QRConfirmed    QRState = "confirmed"
	QRExpired      QRState = "expired"
	QRScanned      QRState = "scanned"
	QRNoAccount    QRState = "no_account"
	QRUnboundEmail QRState = "unbound_email"
)

type Session struct {
	State           SessionState     `json:"state"`
	AccountID       domain.AccountID `json:"accountId,omitempty"`
	AccountName     string           `json:"accountName,omitempty"`
	AvatarURL       string           `json:"avatarUrl,omitempty"`
	CreatedAt       time.Time        `json:"createdAt,omitempty"`
	ExpiresAt       time.Time        `json:"expiresAt,omitempty"`
	LastValidatedAt time.Time        `json:"lastValidatedAt,omitempty"`
	Validation      string           `json:"validation,omitempty"`
	Token           string           `json:"token,omitempty"`
	Cookies         []Cookie         `json:"cookies,omitempty"`
}

type sessionData Session

func (session Session) MarshalJSON() ([]byte, error) {
	value := sessionData(session)
	value.Token = ""
	value.Cookies = nil
	return json.Marshal(value)
}

type sessionSecret struct {
	sessionData
	Token   string   `json:"token"`
	Cookies []Cookie `json:"cookies"`
}

type Cookie struct {
	Name       string    `json:"name"`
	Value      string    `json:"value"`
	Path       string    `json:"path,omitempty"`
	Domain     string    `json:"domain,omitempty"`
	Expires    time.Time `json:"expires,omitempty"`
	RawExpires string    `json:"rawExpires,omitempty"`
	MaxAge     int       `json:"maxAge,omitempty"`
	Secure     bool      `json:"secure,omitempty"`
	HTTPOnly   bool      `json:"httpOnly,omitempty"`
	SameSite   int       `json:"sameSite,omitempty"`
	HostOnly   bool      `json:"hostOnly,omitempty"`
}

type LoginFlow struct {
	SessionID string    `json:"sessionId"`
	QRBytes   []byte    `json:"-"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type PollResult struct {
	State        QRState `json:"state"`
	AccountCount int     `json:"accountCount"`
}

type LoginOptions struct {
	SessionID    string
	PollInterval time.Duration
	MaxRefreshes int
	OnQR         func(LoginFlow) error
	OnStatus     func(PollResult)
}

type SwitchableAccount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Alias     string `json:"alias,omitempty"`
}

type Gateway interface {
	SessionStatus(context.Context) (Session, error)
	SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
}

type SessionGateway interface {
	BeginLogin(context.Context, string) (LoginFlow, error)
	PollLogin(context.Context) (PollResult, error)
	CompleteLogin(context.Context) (Session, error)
	SessionStatus(context.Context) (Session, error)
	ListSwitchableAccounts(context.Context) ([]SwitchableAccount, error)
	SwitchAccount(context.Context, string) (Session, error)
	Logout(context.Context) error
}

type Client struct {
	http    *http.Client
	secrets secrets.Store
	profile string
	now     func() time.Time
	baseURL *url.URL

	mu                 sync.Mutex
	capturedCookies    map[cookieKey]Cookie
	switchableAccounts []SwitchableAccount
}

type cookieKey struct {
	Name   string
	Domain string
	Path   string
}

func NewClient(httpClient *http.Client, secretStore secrets.Store, profile string) *Client {
	return newClient(httpClient, secretStore, profile, upstreamOrigin)
}

// ParseControlledOrigin validates the loopback-only release harness upstream.
func ParseControlledOrigin(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !validControlledOrigin(parsed) {
		return nil, errors.New("controlled WeChat origin must be loopback HTTP without path, query, fragment, or user information")
	}
	return parsed, nil
}

func validControlledOrigin(origin *url.URL) bool {
	if origin == nil || origin.User != nil || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" ||
		origin.Path != "" || origin.RawPath != "" || origin.Opaque != "" || origin.Scheme != "http" || origin.Port() == "" {
		return false
	}
	address := net.ParseIP(origin.Hostname())
	if address == nil || !address.IsLoopback() {
		return false
	}
	port, err := strconv.Atoi(origin.Port())
	return err == nil && port > 0 && port <= 65535
}

// NewClientForControlledOrigin constructs a client for a loopback-only release
// harness. It intentionally rejects public hosts so production callers cannot
// use this seam to redirect credentials or article traffic.
func NewClientForControlledOrigin(httpClient *http.Client, secretStore secrets.Store, profile string, origin *url.URL) (*Client, error) {
	if !validControlledOrigin(origin) {
		return nil, errors.New("controlled WeChat origin must be loopback HTTP without path, query, fragment, or user information")
	}
	clone := *origin
	controlledClient, err := controlledOriginHTTPClient(httpClient, &clone)
	if err != nil {
		return nil, err
	}
	client := newClient(controlledClient, secretStore, profile, clone.String())
	return client, nil
}

func controlledOriginHTTPClient(source *http.Client, origin *url.URL) (*http.Client, error) {
	if source == nil {
		source = &http.Client{Timeout: 30 * time.Second}
	}
	client := *source
	transport := source.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	if direct, ok := transport.(*http.Transport); !ok || direct == nil {
		return nil, errors.New("controlled WeChat origin requires an auditable direct HTTP transport")
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || defaultTransport == nil {
		return nil, errors.New("controlled WeChat origin requires the default direct HTTP transport")
	}
	direct := defaultTransport.Clone()
	direct.Proxy = nil
	direct.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	direct.DialTLSContext = nil
	direct.TLSClientConfig = nil
	client.Transport = controlledOriginTransport{origin: origin, next: direct}
	previousRedirectCheck := source.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !matchesControlledAuthority(request.URL, origin) {
			return errors.New("controlled WeChat redirect changed origin")
		}
		if previousRedirectCheck != nil {
			return previousRedirectCheck(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &client, nil
}

type controlledOriginTransport struct {
	origin *url.URL
	next   http.RoundTripper
}

func (transport controlledOriginTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || !matchesControlledAuthority(request.URL, transport.origin) {
		return nil, errors.New("controlled WeChat request changed origin")
	}
	return transport.next.RoundTrip(request)
}

func matchesControlledAuthority(target, origin *url.URL) bool {
	return validControlledOrigin(origin) && target != nil && target.User == nil && target.Scheme == origin.Scheme &&
		strings.EqualFold(target.Host, origin.Host)
}

func newClient(httpClient *http.Client, secretStore secrets.Store, profile, baseURL string) *Client {
	if httpClient == nil {
		jar, _ := cookiejar.New(nil)
		httpClient = &http.Client{Timeout: 30 * time.Second, Jar: jar}
	} else {
		clone := *httpClient
		if clone.Jar == nil {
			clone.Jar, _ = cookiejar.New(nil)
		}
		httpClient = &clone
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		panic(fmt.Sprintf("invalid WeChat base URL: %v", err))
	}
	return &Client{
		http: httpClient, secrets: secretStore, profile: profile, now: time.Now, baseURL: base,
		capturedCookies: make(map[cookieKey]Cookie),
	}
}

func (client *Client) BeginLogin(ctx context.Context, sessionID string) (LoginFlow, error) {
	if sessionID == "" {
		sessionID = strconv.FormatInt(client.now().UnixNano(), 10)
	}
	client.clearLoginState()
	payload := url.Values{
		"userlang": {"zh_CN"}, "redirect_url": {""}, "login_type": {"3"}, "sessionid": {sessionID},
		"token": {""}, "lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"},
	}
	response, err := client.request(ctx, http.MethodPost, "/cgi-bin/bizlogin", url.Values{"action": {"startlogin"}}, strings.NewReader(payload.Encode()))
	if err != nil {
		return LoginFlow{}, err
	}
	defer response.Body.Close()
	var started struct {
		BaseResp struct {
			Ret    int    `json:"ret"`
			ErrMsg string `json:"err_msg"`
		} `json:"base_resp"`
		UUID string `json:"uuid"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&started); err != nil {
		return LoginFlow{}, fmt.Errorf("decode login session response: %w", err)
	}
	// Some current WeChat responses expose uuid in both Set-Cookie and the JSON
	// body. Keep accepting the cookie as the primary source, but use the body as
	// a compatibility fallback when an intermediary strips Set-Cookie.
	if started.BaseResp.Ret != 0 {
		return LoginFlow{}, fmt.Errorf("start login failed: %s", started.BaseResp.ErrMsg)
	}
	if !client.hasUsableCookie("uuid") && strings.TrimSpace(started.UUID) != "" {
		client.setLoginCookie(Cookie{
			Name: "uuid", Value: strings.TrimSpace(started.UUID), Domain: client.baseURL.Hostname(), Path: "/",
			Secure: client.baseURL.Scheme == "https", HTTPOnly: true, HostOnly: true,
		})
	}
	if !client.hasUsableCookie("uuid") {
		return LoginFlow{}, errors.New("start login response did not contain the required uuid cookie")
	}
	qrResponse, err := client.request(ctx, http.MethodGet, "/cgi-bin/scanloginqrcode", url.Values{
		"action": {"getqrcode"}, "random": {strconv.FormatInt(client.now().UnixMilli(), 10)},
	}, nil)
	if err != nil {
		return LoginFlow{}, err
	}
	defer qrResponse.Body.Close()
	if qrResponse.StatusCode != http.StatusOK {
		return LoginFlow{}, fmt.Errorf("get QR code returned HTTP %d", qrResponse.StatusCode)
	}
	qrBytes, err := io.ReadAll(io.LimitReader(qrResponse.Body, 4<<20))
	if err != nil {
		return LoginFlow{}, err
	}
	if _, err := DecodeQRImage(qrBytes); err != nil {
		return LoginFlow{}, fmt.Errorf("decode login QR image: %w", err)
	}
	return LoginFlow{SessionID: sessionID, QRBytes: qrBytes, ExpiresAt: client.now().Add(5 * time.Minute)}, nil
}

func (client *Client) setLoginCookie(cookie Cookie) {
	client.mu.Lock()
	client.capturedCookies[cookieKey{Name: cookie.Name, Domain: strings.ToLower(cookie.Domain), Path: cookie.Path}] = cookie
	client.mu.Unlock()
	if client.http.Jar == nil {
		return
	}
	client.http.Jar.SetCookies(client.baseURL, []*http.Cookie{{
		Name: cookie.Name, Value: cookie.Value, Path: cookie.Path, Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly,
	}})
}

func (client *Client) PollLogin(ctx context.Context) (PollResult, error) {
	response, err := client.request(ctx, http.MethodGet, "/cgi-bin/scanloginqrcode", url.Values{
		"action": {"ask"}, "token": {""}, "lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"},
	}, nil)
	if err != nil {
		return PollResult{}, err
	}
	defer response.Body.Close()
	var payload struct {
		BaseResp struct {
			Ret    int    `json:"ret"`
			ErrMsg string `json:"err_msg"`
		} `json:"base_resp"`
		Status   int                 `json:"status"`
		AcctSize int                 `json:"acct_size"`
		Accounts []SwitchableAccount `json:"accounts"`
		AcctList []SwitchableAccount `json:"acct_list"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return PollResult{}, fmt.Errorf("decode QR poll response: %w", err)
	}
	if payload.BaseResp.Ret != 0 {
		return PollResult{}, fmt.Errorf("QR poll failed: %s", payload.BaseResp.ErrMsg)
	}
	accounts := payload.Accounts
	if len(accounts) == 0 {
		accounts = payload.AcctList
	}
	if len(accounts) > 0 {
		client.mu.Lock()
		client.switchableAccounts = append([]SwitchableAccount(nil), accounts...)
		client.mu.Unlock()
	}
	state := QRWaiting
	switch payload.Status {
	case 1:
		state = QRConfirmed
	case 2, 3:
		state = QRExpired
	case 4, 6:
		if payload.AcctSize > 0 {
			state = QRScanned
		} else {
			state = QRNoAccount
		}
	case 5:
		state = QRUnboundEmail
	}
	return PollResult{State: state, AccountCount: payload.AcctSize}, nil
}

func (client *Client) CompleteLogin(ctx context.Context) (Session, error) {
	payload := url.Values{
		"userlang": {"zh_CN"}, "redirect_url": {""}, "cookie_forbidden": {"0"}, "cookie_cleaned": {"0"},
		"plugin_used": {"0"}, "login_type": {"3"}, "token": {""}, "lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"},
	}
	response, err := client.request(ctx, http.MethodPost, "/cgi-bin/bizlogin", url.Values{"action": {"login"}}, strings.NewReader(payload.Encode()))
	if err != nil {
		return Session{}, err
	}
	defer response.Body.Close()
	var login struct {
		BaseResp struct {
			Ret    int    `json:"ret"`
			ErrMsg string `json:"err_msg"`
		} `json:"base_resp"`
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&login); err != nil {
		return Session{}, fmt.Errorf("decode login response: %w", err)
	}
	if login.BaseResp.Ret != 0 {
		return Session{}, fmt.Errorf("complete login failed: %s", login.BaseResp.ErrMsg)
	}
	redirect, err := client.validLoginRedirect(login.RedirectURL)
	if err != nil {
		return Session{}, err
	}
	token := redirect.Query().Get("token")
	if token == "" {
		return Session{}, errors.New("login response did not contain a management token")
	}
	cookies := client.cookies()
	if !containsPersistentSessionCookie(cookies) {
		return Session{}, errors.New("login response did not contain authenticated session cookies")
	}
	home, err := client.fetchHome(ctx, token)
	if err != nil {
		return Session{}, err
	}
	now := client.now()
	session := Session{
		State: SessionAuthenticated, AccountID: domain.AccountID(home.AccountID), AccountName: home.AccountName,
		AvatarURL: home.AvatarURL, Token: token, Cookies: cookies, CreatedAt: now,
		ExpiresAt: sessionExpiry(cookies, now), LastValidatedAt: now, Validation: "valid",
	}
	if err := client.persistSession(ctx, session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (client *Client) Login(ctx context.Context, options LoginOptions) (Session, error) {
	interval := options.PollInterval
	if interval <= 0 {
		interval = defaultLoginPollInterval
	}
	refreshes := options.MaxRefreshes
	if refreshes < 0 {
		refreshes = 0
	}
	for attempt := 0; attempt <= refreshes; attempt++ {
		flow, err := client.BeginLogin(ctx, options.SessionID)
		if err != nil {
			return Session{}, err
		}
		if options.OnQR != nil {
			if err := options.OnQR(flow); err != nil {
				return Session{}, err
			}
		}
		for {
			result, err := client.PollLogin(ctx)
			if err != nil {
				return Session{}, err
			}
			if options.OnStatus != nil {
				options.OnStatus(result)
			}
			switch result.State {
			case QRConfirmed, QRScanned:
				return client.CompleteLogin(ctx)
			case QRExpired:
				client.clearLoginState()
				if attempt == refreshes {
					return Session{}, ErrLoginExpired
				}
				goto refresh
			}
			select {
			case <-ctx.Done():
				return Session{}, ctx.Err()
			case <-time.After(interval):
			}
		}
	refresh:
	}
	return Session{}, ErrLoginExpired
}

func (client *Client) SessionStatus(ctx context.Context) (Session, error) {
	session, err := client.loadSession(ctx)
	if errors.Is(err, secrets.ErrNotFound) {
		return Session{State: SessionMissing, Validation: "missing"}, nil
	}
	if err != nil {
		return Session{}, err
	}
	if !session.ExpiresAt.IsZero() && !client.now().Before(session.ExpiresAt) {
		session.State = SessionExpired
		session.Validation = "expired"
		return session.public(), nil
	}
	client.restoreCookies(session.Cookies)
	home, err := client.fetchHome(ctx, session.Token)
	if err != nil {
		var upstream *upstreamError
		if errors.As(err, &upstream) && upstream.Authentication {
			session.State = SessionExpired
			session.Validation = "expired"
			_ = client.persistSession(ctx, session)
			return session.public(), nil
		}
		session.State = SessionNetworkUnknown
		session.Validation = "network_unknown"
		return session.public(), nil
	}
	now := client.now()
	session.State = SessionAuthenticated
	session.AccountID = domain.AccountID(home.AccountID)
	session.AccountName = home.AccountName
	session.AvatarURL = home.AvatarURL
	session.LastValidatedAt = now
	session.Validation = "valid"
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if err := client.persistSession(ctx, session); err != nil {
		return Session{}, err
	}
	return session.public(), nil
}

func (client *Client) RequireSession(ctx context.Context) (Session, error) {
	status, err := client.SessionStatus(ctx)
	if err != nil {
		return Session{}, err
	}
	if status.State == SessionExpired || status.State == SessionMissing {
		return Session{}, ErrSessionExpired
	}
	if status.State == SessionNetworkUnknown {
		return Session{}, errors.New("cannot validate WeChat session because the network state is unknown")
	}
	return status, nil
}

func (client *Client) ListSwitchableAccounts(context.Context) ([]SwitchableAccount, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.switchableAccounts) == 0 {
		return nil, ErrAccountSwitching
	}
	return append([]SwitchableAccount(nil), client.switchableAccounts...), nil
}

func (client *Client) SwitchAccount(ctx context.Context, accountID string) (Session, error) {
	if strings.TrimSpace(accountID) == "" {
		return Session{}, errors.New("switch account requires an account identifier")
	}
	session, err := client.loadSession(ctx)
	if err != nil {
		return Session{}, err
	}
	client.restoreCookies(session.Cookies)
	payload := url.Values{
		"action": {"switch_account"}, "acct_id": {accountID}, "token": {session.Token},
		"lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"},
	}
	response, err := client.request(ctx, http.MethodPost, "/cgi-bin/bizlogin", nil, strings.NewReader(payload.Encode()))
	if err != nil {
		return Session{}, err
	}
	defer response.Body.Close()
	var switched struct {
		BaseResp struct {
			Ret    int    `json:"ret"`
			ErrMsg string `json:"err_msg"`
		} `json:"base_resp"`
		RedirectURL string `json:"redirect_url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&switched); err != nil {
		return Session{}, fmt.Errorf("decode account switch response: %w", err)
	}
	if switched.BaseResp.Ret != 0 {
		return Session{}, fmt.Errorf("switch account failed: %s", switched.BaseResp.ErrMsg)
	}
	if switched.RedirectURL != "" {
		redirect, err := client.validLoginRedirect(switched.RedirectURL)
		if err != nil {
			return Session{}, err
		}
		if token := redirect.Query().Get("token"); token != "" {
			session.Token = token
		}
	}
	home, err := client.fetchHome(ctx, session.Token)
	if err != nil {
		return Session{}, err
	}
	now := client.now()
	session.State = SessionAuthenticated
	session.AccountID = domain.AccountID(home.AccountID)
	session.AccountName = home.AccountName
	session.AvatarURL = home.AvatarURL
	session.Cookies = client.cookies()
	session.LastValidatedAt = now
	session.Validation = "valid"
	if err := client.persistSession(ctx, session); err != nil {
		return Session{}, err
	}
	return session.public(), nil
}

func (client *Client) Logout(ctx context.Context) error {
	session, err := client.loadSession(ctx)
	if err == nil && session.Token != "" {
		client.restoreCookies(session.Cookies)
		response, requestErr := client.request(ctx, http.MethodGet, "/cgi-bin/logout", url.Values{
			"t": {"wxm-logout"}, "token": {session.Token}, "lang": {"zh_CN"},
		}, nil)
		if requestErr == nil && response != nil {
			response.Body.Close()
		}
	}
	client.clearLoginState()
	if client.secrets == nil {
		return nil
	}
	return client.secrets.Delete(ctx, client.sessionRef())
}

type homeInfo struct {
	AccountID   string
	AccountName string
	AvatarURL   string
}

var (
	nicknamePattern  = regexp.MustCompile(`wx\.cgiData\.nick_name\s*=\s*"([^"]*)"`)
	headImagePattern = regexp.MustCompile(`wx\.cgiData\.head_img\s*=\s*"([^"]*)"`)
	accountIDPattern = regexp.MustCompile(`wx\.cgiData\.(?:fakeid|fake_id|account_id)\s*=\s*"([^"]*)"`)
)

func (client *Client) fetchHome(ctx context.Context, token string) (homeInfo, error) {
	response, err := client.request(ctx, http.MethodGet, "/cgi-bin/home", url.Values{
		"t": {"home/index"}, "token": {token}, "lang": {"zh_CN"},
	}, nil)
	if err != nil {
		return homeInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return homeInfo{}, &upstreamError{Authentication: true, Message: ErrSessionExpired.Error()}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return homeInfo{}, err
	}
	nameMatch := nicknamePattern.FindSubmatch(body)
	if len(nameMatch) < 2 {
		return homeInfo{}, &upstreamError{Authentication: true, Message: "WeChat home page did not contain account identity; run login again"}
	}
	avatar := ""
	if match := headImagePattern.FindSubmatch(body); len(match) >= 2 {
		avatar = string(match[1])
	}
	accountID := ""
	if match := accountIDPattern.FindSubmatch(body); len(match) >= 2 {
		accountID = string(match[1])
	}
	return homeInfo{AccountID: accountID, AccountName: string(nameMatch[1]), AvatarURL: avatar}, nil
}

func (client *Client) request(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Response, error) {
	target := *client.baseURL
	target.Path = path
	if query != nil {
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 wechat-article-local/2")
	request.Header.Set("Referer", client.baseURL.String()+"/")
	request.Header.Set("Origin", client.baseURL.String())
	request.Header.Set("Accept-Encoding", "identity")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	client.captureResponseCookies(response, request.URL)
	return response, nil
}

func (client *Client) validLoginRedirect(value string) (*url.URL, error) {
	redirect, err := url.Parse(value)
	if err != nil || redirect.User != nil || !strings.HasPrefix(redirect.Path, "/cgi-bin/") {
		return nil, errors.New("login response contained an invalid redirect URL")
	}
	if redirect.IsAbs() {
		if redirect.Scheme != "https" || !strings.EqualFold(redirect.Hostname(), "mp.weixin.qq.com") {
			if !(client.baseURL.Scheme == "http" && isLoopbackHost(client.baseURL.Hostname()) && redirect.Host == client.baseURL.Host) {
				return nil, errors.New("login response contained an invalid redirect host")
			}
		}
	}
	return redirect, nil
}

func isLoopbackHost(host string) bool {
	address := net.ParseIP(strings.TrimSpace(host))
	return address != nil && address.IsLoopback()
}

func (client *Client) captureResponseCookies(response *http.Response, requestURL *url.URL) {
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, parsed := range response.Cookies() {
		domain := parsed.Domain
		hostOnly := false
		if domain == "" {
			domain = requestURL.Hostname()
			hostOnly = true
		}
		path := parsed.Path
		if path == "" {
			path = defaultCookiePath(requestURL.Path)
		}
		cookie := Cookie{
			Name: parsed.Name, Value: parsed.Value, Path: path, Domain: domain, Expires: parsed.Expires,
			RawExpires: parsed.RawExpires, MaxAge: parsed.MaxAge, Secure: parsed.Secure,
			HTTPOnly: parsed.HttpOnly, SameSite: int(parsed.SameSite), HostOnly: hostOnly,
		}
		key := cookieKey{Name: cookie.Name, Domain: strings.ToLower(cookie.Domain), Path: cookie.Path}
		if parsed.MaxAge < 0 || (!parsed.Expires.IsZero() && !client.now().Before(parsed.Expires)) {
			delete(client.capturedCookies, key)
			continue
		}
		client.capturedCookies[key] = cookie
	}
}

func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' || requestPath == "/" {
		return "/"
	}
	index := strings.LastIndex(requestPath, "/")
	if index <= 0 {
		return "/"
	}
	return requestPath[:index]
}

func (client *Client) cookies() []Cookie {
	client.mu.Lock()
	defer client.mu.Unlock()
	result := make([]Cookie, 0, len(client.capturedCookies))
	for _, cookie := range client.capturedCookies {
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !client.now().Before(cookie.Expires)) {
			continue
		}
		result = append(result, cookie)
	}
	return result
}

func (client *Client) restoreCookies(cookies []Cookie) {
	if client.http.Jar == nil {
		return
	}
	client.mu.Lock()
	client.capturedCookies = make(map[cookieKey]Cookie, len(cookies))
	client.mu.Unlock()
	byURL := make(map[string][]*http.Cookie)
	for _, cookie := range cookies {
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && !client.now().Before(cookie.Expires)) {
			continue
		}
		domain := strings.TrimPrefix(cookie.Domain, ".")
		if domain == "" {
			domain = client.baseURL.Hostname()
		}
		scheme := client.baseURL.Scheme
		if cookie.Secure {
			scheme = "https"
		}
		origin := &url.URL{Scheme: scheme, Host: domain, Path: cookie.Path}
		converted := &http.Cookie{
			Name: cookie.Name, Value: cookie.Value, Path: cookie.Path, Expires: cookie.Expires,
			RawExpires: cookie.RawExpires, MaxAge: cookie.MaxAge, Secure: cookie.Secure,
			HttpOnly: cookie.HTTPOnly, SameSite: http.SameSite(cookie.SameSite),
		}
		if !cookie.HostOnly {
			converted.Domain = cookie.Domain
		}
		byURL[origin.String()] = append(byURL[origin.String()], converted)
		client.mu.Lock()
		client.capturedCookies[cookieKey{Name: cookie.Name, Domain: strings.ToLower(cookie.Domain), Path: cookie.Path}] = cookie
		client.mu.Unlock()
	}
	for origin, values := range byURL {
		parsed, _ := url.Parse(origin)
		client.http.Jar.SetCookies(parsed, values)
	}
}

func containsPersistentSessionCookie(cookies []Cookie) bool {
	for _, cookie := range cookies {
		if cookie.Name != "uuid" && cookie.Value != "" {
			return true
		}
	}
	return false
}

func sessionExpiry(cookies []Cookie, now time.Time) time.Time {
	expiry := now.Add(defaultSessionLifetime)
	for _, cookie := range cookies {
		if !cookie.Expires.IsZero() && cookie.Expires.Before(expiry) && cookie.Expires.After(now) {
			expiry = cookie.Expires
		}
	}
	return expiry
}

func (client *Client) hasUsableCookie(name string) bool {
	for _, cookie := range client.cookies() {
		if cookie.Name == name && cookie.Value != "" {
			return true
		}
	}
	return false
}

func (client *Client) clearLoginState() {
	client.mu.Lock()
	client.capturedCookies = make(map[cookieKey]Cookie)
	client.switchableAccounts = nil
	client.mu.Unlock()
	jar, _ := cookiejar.New(nil)
	client.http.Jar = jar
}

func (client *Client) persistSession(ctx context.Context, session Session) error {
	if client.secrets == nil {
		return errors.New("secret store is required to persist a WeChat session")
	}
	encoded, err := json.Marshal(sessionSecret{sessionData: sessionData(session), Token: session.Token, Cookies: session.Cookies})
	if err != nil {
		return err
	}
	return client.secrets.Set(ctx, client.sessionRef(), encoded)
}

func (client *Client) loadSession(ctx context.Context) (Session, error) {
	if client.secrets == nil {
		return Session{}, secrets.ErrNotFound
	}
	encoded, err := client.secrets.Get(ctx, client.sessionRef())
	if err != nil {
		return Session{}, err
	}
	var secret sessionSecret
	if err := json.Unmarshal(encoded, &secret); err != nil {
		return Session{}, fmt.Errorf("decode WeChat session: %w", err)
	}
	session := Session(secret.sessionData)
	session.Token = secret.Token
	session.Cookies = secret.Cookies
	return session, nil
}

func (client *Client) sessionRef() secrets.Ref {
	return secrets.Ref{Profile: client.profile, Kind: "wechat-session", Name: "current"}
}

func (session Session) public() Session {
	session.Token = ""
	session.Cookies = nil
	return session
}

type upstreamError struct {
	Authentication bool
	Message        string
}

func (err *upstreamError) Error() string { return err.Message }

func WriteQRImage(path string, imageBytes []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("QR image output path is empty")
	}
	if _, err := DecodeQRImage(imageBytes); err != nil {
		return fmt.Errorf("invalid QR image: %w", err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	directory := filepath.Dir(absolute)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".qr-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(imageBytes); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, absolute); err != nil {
		return err
	}
	committed = true
	return nil
}

func DecodeQRImage(imageBytes []byte) (image.Image, error) {
	if len(imageBytes) == 0 {
		return nil, errors.New("QR image bytes are empty")
	}
	imageValue, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	if imageValue.Bounds().Dx() < 21 || imageValue.Bounds().Dy() < 21 {
		return nil, errors.New("QR image dimensions are too small")
	}
	return imageValue, nil
}

func RenderQRImageText(imageBytes []byte) (string, error) {
	imageValue, err := DecodeQRImage(imageBytes)
	if err != nil {
		return "", err
	}
	return RenderQRCodeText(imageValue), nil
}

// RenderQRCodeText renders square QR modules at a terminal-friendly width.
// WeChat commonly returns a raster QR with a generous quiet zone and multiple
// pixels per module. Rendering raw pixels makes the QR hundreds of terminal
// columns wide, so infer its complete module grid, crop the quiet zone, and
// render one terminal cell per module. The result still uses upper/lower half
// blocks to preserve the QR's square aspect ratio in terminals whose cells are
// taller than wide.
func RenderQRCodeText(imageValue image.Image) string {
	moduleScale, quietZone, moduleCount, ok := qrRasterGrid(imageValue)
	if !ok {
		return RenderImageText(imageValue)
	}
	bounds := imageValue.Bounds()
	left := bounds.Min.X + quietZone*moduleScale
	top := bounds.Min.Y + quietZone*moduleScale
	var builder strings.Builder
	for y := 0; y < moduleCount; y += 2 {
		for x := 0; x < moduleCount; x++ {
			topDark := moduleIsDark(imageValue, left+x*moduleScale, top+y*moduleScale, moduleScale)
			bottomDark := false
			if y+1 < moduleCount {
				bottomDark = moduleIsDark(imageValue, left+x*moduleScale, top+(y+1)*moduleScale, moduleScale)
			}
			switch {
			case topDark && bottomDark:
				builder.WriteRune('█')
			case topDark:
				builder.WriteRune('▀')
			case bottomDark:
				builder.WriteRune('▄')
			default:
				builder.WriteRune(' ')
			}
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

func qrRasterGrid(imageValue image.Image) (scale, quietZone, moduleCount int, ok bool) {
	bounds := imageValue.Bounds()
	if bounds.Dx() != bounds.Dy() {
		return 0, 0, 0, false
	}
	for candidate := bounds.Dx(); candidate >= 1; candidate-- {
		if bounds.Dx()%candidate != 0 {
			continue
		}
		grid := bounds.Dx() / candidate
		for quiet := 8; quiet >= 0; quiet-- {
			modules := grid - quiet*2
			if !validQRModuleCount(modules) || !uniformModuleGrid(imageValue, candidate) ||
				!hasLightQuietZone(imageValue, candidate, quiet) {
				continue
			}
			return candidate, quiet, modules, true
		}
	}
	return 0, 0, 0, false
}

func validQRModuleCount(value int) bool {
	return value >= 21 && value <= 177 && (value-17)%4 == 0
}

func uniformModuleGrid(imageValue image.Image, scale int) bool {
	bounds := imageValue.Bounds()
	if scale == 1 {
		return true
	}
	mismatches, pixels := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += scale {
		for x := bounds.Min.X; x < bounds.Max.X; x += scale {
			dark := moduleIsDark(imageValue, x, y, scale)
			for innerY := y; innerY < y+scale; innerY++ {
				for innerX := x; innerX < x+scale; innerX++ {
					pixels++
					if isDark(imageValue.At(innerX, innerY)) != dark {
						mismatches++
					}
				}
			}
		}
	}
	return mismatches*100 <= pixels*2
}

func hasLightQuietZone(imageValue image.Image, scale, quietZone int) bool {
	if quietZone == 0 {
		return true
	}
	bounds := imageValue.Bounds()
	grid := bounds.Dx() / scale
	for moduleY := 0; moduleY < grid; moduleY++ {
		for moduleX := 0; moduleX < grid; moduleX++ {
			if moduleX >= quietZone && moduleX < grid-quietZone && moduleY >= quietZone && moduleY < grid-quietZone {
				continue
			}
			if moduleIsDark(imageValue, bounds.Min.X+moduleX*scale, bounds.Min.Y+moduleY*scale, scale) {
				return false
			}
		}
	}
	return true
}

func moduleIsDark(imageValue image.Image, left, top, scale int) bool {
	dark, pixels := 0, scale*scale
	for y := top; y < top+scale; y++ {
		for x := left; x < left+scale; x++ {
			if isDark(imageValue.At(x, y)) {
				dark++
			}
		}
	}
	return dark*2 >= pixels
}

func RenderImageText(imageValue image.Image) string {
	bounds := imageValue.Bounds()
	var builder strings.Builder
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			top := isDark(imageValue.At(x, y))
			bottom := false
			if y+1 < bounds.Max.Y {
				bottom = isDark(imageValue.At(x, y+1))
			}
			switch {
			case top && bottom:
				builder.WriteRune('█')
			case top:
				builder.WriteRune('▀')
			case bottom:
				builder.WriteRune('▄')
			default:
				builder.WriteRune(' ')
			}
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}

func isDark(colorValue interface{ RGBA() (r, g, b, a uint32) }) bool {
	r, g, b, a := colorValue.RGBA()
	if a < 0x8000 {
		return false
	}
	return (299*r+587*g+114*b)/1000 < 0x8000
}
