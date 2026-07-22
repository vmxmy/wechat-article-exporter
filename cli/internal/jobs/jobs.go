package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

var ErrInvalidTransition = errors.New("invalid job state transition")

var (
	ErrLeaseUnavailable = errors.New("job execution lease unavailable")
	ErrStateChanged     = errors.New("job state changed concurrently")
)

type FailureClass string

const (
	FailureNetwork        FailureClass = "network"
	FailureThrottling     FailureClass = "throttling"
	FailureAuthentication FailureClass = "authentication"
	FailureDeleted        FailureClass = "deleted"
	FailureUnavailable    FailureClass = "unavailable"
	FailureParsing        FailureClass = "parsing"
	FailureStorage        FailureClass = "storage"
	FailureInterrupted    FailureClass = "interrupted"
)

type ClassifiedError struct {
	Class     FailureClass
	Retryable bool
	After     time.Duration
	Err       error
}

func (err *ClassifiedError) Error() string {
	if err.Err == nil {
		return string(err.Class)
	}
	return err.Err.Error()
}

func (err *ClassifiedError) Unwrap() error { return err.Err }

func Classify(err error) (FailureClass, bool) {
	if err == nil {
		return "", false
	}
	var classified *ClassifiedError
	if errors.As(err, &classified) {
		return classified.Class, classified.Retryable
	}
	return FailureStorage, false
}

type Spec struct {
	Kind           string           `json:"kind"`
	Profile        domain.ProfileID `json:"profile"`
	IdempotencyKey string           `json:"idempotencyKey,omitempty"`
	Payload        any              `json:"payload,omitempty"`
}

type Manager interface {
	Create(context.Context, Spec) (domain.Job, error)
	Get(context.Context, domain.JobID) (domain.Job, error)
	Query(context.Context, domain.JobQuery) (domain.Page[domain.Job], error)
	Cancel(context.Context, domain.JobID) (domain.Job, error)
}

func CanTransition(from, to domain.JobState) bool {
	allowed := map[domain.JobState]map[domain.JobState]struct{}{
		domain.JobQueued: {
			domain.JobRunning: {}, domain.JobCancelled: {}, domain.JobPaused: {}, domain.JobBlockedAuth: {},
		},
		domain.JobRunning: {
			domain.JobCompleted: {}, domain.JobPartial: {}, domain.JobFailed: {}, domain.JobCancelled: {},
			domain.JobPaused: {}, domain.JobBlockedAuth: {},
		},
		domain.JobPaused: {
			domain.JobQueued: {}, domain.JobRunning: {}, domain.JobCancelled: {},
		},
		domain.JobBlockedAuth: {
			domain.JobQueued: {}, domain.JobRunning: {}, domain.JobCancelled: {},
		},
		domain.JobFailed:    {domain.JobQueued: {}, domain.JobRunning: {}},
		domain.JobPartial:   {domain.JobQueued: {}, domain.JobRunning: {}},
		domain.JobCancelled: {domain.JobQueued: {}, domain.JobRunning: {}},
	}
	_, ok := allowed[from][to]
	return ok
}

func ValidateTransition(from, to domain.JobState) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%s -> %s: %w", from, to, ErrInvalidTransition)
	}
	return nil
}

type Backoff struct {
	Base   time.Duration
	Max    time.Duration
	Jitter func(time.Duration) time.Duration
}

func (backoff Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := backoff.Base
	if base <= 0 {
		base = time.Second
	}
	maximum := backoff.Max
	if maximum <= 0 {
		maximum = time.Minute
	}
	delay := base
	for index := 1; index < attempt && delay < maximum; index++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	if backoff.Jitter != nil {
		delay += backoff.Jitter(delay)
	}
	if delay < 0 {
		return 0
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
