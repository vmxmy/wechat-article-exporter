// Package htmlx is the shared bounded HTML parsing layer over
// golang.org/x/net/html. It exposes the x/net node type directly and stays a
// pure function library: it reports input violations only — layout drift,
// missing content, and other domain semantics belong to callers, so a markup
// change can never masquerade as an authentication failure.
//
// The package is deliberately two-layered. Tree queries walk the parsed
// *html.Node document; script extraction works on the raw input bytes via the
// tokenizer instead, because the x/net parse tree normalizes raw text
// (\r→\n, NUL→U+FFFD) and drops byte offsets, which script payload extraction
// cannot afford.
package htmlx

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	defaultMaxInputBytes  = 8 << 20
	defaultMaxScriptBytes = 6 << 20
	defaultMaxHTMLDepth   = 256
	defaultMaxHTMLNodes   = 250_000
)

var (
	ErrInputTooLarge  = errors.New("html input exceeds size limit")
	ErrTooDeep        = errors.New("html tree exceeds depth limit")
	ErrTooManyNodes   = errors.New("html tree exceeds node limit")
	ErrScriptTooLarge = errors.New("script block exceeds size limit")
)

// Limits bounds parsing. x/net/html has no configurable limits of its own
// (only a fixed 512 open-element cap), so enforcement wraps it: input size via
// a limited reader before parsing, depth and node count via a walk after.
type Limits struct {
	MaxInputBytes  int64
	MaxScriptBytes int
	MaxHTMLDepth   int
	MaxHTMLNodes   int
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:  defaultMaxInputBytes,
		MaxScriptBytes: defaultMaxScriptBytes,
		MaxHTMLDepth:   defaultMaxHTMLDepth,
		MaxHTMLNodes:   defaultMaxHTMLNodes,
	}
}

// Document carries both the parse tree and the raw input: anchor chains mix
// tree anchors (element id/class) with raw anchors (script variables), and
// script extraction needs the original bytes for offset fidelity.
type Document struct {
	Root   *html.Node
	Raw    []byte
	limits Limits

	scripts    []ScriptBlock
	scriptsErr error
}

// Parse reads a complete HTML document. WeChat pages arrive as full documents,
// so html/head/body completion by the parser is correct here; use
// ParseFragment for partial markup.
func Parse(reader io.Reader, limits Limits) (*Document, error) {
	raw, err := readBounded(reader, limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	if err := validateTree(root, limits); err != nil {
		return nil, err
	}
	return &Document{Root: root, Raw: raw, limits: limits}, nil
}

// ParseFragment reads partial markup in a body context, without the
// html/head/body completion Parse performs.
func ParseFragment(reader io.Reader, limits Limits) (*Document, error) {
	raw, err := readBounded(reader, limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	fragments, err := html.ParseFragment(bytes.NewReader(raw), context)
	if err != nil {
		return nil, fmt.Errorf("parse html fragment: %w", err)
	}
	root := &html.Node{Type: html.DocumentNode}
	for _, fragment := range fragments {
		root.AppendChild(fragment)
	}
	if err := validateTree(root, limits); err != nil {
		return nil, err
	}
	return &Document{Root: root, Raw: raw, limits: limits}, nil
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxInputBytes
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read html input: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("%w: over %d bytes", ErrInputTooLarge, maxBytes)
	}
	return raw, nil
}

func validateTree(root *html.Node, limits Limits) error {
	maxDepth := limits.MaxHTMLDepth
	if maxDepth <= 0 {
		maxDepth = defaultMaxHTMLDepth
	}
	maxNodes := limits.MaxHTMLNodes
	if maxNodes <= 0 {
		maxNodes = defaultMaxHTMLNodes
	}
	nodes := 0
	var walk func(node *html.Node, depth int) error
	walk = func(node *html.Node, depth int) error {
		if depth > maxDepth {
			return fmt.Errorf("%w: over %d levels", ErrTooDeep, maxDepth)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			nodes++
			if nodes > maxNodes {
				return fmt.Errorf("%w: over %d nodes", ErrTooManyNodes, maxNodes)
			}
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root, 0)
}
