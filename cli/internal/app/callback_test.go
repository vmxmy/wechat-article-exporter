package app

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestCallbackIgnoresWrongStateAndAcceptsExpectedState(t *testing.T) {
	callback, err := newCallbackServer(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer callback.Close()

	result := make(chan *callbackResult, 1)
	errors := make(chan error, 1)
	go func() {
		value, waitErr := callback.Wait(context.Background(), "expected")
		result <- value
		errors <- waitErr
	}()

	requestCallback(t, callback.RedirectURL, url.Values{"code": {"wrong-code"}, "state": {"wrong"}})
	requestCallback(t, callback.RedirectURL, url.Values{"code": {"right-code"}, "state": {"expected"}})
	if waitErr := <-errors; waitErr != nil {
		t.Fatalf("Wait() error = %v", waitErr)
	}
	if got := <-result; got == nil || got.Code != "right-code" {
		t.Fatalf("Wait() result = %#v", got)
	}
}

func requestCallback(t *testing.T, redirectURL string, values url.Values) {
	t.Helper()
	response, err := http.Get(redirectURL + "?" + values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}
