package network

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
)

const proxyAuthorizationKind = "proxy-authorization"

type AddProxyRequest struct {
	Name          string         `json:"name"`
	Endpoint      string         `json:"endpoint"`
	Authorization string         `json:"-"`
	Trust         TrustLevel     `json:"trust"`
	Classes       []RequestClass `json:"classes"`
	Priority      int            `json:"priority"`
}

type ProbeResult struct {
	Route              RouteConfig   `json:"route"`
	Latency            time.Duration `json:"latency"`
	StatusCode         int           `json:"statusCode,omitempty"`
	ResponseValid      bool          `json:"responseValid"`
	CredentialEligible bool          `json:"credentialEligible"`
	ErrorClass         string        `json:"errorClass,omitempty"`
}

type ServiceOptions struct {
	Profile          string
	Routes           RouteRepository
	Secrets          secrets.Store
	HTTP             Doer
	EndpointPolicy   DestinationPolicy
	TargetPolicy     DestinationPolicy
	ProbeTarget      *url.URL
	Now              func() time.Time
	FailureThreshold int
	Cooldown         time.Duration
}

type Service struct {
	profile          string
	routes           RouteRepository
	secrets          secrets.Store
	http             Doer
	endpointPolicy   DestinationPolicy
	targetPolicy     DestinationPolicy
	probeTarget      *url.URL
	now              func() time.Time
	failureThreshold int
	cooldown         time.Duration
}

func NewService(options ServiceOptions) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	failureThreshold := options.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	cooldown := options.Cooldown
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	return &Service{
		profile: options.Profile, routes: options.Routes, secrets: options.Secrets, http: options.HTTP,
		endpointPolicy: options.EndpointPolicy, targetPolicy: options.TargetPolicy, probeTarget: options.ProbeTarget,
		now: now, failureThreshold: failureThreshold, cooldown: cooldown,
	}
}

func (service *Service) Add(ctx context.Context, request AddProxyRequest) (networkRoute RouteConfig, err error) {
	if service.routes == nil {
		return RouteConfig{}, errors.New("route repository is not configured")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return RouteConfig{}, errors.New("proxy name is required")
	}
	endpoint, err := ParseEndpoint(strings.TrimSpace(request.Endpoint))
	if err != nil {
		return RouteConfig{}, err
	}
	if err := service.endpointPolicy.Validate(ctx, endpoint); err != nil {
		return RouteConfig{}, fmt.Errorf("proxy endpoint: %w", err)
	}
	trust := request.Trust
	if trust == "" {
		trust = TrustPublicOnly
	}
	if _, err := ParseTrustLevel(string(trust)); err != nil {
		return RouteConfig{}, err
	}
	classes, err := NormalizeRequestClasses(request.Classes)
	if err != nil {
		return RouteConfig{}, err
	}
	if err := ValidateClassTrust(classes, trust); err != nil {
		return RouteConfig{}, err
	}
	priority := request.Priority
	if priority == 0 {
		priority = 100
	}
	id, err := randomRouteID()
	if err != nil {
		return RouteConfig{}, err
	}
	authorizationRef := ""
	if request.Authorization != "" {
		if service.secrets == nil {
			return RouteConfig{}, errors.New("secret store is required for proxy authorization")
		}
		authorizationRef = id
		if err := service.secrets.Set(ctx, service.secretRef(authorizationRef), []byte(request.Authorization)); err != nil {
			return RouteConfig{}, fmt.Errorf("store proxy authorization: %w", err)
		}
		defer func() {
			if err != nil {
				_ = service.secrets.Delete(ctx, service.secretRef(authorizationRef))
			}
		}()
	}
	now := service.now()
	return service.routes.AddRoute(ctx, RouteConfig{
		ID: id, Name: name, Kind: RouteURLWrapper, Endpoint: endpoint.String(),
		AuthorizationRef: authorizationRef, Trust: trust, Classes: classes, Priority: priority,
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	})
}

