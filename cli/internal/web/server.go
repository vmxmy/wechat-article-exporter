package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const (
	bootstrapTokenBytes = 32
	sessionTokenBytes   = 32
	csrfTokenBytes      = 32
	defaultSessionTTL   = 30 * time.Minute
	defaultShutdownWait = 5 * time.Second
	maxMutationBytes    = 64 << 10
	sessionCookieName   = "wechat_article_session"
	csrfCookieName      = "wechat_article_csrf"
)

// Options are intentionally limited to presentation-safe application seams.
// Listener address selection is not configurable: browser workspaces are
// always a random IPv4 loopback listener.
type Options struct {
	Application     application.Application
	SessionTTL      time.Duration
	ShutdownTimeout time.Duration
	Now             func() time.Time
}

// Server owns one in-memory bootstrap token and its browser sessions.
// It never persists or logs either credential.
type Server struct {
	application     application.Application
	workspace       application.WorkspaceReader
	sessionTTL      time.Duration
	shutdownTimeout time.Duration
	now             func() time.Time

	bootstrapToken string

	mu             sync.Mutex
	listener       net.Listener
	httpServer     *http.Server
	sessions       map[string]session
	bootstrapUsed  bool
	closed         bool
	serveCompleted chan struct{}
}

type session struct {
	csrf      string
	expiresAt time.Time
}

// New creates an unstarted local workspace server. It fails closed if the
// required application seam is missing or a credential cannot be generated.
func New(options Options) (*Server, error) {
	if options.Application == nil {
		return nil, errors.New("local browser workspace requires an application")
	}
	if options.SessionTTL <= 0 {
		options.SessionTTL = defaultSessionTTL
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = defaultShutdownWait
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	bootstrap, err := randomToken(bootstrapTokenBytes)
	if err != nil {
		return nil, fmt.Errorf("generate local browser bootstrap credential: %w", err)
	}
	return &Server{
		application: options.Application, sessionTTL: options.SessionTTL, shutdownTimeout: options.ShutdownTimeout,
		workspace: application.NewWorkspace(options.Application), now: options.Now, bootstrapToken: bootstrap,
		sessions: make(map[string]session), serveCompleted: make(chan struct{}),
	}, nil
}

// Start binds exactly net.Listen("tcp4", "127.0.0.1:0"). It is separated
// from Serve so the command can print the local URL before opening a browser.
func (server *Server) Start() error {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closed {
		return errors.New("local browser workspace is closed")
	}
	if server.listener != nil {
		return errors.New("local browser workspace is already started")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on local IPv4 loopback: %w", err)
	}
	if err := validateLoopbackListener(listener); err != nil {
		_ = listener.Close()
		return err
	}
	server.listener = listener
	server.httpServer = &http.Server{
		Handler:           server.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	return nil
}

// URL returns the one-time bootstrap URL. It is intentionally only available
// after Start, and callers must never log it beyond the command's stdout.
func (server *Server) URL() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.listener == nil || server.closed {
		return ""
	}
	return "http://" + server.listener.Addr().String() + "/?token=" + url.QueryEscape(server.bootstrapToken)
}

// Serve blocks until the command context ends or the listener fails. It
// invalidates all in-memory credentials before returning.
func (server *Server) Serve(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		server.invalidate()
		_ = server.Close()
		return nil
	}
	server.mu.Lock()
	httpServer := server.httpServer
	server.mu.Unlock()
	if httpServer == nil {
		return errors.New("local browser workspace has not been started")
	}
	result := make(chan error, 1)
	go func() {
		err := httpServer.Serve(server.listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		result <- err
		close(server.serveCompleted)
	}()

	select {
	case err := <-result:
		server.invalidate()
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)
		err := httpServer.Shutdown(shutdownCtx)
		cancel()
		server.invalidate()
		serveErr := <-result
		return errors.Join(err, serveErr)
	}
}

// Close is safe before or after Serve and invalidates bootstrap and session
// credentials immediately.
func (server *Server) Close() error {
	server.invalidate()
	server.mu.Lock()
	httpServer := server.httpServer
	listener := server.listener
	server.mu.Unlock()
	if httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), server.shutdownTimeout)
		defer cancel()
		err := httpServer.Shutdown(shutdownCtx)
		if listener != nil {
			closeErr := listener.Close()
			if !errors.Is(closeErr, net.ErrClosed) {
				err = errors.Join(err, closeErr)
			}
		}
		return err
	}
	if listener != nil {
		return listener.Close()
	}
	return nil
}

func (server *Server) invalidate() {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.bootstrapToken = ""
	server.bootstrapUsed = true
	server.sessions = make(map[string]session)
	server.closed = true
}

func validateLoopbackListener(listener net.Listener) error {
	if listener == nil {
		return errors.New("local browser workspace listener is required")
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port == 0 || !address.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		return errors.New("browser workspace access is local-only on 127.0.0.1")
	}
	return nil
}

func (server *Server) handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(writer.Header())
		if !server.validRequestTarget(request) {
			server.error(writer, http.StatusMisdirectedRequest)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/") && request.URL.Path != "/api/v1/session/logout" && request.URL.Path != "/api/v1/status" {
			server.api(writer, request)
			return
		}
		if request.Method == http.MethodPost || request.Method == http.MethodPut || request.Method == http.MethodPatch || request.Method == http.MethodDelete {
			if !server.validMutationShape(request) {
				server.error(writer, http.StatusUnsupportedMediaType)
				return
			}
		}
		switch request.URL.Path {
		case "/":
			server.root(writer, request)
		case "/api/v1/status":
			server.status(writer, request)
		case "/api/v1/session/logout":
			server.logout(writer, request)
		default:
			server.workspaceAsset(writer, request)
		}
	})
}

