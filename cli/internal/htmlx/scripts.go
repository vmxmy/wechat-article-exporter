package htmlx

import (
	"bytes"
	"fmt"
	"io"

	"golang.org/x/net/html"
)

// ScriptBlock is one <script> body with its byte offset into Document.Raw.
// Extraction runs the tokenizer over the raw input rather than the parse
// tree: Raw() partitions the input byte-for-byte, so offsets are exact and
// script text is never normalized the way tree construction normalizes it.
type ScriptBlock struct {
	Body   []byte
	Offset int
}

// Scripts returns every <script> body in document order. The result is cached
// on first call; the returned slices alias Document.Raw and must not be
// mutated.
func (document *Document) Scripts() ([]ScriptBlock, error) {
	if document.scripts != nil || document.scriptsErr != nil {
		return document.scripts, document.scriptsErr
	}
	blocks, err := scanScripts(document.Raw, document.limits)
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

func scanScripts(raw []byte, limits Limits) ([]ScriptBlock, error) {
	maxScript := limits.MaxScriptBytes
	if maxScript <= 0 {
		maxScript = defaultMaxScriptBytes
	}
	tokenizer := html.NewTokenizer(bytes.NewReader(raw))
	tokenizer.AllowCDATA(false)
	var blocks []ScriptBlock
	offset := 0
	inScript := false
	for {
		tokenType := tokenizer.Next()
		token := tokenizer.Raw()
		if tokenType == html.ErrorToken {
			if err := tokenizer.Err(); err != io.EOF {
				return nil, fmt.Errorf("tokenize html: %w", err)
			}
			return blocks, nil
		}
		switch tokenType {
		case html.StartTagToken:
			name, _ := tokenizer.TagName()
			inScript = string(name) == "script"
		case html.TextToken:
			if inScript {
				if len(token) > maxScript {
					return nil, fmt.Errorf("%w: over %d bytes at offset %d", ErrScriptTooLarge, maxScript, offset)
				}
				blocks = append(blocks, ScriptBlock{Body: raw[offset : offset+len(token)], Offset: offset})
			}
		case html.EndTagToken:
			inScript = false
		}
		offset += len(token)
	}
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
