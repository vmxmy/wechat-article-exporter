package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"time"
)

type RoutedClient interface {
	Name() string
	Do(context.Context, Request) (Result, error)
}

type Candidate struct {
	Client        RoutedClient
	Trust         TrustLevel
	Direct        bool
	Priority      int
	Enabled       bool
	CooldownUntil time.Time
	ProbeRequired bool
	Probe         func(context.Context) error
	Classes       map[RequestClass]struct{}
}

type Router struct {
	Routes    []Candidate
	Now       func() time.Time
	Retryable func(error) bool
}

func (router Router) Do(ctx context.Context, request Request) (Result, error) {
	now := router.Now
	if now == nil {
		now = time.Now
	}
	candidates := append([]Candidate(nil), router.Routes...)
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].Direct != candidates[right].Direct {
			return candidates[left].Direct
		}
		if candidates[left].ProbeRequired != candidates[right].ProbeRequired {
			return !candidates[left].ProbeRequired
		}
		return candidates[left].Priority < candidates[right].Priority
	})
	var failures []error
	for _, candidate := range candidates {
		if candidate.Client == nil || !candidate.Enabled || candidate.CooldownUntil.After(now()) {
			continue
		}
		if len(candidate.Classes) > 0 {
			if _, ok := candidate.Classes[request.Class]; !ok {
				continue
			}
		}
		if err := ValidateRoute(request.Class, candidate.Direct, candidate.Trust); err != nil {
			failures = append(failures, err)
			continue
		}
		if candidate.ProbeRequired {
			if candidate.Probe == nil {
				failures = append(failures, fmt.Errorf("route %s requires a recovery probe", candidate.Client.Name()))
				continue
			}
			if err := candidate.Probe(ctx); err != nil {
				failures = append(failures, fmt.Errorf("route %s recovery probe: %w", candidate.Client.Name(), err))
				continue
			}
		}
		result, err := candidate.Client.Do(ctx, request)
		if err == nil {
			return result, nil
		}
		failures = append(failures, fmt.Errorf("route %s: %w", candidate.Client.Name(), err))
		retryable := router.Retryable
		if retryable == nil || !retryable(err) {
			return Result{}, failures[len(failures)-1]
		}
		if request.Body != nil {
			if seeker, ok := request.Body.(io.Seeker); ok {
				if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr != nil {
					return Result{}, fmt.Errorf("rewind request body for route fallback: %w", seekErr)
				}
			} else {
				return Result{}, errors.New("request body is not replayable for route fallback")
			}
		}
	}
	if len(failures) == 0 {
		return Result{}, errors.New("no eligible network route")
	}
	return Result{}, errors.Join(failures...)
}

type StaticClient struct {
	RouteName string
	Call      func(context.Context, Request) (Result, error)
}

func (client StaticClient) Name() string { return client.RouteName }
func (client StaticClient) Do(ctx context.Context, request Request) (Result, error) {
	return client.Call(ctx, request)
}

func ParseEndpoint(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("route endpoint must be an absolute URL")
	}
	return parsed, nil
}
