package processor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/htmlx"
)

type scriptBlock struct {
	attributes []byte
	content    []byte
	offset     int
}

type payloadCandidate struct {
	variant PayloadVariant
	data    []byte
	offset  int
}

func extractPayload(html []byte, limits Limits) (map[string]any, PayloadVariant, error) {
	scripts, err := scanScripts(html, limits)
	if err != nil {
		return nil, "", err
	}

	for _, variant := range []PayloadVariant{PayloadCGIDataNew, PayloadEmbeddedJSON, PayloadCGIData} {
		for _, script := range scripts {
			candidate, ok, candidateErr := findPayloadCandidate(script, variant, limits)
			if candidateErr != nil {
				return nil, "", candidateErr
			}
			if !ok {
				continue
			}
			var value any
			if candidate.variant == PayloadEmbeddedJSON {
				decoder := json.NewDecoder(bytes.NewReader(candidate.data))
				decoder.UseNumber()
				if err := decoder.Decode(&value); err != nil {
					return nil, "", processError(ErrorMalformed, ReasonMalformedPayload, candidate.offset, "invalid embedded JSON: %v", err)
				}
				var trailing any
				if err := decoder.Decode(&trailing); err == nil {
					return nil, "", processError(ErrorMalformed, ReasonMalformedPayload, candidate.offset, "embedded JSON has trailing data")
				} else if err != io.EOF {
					return nil, "", processError(ErrorMalformed, ReasonMalformedPayload, candidate.offset, "invalid trailing embedded JSON: %v", err)
				}
				stats, limitErr := validateDecodedJSON(value, limits, 0, &decodedStats{})
				if limitErr != nil {
					if typed, ok := limitErr.(*ProcessError); ok {
						typed.Offset += candidate.offset
					}
					return nil, "", limitErr
				}
				if stats.bytes > limits.MaxDecodedPayloadBytes {
					return nil, "", processError(ErrorLimit, ReasonPayloadLimit, candidate.offset, "decoded payload exceeds %d bytes", limits.MaxDecodedPayloadBytes)
				}
			} else {
				value, err = parseObjectLiteral(candidate.data, candidate.offset, limits)
				if err != nil {
					return nil, "", err
				}
			}
			object, ok := value.(map[string]any)
			if !ok {
				return nil, "", processError(ErrorMalformed, ReasonMalformedPayload, candidate.offset, "CGI payload must be an object")
			}
			return object, candidate.variant, nil
		}
	}
	return nil, "", nil
}

type decodedStats struct {
	bytes   int
	members int
	items   int
}

func validateDecodedJSON(value any, limits Limits, depth int, stats *decodedStats) (*decodedStats, error) {
	if depth > limits.MaxDepth {
		return nil, processError(ErrorLimit, ReasonPayloadLimit, 0, "payload nesting exceeds %d", limits.MaxDepth)
	}
	switch item := value.(type) {
	case nil:
		stats.bytes += 4
	case string:
		if len(item) > limits.MaxStringBytes {
			return nil, processError(ErrorLimit, ReasonPayloadLimit, 0, "decoded string exceeds %d bytes", limits.MaxStringBytes)
		}
		stats.bytes += len(item)
	case json.Number:
		stats.bytes += len(item)
	case bool:
		stats.bytes += 5
	case []any:
		stats.items += len(item)
		if stats.items > limits.MaxArrayItems {
			return nil, processError(ErrorLimit, ReasonPayloadLimit, 0, "array items exceed %d", limits.MaxArrayItems)
		}
		for _, child := range item {
			if _, err := validateDecodedJSON(child, limits, depth+1, stats); err != nil {
				return nil, err
			}
		}
	case map[string]any:
		stats.members += len(item)
		if stats.members > limits.MaxObjectMembers {
			return nil, processError(ErrorLimit, ReasonPayloadLimit, 0, "object members exceed %d", limits.MaxObjectMembers)
		}
		for key, child := range item {
			if len(key) > limits.MaxStringBytes {
				return nil, processError(ErrorLimit, ReasonPayloadLimit, 0, "decoded object key exceeds %d bytes", limits.MaxStringBytes)
			}
			stats.bytes += len(key)
			if _, err := validateDecodedJSON(child, limits, depth+1, stats); err != nil {
				return nil, err
			}
		}
	default:
		stats.bytes += len(fmt.Sprint(item))
	}
	if stats.bytes > limits.MaxDecodedPayloadBytes {
		return nil, processError(ErrorLimit, ReasonPayloadLimit, 0, "decoded payload exceeds %d bytes", limits.MaxDecodedPayloadBytes)
	}
	return stats, nil
}

