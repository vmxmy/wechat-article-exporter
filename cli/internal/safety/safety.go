package safety

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var currentReadTools = map[string]struct{}{
	"download_article":    {},
	"search_accounts":     {},
	"list_articles":       {},
	"get_account_by_url":  {},
	"get_account_details": {},
	"get_author_info":     {},
	"list_album":          {},
	"get_account_name":    {},
}

var sensitiveKey = regexp.MustCompile(`(?i)(?:authorization|proxy[_-]?authorization|access[_-]?token|refresh[_-]?token|appmsg[_-]?token|pass[_-]?ticket|session(?:id|token|cookie|secret)?$|api[_-]?token|auth[_-]?key|api[_-]?key|client[_-]?secret|private[_-]?key|password|cookies?|credentials?(?:value|data|payload|secret|ref)?$|secrets?(?:value|data|payload|ref)?$|uin|user[_-]?uin|wxtoken|(^|[_-])token($|[_-])|(^|[_-])key($|[_-]))`)
var sensitiveQueryKey = regexp.MustCompile(`(?i)^(?:access_token|refresh_token|appmsg_token|pass_ticket|key|uin|user_uin|wxtoken|auth_key|authorization|proxy_authorization|cookie|credential|session)$`)
var bearerValue = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[^\s,;]+`)
var headerValue = regexp.MustCompile(`(?im)\b(?:proxy-authorization|authorization|set-cookie|cookie)\s*:\s*[^\r\n]+`)
var namedSecretValue = regexp.MustCompile(`(?i)["']?\b(?:access[_-]?token|refresh[_-]?token|appmsg[_-]?token|pass[_-]?ticket|auth[_-]?key|proxy[_-]?authorization|authorization|cookies?|set[-_]?cookie|session(?:id)?|sid|bizuin|uuid|key)["']?\s*[:=]\s*["']?[^&\s,;"'}]+["']?`)

func RequiredConfirmation(tool *mcp.Tool) string {
	if tool.Annotations != nil {
		if tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			return tool.Name
		}
	}
	if _, ok := currentReadTools[tool.Name]; ok {
		return ""
	}
	return tool.Name
}

func AssertConfirmation(tool *mcp.Tool, confirmation string) error {
	required := RequiredConfirmation(tool)
	if required != "" && confirmation != required {
		return fmt.Errorf("refusing protected operation without exact confirmation; retry with --confirm %s, or inspect it first with --dry-run", required)
	}
	return nil
}

func DryRun(toolName string, arguments map[string]any) map[string]any {
	return map[string]any{
		"success":   true,
		"dryRun":    true,
		"operation": "mcp.tools/call",
		"tool":      toolName,
		"arguments": Redact(arguments, ""),
		"note":      "No MCP connection or tool call was made.",
	}
}

func Redact(value any, key string) any {
	if isSensitiveKey(key) && !isPublicSessionContainer(value, key) {
		return "[REDACTED]"
	}
	if value == nil {
		return nil
	}
	if marshaler, ok := value.(json.Marshaler); ok {
		if raw, err := marshaler.MarshalJSON(); err == nil {
			return redactRawJSON(raw)
		}
	}
	return redactValue(reflect.ValueOf(value), key, make(map[visit]bool))
}

type visit struct {
	typeOf  reflect.Type
	pointer uintptr
}

func redactValue(value reflect.Value, key string, seen map[visit]bool) any {
	if isSensitiveKey(key) && !isPublicSessionReflectValue(value, key) {
		return "[REDACTED]"
	}
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return redactValue(value.Elem(), key, seen)
	}
	if value.CanInterface() {
		switch typed := value.Interface().(type) {
		case error:
			return RedactError(typed).Error()
		case json.RawMessage:
			return redactRawJSON(typed)
		case json.Marshaler:
			if raw, err := typed.MarshalJSON(); err == nil {
				return redactRawJSON(raw)
			}
		case string:
			return redactString(typed)
		}
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		current := visit{typeOf: value.Type(), pointer: value.Pointer()}
		if seen[current] {
			return "[REDACTED]"
		}
		seen[current] = true
		defer delete(seen, current)
		return redactValue(value.Elem(), key, seen)
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		result := make(map[string]any, value.Len())
		pairName := mapPairName(value)
		iterator := value.MapRange()
		for iterator.Next() {
			childKey := fmt.Sprint(iterator.Key().Interface())
			if strings.EqualFold(childKey, "value") && isSensitiveKey(pairName) {
				result[childKey] = "[REDACTED]"
				continue
			}
			if isCookiePair(pairName, childKey) {
				result[childKey] = "[REDACTED]"
				continue
			}
			result[childKey] = redactValue(iterator.Value(), childKey, seen)
		}
		return result
	case reflect.Struct:
		if value.CanInterface() {
			if marshaler, ok := value.Interface().(json.Marshaler); ok {
				if data, err := marshaler.MarshalJSON(); err == nil {
					return redactRawJSON(data)
				}
			}
		}
		result := make(map[string]any, value.NumField())
		pairName := structPairName(value)
		valueType := value.Type()
		for index := 0; index < value.NumField(); index++ {
			fieldType := valueType.Field(index)
			if fieldType.PkgPath != "" {
				continue
			}
			fieldName, omitted := jsonFieldName(fieldType)
			if omitted {
				continue
			}
			fieldValue := value.Field(index)
			if strings.EqualFold(fieldName, "value") && isSensitiveKey(pairName) {
				result[fieldName] = "[REDACTED]"
				continue
			}
			if isCookiePair(pairName, fieldName) {
				result[fieldName] = "[REDACTED]"
				continue
			}
			result[fieldName] = redactValue(fieldValue, fieldName, seen)
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			result[index] = redactValue(value.Index(index), "", seen)
		}
		return result
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Interface()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Interface()
	case reflect.Float32, reflect.Float64:
		return value.Interface()
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return nil
	}
}

func redactRawJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return json.RawMessage(nil)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return RedactText(string(raw))
	}
	return Redact(decoded, "")
}

func redactString(value string) string {
	if strings.Contains(value, "://") {
		return RedactURL(value)
	}
	return RedactText(value)
}

func mapPairName(value reflect.Value) string {
	iterator := value.MapRange()
	for iterator.Next() {
		if strings.EqualFold(fmt.Sprint(iterator.Key().Interface()), "name") {
			name := iterator.Value()
			if name.Kind() == reflect.Interface && !name.IsNil() {
				name = name.Elem()
			}
			if name.IsValid() && name.Kind() == reflect.String {
				return name.String()
			}
		}
	}
	return ""
}

func structPairName(value reflect.Value) string {
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		fieldType := valueType.Field(index)
		fieldName, omitted := jsonFieldName(fieldType)
		if omitted || !strings.EqualFold(fieldName, "name") || value.Field(index).Kind() != reflect.String {
			continue
		}
		return value.Field(index).String()
	}
	return ""
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return "", true
	}
	if name == "" {
		name = field.Name
	}
	return name, false
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
	for _, safe := range []string{"secretbackend", "secretref", "authorizationref"} {
		if lower == safe {
			return false
		}
	}
	for _, suffix := range []string{"configured", "removed", "present", "available", "enabled", "count"} {
		if strings.HasSuffix(lower, suffix) {
			return false
		}
	}
	return sensitiveKey.MatchString(key)
}

func isPublicSessionContainer(value any, key string) bool {
	if !strings.EqualFold(key, "session") || value == nil {
		return false
	}
	return isCompositeKind(reflect.ValueOf(value))
}

func isPublicSessionReflectValue(value reflect.Value, key string) bool {
	return strings.EqualFold(key, "session") && isCompositeKind(value)
}

func isCompositeKind(value reflect.Value) bool {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
		return true
	default:
		return false
	}
}

func isCookiePair(pairName, fieldName string) bool {
	return strings.EqualFold(fieldName, "value") && pairName != "" && likelyCookieName(pairName)
}

func likelyCookieName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" || strings.ContainsAny(lower, " \t\r\n:/") {
		return false
	}
	for _, marker := range []string{"cookie", "sid", "uin", "token", "ticket", "key", "session", "uuid", "biz"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func RedactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" {
		return RedactText(raw)
	}
	if parsed.User != nil {
		parsed.User = url.User("[REDACTED]")
	}
	query := parsed.Query()
	for key := range query {
		if sensitiveQueryKey.MatchString(key) || sensitiveKey.MatchString(key) {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func RedactText(text string) string {
	redacted := bearerValue.ReplaceAllString(text, "$1 [REDACTED]")
	redacted = headerValue.ReplaceAllStringFunc(redacted, func(value string) string {
		separator := strings.Index(value, ":")
		return value[:separator+1] + " [REDACTED]"
	})
	redacted = namedSecretValue.ReplaceAllStringFunc(redacted, func(value string) string {
		separator := strings.IndexAny(value, ":=")
		return strings.TrimRight(value[:separator], " \t") + value[separator:separator+1] + "[REDACTED]"
	})
	return redacted
}

type redactedError struct {
	message string
	cause   error
	causes  []error
}

func (err redactedError) Error() string { return err.message }
func (err redactedError) Unwrap() error {
	return err.cause
}
func (err redactedError) Is(target error) bool {
	for _, cause := range err.causes {
		if errors.Is(cause, target) {
			return true
		}
	}
	return false
}
func (err redactedError) As(target any) bool {
	for _, cause := range err.causes {
		if errors.As(cause, target) {
			return true
		}
	}
	return false
}
func (err redactedError) Format(state fmt.State, verb rune) {
	fmt.Fprint(state, err.message)
}

func RedactError(err error) error {
	if err == nil {
		return nil
	}
	causes := errorCauses(err)
	redacted := redactedError{message: redactErrorMessage(err), causes: causes}
	if len(causes) == 1 {
		redacted.cause = causes[0]
	}
	return redacted
}

func redactErrorMessage(err error) string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		messages := make([]string, 0, len(children))
		for _, child := range children {
			messages = append(messages, redactErrorMessage(child))
		}
		if err.Error() == strings.Join(errorStrings(children), "\n") {
			return strings.Join(messages, "\n")
		}
	}
	if cause := errors.Unwrap(err); cause != nil && strings.HasSuffix(err.Error(), cause.Error()) {
		prefix := strings.TrimSuffix(err.Error(), cause.Error())
		return redactString(prefix) + redactErrorMessage(cause)
	}
	return redactString(err.Error())
}

func errorStrings(errorsList []error) []string {
	result := make([]string, len(errorsList))
	for index, err := range errorsList {
		result[index] = err.Error()
	}
	return result
}

func errorCauses(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return append([]error(nil), joined.Unwrap()...)
	}
	if cause := errors.Unwrap(err); cause != nil {
		return []error{cause}
	}
	return []error{err}
}
