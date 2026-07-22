package safety

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestKnownReadToolsAreSafeAndUnknownToolsRequireExactConfirmation(t *testing.T) {
	known := &mcp.Tool{Name: "download_article", InputSchema: map[string]any{"type": "object"}}
	if got := RequiredConfirmation(known); got != "" {
		t.Fatalf("RequiredConfirmation(known) = %q", got)
	}
	unknown := &mcp.Tool{Name: "delete_export_cache", InputSchema: map[string]any{"type": "object"}}
	if got := RequiredConfirmation(unknown); got != "delete_export_cache" {
		t.Fatalf("RequiredConfirmation(unknown) = %q", got)
	}
	if err := AssertConfirmation(unknown, ""); err == nil {
		t.Fatal("AssertConfirmation() accepted missing confirmation")
	}
	if err := AssertConfirmation(unknown, "delete_export_cache"); err != nil {
		t.Fatalf("AssertConfirmation() error = %v", err)
	}
	readOnly := true
	unknown.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: readOnly}
	if got := RequiredConfirmation(unknown); got != "delete_export_cache" {
		t.Fatalf("server readOnlyHint bypassed confirmation: %q", got)
	}
}

func TestRedactCoversURLsStringsAndNestedErrors(t *testing.T) {
	rawURL := "https://user:password@mp.weixin.qq.com/s/example?foo=ok&pass_ticket=pass-secret&key=key-secret"
	redactedURL := RedactURL(rawURL)
	for _, forbidden := range []string{"password@", "pass-secret", "key-secret"} {
		if strings.Contains(redactedURL, forbidden) {
			t.Fatalf("RedactURL leaked %q: %s", forbidden, redactedURL)
		}
	}
	if !strings.Contains(redactedURL, "foo=ok") {
		t.Fatalf("RedactURL removed diagnostic query: %s", redactedURL)
	}
	message := RedactText("Authorization: Bearer abcdefghijklmnop Cookie=session-secret access_token=token-secret")
	for _, forbidden := range []string{"abcdefghijklmnop", "session-secret", "token-secret"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("RedactText leaked %q: %s", forbidden, message)
		}
	}
	cause := errors.New("request failed: " + rawURL)
	wrapped := fmt.Errorf("sync failed: %w", cause)
	redacted := RedactError(wrapped)
	if strings.Contains(redacted.Error(), "pass-secret") || !errors.Is(redacted, cause) {
		t.Fatalf("RedactError() = %v", redacted)
	}
}

type redactionFixture struct {
	URL     string            `json:"url"`
	Headers http.Header       `json:"headers"`
	Pair    *redactionPair    `json:"pair"`
	Raw     json.RawMessage   `json:"raw"`
	Nested  []redactionSecret `json:"nested"`
}

type redactionPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type redactionSecret struct {
	Session string `json:"session"`
	Visible string `json:"visible"`
}

func TestRedactRecursesThroughTypedValuesPointersHeadersAndRawJSON(t *testing.T) {
	input := map[string]any{
		"fixture": &redactionFixture{
			URL: "https://mp.weixin.qq.com/s/example?keep=yes&pass_ticket=pass-secret&key=key-secret",
			Headers: http.Header{
				"Authorization":       {"Bearer authorization-secret"},
				"Proxy-Authorization": {"Basic proxy-secret"},
				"X-Trace-ID":          {"trace-visible"},
			},
			Pair: &redactionPair{Name: "appmsg_token", Value: "pair-secret"},
			Raw:  json.RawMessage(`{"refresh_token":"raw-secret","keep":"raw-visible"}`),
			Nested: []redactionSecret{{
				Session: "session-secret",
				Visible: "nested-visible",
			}},
		},
		"typedMap": map[string]string{
			"access_token": "map-secret",
			"key":          "map-key-secret",
			"visible":      "map-visible",
		},
		"cookies": []redactionPair{{Name: "sid", Value: "cookie-secret"}},
	}

	encoded, err := json.Marshal(Redact(input, ""))
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	for _, forbidden := range []string{
		"pass-secret", "key-secret", "authorization-secret", "proxy-secret", "pair-secret",
		"raw-secret", "session-secret", "map-secret", "map-key-secret", "cookie-secret",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("Redact leaked %q: %s", forbidden, output)
		}
	}
	for _, retained := range []string{"keep=yes", "trace-visible", "raw-visible", "nested-visible", "map-visible"} {
		if !strings.Contains(output, retained) {
			t.Fatalf("Redact removed diagnostic value %q: %s", retained, output)
		}
	}
}

type classifiedFixtureError struct {
	message string
}

func (err *classifiedFixtureError) Error() string { return err.message }

func TestRedactErrorPreservesWrappedAndJoinedIdentityWithoutFormattingLeaks(t *testing.T) {
	sentinel := errors.New("sentinel failure")
	typed := &classifiedFixtureError{message: "refresh_token=typed-secret"}
	joined := errors.Join(
		fmt.Errorf("request https://mp.weixin.qq.com/s/example?access_token=url-secret: %w", sentinel),
		fmt.Errorf("classified: %w", typed),
	)
	redacted := RedactError(fmt.Errorf("job failed with Cookie: sid=cookie-secret; bizuin=second-secret: %w", joined))

	if !errors.Is(redacted, sentinel) {
		t.Fatalf("errors.Is(redacted, sentinel) = false: %v", redacted)
	}
	var got *classifiedFixtureError
	if !errors.As(redacted, &got) || got != typed {
		t.Fatalf("errors.As(redacted) = %#v, want %#v", got, typed)
	}
	for _, rendered := range []string{redacted.Error(), fmt.Sprintf("%v", redacted), fmt.Sprintf("%+v", redacted), fmt.Sprintf("%#v", redacted)} {
		for _, forbidden := range []string{"url-secret", "cookie-secret", "second-secret", "typed-secret"} {
			if strings.Contains(rendered, forbidden) {
				t.Fatalf("redacted error formatting leaked %q: %s", forbidden, rendered)
			}
		}
	}
}

