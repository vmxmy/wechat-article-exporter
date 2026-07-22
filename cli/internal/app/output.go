package app

import (
	"encoding/json"
	"io"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

const JSONSchemaVersion = "wechat-article-cli/v1"

type successEnvelope struct {
	SchemaVersion string `json:"schemaVersion"`
	Success       bool   `json:"success"`
	Data          any    `json:"data,omitempty"`
}

type errorEnvelope struct {
	SchemaVersion string      `json:"schemaVersion"`
	Success       bool        `json:"success"`
	Error         errorDetail `json:"error"`
}

type errorDetail struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	ExitCode int    `json:"exitCode"`
}

func (a *App) output(value any) error {
	return writeSuccessJSON(a.stdout, value)
}

func writeSuccessJSON(output io.Writer, value any) error {
	value = normalizeSuccessData(value)
	envelope := successEnvelope{SchemaVersion: JSONSchemaVersion, Success: true, Data: safety.Redact(value, "")}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(envelope)
}

func normalizeSuccessData(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	success, hasSuccess := object["success"]
	data, hasData := object["data"]
	if hasSuccess && success == true && hasData {
		return data
	}
	return value
}

// WriteErrorJSON writes the single structured error document required by the
// CLI process contract. It is exported so the tiny main package does not need
// to duplicate schema or redaction behavior.
func WriteErrorJSON(output io.Writer, err error) error {
	exitCode := ExitCode(err)
	kind := "runtime"
	if exitCode == 2 {
		kind = "usage"
	} else if IsInterrupted(err) {
		kind = "interrupted"
	}
	envelope := errorEnvelope{
		SchemaVersion: JSONSchemaVersion,
		Success:       false,
		Error: errorDetail{
			Kind: kind, Message: safety.RedactText(err.Error()), ExitCode: exitCode,
		},
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(envelope)
}
