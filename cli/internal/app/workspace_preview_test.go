package app

import (
	"strings"
	"testing"
)

func TestStripPreviewStyleElementsRemovesInlineStyles(t *testing.T) {
	input := `<!doctype html><html><head><style>body{color:red}</style></head><body><style>.hidden{display:none}</style><p>safe</p></body></html>`
	output := stripPreviewStyleElements(input)
	if strings.Contains(strings.ToLower(output), "<style") {
		t.Fatalf("preview document retained inline style: %q", output)
	}
	if !strings.Contains(output, "<p>safe</p>") {
		t.Fatalf("preview document lost article content: %q", output)
	}
}
