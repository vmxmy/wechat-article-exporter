package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type callbackResult struct {
	Code  string
	State string
}

type callbackServer struct {
	RedirectURL string
	server      *http.Server
	terminal    chan callbackEvent
	stopTimeout chan struct{}
	expectedMu  sync.RWMutex
	expected    string
	pendingMu   sync.Mutex
	pending     *pendingCallback
	deliverOnce sync.Once
	closeOnce   sync.Once
}

type callbackEvent struct {
	result *callbackResult
	err    error
}

type pendingCallback struct {
	state string
	event callbackEvent
}

func newCallbackServer(timeout time.Duration) (*callbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start OAuth callback server: %w", err)
	}
	callback := &callbackServer{
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port),
		terminal:    make(chan callbackEvent, 1),
		stopTimeout: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		state := query.Get("state")
		if oauthError := query.Get("error"); oauthError != "" {
			http.Error(writer, "Authorization failed. Return to the CLI.", http.StatusBadRequest)
			callback.deliver(state, callbackEvent{err: fmt.Errorf("OAuth authorization failed: %s", oauthError)})
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(writer, "Missing authorization code.", http.StatusBadRequest)
			callback.deliver(state, callbackEvent{err: fmt.Errorf("missing authorization code")})
			return
		}
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = writer.Write([]byte("Authorization complete. You can close this page."))
		callback.deliver(state, callbackEvent{result: &callbackResult{Code: code, State: state}})
	})
	callback.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if serveErr := callback.server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			callback.deliver("", callbackEvent{err: serveErr})
		}
	}()
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			callback.deliver("", callbackEvent{err: fmt.Errorf("OAuth callback timed out")})
		case <-callback.stopTimeout:
		}
	}()
	return callback, nil
}

func (c *callbackServer) Wait(ctx context.Context, expectedState string) (*callbackResult, error) {
	c.expectedMu.Lock()
	c.expected = expectedState
	c.expectedMu.Unlock()
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = nil
	c.pendingMu.Unlock()
	if pending != nil {
		c.deliver(pending.state, pending.event)
	}
	select {
	case event := <-c.terminal:
		return event.result, event.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *callbackServer) deliver(state string, event callbackEvent) {
	c.expectedMu.RLock()
	expected := c.expected
	c.expectedMu.RUnlock()
	if expected == "" {
		c.pendingMu.Lock()
		c.pending = &pendingCallback{state: state, event: event}
		c.pendingMu.Unlock()
		return
	}
	if state != "" && state != expected {
		return
	}
	c.deliverOnce.Do(func() { c.terminal <- event })
}

func (c *callbackServer) Close() {
	c.closeOnce.Do(func() {
		close(c.stopTimeout)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = c.server.Shutdown(ctx)
	})
}