func TestRedactTextRemovesCompleteCookieAndAuthorizationValues(t *testing.T) {
	redacted := RedactText("Cookie: sid=first-secret; bizuin=second-secret\nCookie=standalone-secret\n" +
		"Proxy-Authorization: Basic proxy-secret\njson={\"access_token\":\"quoted-secret\"}\nkeep=visible")
	for _, forbidden := range []string{"first-secret", "second-secret", "standalone-secret", "proxy-secret", "quoted-secret"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("RedactText leaked %q: %s", forbidden, redacted)
		}
	}
	if !strings.Contains(redacted, "keep=visible") {
		t.Fatalf("RedactText removed non-secret context: %s", redacted)
	}
}

func TestDryRunRedactsSecrets(t *testing.T) {
	preview := DryRun("download_article", map[string]any{
		"url":          "https://mp.weixin.qq.com/s/example",
		"access_token": "secret-token",
		"nested":       map[string]any{"authKey": "secret-key", "format": "markdown"},
	})
	want := map[string]any{
		"url":          "https://mp.weixin.qq.com/s/example",
		"access_token": "[REDACTED]",
		"nested":       map[string]any{"authKey": "[REDACTED]", "format": "markdown"},
	}
	if !reflect.DeepEqual(preview["arguments"], want) {
		t.Fatalf("DryRun().arguments = %#v, want %#v", preview["arguments"], want)
	}
	headerPreview := DryRun("future_tool", map[string]any{
		"headers": []any{map[string]any{"name": "Authorization", "value": "Bearer secret"}},
	})
	header := headerPreview["arguments"].(map[string]any)["headers"].([]any)[0].(map[string]any)
	if header["value"] != "[REDACTED]" {
		t.Fatalf("header value was not redacted: %#v", header)
	}
}

func TestDiagnosticBundleDefaultsToRedactedMetadataWithoutBodiesOrSecrets(t *testing.T) {
	bundle, err := AssembleDiagnosticBundle(DiagnosticBundleInput{
		System: map[string]any{"platform": "darwin", "url": "https://example.test?key=system-secret&keep=yes"},
		Configuration: map[string]any{
			"proxy":         "https://proxy.example/wrap?authorization=config-secret",
			"access_token":  "config-token-secret",
			"publicSetting": "visible-setting",
		},
		SchemaVersion: 7,
		Logs: []any{map[string]any{
			"message": "Cookie: sid=log-secret; bizuin=second-log-secret",
			"trace":   "visible-trace",
		}},
		Integrity:     map[string]any{"healthy": true},
		ArticleBodies: []any{map[string]any{"html": "<article>private body</article>"}},
		Secrets:       map[string]any{"refresh_token": "bundle-secret"},
	}, DiagnosticBundleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	for _, forbidden := range []string{
		"system-secret", "config-secret", "config-token-secret", "log-secret", "second-log-secret",
		"private body", "bundle-secret", "articleBodies", "secrets",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("default diagnostic bundle leaked %q: %s", forbidden, output)
		}
	}
	for _, retained := range []string{"darwin", "keep=yes", "visible-setting", "visible-trace", `"schemaVersion":7`, `"healthy":true`} {
		if !strings.Contains(output, retained) {
			t.Fatalf("default diagnostic bundle removed %q: %s", retained, output)
		}
	}
}

func TestDiagnosticBundleRequiresExactConfirmationForRestrictedSections(t *testing.T) {
	input := DiagnosticBundleInput{
		ArticleBodies: map[string]any{
			"html":  "<article>confirmed body</article>",
			"token": "body-secret",
		},
		Secrets: map[string]any{"access_token": "confirmed-secret"},
	}
	for _, options := range []DiagnosticBundleOptions{
		{IncludeArticleBodies: true},
		{IncludeSecrets: true, Confirmation: "wrong"},
	} {
		bundle, err := AssembleDiagnosticBundle(input, options)
		if bundle != nil || !errors.Is(err, ErrDiagnosticBundleConfirmation) ||
			!strings.Contains(err.Error(), DiagnosticBundleConfirmation) {
			t.Fatalf("AssembleDiagnosticBundle(%#v) = %#v, %v", options, bundle, err)
		}
	}

	bundle, err := AssembleDiagnosticBundle(input, DiagnosticBundleOptions{
		IncludeArticleBodies: true,
		IncludeSecrets:       true,
		Confirmation:         DiagnosticBundleConfirmation,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	if !strings.Contains(output, "confirmed body") || !strings.Contains(output, `"articleBodies"`) ||
		!strings.Contains(output, `"secrets":"[REDACTED]"`) {
		t.Fatalf("confirmed diagnostic bundle = %s", output)
	}
	for _, forbidden := range []string{"body-secret", "confirmed-secret"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("confirmed diagnostic bundle leaked %q: %s", forbidden, output)
		}
	}
}