// scanScripts delegates tokenization to htmlx and keeps the processor's own
// policy layered on top: only payload-bearing scripts are size-capped, and
// htmlx input errors are translated into this package's typed ProcessError
// contract.
func scanScripts(html []byte, limits Limits) ([]scriptBlock, error) {
	scanned, err := htmlx.ScanScripts(html)
	if err != nil {
		if errors.Is(err, htmlx.ErrUnterminatedScript) {
			return nil, processError(ErrorMalformed, ReasonMalformedPayload, 0, "unterminated script element")
		}
		return nil, processError(ErrorMalformed, ReasonMalformedPayload, 0, "%v", err)
	}
	blocks := make([]scriptBlock, 0, len(scanned))
	for _, block := range scanned {
		if len(block.Body) > limits.MaxScriptBytes && containsPayloadMarker(block.Body) {
			return nil, processError(ErrorLimit, ReasonPayloadLimit, block.Offset, "script containing CGI payload exceeds %d bytes", limits.MaxScriptBytes)
		}
		blocks = append(blocks, scriptBlock{attributes: block.RawAttrs, content: block.Body, offset: block.Offset})
	}
	return blocks, nil
}

func findTagEnd(html []byte, position int) (int, error) {
	var quote byte
	for index := position; index < len(html); index++ {
		char := html[index]
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '>':
			return index, nil
		}
	}
	return 0, processError(ErrorMalformed, ReasonMalformedPayload, position, "unterminated HTML tag")
}

func findPayloadCandidate(script scriptBlock, variant PayloadVariant, limits Limits) (payloadCandidate, bool, error) {
	if variant == PayloadEmbeddedJSON {
		if !isEmbeddedCGIScript(script.attributes) {
			return payloadCandidate{}, false, nil
		}
		data := bytes.TrimSpace(script.content)
		if len(data) > limits.MaxPayloadBytes {
			return payloadCandidate{}, false, processError(ErrorLimit, ReasonPayloadLimit, script.offset, "embedded payload exceeds %d bytes", limits.MaxPayloadBytes)
		}
		return payloadCandidate{variant: variant, data: data, offset: script.offset}, true, nil
	}

	marker := []byte(variant)
	searchFrom := 0
	for {
		relative := bytes.Index(script.content[searchFrom:], marker)
		if relative < 0 {
			return payloadCandidate{}, false, nil
		}
		markerStart := searchFrom + relative
		markerEnd := markerStart + len(marker)
		if markerStart > 0 && isIdentifierPart(script.content[markerStart-1]) {
			searchFrom = markerEnd
			continue
		}
		position := skipWhitespaceAndComments(script.content, markerEnd)
		if position >= len(script.content) || script.content[position] != '=' {
			searchFrom = markerEnd
			continue
		}
		position = skipWhitespaceAndComments(script.content, position+1)
		if position >= len(script.content) || script.content[position] != '{' {
			return payloadCandidate{}, false, processError(ErrorUnsupported, ReasonUnsupportedPayload, script.offset+position, "%s must be assigned an object literal", variant)
		}
		end, err := findBalancedObject(script.content, position, limits.MaxPayloadBytes)
		if err != nil {
			if typed, ok := err.(*ProcessError); ok {
				typed.Offset += script.offset
			}
			return payloadCandidate{}, false, err
		}
		return payloadCandidate{variant: variant, data: script.content[position:end], offset: script.offset + position}, true, nil
	}
}

// findBalancedObject translates htmlx's balanced-object scan back into this
// package's typed error contract, reconstructing the offsets the byte scanner
// used to report.
func findBalancedObject(script []byte, start, maxBytes int) (int, error) {
	end, err := htmlx.FindBalancedObject(script, start, maxBytes)
	if err == nil {
		return end, nil
	}
	if errors.Is(err, htmlx.ErrScriptTooLarge) {
		return 0, processError(ErrorLimit, ReasonPayloadLimit, start+maxBytes, "payload exceeds %d bytes", maxBytes)
	}
	return 0, processError(ErrorMalformed, ReasonMalformedPayload, start, "unterminated CGI object literal")
}

func skipWhitespaceAndComments(data []byte, position int) int {
	for position < len(data) {
		for position < len(data) && isSpace(data[position]) {
			position++
		}
		if position+1 >= len(data) || data[position] != '/' {
			return position
		}
		switch data[position+1] {
		case '/':
			position += 2
			for position < len(data) && data[position] != '\n' && data[position] != '\r' {
				position++
			}
		case '*':
			end := bytes.Index(data[position+2:], []byte("*/"))
			if end < 0 {
				return len(data)
			}
			position += end + 4
		default:
			return position
		}
	}
	return position
}

func isEmbeddedCGIScript(attributes []byte) bool {
	lower := strings.ToLower(string(attributes))
	hasJSONType := strings.Contains(lower, "application/json") || strings.Contains(lower, "application/ld+json")
	hasCGIName := strings.Contains(lower, "cgi-data") || strings.Contains(lower, "cgi_data") || strings.Contains(lower, "cgidata")
	return hasJSONType && hasCGIName
}

func containsPayloadMarker(data []byte) bool {
	return bytes.Contains(data, []byte(PayloadCGIDataNew)) || bytes.Contains(data, []byte(PayloadCGIData))
}

func isHTMLNameChar(char byte) bool {
	return char == '-' || char == ':' || char == '_' || char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func isIdentifierPart(char byte) bool {
	return char == '_' || char == '$' || char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func isSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\f'
}