func (service *Service) List(ctx context.Context) ([]RouteConfig, error) {
	if service.routes == nil {
		return nil, errors.New("route repository is not configured")
	}
	routes, err := service.routes.ListRoutes(ctx)
	for index := range routes {
		routes[index] = service.observe(routes[index])
	}
	return routes, err
}

func (service *Service) Remove(ctx context.Context, idOrName string) (RouteConfig, error) {
	if service.routes == nil {
		return RouteConfig{}, errors.New("route repository is not configured")
	}
	route, err := service.routes.GetRoute(ctx, idOrName)
	if err != nil {
		return RouteConfig{}, err
	}
	if route.AuthorizationRef != "" && service.secrets != nil {
		if err := service.secrets.Delete(ctx, service.secretRef(route.AuthorizationRef)); err != nil {
			return RouteConfig{}, fmt.Errorf("remove proxy authorization: %w", err)
		}
	}
	removed, err := service.routes.RemoveRoute(ctx, route.ID)
	if err != nil {
		return RouteConfig{}, err
	}
	return removed, nil
}

func (service *Service) Enable(ctx context.Context, idOrName string) (RouteConfig, error) {
	return service.setEnabled(ctx, idOrName, true)
}

func (service *Service) Disable(ctx context.Context, idOrName string) (RouteConfig, error) {
	return service.setEnabled(ctx, idOrName, false)
}

func (service *Service) setEnabled(ctx context.Context, idOrName string, enabled bool) (RouteConfig, error) {
	if service.routes == nil {
		return RouteConfig{}, errors.New("route repository is not configured")
	}
	return service.routes.SetRouteEnabled(ctx, idOrName, enabled, service.now())
}

func (service *Service) Test(ctx context.Context, idOrName string) (ProbeResult, error) {
	if service.routes == nil {
		return ProbeResult{}, errors.New("route repository is not configured")
	}
	route, err := service.routes.GetRoute(ctx, idOrName)
	if err != nil {
		return ProbeResult{}, err
	}
	route = service.observe(route)
	if service.probeTarget == nil {
		return ProbeResult{}, errors.New("proxy probe target is not configured")
	}
	parsedEndpoint, err := url.Parse(route.Endpoint)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("parse stored proxy endpoint: %w", err)
	}
	if err := service.targetPolicy.Validate(ctx, service.probeTarget); err != nil {
		return ProbeResult{}, fmt.Errorf("probe target: %w", err)
	}
	if err := service.endpointPolicy.Validate(ctx, parsedEndpoint); err != nil {
		return ProbeResult{}, fmt.Errorf("proxy endpoint: %w", err)
	}
	probe := &URLWrapper{
		RouteName: route.Name, Endpoint: parsedEndpoint,
		Trust: route.Trust, HTTP: service.http, Policy: service.endpointPolicy, Now: service.now,
	}
	if route.AuthorizationRef != "" {
		if service.secrets == nil {
			return ProbeResult{}, errors.New("secret store is required for proxy authorization")
		}
		authorization, getErr := service.secrets.Get(ctx, service.secretRef(route.AuthorizationRef))
		if getErr != nil {
			return ProbeResult{}, fmt.Errorf("load proxy authorization: %w", getErr)
		}
		probe = probe.WithAuthorization(string(authorization))
	}
	started := service.now()
	result, probeErr := probe.Do(ctx, Request{Class: RouteProbe, URL: cloneURL(service.probeTarget), MaxResponseBytes: 64 << 10})
	probeErr = sanitizeNetworkError(probeErr, parsedEndpoint, service.probeTarget)
	latency := service.now().Sub(started)
	statusCode := 0
	responseValid := false
	if probeErr == nil && result.Response != nil {
		statusCode = result.Response.StatusCode
		_, readErr := io.Copy(io.Discard, result.Response.Body)
		closeErr := result.Response.Body.Close()
		responseValid = readErr == nil && closeErr == nil && statusCode >= 200 && statusCode < 400
		if !responseValid {
			if readErr != nil {
				probeErr = readErr
			} else if closeErr != nil {
				probeErr = closeErr
			} else {
				probeErr = fmt.Errorf("proxy probe returned HTTP %d", statusCode)
			}
		}
	}
	errorClass := classifyProbeError(probeErr)
	updated, recordErr := service.routes.RecordRouteHealth(ctx, HealthSample{
		RouteID: route.ID, Success: probeErr == nil && responseValid, Latency: latency,
		StatusCode: statusCode, ErrorClass: errorClass, SampledAt: service.now(),
	}, service.failureThreshold, service.cooldown)
	if recordErr != nil {
		return ProbeResult{}, recordErr
	}
	if probeErr == nil && responseValid && !route.Health.CooldownUntil.IsZero() {
		updated, recordErr = service.routes.ResetRouteHealth(ctx, route.ID, service.now())
		if recordErr != nil {
			return ProbeResult{}, recordErr
		}
	}
	probeResult := ProbeResult{
		Route: updated, Latency: latency, StatusCode: statusCode, ResponseValid: responseValid,
		CredentialEligible: route.Trust == TrustCredential, ErrorClass: errorClass,
	}
	if probeErr != nil {
		return probeResult, fmt.Errorf("proxy probe failed: %w", sanitizeNetworkError(probeErr, parsedEndpoint, service.probeTarget))
	}
	return probeResult, nil
}

