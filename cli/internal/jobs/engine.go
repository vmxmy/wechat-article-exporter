package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

type Item struct {
	ID           string
	JobID        domain.JobID
	Key          string
	State        domain.JobState
	AttemptCount int
	Checkpoint   json.RawMessage
	ErrorClass   FailureClass
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Attempt struct {
	JobID        domain.JobID
	ItemID       string
	Number       int
	RouteID      string
	RequestID    string
	FailureClass FailureClass
	ErrorMessage string
	StartedAt    time.Time
	CompletedAt  time.Time
}

type EngineStore interface {
	Get(context.Context, domain.JobID) (domain.Job, error)
	ListItems(context.Context, domain.JobID) ([]Item, error)
	StartJob(context.Context, domain.JobID, string, time.Duration) (domain.Job, error)
	RenewLease(context.Context, domain.JobID, string, time.Duration) (bool, error)
	Pause(context.Context, domain.JobID) (domain.Job, error)
	ClaimItem(context.Context, domain.JobID, string, string) (Item, error)
	SaveCheckpoint(context.Context, domain.JobID, string, string, any) error
	TransitionItem(context.Context, domain.JobID, string, string, domain.JobState, domain.JobState, any, FailureClass, string) (Item, error)
	BeginAttempt(context.Context, Item, string, string) (Attempt, error)
	FinishAttempt(context.Context, Attempt) error
	BlockAuthentication(context.Context, domain.JobID, string, string, string) (domain.Job, error)
	FinalizeJob(context.Context, domain.JobID, string) (domain.Job, error)
}

type WorkMetadata struct {
	Operation string
	Host      string
	Sensitive bool
}

type CheckpointFunc func(any) error

type ExecuteFunc func(context.Context, Item, CheckpointFunc) error

type SleepFunc func(context.Context, time.Duration) error

type EngineOptions struct {
	Owner         string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	MaxAttempts   int
	Backoff       Backoff
	Scheduler     *Scheduler
	Metadata      func(Item) WorkMetadata
	RouteID       func(Item, int) string
	RequestID     func(Item, int) string
	Sleep         SleepFunc
}

type Engine struct {
	store   EngineStore
	options EngineOptions
}

func NewEngine(store EngineStore, options EngineOptions) (*Engine, error) {
	if store == nil {
		return nil, errors.New("job engine store is required")
	}
	if options.Owner == "" {
		return nil, errors.New("job engine owner is required")
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 30 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = options.LeaseDuration / 3
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 3
	}
	if options.Scheduler == nil {
		options.Scheduler = NewScheduler(Limits{})
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	return &Engine{store: store, options: options}, nil
}

func (engine *Engine) Run(ctx context.Context, id domain.JobID, execute ExecuteFunc) (domain.Job, error) {
	if execute == nil {
		return domain.Job{}, errors.New("job execute function is required")
	}
	job, err := engine.store.StartJob(ctx, id, engine.options.Owner, engine.options.LeaseDuration)
	if err != nil {
		return domain.Job{}, err
	}
	items, err := engine.store.ListItems(ctx, id)
	if err != nil {
		return domain.Job{}, fmt.Errorf("list job items: %w", err)
	}

	runContext, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	monitorDone := make(chan struct{})
	go engine.monitor(runContext, id, cancelRun, monitorDone)

	work := make([]Work, 0, len(items))
	for _, item := range items {
		if item.State != domain.JobQueued {
			continue
		}
		item := item
		metadata := WorkMetadata{Operation: job.Kind}
		if engine.options.Metadata != nil {
			metadata = engine.options.Metadata(item)
			if metadata.Operation == "" {
				metadata.Operation = job.Kind
			}
		}
		work = append(work, Work{
			ID:        item.ID,
			Operation: metadata.Operation,
			Host:      metadata.Host,
			Sensitive: metadata.Sensitive,
			Run: func(workContext context.Context) error {
				return engine.runItem(workContext, id, item, execute, cancelRun)
			},
		})
	}

	results := engine.options.Scheduler.RunResults(runContext, work)
	cancelRun()
	<-monitorDone

	if ctx.Err() != nil {
		current, getErr := engine.store.Get(context.Background(), id)
		if getErr == nil && current.State == domain.JobRunning {
			if _, pauseErr := engine.store.Pause(context.Background(), id); pauseErr != nil && !errors.Is(pauseErr, ErrStateChanged) {
				return domain.Job{}, fmt.Errorf("pause interrupted job: %w", pauseErr)
			}
		}
	}

	current, err := engine.store.Get(context.Background(), id)
	if err != nil {
		return domain.Job{}, err
	}
	if current.State != domain.JobRunning {
		return current, nil
	}
	for _, result := range results {
		if result.Err != nil && errors.Is(result.Err, context.Canceled) {
			if _, pauseErr := engine.store.Pause(context.Background(), id); pauseErr != nil && !errors.Is(pauseErr, ErrStateChanged) {
				return domain.Job{}, fmt.Errorf("pause interrupted job: %w", pauseErr)
			}
			return engine.store.Get(context.Background(), id)
		}
	}
	for _, result := range results {
		if result.Err != nil && !errors.Is(result.Err, context.Canceled) && !errors.Is(result.Err, ErrStateChanged) {
			// Item-level failures are persisted by runItem. Scheduler errors only
			// matter here when work could not enter the persistent state machine.
			continue
		}
	}
	return engine.store.FinalizeJob(context.Background(), id, engine.options.Owner)
}

func (engine *Engine) runItem(
	ctx context.Context,
	jobID domain.JobID,
	item Item,
	execute ExecuteFunc,
	cancelRun context.CancelFunc,
) error {
	claimed, err := engine.store.ClaimItem(ctx, jobID, item.ID, engine.options.Owner)
	if err != nil {
		return err
	}
	item = claimed
	checkpoint := func(value any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := engine.store.SaveCheckpoint(ctx, jobID, item.ID, engine.options.Owner, value); err != nil {
			return fmt.Errorf("save checkpoint for %s: %w", item.Key, err)
		}
		encoded, err := encodeCheckpoint(value)
		if err == nil {
			item.Checkpoint = encoded
		}
		return nil
	}

	for runAttempt := 1; runAttempt <= engine.options.MaxAttempts; runAttempt++ {
		routeID := ""
		requestID := ""
		if engine.options.RouteID != nil {
			routeID = engine.options.RouteID(item, runAttempt)
		}
		if engine.options.RequestID != nil {
			requestID = engine.options.RequestID(item, runAttempt)
		}
		attempt, err := engine.store.BeginAttempt(ctx, item, routeID, requestID)
		if err != nil {
			return fmt.Errorf("begin attempt for %s: %w", item.Key, err)
		}
		item.AttemptCount = attempt.Number
		runErr := execute(ctx, item, checkpoint)
		class, retryable := Classify(runErr)
		attempt.FailureClass = class
		if runErr != nil {
			attempt.ErrorMessage = safety.RedactError(runErr).Error()
		}
		if finishErr := engine.store.FinishAttempt(context.Background(), attempt); finishErr != nil {
			return fmt.Errorf("finish attempt for %s: %w", item.Key, finishErr)
		}

		if runErr == nil {
			_, err = engine.store.TransitionItem(context.Background(), jobID, item.ID, engine.options.Owner,
				domain.JobRunning, domain.JobCompleted, nil, "", "")
			return err
		}
		if class == FailureAuthentication {
			_, blockErr := engine.store.BlockAuthentication(context.Background(), jobID, item.ID, engine.options.Owner,
				safety.RedactError(runErr).Error())
			cancelRun()
			return blockErr
		}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			current, getErr := engine.store.Get(context.Background(), jobID)
			if getErr == nil && current.State != domain.JobRunning {
				return currentControlError(current.State)
			}
			_, transitionErr := engine.store.TransitionItem(context.Background(), jobID, item.ID, engine.options.Owner,
				domain.JobRunning, domain.JobPaused, nil, FailureInterrupted, safety.RedactError(runErr).Error())
			if transitionErr != nil && !errors.Is(transitionErr, ErrStateChanged) {
				return transitionErr
			}
			return runErr
		}
		if retryable && runAttempt < engine.options.MaxAttempts {
			if waitErr := engine.options.Sleep(ctx, engine.options.Backoff.Delay(runAttempt)); waitErr != nil {
				return waitErr
			}
			continue
		}
		_, err = engine.store.TransitionItem(context.Background(), jobID, item.ID, engine.options.Owner,
			domain.JobRunning, domain.JobFailed, nil, class, safety.RedactError(runErr).Error())
		return err
	}
	return nil
}

func (engine *Engine) monitor(ctx context.Context, id domain.JobID, cancel context.CancelFunc, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(engine.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := engine.store.Get(context.Background(), id)
			if err != nil || job.State != domain.JobRunning {
				cancel()
				return
			}
			renewed, err := engine.store.RenewLease(context.Background(), id, engine.options.Owner, engine.options.LeaseDuration)
			if err != nil || !renewed {
				cancel()
				return
			}
		}
	}
}

func encodeCheckpoint(value any) (json.RawMessage, error) {
	if raw, ok := value.(json.RawMessage); ok && !json.Valid(raw) {
		return nil, errors.New("checkpoint contains invalid JSON")
	}
	encoded, err := json.Marshal(safety.Redact(value, ""))
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func currentControlError(state domain.JobState) error {
	switch state {
	case domain.JobPaused, domain.JobCancelled, domain.JobBlockedAuth:
		return ErrStateChanged
	default:
		return nil
	}
}
