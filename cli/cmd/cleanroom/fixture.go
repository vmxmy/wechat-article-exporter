package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fixtureServer struct {
	server *http.Server
	base   string

	mu           sync.Mutex
	requests     []fixtureRequest
	closed       bool
	loginStarted bool
	qrRequested  bool
	qrConfirmed  bool
	loggedIn     bool
}

type fixtureRequest struct {
	Method string
	Path   string
	Host   string
}

func startFixtureServer(ctx context.Context) (*fixtureServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	fixture := &fixtureServer{base: "http://" + listener.Addr().String()}
	fixture.server = &http.Server{Handler: http.HandlerFunc(fixture.serveHTTP), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = fixture.server.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = fixture.server.Shutdown(shutdownContext)
	}()
	return fixture, nil
}

func (fixture *fixtureServer) Close() error {
	if fixture == nil || fixture.server == nil {
		return nil
	}
	fixture.mu.Lock()
	if fixture.closed {
		fixture.mu.Unlock()
		return nil
	}
	fixture.closed = true
	fixture.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return fixture.server.Shutdown(ctx)
}

func (fixture *fixtureServer) Origin() string { return fixture.base }

func (fixture *fixtureServer) ObservedHosts() []string {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	hosts := make([]string, 0, len(fixture.requests))
	for _, request := range fixture.requests {
		hosts = append(hosts, request.Host)
	}
	return sortedUnique(hosts)
}

func (fixture *fixtureServer) RequestCount(path string) int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	count := 0
	for _, request := range fixture.requests {
		if request.Path == path {
			count++
		}
	}
	return count
}

