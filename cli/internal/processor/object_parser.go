package processor

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

type objectParser struct {
	data         []byte
	baseOffset   int
	position     int
	limits       Limits
	decodedBytes int
	members      int
	items        int
}

// jsNaN preserves JavaScript's result for non-numeric primitive coercion. It
// cannot escape into the normalized model because all field readers reject it.
type jsNaN struct{}

func parseObjectLiteral(data []byte, baseOffset int, limits Limits) (any, error) {
	if len(data) > limits.MaxPayloadBytes {
		return nil, processError(ErrorLimit, ReasonPayloadLimit, baseOffset, "payload exceeds %d bytes", limits.MaxPayloadBytes)
	}
	parser := &objectParser{data: data, baseOffset: baseOffset, limits: limits}
	value, err := parser.parseValue(0)
	if err != nil {
		return nil, err
	}
	if err := parser.skipSpaceAndComments(); err != nil {
		return nil, err
	}
	if parser.position != len(parser.data) {
		return nil, parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "unsupported trailing expression")
	}
	return value, nil
}

func (parser *objectParser) parseValue(depth int) (any, error) {
	if depth > parser.limits.MaxDepth {
		return nil, parser.error(ErrorLimit, ReasonPayloadLimit, "payload nesting exceeds %d", parser.limits.MaxDepth)
	}
	if err := parser.skipSpaceAndComments(); err != nil {
		return nil, err
	}
	if parser.position >= len(parser.data) {
		return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "expected a value")
	}

	var value any
	var err error
	switch char := parser.data[parser.position]; {
	case char == '{':
		value, err = parser.parseObject(depth + 1)
	case char == '[':
		value, err = parser.parseArray(depth + 1)
	case char == '\'' || char == '"':
		value, err = parser.parseString()
	case char == '-' || char == '+' || char == '.' || char >= '0' && char <= '9':
		value, err = parser.parseNumber()
	case isIdentifierStart(char):
		value, err = parser.parseIdentifierValue(depth)
	default:
		return nil, parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "unsupported value token %q", char)
	}
	if err != nil {
		return nil, err
	}

	if err := parser.skipSpaceAndComments(); err != nil {
		return nil, err
	}
	if parser.position < len(parser.data) && parser.data[parser.position] == '*' {
		parser.position++
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		multiplier, numberErr := parser.parseNumber()
		if numberErr != nil {
			return nil, numberErr
		}
		factor, ok := multiplier.(json.Number)
		if !ok || factor.String() != "1" {
			return nil, parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "only numeric coercion with * 1 is supported")
		}
		value, err = coerceNumber(value)
		if err != nil {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "%v", err)
		}
	}
	return value, nil
}

func (parser *objectParser) parseObject(depth int) (map[string]any, error) {
	parser.position++
	object := make(map[string]any)
	for {
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated object")
		}
		if parser.data[parser.position] == '}' {
			parser.position++
			return object, nil
		}

		key, err := parser.parseKey()
		if err != nil {
			return nil, err
		}
		parser.members++
		if parser.members > parser.limits.MaxObjectMembers {
			return nil, parser.error(ErrorLimit, ReasonPayloadLimit, "object members exceed %d", parser.limits.MaxObjectMembers)
		}
		if _, exists := object[key]; exists {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "duplicate object key %q", key)
		}
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) || parser.data[parser.position] != ':' {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "expected ':' after object key")
		}
		parser.position++
		value, err := parser.parseValue(depth)
		if err != nil {
			return nil, err
		}
		object[key] = value

		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated object")
		}
		switch parser.data[parser.position] {
		case ',':
			parser.position++
		case '}':
			parser.position++
			return object, nil
		default:
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "expected ',' or '}'")
		}
	}
}

func (parser *objectParser) parseArray(depth int) ([]any, error) {
	parser.position++
	array := make([]any, 0)
	for {
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated array")
		}
		if parser.data[parser.position] == ']' {
			parser.position++
			return array, nil
		}
		value, err := parser.parseValue(depth)
		if err != nil {
			return nil, err
		}
		parser.items++
		if parser.items > parser.limits.MaxArrayItems {
			return nil, parser.error(ErrorLimit, ReasonPayloadLimit, "array items exceed %d", parser.limits.MaxArrayItems)
		}
		array = append(array, value)
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated array")
		}
		switch parser.data[parser.position] {
		case ',':
			parser.position++
		case ']':
			parser.position++
			return array, nil
		default:
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "expected ',' or ']'")
		}
	}
}

