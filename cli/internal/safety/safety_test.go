package safety

import (
	"reflect"
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
