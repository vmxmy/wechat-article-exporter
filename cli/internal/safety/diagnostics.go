package safety

import (
	"errors"
	"fmt"
)

var ErrDiagnosticBundleConfirmation = errors.New("diagnostic bundle inclusion requires exact confirmation")

const DiagnosticBundleConfirmation = "include-diagnostic-secrets-and-article-bodies"

type DiagnosticBundleInput struct {
	System        any
	Configuration any
	SchemaVersion any
	Logs          any
	Integrity     any
	ArticleBodies any
	Secrets       any
}

type DiagnosticBundleOptions struct {
	IncludeArticleBodies bool
	IncludeSecrets       bool
	Confirmation         string
}

func AssembleDiagnosticBundle(input DiagnosticBundleInput, options DiagnosticBundleOptions) (map[string]any, error) {
	includeRestricted := options.IncludeArticleBodies || options.IncludeSecrets
	if includeRestricted && options.Confirmation != DiagnosticBundleConfirmation {
		return nil, fmt.Errorf("%w: use %q", ErrDiagnosticBundleConfirmation, DiagnosticBundleConfirmation)
	}
	bundle := map[string]any{
		"system":        Redact(input.System, ""),
		"configuration": Redact(input.Configuration, ""),
		"schemaVersion": Redact(input.SchemaVersion, ""),
		"logs":          Redact(input.Logs, ""),
		"integrity":     Redact(input.Integrity, ""),
	}
	if options.IncludeArticleBodies {
		bundle["articleBodies"] = Redact(input.ArticleBodies, "")
	}
	if options.IncludeSecrets {
		// Explicit confirmation permits the section to exist, but the central
		// redactor still prevents raw secret bytes from entering diagnostics.
		bundle["secrets"] = Redact(input.Secrets, "secrets")
	}
	return bundle, nil
}