func (fixture *fixtureServer) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	fixture.mu.Lock()
	fixture.requests = append(fixture.requests, fixtureRequest{Method: request.Method, Path: request.URL.Path, Host: request.Host})
	fixture.mu.Unlock()

	switch request.URL.Path {
	case "/cgi-bin/bizlogin":
		fixture.handleBizLogin(writer, request)
	case "/cgi-bin/scanloginqrcode":
		fixture.handleQR(writer, request)
	case "/cgi-bin/home":
		if !fixture.requireAuthenticatedGET(writer, request) {
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(writer, `wx.cgiData.nick_name = "Controlled Fixture"; wx.cgiData.head_img = "https://example.invalid/avatar"; wx.cgiData.fakeid = "fixture-fakeid";`)
	case "/cgi-bin/appmsgpublish":
		query := request.URL.Query()
		if !fixture.requireAuthenticatedGET(writer, request) || requireExactArticleListQuery(query) != nil {
			if writer.Header().Get("Content-Type") == "" {
				http.Error(writer, "invalid article-list contract", http.StatusBadRequest)
			}
			return
		}
		fixture.handleArticleList(writer, request)
	case "/s/fixture-one":
		if request.Method != http.MethodGet {
			http.Error(writer, "GET required", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(writer, fixtureArticleHTML(fixture.base))
	case "/asset.png":
		if request.Method != http.MethodGet {
			http.Error(writer, "GET required", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(fixturePNG())
	case "/mp/appmsg_comment":
		if request.Method != http.MethodGet {
			http.Error(writer, "GET required", http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"},"elected_comment":[],"continue_flag":0,"buffer":"","comment_id":"fixture-comment"}`)
	default:
		http.NotFound(writer, request)
	}
}

func (fixture *fixtureServer) handleBizLogin(writer http.ResponseWriter, request *http.Request) {
	action := request.URL.Query().Get("action")
	writer.Header().Set("Content-Type", "application/json")
	switch action {
	case "startlogin":
		if request.Method != http.MethodPost {
			http.Error(writer, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if err := requireExactQuery(request, map[string]string{"action": "startlogin"}); err != nil {
			http.Error(writer, "invalid start-login query contract", http.StatusBadRequest)
			return
		}
		if err := requireExactForm(request, map[string]string{
			"userlang": "zh_CN", "redirect_url": "", "login_type": "3", "sessionid": "*",
			"token": "", "lang": "zh_CN", "f": "json", "ajax": "1",
		}); err != nil {
			http.Error(writer, "invalid start-login contract", http.StatusBadRequest)
			return
		}
		fixture.mu.Lock()
		if fixture.loginStarted || fixture.qrRequested || fixture.qrConfirmed || fixture.loggedIn {
			fixture.mu.Unlock()
			http.Error(writer, "invalid start-login sequence", http.StatusBadRequest)
			return
		}
		fixture.loginStarted = true
		fixture.mu.Unlock()
		http.SetCookie(writer, &http.Cookie{Name: "uuid", Value: "fixture-uuid", Path: "/", HttpOnly: true})
		io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"},"uuid":"fixture-uuid"}`)
	case "login":
		if request.Method != http.MethodPost || cookieValue(request, "uuid") != "fixture-uuid" {
			http.Error(writer, "invalid login sequence", http.StatusBadRequest)
			return
		}
		if err := requireExactQuery(request, map[string]string{"action": "login"}); err != nil {
			http.Error(writer, "invalid complete-login query contract", http.StatusBadRequest)
			return
		}
		if err := requireExactForm(request, map[string]string{
			"userlang": "zh_CN", "redirect_url": "", "cookie_forbidden": "0", "cookie_cleaned": "0",
			"plugin_used": "0", "login_type": "3", "token": "", "lang": "zh_CN", "f": "json", "ajax": "1",
		}); err != nil {
			http.Error(writer, "invalid complete-login contract", http.StatusBadRequest)
			return
		}
		fixture.mu.Lock()
		if !fixture.qrConfirmed || fixture.loggedIn {
			fixture.mu.Unlock()
			http.Error(writer, "invalid login sequence", http.StatusBadRequest)
			return
		}
		fixture.loggedIn = true
		fixture.mu.Unlock()
		http.SetCookie(writer, &http.Cookie{Name: "bizuin", Value: "fixture-session-cookie", Path: "/", HttpOnly: true})
		io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"},"redirect_url":"/cgi-bin/home?t=home/index&token=fixture-token"}`)
	default:
		io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"}}`)
	}
}

func (fixture *fixtureServer) handleQR(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Query().Get("action") {
	case "getqrcode":
		if request.Method != http.MethodGet || cookieValue(request, "uuid") != "fixture-uuid" {
			http.Error(writer, "invalid QR sequence", http.StatusBadRequest)
			return
		}
		if err := requireExactQuery(request, map[string]string{"action": "getqrcode", "random": "#"}); err != nil {
			http.Error(writer, "invalid QR request contract", http.StatusBadRequest)
			return
		}
		fixture.mu.Lock()
		if !fixture.loginStarted || fixture.qrRequested {
			fixture.mu.Unlock()
			http.Error(writer, "invalid QR sequence", http.StatusBadRequest)
			return
		}
		fixture.qrRequested = true
		fixture.mu.Unlock()
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(fixturePNG())
	case "ask":
		if request.Method != http.MethodGet || cookieValue(request, "uuid") != "fixture-uuid" {
			http.Error(writer, "invalid poll sequence", http.StatusBadRequest)
			return
		}
		if err := requireExactQuery(request, map[string]string{
			"action": "ask", "token": "", "lang": "zh_CN", "f": "json", "ajax": "1",
		}); err != nil {
			http.Error(writer, "invalid QR poll contract", http.StatusBadRequest)
			return
		}
		fixture.mu.Lock()
		if !fixture.qrRequested || fixture.qrConfirmed {
			fixture.mu.Unlock()
			http.Error(writer, "invalid poll sequence", http.StatusBadRequest)
			return
		}
		fixture.qrConfirmed = true
		fixture.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		io.WriteString(writer, `{"base_resp":{"ret":0,"err_msg":"ok"},"status":1,"acct_size":1}`)
	default:
		http.NotFound(writer, request)
	}
}

func requireExactArticleListQuery(query map[string][]string) error {
	if err := requireExactValues(query, map[string]string{
		"fakeid": "fixture-fakeid", "token": "fixture-token", "begin": "#", "count": "#", "sub": "list",
		"search_field": "null", "query": "", "type": "101_1", "free_publish_type": "1",
		"sub_action": "list_ex", "lang": "zh_CN", "f": "json", "ajax": "1",
	}); err != nil {
		return err
	}
	begin, beginErr := strconv.Atoi(query["begin"][0])
	count, countErr := strconv.Atoi(query["count"][0])
	if beginErr != nil || begin < 0 || countErr != nil || count <= 0 || count > 50 {
		return errors.New("article list pagination is out of range")
	}
	return nil
}

func requireExactForm(request *http.Request, expected map[string]string) error {
	if err := request.ParseForm(); err != nil {
		return err
	}
	return requireExactValues(request.PostForm, expected)
}

func requireExactQuery(request *http.Request, expected map[string]string) error {
	return requireExactValues(request.URL.Query(), expected)
}

func requireExactValues(actual map[string][]string, expected map[string]string) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("field count=%d, want %d", len(actual), len(expected))
	}
	for name, wanted := range expected {
		values, ok := actual[name]
		if !ok || len(values) != 1 {
			return fmt.Errorf("field %s is missing or repeated", name)
		}
		switch wanted {
		case "*":
			if strings.TrimSpace(values[0]) == "" {
				return fmt.Errorf("field %s is empty", name)
			}
		case "#":
			if _, err := strconv.ParseInt(values[0], 10, 64); err != nil {
				return fmt.Errorf("field %s is not numeric", name)
			}
		default:
			if values[0] != wanted {
				return fmt.Errorf("field %s=%q, want %q", name, values[0], wanted)
			}
		}
	}
	return nil
}

func (fixture *fixtureServer) hasState(check func() bool) bool {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return check()
}

func (fixture *fixtureServer) requireAuthenticatedGET(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodGet {
		http.Error(writer, "GET required", http.StatusMethodNotAllowed)
		return false
	}
	if !fixture.hasState(func() bool { return fixture.loggedIn }) || cookieValue(request, "bizuin") != "fixture-session-cookie" {
		http.Error(writer, "authenticated fixture session required", http.StatusUnauthorized)
		return false
	}
	return true
}

func cookieValue(request *http.Request, name string) string {
	cookie, err := request.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (fixture *fixtureServer) handleArticleList(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	link := fixture.base + "/s/fixture-one"
	article := map[string]any{
		"aid": "fixture-aid-1", "appmsgid": 1001, "itemidx": 1, "title": "Clean-room fixture article",
		"author_name": "Fixture Author", "digest": "Controlled fixture", "link": link,
		"cover": fixture.base + "/asset.png", "create_time": 1_710_000_000, "update_time": 1_710_000_300,
		"item_show_type": 0, "is_deleted": false, "is_pay_subscribe": 0,
	}
	publishInfo, _ := json.Marshal(map[string]any{"appmsgex": []any{article}})
	publishPage, _ := json.Marshal(map[string]any{
		"total_count":  1,
		"publish_list": []any{map[string]any{"publish_info": string(publishInfo)}},
	})
	response, _ := json.Marshal(map[string]any{
		"base_resp":    map[string]any{"ret": 0, "err_msg": "ok"},
		"total_count":  1,
		"publish_page": string(publishPage),
	})
	_, _ = writer.Write(response)
}

func fixtureArticleHTML(origin string) string {
	articleURL := origin + "/s/fixture-one"
	assetURL := origin + "/asset.png"
	payload := map[string]any{
		"title": "Clean-room fixture article", "author": "Fixture Author", "user_name": "fixture-user",
		"bizuin": "fixture-fakeid", "nick_name": "Controlled Fixture", "mid": "1001", "idx": 1,
		"aid": "fixture-aid-1", "link": articleURL, "comment_id": "fixture-comment", "show_comment": 1,
		"content_noencode": `<h1>Fixture heading</h1><p>Local clean-room content.</p><p><img src="` + assetURL + `" alt="fixture"></p>`,
		"create_timestamp": 1_710_000_000, "real_item_show_type": 0,
	}
	encoded, _ := json.Marshal(payload)
	return `<!doctype html><html><body><article id="js_article"><div id="js_content">fixture</div></article><script>window.cgiDataNew=` + string(encoded) + `;</script></body></html>`
}

func fixturePNG() []byte {
	imageValue := image.NewRGBA(image.Rect(0, 0, 29, 29))
	for y := 0; y < 29; y++ {
		for x := 0; x < 29; x++ {
			value := uint8(255)
			if x < 7 || y < 7 || x > 21 || y > 21 || (x+y)%3 == 0 {
				value = 0
			}
			imageValue.SetRGBA(x, y, color.RGBA{R: value, G: value, B: value, A: 255})
		}
	}
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, imageValue)
	return buffer.Bytes()
}

func localOnlyTransport(observed *[]string, mu *sync.Mutex) http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		*observed = append(*observed, strings.ToLower(request.URL.Hostname()))
		mu.Unlock()
		address := net.ParseIP(request.URL.Hostname())
		if request.URL.Scheme != "http" || address == nil || !address.IsLoopback() {
			return nil, fmt.Errorf("clean-room transport blocked non-loopback request to %s", request.URL.Hostname())
		}
		return transport.RoundTrip(request)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
