package safety

import (
	"fmt"
	"regexp"

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

var sensitiveKey = regexp.MustCompile(`(?i)(authorization|access[_-]?token|refresh[_-]?token|api[_-]?token|auth[_-]?key|api[_-]?key|client[_-]?secret|private[_-]?key|password|cookie|credential|secret|(^|[_-])token($|[_-]))`)

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
	if sensitiveKey.MatchString(key) {
		return "[REDACTED]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		name, hasName := typed["name"].(string)
		for childKey, childValue := range typed {
			if hasName && childKey == "value" && sensitiveKey.MatchString(name) {
				result[childKey] = "[REDACTED]"
				continue
			}
			result[childKey] = Redact(childValue, childKey)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, childValue := range typed {
			result[index] = Redact(childValue, "")
		}
		return result
	default:
		return value
	}
}
