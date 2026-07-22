package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Route interface {
	Name() string
	DoHTTP(*http.Request) (*http.Response, error)
}

type RequestClass string

const (
	PublicContent     RequestClass = "public_content"
	PublicResource    RequestClass = "public_resource"
	ManagementSession RequestClass = "management_session"
	ArticleCredential RequestClass = "article_credential"
	EngagementMetrics RequestClass = "engagement_metrics"
	Comments          RequestClass = "comments"
	PaidContent       RequestClass = "paid_content"
	RouteProbe        RequestClass = "route_probe"
)

type TrustLevel string

const (
	TrustPublicOnly TrustLevel = "public-only"
	TrustCredential TrustLevel = "credential-trusted"
)

type RouteKind string

const (
	RouteURLWrapper RouteKind = "url-wrapper"
)

type HealthState string

const (
	HealthUnknown    HealthState = "unknown"
	HealthHealthy    HealthState = "healthy"
	HealthDegraded   HealthState = "degraded"
	HealthCooldown   HealthState = "cooldown"
	HealthRecovering HealthState = "recovery-probe-required"
	HealthDisabled   HealthState = "disabled"
)

type RouteHealth struct {
	State               HealthState   `json:"state"`
	ConsecutiveFailures int           `json:"consecutiveFailures"`
	CooldownUntil       time.Time     `json:"cooldownUntil,omitempty"`
	LastSampleAt        time.Time     `json:"lastSampleAt,omitempty"`
	LastSuccessAt       time.Time     `json:"lastSuccessAt,omitempty"`
	LastLatency         time.Duration `json:"lastLatency,omitempty"`
	LastStatusCode      int           `json:"lastStatusCode,omitempty"`
	LastErrorClass      string        `json:"lastErrorClass,omitempty"`
}

type RouteConfig struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Kind                    RouteKind      `json:"kind"`
	Endpoint                string         `json:"endpoint"`
	AuthorizationRef        string         `json:"-"`
	AuthorizationConfigured bool           `json:"authorizationConfigured"`
	Trust                   TrustLevel     `json:"trust"`
	Classes                 []RequestClass `json:"classes"`
	Priority                int            `json:"priority"`
	Enabled                 bool           `json:"enabled"`
	Health                  RouteHealth    `json:"health"`
	CreatedAt               time.Time      `json:"createdAt"`
	UpdatedAt               time.Time      `json:"updatedAt"`
	ObservedAt              time.Time      `json:"-"`
}

func (route RouteConfig) MarshalJSON() ([]byte, error) {
	type routeJSON RouteConfig
	sanitized := routeJSON(route)
	sanitized.Endpoint = safety.RedactURL(route.Endpoint)
	sanitized.AuthorizationRef = ""
	return json.Marshal(sanitized)
}

type HealthSample struct {
	ID         string        `json:"id"`
	RouteID    string        `json:"routeId"`
	Success    bool          `json:"success"`
	Latency    time.Duration `json:"latency"`
	StatusCode int           `json:"statusCode,omitempty"`
	ErrorClass string        `json:"errorClass,omitempty"`
	SampledAt  time.Time     `json:"sampledAt"`
}

type RouteRepository interface {
	AddRoute(context.Context, RouteConfig) (RouteConfig, error)
	ListRoutes(context.Context) ([]RouteConfig, error)
	GetRoute(context.Context, string) (RouteConfig, error)
	RemoveRoute(context.Context, string) (RouteConfig, error)
	SetRouteEnabled(context.Context, string, bool, time.Time) (RouteConfig, error)
	RecordRouteHealth(context.Context, HealthSample, int, time.Duration) (RouteConfig, error)
	ResetRouteHealth(context.Context, string, time.Time) (RouteConfig, error)
}

type Request struct {
	Class            RequestClass
	Method           string
	URL              *url.URL
	Header           http.Header
	Body             io.Reader
	MaxResponseBytes int64
}

type Result struct {
	Response  *http.Response
	Route     string
	RequestID string
	Duration  time.Duration
}

type Client interface {
	Do(context.Context, Request) (Result, error)
}

var ErrSensitiveRouteRequired = errors.New("sensitive request requires direct or credential-trusted route")
var ErrRouteNotFound = errors.New("network route not found")

var validRequestClasses = map[RequestClass]struct{}{
	PublicContent: {}, PublicResource: {}, ManagementSession: {}, ArticleCredential: {},
	EngagementMetrics: {}, Comments: {}, PaidContent: {}, RouteProbe: {},
}

