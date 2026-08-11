package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	PauseOwned(context.Context, domain.JobID, string) (domain.Job, error)
	ClaimItem(context.Context, domain.JobID, string, string) (Item, error)
	SaveCheckpoint(context.Context, domain.JobID, string, string, any) error
	TransitionItem(context.Context, domain.JobID, string, string, domain.JobState, domain.JobState, any, FailureClass, string) (Item, error)
	BeginAttempt(context.Context, Item, string, string, string) (Attempt, error)
	FinishAttempt(context.Context, Attempt, string) error
	BlockAuthentication(context.Context, domain.JobID, string, string, string) (domain.Job, error)
	FinalizeJob(context.Context, domain.JobID, string) (domain.Job, error)
}

type JobLogger interface {
	AppendLog(context.Context, domain.JobID, string, string, string, any) error
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
	LogTimeout    time.Duration
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
	if options.LogTimeout <= 0 {
		options.LogTimeout = time.Second
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
	executionOwner := engine.executionOwner()
	job, err := engine.store.StartJob(ctx, id, executionOwner, engine.options.LeaseDuration)
	if err != nil {
		return domain.Job{}, err
	}
	engine.log(context.Background(), id, "", "info", "job worker started", map[string]any{
		"owner": executionOwner, "ownerPrefix": engine.options.Owner, "leaseDuration": engine.options.LeaseDuration.String(),
	})
	items, err := engine.store.ListItems(ctx, id)
	if err != nil {
		return domain.Job{}, fmt.Errorf("list job items: %w", err)
	}

	runContext, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	monitorDone := make(chan struct{})
	go engine.monitor(runContext, id, executionOwner, cancelRun, monitorDone)

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
				err := engine.runItem(workContext, id, item, executionOwner, execute, cancelRun)
				if errors.Is(err, ErrStateChanged) {
					cancelRun()
				}
				return err
			},
		})
	}

	results := engine.options.Scheduler.RunResults(runContext, work)
	cancelRun()
	<-monitorDone

	if ctx.Err() != nil {
		paused, pauseErr := engine.pauseIfOwner(context.Background(), id, executionOwner)
		if pauseErr != nil && !errors.Is(pauseErr, ErrStateChanged) {
			return domain.Job{}, fmt.Errorf("pause interrupted job: %w", pauseErr)
		}
		if pauseErr == nil && paused.State != "" && paused.State != domain.JobRunning {
			return paused, nil
		}
	}

	current, err := engine.store.Get(context.Background(), id)
	if err != nil {
		return domain.Job{}, err
	}
	if current.State != domain.JobRunning {
		engine.log(context.Background(), id, "", "info", "job worker stopped after external state change", map[string]any{"state": current.State})
		return current, nil
	}
	for _, result := range results {
		if errors.Is(result.Err, ErrStateChanged) {
			return current, result.Err
		}
	}
	for _, result := range results {
		if result.Err != nil && errors.Is(result.Err, context.Canceled) {
			if _, pauseErr := engine.pauseIfOwner(context.Background(), id, executionOwner); pauseErr != nil && !errors.Is(pauseErr, ErrStateChanged) {
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
	finalized, err := engine.store.FinalizeJob(context.Background(), id, executionOwner)
	if err == nil {
		engine.log(context.Background(), id, "", "info", "job finalized", map[string]any{"state": finalized.State})
	}
	return finalized, err
}

func (engine *Engine) pauseIfOwner(ctx context.Context, id domain.JobID, owner string) (domain.Job, error) {
	return engine.store.PauseOwned(ctx, id, owner)
}

func (engine *Engine) runItem(
	ctx context.Context,
	jobID domain.JobID,
	item Item,
	executionOwner string,
	execute ExecuteFunc,
	cancelRun context.CancelFunc,
) error {
	claimed, err := engine.store.ClaimItem(ctx, jobID, item.ID, executionOwner)
	if err != nil {
		return err
	}
	item = claimed
	checkpoint := func(value any) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := engine.store.SaveCheckpoint(ctx, jobID, item.ID, executionOwner, value); err != nil {
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
		attempt, err := engine.store.BeginAttempt(ctx, item, executionOwner, routeID, requestID)
		if err != nil {
			return fmt.Errorf("begin attempt for %s: %w", item.Key, err)
		}
		item.AttemptCount = attempt.Number
		startedAt := time.Now()
		engine.log(context.Background(), jobID, item.ID, "info", "job item attempt started", map[string]any{
			"attempt": attempt.Number, "routeId": routeID, "requestId": requestID,
		})
		runErr := execute(ctx, item, checkpoint)
		class, retryable := Classify(runErr)
		attempt.FailureClass = class
		if runErr != nil {
			attempt.ErrorMessage = safety.RedactError(runErr).Error()
		}
		if finishErr := engine.store.FinishAttempt(context.Background(), attempt, executionOwner); finishErr != nil {
			return fmt.Errorf("finish attempt for %s: %w", item.Key, finishErr)
		}
		if errors.Is(runErr, ErrStateChanged) {
			cancelRun()
			return runErr
		}

		if runErr == nil {
			_, err = engine.store.TransitionItem(context.Background(), jobID, item.ID, executionOwner,
				domain.JobRunning, domain.JobCompleted, nil, "", "")
			if err == nil {
				engine.log(context.Background(), jobID, item.ID, "info", "job item attempt completed", map[string]any{
					"attempt": attempt.Number, "duration": time.Since(startedAt).String(),
				})
			}
			return err
		}
		if partial, ok := AsPartial(runErr); ok {
			if partial.Class != "" {
				class = partial.Class
			}
			_, err = engine.store.TransitionItem(context.Background(), jobID, item.ID, executionOwner,
				domain.JobRunning, domain.JobPartial, nil, class, safety.RedactError(runErr).Error())
			if err == nil {
				engine.log(context.Background(), jobID, item.ID, "warning", "job item completed partially", map[string]any{
					"attempt": attempt.Number, "duration": time.Since(startedAt).String(), "failureClass": class,
				})
			}
			return err
		}
		if class == FailureAuthentication {
			_, blockErr := engine.store.BlockAuthentication(context.Background(), jobID, item.ID, executionOwner,
				safety.RedactError(runErr).Error())
			if blockErr == nil {
				engine.log(context.Background(), jobID, item.ID, "error", "job blocked on authentication", map[string]any{
					"attempt": attempt.Number, "duration": time.Since(startedAt).String(), "failureClass": class,
				})
			}
			cancelRun()
			return blockErr
		}
		// Rate limiting is a session-wide condition: retrying this item or moving
		// on to the next one hits the same upstream window, so the whole job
		// pauses immediately and stays resumable once the window passes.
		if class == FailureThrottling {
			_, transitionErr := engine.store.TransitionItem(context.Background(), jobID, item.ID, executionOwner,
				domain.JobRunning, domain.JobPaused, nil, class, safety.RedactError(runErr).Error())
			if transitionErr != nil && !errors.Is(transitionErr, ErrStateChanged) {
				return transitionErr
			}
			if transitionErr == nil {
				engine.log(context.Background(), jobID, item.ID, "warning", "job paused after upstream rate limiting", map[string]any{
					"attempt": attempt.Number, "duration": time.Since(startedAt).String(), "failureClass": class,
				})
			}
			if _, pauseErr := engine.pauseIfOwner(context.Background(), jobID, executionOwner); pauseErr != nil && !errors.Is(pauseErr, ErrStateChanged) {
				return pauseErr
			}
			cancelRun()
			return runErr
		}
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			current, getErr := engine.store.Get(context.Background(), jobID)
			if getErr == nil && current.State != domain.JobRunning {
				return currentControlError(current.State)
			}
			_, transitionErr := engine.store.TransitionItem(context.Background(), jobID, item.ID, executionOwner,
				domain.JobRunning, domain.JobPaused, nil, FailureInterrupted, safety.RedactError(runErr).Error())
			if transitionErr != nil && !errors.Is(transitionErr, ErrStateChanged) {
				return transitionErr
			}
			if transitionErr == nil {
				engine.log(context.Background(), jobID, item.ID, "warning", "job item interrupted", map[string]any{
					"attempt": attempt.Number, "duration": time.Since(startedAt).String(), "failureClass": FailureInterrupted,
				})
			}
			return runErr
		}
		if retryable && runAttempt < engine.options.MaxAttempts {
			delay := engine.options.Backoff.Delay(runAttempt)
			engine.log(context.Background(), jobID, item.ID, "warning", "job item retry scheduled", map[string]any{
				"attempt": attempt.Number, "duration": time.Since(startedAt).String(), "failureClass": class,
				"retryDelay": delay.String(),
			})
			if waitErr := engine.options.Sleep(ctx, delay); waitErr != nil {
				return waitErr
			}
			continue
		}
		_, err = engine.store.TransitionItem(context.Background(), jobID, item.ID, executionOwner,
			domain.JobRunning, domain.JobFailed, nil, class, safety.RedactError(runErr).Error())
		if err == nil {
			engine.log(context.Background(), jobID, item.ID, "error", "job item attempt failed", map[string]any{
				"attempt": attempt.Number, "duration": time.Since(startedAt).String(), "failureClass": class,
			})
		}
		return err
	}
	return nil
}

func (engine *Engine) log(ctx context.Context, jobID domain.JobID, itemID, level, message string, fields any) {
	logger, ok := engine.store.(JobLogger)
	if !ok || logger == nil {
		return
	}
	logCtx, cancel := context.WithTimeout(ctx, engine.options.LogTimeout)
	defer cancel()
	_ = logger.AppendLog(logCtx, jobID, itemID, level, message, fields)
}

func (engine *Engine) executionOwner() string {
	return engine.options.Owner + "/" + uuid.NewString()
}

func (engine *Engine) monitor(
	ctx context.Context,
	id domain.JobID,
	executionOwner string,
	cancel context.CancelFunc,
	done chan<- struct{},
) {
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
			renewed, err := engine.store.RenewLease(context.Background(), id, executionOwner, engine.options.LeaseDuration)
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
