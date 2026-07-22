package processor

import "fmt"

type ClassificationState string

const (
	ClassificationValid       ClassificationState = "valid"
	ClassificationDeleted     ClassificationState = "deleted"
	ClassificationUnavailable ClassificationState = "unavailable"
	ClassificationRiskControl ClassificationState = "risk_control"
	ClassificationParseError  ClassificationState = "parse_error"
)

type ReasonCode string

const (
	ReasonNone                   ReasonCode = ""
	ReasonAuthorDeleted          ReasonCode = "author_deleted"
	ReasonPolicyViolation        ReasonCode = "policy_violation"
	ReasonTemporarilyUnavailable ReasonCode = "temporarily_unavailable"
	ReasonKnownUnavailable       ReasonCode = "known_unavailable"
	ReasonSecurityVerification   ReasonCode = "security_verification"
	ReasonRateLimited            ReasonCode = "rate_limited"
	ReasonAbnormalEnvironment    ReasonCode = "abnormal_environment"
	ReasonInputLimit             ReasonCode = "input_limit"
	ReasonPayloadLimit           ReasonCode = "payload_limit"
	ReasonHTMLLimit              ReasonCode = "html_limit"
	ReasonResourceLimit          ReasonCode = "resource_limit"
	ReasonOutputLimit            ReasonCode = "output_limit"
	ReasonMalformedPayload       ReasonCode = "malformed_payload"
	ReasonUnsupportedPayload     ReasonCode = "unsupported_payload"
	ReasonMissingPayload         ReasonCode = "missing_payload"
	ReasonMissingContentRoot     ReasonCode = "missing_content_root"
	ReasonInvalidArticle         ReasonCode = "invalid_article"
)

type Classification struct {
	State   ClassificationState `json:"state"`
	Reason  ReasonCode          `json:"reason,omitempty"`
	Message string              `json:"message,omitempty"`
}

type ErrorKind string

const (
	ErrorLimit       ErrorKind = "limit"
	ErrorMalformed   ErrorKind = "malformed"
	ErrorUnsupported ErrorKind = "unsupported"
	ErrorInvalid     ErrorKind = "invalid"
)

type ProcessError struct {
	Kind   ErrorKind
	Reason ReasonCode
	Offset int
	Detail string
}

func (err *ProcessError) Error() string {
	if err.Offset > 0 {
		return fmt.Sprintf("processor %s at byte %d: %s", err.Kind, err.Offset, err.Detail)
	}
	return fmt.Sprintf("processor %s: %s", err.Kind, err.Detail)
}

func processError(kind ErrorKind, reason ReasonCode, offset int, format string, args ...any) *ProcessError {
	return &ProcessError{Kind: kind, Reason: reason, Offset: offset, Detail: fmt.Sprintf(format, args...)}
}