func (server *Server) workspaceAsset(writer http.ResponseWriter, request *http.Request) {
	if _, ok := server.authorize(request); !ok {
		server.error(writer, http.StatusUnauthorized)
		return
	}
	AssetHandler().ServeHTTP(writer, request)
}

func (server *Server) validRequestTarget(request *http.Request) bool {
	server.mu.Lock()
	listener := server.listener
	server.mu.Unlock()
	if listener == nil || request.Host != listener.Addr().String() || request.URL.IsAbs() {
		return false
	}
	host, _, err := net.SplitHostPort(request.Host)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	remoteHost, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return false
	}
	remoteIP := net.ParseIP(remoteHost)
	return remoteIP != nil && remoteIP.IsLoopback() && remoteIP.To4() != nil
}

func (server *Server) validMutationShape(request *http.Request) bool {
	if request.ContentLength > maxMutationBytes {
		return false
	}
	if request.ContentLength > 0 {
		mediaType := strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0])
		if mediaType != "application/json" {
			return false
		}
	}
	request.Body = http.MaxBytesReader(nil, request.Body, maxMutationBytes)
	return true
}

func (server *Server) root(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		server.error(writer, http.StatusMethodNotAllowed)
		return
	}
	if token := request.URL.Query().Get("token"); token != "" {
		if request.URL.Query().Has("token") && len(request.URL.Query()["token"]) == 1 && server.consumeBootstrap(token) {
			sessionID, csrf, err := server.newSession()
			if err != nil {
				server.error(writer, http.StatusInternalServerError)
				return
			}
			server.setSessionCookies(writer, sessionID, csrf)
			http.Redirect(writer, request, "/", http.StatusSeeOther)
			return
		}
		server.error(writer, http.StatusUnauthorized)
		return
	}
	if _, ok := server.authorize(request); !ok {
		server.error(writer, http.StatusUnauthorized)
		return
	}
	AssetHandler().ServeHTTP(writer, request)
}

func (server *Server) status(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		server.apiError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
		return
	}
	current, ok := server.authorize(request)
	if !ok {
		server.apiError(writer, http.StatusUnauthorized, "authentication_required", "workspace session is required")
		return
	}
	runtimeStatus, err := server.workspace.Runtime(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	sessionStatus, err := server.workspace.Session(request.Context())
	if err != nil {
		server.workspaceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, map[string]any{"runtime": runtimeStatus, "session": sessionStatus, "csrfToken": current.csrf})
}

func (server *Server) logout(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		server.error(writer, http.StatusMethodNotAllowed)
		return
	}
	if _, ok := server.authorizeMutation(request); !ok {
		server.error(writer, http.StatusForbidden)
		return
	}
	server.deleteSession(request)
	server.clearSessionCookies(writer)
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) consumeBootstrap(value string) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closed || server.bootstrapUsed || server.bootstrapToken == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(value), []byte(server.bootstrapToken)) != 1 {
		return false
	}
	server.bootstrapUsed = true
	server.bootstrapToken = ""
	return true
}

func (server *Server) newSession() (string, string, error) {
	sessionID, err := randomToken(sessionTokenBytes)
	if err != nil {
		return "", "", err
	}
	csrf, err := randomToken(csrfTokenBytes)
	if err != nil {
		return "", "", err
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closed {
		return "", "", errors.New("local browser workspace is closed")
	}
	server.sessions[sessionID] = session{csrf: csrf, expiresAt: server.now().Add(server.sessionTTL)}
	return sessionID, csrf, nil
}

func (server *Server) authorize(request *http.Request) (session, bool) {
	return server.authorizeCookie(request)
}

func (server *Server) authorizeCookie(request *http.Request) (session, bool) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return session{}, false
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	value, ok := server.sessions[cookie.Value]
	if !ok || !server.now().Before(value.expiresAt) {
		delete(server.sessions, cookie.Value)
		return session{}, false
	}
	return value, true
}

func (server *Server) authorizeMutation(request *http.Request) (session, bool) {
	value, ok := server.authorizeCookie(request)
	if !ok {
		return session{}, false
	}
	server.mu.Lock()
	listener := server.listener
	server.mu.Unlock()
	if listener == nil || request.Header.Get("Origin") != "http://"+listener.Addr().String() ||
		subtle.ConstantTimeCompare([]byte(request.Header.Get("X-CSRF-Token")), []byte(value.csrf)) != 1 {
		return session{}, false
	}
	return value, true
}

func (server *Server) deleteSession(request *http.Request) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	delete(server.sessions, cookie.Value)
}

func (server *Server) setSessionCookies(writer http.ResponseWriter, sessionID, csrf string) {
	expiresAt := server.now().Add(server.sessionTTL)
	http.SetCookie(writer, &http.Cookie{Name: sessionCookieName, Value: sessionID, Path: "/", Expires: expiresAt, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(writer, &http.Cookie{Name: csrfCookieName, Value: csrf, Path: "/", Expires: expiresAt, SameSite: http.SameSiteStrictMode})
}

func (server *Server) clearSessionCookies(writer http.ResponseWriter) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(writer, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: name != csrfCookieName, SameSite: http.SameSiteStrictMode})
	}
}

func (server *Server) error(writer http.ResponseWriter, status int) {
	writeJSON(writer, status, map[string]any{"error": http.StatusText(status)})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func setSecurityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Cache-Control", "no-store, max-age=0")
	header.Set("Pragma", "no-cache")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
