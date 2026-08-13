package htmlx

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/html"
)

// ErrUnterminatedScript reports a <script> that never closes. Browsers recover
// from this, but for payload extraction an unterminated script means the
// input is truncated or malformed and silently returning a partial body would
// hide it.
var ErrUnterminatedScript = errors.New("unterminated script element")

// ScriptBlock is one <script> body with its byte offset into the input and
// the raw bytes of the opening tag's attribute list. Extraction runs the
// tokenizer over the raw input rather than the parse tree: Raw() partitions
// the input byte-for-byte, so offsets are exact and script text is never
// normalized the way tree construction normalizes it. Per-block size policy
// deliberately stays with callers — the processor caps only payload-bearing
// scripts, a domain rule htmlx must not guess at.
type ScriptBlock struct {
	Body     []byte
	Offset   int
	RawAttrs []byte
}

// Scripts returns every <script> body in document order, cached on first
// call. The returned slices alias Document.Raw and must not be mutated.
func (document *Document) Scripts() ([]ScriptBlock, error) {
	if document.scripts != nil || document.scriptsErr != nil {
		return document.scripts, document.scriptsErr
	}
	blocks, err := ScanScripts(document.Raw)
	if err != nil {
		document.scriptsErr = err
		return nil, err
	}
	if blocks == nil {
		blocks = []ScriptBlock{}
	}
	document.scripts = blocks
	return blocks, nil
}

// ScanScripts tokenizes raw HTML without building a tree. It exists apart
// from Document for extraction paths that never need tree queries.
func ScanScripts(raw []byte) ([]ScriptBlock, error) {
	tokenizer := html.NewTokenizer(bytes.NewReader(raw))
	tokenizer.AllowCDATA(false)
	var blocks []ScriptBlock
	offset := 0
	inScript := false
	var rawAttrs []byte
	for {
		tokenType := tokenizer.Next()
		token := tokenizer.Raw()
		if tokenType == html.ErrorToken {
			if err := tokenizer.Err(); err != io.EOF {
				return nil, fmt.Errorf("tokenize html: %w", err)
			}
			if inScript {
				return nil, fmt.Errorf("%w: opened at offset %d", ErrUnterminatedScript, offset)
			}
			return blocks, nil
		}
		switch tokenType {
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			inScript = string(name) == "script"
			if inScript {
				rawAttrs = openTagAttributes(token, len(name))
			}
		case html.TextToken:
			if inScript {
				blocks = append(blocks, ScriptBlock{Body: raw[offset : offset+len(token)], Offset: offset, RawAttrs: rawAttrs})
			}
		case html.EndTagToken, html.SelfClosingTagToken:
			inScript = false
		}
		offset += len(token)
	}
}

// openTagAttributes slices the attribute bytes out of a raw "<name ...>"
// token, tolerating self-closing tails.
func openTagAttributes(token []byte, nameLength int) []byte {
	if len(token) < nameLength+2 {
		return nil
	}
	attrs := token[1+nameLength : len(token)-1]
	attrs = bytes.TrimSuffix(attrs, []byte("/"))
	return attrs
}

// FindBalancedObject scans a script body from start (which must point at '{')
// and returns the exclusive end offset of the balanced object literal,
// honoring strings, escapes, and line/block comments. Logic is ported intact
// from the processor byte scanner; only the error surface changed.
func FindBalancedObject(script []byte, start, maxBytes int) (int, error) {
	depth := 0
	var quote byte
	escaped := false
	lineComment := false
	blockComment := false
	for position := start; position < len(script); position++ {
		if maxBytes > 0 && position-start+1 > maxBytes {
			return 0, fmt.Errorf("%w: object at offset %d exceeds %d bytes", ErrScriptTooLarge, start, maxBytes)
		}
		char := script[position]
		if lineComment {
			if char == '\n' || char == '\r' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if char == '*' && position+1 < len(script) && script[position+1] == '/' {
				blockComment = false
				position++
			}
			continue
		}
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '/' && position+1 < len(script) {
			switch script[position+1] {
			case '/':
				lineComment = true
				position++
				continue
			case '*':
				blockComment = true
				position++
				continue
			}
		}
		switch char {
		case '\'', '"':
			quote = char
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return position + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated object literal at offset %d", start)
}