func (parser *objectParser) parseKey() (string, error) {
	if parser.position >= len(parser.data) {
		return "", parser.error(ErrorMalformed, ReasonMalformedPayload, "expected object key")
	}
	if parser.data[parser.position] == '\'' || parser.data[parser.position] == '"' {
		return parser.parseString()
	}
	if !isIdentifierStart(parser.data[parser.position]) {
		return "", parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "unsupported object key")
	}
	return parser.parseIdentifier(), nil
}

func (parser *objectParser) parseIdentifierValue(depth int) (any, error) {
	identifier := parser.parseIdentifier()
	switch identifier {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	case "JsDecode":
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) || parser.data[parser.position] != '(' {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "JsDecode must be called with one string")
		}
		parser.position++
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) || parser.data[parser.position] != '\'' && parser.data[parser.position] != '"' {
			return nil, parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "JsDecode argument must be a string literal")
		}
		value, err := parser.parseString()
		if err != nil {
			return nil, err
		}
		if err := parser.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if parser.position >= len(parser.data) || parser.data[parser.position] != ')' {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated JsDecode call")
		}
		parser.position++
		return value, nil
	default:
		return nil, parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "identifier %q is not allowed as a value", identifier)
	}
}

func (parser *objectParser) parseIdentifier() string {
	start := parser.position
	parser.position++
	for parser.position < len(parser.data) && isIdentifierPart(parser.data[parser.position]) {
		parser.position++
	}
	return string(parser.data[start:parser.position])
}

func (parser *objectParser) parseString() (string, error) {
	quote := parser.data[parser.position]
	parser.position++
	var builder strings.Builder
	for parser.position < len(parser.data) {
		char := parser.data[parser.position]
		parser.position++
		if char == quote {
			value := builder.String()
			if len(value) > parser.limits.MaxStringBytes {
				return "", parser.error(ErrorLimit, ReasonPayloadLimit, "decoded string exceeds %d bytes", parser.limits.MaxStringBytes)
			}
			parser.decodedBytes += len(value)
			if parser.decodedBytes > parser.limits.MaxDecodedPayloadBytes {
				return "", parser.error(ErrorLimit, ReasonPayloadLimit, "decoded payload exceeds %d bytes", parser.limits.MaxDecodedPayloadBytes)
			}
			return value, nil
		}
		if char == '\n' || char == '\r' {
			return "", parser.error(ErrorMalformed, ReasonMalformedPayload, "unescaped newline in string")
		}
		if char != '\\' {
			builder.WriteByte(char)
			continue
		}
		if parser.position >= len(parser.data) {
			return "", parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated string escape")
		}
		escape := parser.data[parser.position]
		parser.position++
		switch escape {
		case '\\', '\'', '"', '/':
			builder.WriteByte(escape)
		case 'b':
			builder.WriteByte('\b')
		case 'f':
			builder.WriteByte('\f')
		case 'n':
			builder.WriteByte('\n')
		case 'r':
			builder.WriteByte('\r')
		case 't':
			builder.WriteByte('\t')
		case 'v':
			builder.WriteByte('\v')
		case '0':
			if parser.position < len(parser.data) && parser.data[parser.position] >= '0' && parser.data[parser.position] <= '9' {
				return "", parser.error(ErrorUnsupported, ReasonUnsupportedPayload, "legacy octal string escape is unsupported")
			}
			builder.WriteByte(0)
		case 'x':
			decoded, err := parser.parseHexEscape(2)
			if err != nil {
				return "", err
			}
			builder.WriteByte(byte(decoded))
		case 'u':
			decoded, err := parser.parseUnicodeEscape()
			if err != nil {
				return "", err
			}
			builder.WriteRune(decoded)
		case '\n':
			// JavaScript line continuation.
		case '\r':
			if parser.position < len(parser.data) && parser.data[parser.position] == '\n' {
				parser.position++
			}
		default:
			// JavaScript string literals treat an unknown non-octal escape as
			// the escaped character itself.
			builder.WriteByte(escape)
		}
	}
	return "", parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated string")
}

func (parser *objectParser) parseHexEscape(length int) (uint64, error) {
	if parser.position+length > len(parser.data) {
		return 0, parser.error(ErrorMalformed, ReasonMalformedPayload, "short hexadecimal escape")
	}
	buffer := make([]byte, length/2+1)
	decoded, err := hex.Decode(buffer, parser.data[parser.position:parser.position+length])
	if err != nil || decoded == 0 {
		return 0, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid hexadecimal escape")
	}
	parser.position += length
	value, _ := strconv.ParseUint(string(parser.data[parser.position-length:parser.position]), 16, length*4)
	return value, nil
}