func (service *Service) Candidates(ctx context.Context, direct RoutedClient) ([]Candidate, error) {
	routes, err := service.List(ctx)
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(routes)+1)
	if direct != nil {
		candidates = append(candidates, Candidate{Client: direct, Direct: true, Enabled: true})
	}
	for _, route := range routes {
		parsedEndpoint, parseErr := url.Parse(route.Endpoint)
		if parseErr != nil {
			return nil, fmt.Errorf("parse route %s endpoint: %w", route.Name, parseErr)
		}
		wrapped := &URLWrapper{
			RouteName: route.Name, Endpoint: parsedEndpoint,
			Trust: route.Trust, HTTP: service.http, Policy: service.endpointPolicy, Now: service.now,
		}
		if route.AuthorizationRef != "" {
			if service.secrets == nil {
				return nil, fmt.Errorf("load route %s authorization: secret store is not configured", route.Name)
			}
			authorization, getErr := service.secrets.Get(ctx, service.secretRef(route.AuthorizationRef))
			if getErr != nil {
				return nil, fmt.Errorf("load route %s authorization: %w", route.Name, getErr)
			}
			wrapped = wrapped.WithAuthorization(string(authorization))
		}
		candidate := Candidate{
			Client: wrapped, Trust: route.Trust, Priority: route.Priority, Enabled: route.Enabled,
			CooldownUntil: route.Health.CooldownUntil, ProbeRequired: route.Health.State == HealthRecovering,
			Classes: ClassesMap(route.Classes),
		}
		if candidate.ProbeRequired {
			identifier := route.ID
			candidate.Probe = func(probeContext context.Context) error {
				_, probeErr := service.Test(probeContext, identifier)
				return probeErr
			}
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (service *Service) observe(route RouteConfig) RouteConfig {
	route.ObservedAt = service.now()
	if !route.Enabled {
		route.Health.State = HealthDisabled
	} else if route.Health.CooldownUntil.After(route.ObservedAt) {
		route.Health.State = HealthCooldown
	} else if !route.Health.CooldownUntil.IsZero() && route.Health.LastSuccessAt.Before(route.Health.LastSampleAt) {
		route.Health.State = HealthRecovering
	}
	return route
}

func (service *Service) secretRef(name string) secrets.Ref {
	return secrets.Ref{Profile: service.profile, Kind: proxyAuthorizationKind, Name: name}
}

func randomRouteID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate route ID: %w", err)
	}
	return "route-" + hex.EncodeToString(buffer), nil
}

func classifyProbeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, ErrDestinationPolicy) {
		return "policy"
	}
	return "network"
}

func cloneURL(value *url.URL) *url.URL {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}