func IsSensitive(class RequestClass) bool {
	switch class {
	case ManagementSession, ArticleCredential, EngagementMetrics, Comments, PaidContent:
		return true
	default:
		return false
	}
}

func ValidateRoute(class RequestClass, direct bool, trust TrustLevel) error {
	if IsSensitive(class) && !direct && trust != TrustCredential {
		return fmt.Errorf("request class %s: %w", class, ErrSensitiveRouteRequired)
	}
	return nil
}

func ParseTrustLevel(value string) (TrustLevel, error) {
	trust := TrustLevel(strings.TrimSpace(value))
	switch trust {
	case TrustPublicOnly, TrustCredential:
		return trust, nil
	default:
		return "", fmt.Errorf("unsupported proxy trust level %q", value)
	}
}

func NormalizeRequestClasses(classes []RequestClass) ([]RequestClass, error) {
	if len(classes) == 0 {
		classes = []RequestClass{PublicContent, PublicResource}
	}
	seen := make(map[RequestClass]struct{}, len(classes))
	result := make([]RequestClass, 0, len(classes))
	for _, class := range classes {
		class = RequestClass(strings.TrimSpace(string(class)))
		if _, ok := validRequestClasses[class]; !ok || class == RouteProbe {
			return nil, fmt.Errorf("unsupported configurable request class %q", class)
		}
		if _, ok := seen[class]; ok {
			continue
		}
		seen[class] = struct{}{}
		result = append(result, class)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result, nil
}

func ValidateClassTrust(classes []RequestClass, trust TrustLevel) error {
	for _, class := range classes {
		if err := ValidateRoute(class, false, trust); err != nil {
			return err
		}
	}
	return nil
}

func ClassesMap(classes []RequestClass) map[RequestClass]struct{} {
	result := make(map[RequestClass]struct{}, len(classes))
	for _, class := range classes {
		result[class] = struct{}{}
	}
	return result
}

func CredentialTrustConfirmation(name string) string {
	return "trust-proxy-credentials:" + strings.TrimSpace(name)
}

func CredentialSecretsForClasses(classes []RequestClass) []string {
	secrets := map[string]struct{}{}
	for _, class := range classes {
		switch class {
		case ManagementSession:
			secrets["WeChat session cookies"] = struct{}{}
			secrets["WeChat management token"] = struct{}{}
		case ArticleCredential:
			secrets["WeChat article credential fields (cookie, key, pass_ticket, appmsg_token)"] = struct{}{}
		case EngagementMetrics:
			secrets["WeChat article credential fields (cookie, key, pass_ticket, appmsg_token)"] = struct{}{}
			secrets["engagement metrics request parameters"] = struct{}{}
		case Comments:
			secrets["WeChat article credential fields (cookie, key, pass_ticket, appmsg_token)"] = struct{}{}
			secrets["comment request parameters"] = struct{}{}
		case PaidContent:
			secrets["WeChat article credential fields (cookie, key, pass_ticket, appmsg_token)"] = struct{}{}
			secrets["paid-content authorization"] = struct{}{}
		}
	}
	result := make([]string, 0, len(secrets))
	for secret := range secrets {
		result = append(result, secret)
	}
	sort.Strings(result)
	return result
}

type safeNetworkError struct {
	message string
	cause   error
}

func (err safeNetworkError) Error() string { return err.message }
func (err safeNetworkError) Unwrap() error { return err.cause }

func sanitizeNetworkError(err error, sensitiveURLs ...*url.URL) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, sensitiveURL := range sensitiveURLs {
		if sensitiveURL == nil {
			continue
		}
		redacted := *sensitiveURL
		query := redacted.Query()
		for key := range query {
			query.Set(key, "[REDACTED]")
		}
		redacted.RawQuery = query.Encode()
		message = strings.ReplaceAll(message, sensitiveURL.String(), redacted.String())
		for _, value := range sensitiveURL.Query() {
			for _, item := range value {
				if item != "" {
					message = strings.ReplaceAll(message, url.QueryEscape(item), url.QueryEscape("[REDACTED]"))
					message = strings.ReplaceAll(message, item, "[REDACTED]")
				}
			}
		}
	}
	message = safety.RedactText(message)
	return safeNetworkError{message: message, cause: err}
}