func (parser *objectParser) parseUnicodeEscape() (rune, error) {
	value, err := parser.parseHexEscape(4)
	if err != nil {
		return 0, err
	}
	first := rune(value)
	if first < 0xD800 || first > 0xDFFF {
		return first, nil
	}
	if first > 0xDBFF || parser.position+6 > len(parser.data) || parser.data[parser.position] != '\\' || parser.data[parser.position+1] != 'u' {
		return 0, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid Unicode surrogate pair")
	}
	parser.position += 2
	secondValue, err := parser.parseHexEscape(4)
	if err != nil {
		return 0, err
	}
	second := rune(secondValue)
	if second < 0xDC00 || second > 0xDFFF {
		return 0, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid Unicode surrogate pair")
	}
	return utf16.DecodeRune(first, second), nil
}

func (parser *objectParser) parseNumber() (any, error) {
	start := parser.position
	if parser.position < len(parser.data) && (parser.data[parser.position] == '+' || parser.data[parser.position] == '-') {
		parser.position++
	}
	if parser.position < len(parser.data)-1 && parser.data[parser.position] == '0' && (parser.data[parser.position+1] == 'x' || parser.data[parser.position+1] == 'X') {
		parser.position += 2
		digits := parser.position
		for parser.position < len(parser.data) && isHexDigit(parser.data[parser.position]) {
			parser.position++
		}
		if parser.position == digits {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid hexadecimal number")
		}
		value, err := strconv.ParseInt(string(parser.data[digits:parser.position]), 16, 64)
		if err != nil {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid hexadecimal number")
		}
		if parser.data[start] == '-' {
			value = -value
		}
		return json.Number(strconv.FormatInt(value, 10)), nil
	}

	digits := 0
	for parser.position < len(parser.data) && parser.data[parser.position] >= '0' && parser.data[parser.position] <= '9' {
		parser.position++
		digits++
	}
	if parser.position < len(parser.data) && parser.data[parser.position] == '.' {
		parser.position++
		for parser.position < len(parser.data) && parser.data[parser.position] >= '0' && parser.data[parser.position] <= '9' {
			parser.position++
			digits++
		}
	}
	if digits == 0 {
		return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid number")
	}
	if parser.position < len(parser.data) && (parser.data[parser.position] == 'e' || parser.data[parser.position] == 'E') {
		parser.position++
		if parser.position < len(parser.data) && (parser.data[parser.position] == '+' || parser.data[parser.position] == '-') {
			parser.position++
		}
		exponentDigits := 0
		for parser.position < len(parser.data) && parser.data[parser.position] >= '0' && parser.data[parser.position] <= '9' {
			parser.position++
			exponentDigits++
		}
		if exponentDigits == 0 {
			return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid exponent")
		}
	}
	number := json.Number(string(parser.data[start:parser.position]))
	if _, err := number.Float64(); err != nil {
		return nil, parser.error(ErrorMalformed, ReasonMalformedPayload, "invalid number")
	}
	return number, nil
}

func (parser *objectParser) skipSpaceAndComments() error {
	for parser.position < len(parser.data) {
		for parser.position < len(parser.data) && isSpace(parser.data[parser.position]) {
			parser.position++
		}
		if parser.position+1 >= len(parser.data) || parser.data[parser.position] != '/' {
			return nil
		}
		switch parser.data[parser.position+1] {
		case '/':
			parser.position += 2
			for parser.position < len(parser.data) && parser.data[parser.position] != '\n' && parser.data[parser.position] != '\r' {
				parser.position++
			}
		case '*':
			end := strings.Index(string(parser.data[parser.position+2:]), "*/")
			if end < 0 {
				return parser.error(ErrorMalformed, ReasonMalformedPayload, "unterminated block comment")
			}
			parser.position += end + 4
		default:
			return nil
		}
	}
	return nil
}

func (parser *objectParser) error(kind ErrorKind, reason ReasonCode, format string, arguments ...any) error {
	return processError(kind, reason, parser.baseOffset+parser.position, format, arguments...)
}

func coerceNumber(value any) (any, error) {
	switch item := value.(type) {
	case json.Number:
		return item, nil
	case string:
		if item == "" {
			return json.Number("0"), nil
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		if err != nil || math.IsNaN(parsed) {
			return jsNaN{}, nil
		}
		if math.IsInf(parsed, 0) {
			return jsNaN{}, nil
		}
		return json.Number(strconv.FormatFloat(parsed, 'f', -1, 64)), nil
	case bool:
		if item {
			return json.Number("1"), nil
		}
		return json.Number("0"), nil
	case nil:
		return json.Number("0"), nil
	default:
		return jsNaN{}, nil
	}
}

func isIdentifierStart(char byte) bool {
	return char == '_' || char == '$' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func isHexDigit(char byte) bool {
	return char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F'
}
