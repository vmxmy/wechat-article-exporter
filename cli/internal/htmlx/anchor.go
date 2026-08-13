package htmlx

import (
	"html"
	"regexp"
	"strings"
)

// Anchor is one named way of locating a value in a document. The name is the
// observability identity: callers report which anchor of a chain resolved, so
// a layout drift shows up as the primary anchor going quiet instead of as a
// silent fallback.
type Anchor struct {
	Name    string
	Extract func(*Document) (value string, matched bool)
}

// Chain tries anchors in order, newest layout first by convention.
type Chain []Anchor

// Resolve returns the first non-empty value. anchorName identifies the anchor
// that produced it. matched reports whether any anchor structurally matched at
// all — it distinguishes "the markup changed" (no anchor matched) from "the
// anchor is present but carries no value", which is what deleted-author and
// blocked-content pages legitimately look like.
func (chain Chain) Resolve(document *Document) (value, anchorName string, matched bool) {
	for _, anchor := range chain {
		extracted, ok := anchor.Extract(document)
		if !ok {
			continue
		}
		matched = true
		if extracted != "" {
			return extracted, anchor.Name, true
		}
	}
	return "", "", matched
}

// ByID anchors on an element id and yields its trimmed descendant text.
func ByID(name, id string) Anchor {
	return Anchor{Name: name, Extract: func(document *Document) (string, bool) {
		node := FindByID(document.Root, id)
		if node == nil {
			return "", false
		}
		return Text(node), true
	}}
}

// ByClass anchors on a class token and yields its trimmed descendant text.
func ByClass(name, class string) Anchor {
	return Anchor{Name: name, Extract: func(document *Document) (string, bool) {
		node := FindByClass(document.Root, class)
		if node == nil {
			return "", false
		}
		return Text(node), true
	}}
}

// ByScriptVar anchors on a regular expression over the raw input — script
// bootstrap variables never surface in the tree as text. The pattern must
// carry exactly one capture group; the captured value is entity-decoded and
// trimmed, matching how the page's own htmlDecode consumes it. For URLs and
// identifiers use ByScriptVarRaw: entity decoding corrupts query strings
// (&copy= decodes to ©=).
func ByScriptVar(name, pattern string) Anchor {
	return scriptVarAnchor(name, pattern, func(value string) string {
		return strings.TrimSpace(html.UnescapeString(value))
	})
}

// ByScriptVarRaw is ByScriptVar without entity decoding: the capture is
// returned byte-exact apart from trimming.
func ByScriptVarRaw(name, pattern string) Anchor {
	return scriptVarAnchor(name, pattern, strings.TrimSpace)
}

func scriptVarAnchor(name, pattern string, clean func(string) string) Anchor {
	compiled := regexp.MustCompile(pattern)
	return Anchor{Name: name, Extract: func(document *Document) (string, bool) {
		match := compiled.FindSubmatch(document.Raw)
		if len(match) < 2 {
			return "", false
		}
		return clean(string(match[1])), true
	}}
}
