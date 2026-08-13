package processor

import (
	"errors"
	"sort"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/htmlx"
	"golang.org/x/net/html"
)

// parseContentTree parses article markup through the shared htmlx layer and
// translates its input errors into this package's typed contract.
func parseContentTree(content string, limits Limits) (*html.Node, error) {
	document, err := htmlx.ParseFragment(strings.NewReader(content), htmlx.Limits{
		MaxInputBytes: limits.MaxInputBytes,
		MaxHTMLDepth:  limits.MaxHTMLDepth,
		MaxHTMLNodes:  limits.MaxHTMLNodes,
	})
	if err != nil {
		switch {
		case errors.Is(err, htmlx.ErrInputTooLarge):
			return nil, processError(ErrorLimit, ReasonInputLimit, 0, "content exceeds %d bytes", limits.MaxInputBytes)
		case errors.Is(err, htmlx.ErrTooDeep):
			return nil, processError(ErrorLimit, ReasonHTMLLimit, 0, "HTML nesting exceeds %d", limits.MaxHTMLDepth)
		case errors.Is(err, htmlx.ErrTooManyNodes):
			return nil, processError(ErrorLimit, ReasonHTMLLimit, 0, "HTML nodes exceed %d", limits.MaxHTMLNodes)
		default:
			return nil, processError(ErrorMalformed, ReasonMalformedPayload, 0, "%v", err)
		}
	}
	return document.Root, nil
}

// nodeText concatenates descendant text without trimming — pre blocks and
// table cells depend on the caller deciding what whitespace survives.
func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.TextNode {
		return node.Data
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(nodeText(child))
	}
	return builder.String()
}

func hasAncestorTag(node *html.Node, tag string) bool {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.Data == tag {
			return true
		}
	}
	return false
}

// newElement builds an element with its attributes sorted by name, matching
// the retired serializer's deterministic output so rendered HTML stays stable
// run to run.
func newElement(tag string, attrs map[string]string) *html.Node {
	node := &html.Node{Type: html.ElementNode, Data: tag}
	if len(attrs) > 0 {
		keys := make([]string, 0, len(attrs))
		for key := range attrs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			node.Attr = append(node.Attr, html.Attribute{Key: key, Val: attrs[key]})
		}
	}
	return node
}

func newText(text string) *html.Node {
	return &html.Node{Type: html.TextNode, Data: text}
}

func serializeChildren(node *html.Node) string {
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&builder, child); err != nil {
			return builder.String()
		}
	}
	return builder.String()
}

func serializeNode(node *html.Node) string {
	var builder strings.Builder
	if err := html.Render(&builder, node); err != nil {
		return builder.String()
	}
	return builder.String()
}
