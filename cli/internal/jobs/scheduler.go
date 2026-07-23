package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Work struct {
	ID        string
	Operation string
	Host      string
	Sensitive bool
	Run       func(context.Context) error
}

type Result struct {
	ID  string
	Err error
}

type Limits struct {
	Global       int
	PerOperation map[string]int
	PerHost      int
	Sensitive    int
}

type PermitRequest struct {
	Owner         string
	Operation     string
	Host          string
	Sensitive     bool
	Limits        Limits
	LeaseDuration time.Duration
	Now           time.Time
}

type Permit struct {
	ID    string
	Owner string
}

type PermitStore interface {
	TryAcquire(context.Context, PermitRequest) (Permit, bool, error)
	Renew(context.Context, Permit, time.Time, time.Duration) (bool, error)
	Release(context.Context, Permit) error
}

type SchedulerOptions struct {
	PermitStore            PermitStore
	Owner                  string
	LeaseDuration          time.Duration
	RenewalInterval        time.Duration
	PollInterval           time.Duration
	PermitOperationTimeout time.Duration
	Now                    func() time.Time
}

type Scheduler struct {
	limits                 Limits
	permitStore            PermitStore
	owner                  string
	leaseDuration          time.Duration
	renewalInterval        time.Duration
	pollInterval           time.Duration
	permitOperationTimeout time.Duration
	now                    func() time.Time
	configurationError     error
	mu                     sync.Mutex
	global                 int
	operations             map[string]int
	hosts                  map[string]int
	sensitive              int
}

const minimumSchedulerLeaseDuration = 10 * time.Millisecond

func schedulerConfigurationError(options SchedulerOptions) error {
	if options.PermitStore == nil {
		return nil
	}
	if options.LeaseDuration < minimumSchedulerLeaseDuration {
		return fmt.Errorf("scheduler permit lease duration %s is shorter than minimum %s",
			options.LeaseDuration, minimumSchedulerLeaseDuration)
	}
	if options.RenewalInterval <= 0 || options.RenewalInterval >= options.LeaseDuration {
		return fmt.Errorf("scheduler permit renewal interval %s must be positive and shorter than lease duration %s",
			options.RenewalInterval, options.LeaseDuration)
	}
	if options.PermitOperationTimeout <= 0 || options.PermitOperationTimeout >= options.LeaseDuration-options.RenewalInterval {
		return fmt.Errorf("scheduler permit operation timeout %s must be positive and shorter than renewal safety margin %s",
			options.PermitOperationTimeout, options.LeaseDuration-options.RenewalInterval)
	}
	return nil
}

func NewScheduler(limits Limits, configured ...SchedulerOptions) *Scheduler {
	if limits.Global <= 0 {
		limits.Global = 4
	}
	if limits.PerHost <= 0 {
		limits.PerHost = limits.Global
	}
	if limits.Sensitive <= 0 {
		limits.Sensitive = 1
	}
	options := SchedulerOptions{}
	if len(configured) > 0 {
		options = configured[0]
	}
	if options.Owner == "" {
		options.Owner = "scheduler-" + uuid.NewString()
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 30 * time.Second
	}
	if options.RenewalInterval <= 0 {
		options.RenewalInterval = options.LeaseDuration / 3
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 100 * time.Millisecond
	}
	if options.PermitOperationTimeout <= 0 {
		options.PermitOperationTimeout = options.LeaseDuration / 3
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	configurationError := schedulerConfigurationError(options)
	return &Scheduler{
		limits: limits, permitStore: options.PermitStore, owner: options.Owner,
		leaseDuration: options.LeaseDuration, renewalInterval: options.RenewalInterval, pollInterval: options.PollInterval,
		permitOperationTimeout: options.PermitOperationTimeout, now: options.Now, configurationError: configurationError,
		operations: make(map[string]int), hosts: make(map[string]int),
	}
}

func (scheduler *Scheduler) Run(ctx context.Context, work []Work) []error {
	results := scheduler.RunResults(ctx, work)
	errorsList := make([]error, len(results))
	for index, result := range results {
		errorsList[index] = result.Err
	}
	return errorsList
}

func (scheduler *Scheduler) RunResults(ctx context.Context, work []Work) []Result {
	if len(work) == 0 {
		return nil
	}
	type indexedWork struct {
		index int
		work  Work
	}
	queues := make(map[string][]indexedWork)
	operations := make([]string, 0)
	results := make([]Result, len(work))
	remaining := 0
	var resultsMu sync.Mutex
	for index, item := range work {
		results[index].ID = item.ID
		if item.Run == nil {
			results[index].Err = errors.New("scheduled work has no run function")
			continue
		}
		if _, exists := queues[item.Operation]; !exists {
			operations = append(operations, item.Operation)
		}
		queues[item.Operation] = append(queues[item.Operation], indexedWork{index: index, work: item})
		remaining++
	}
	if scheduler.configurationError != nil {
		for index := range results {
			if results[index].Err == nil {
				results[index].Err = scheduler.configurationError
			}
		}
		return results
	}
	wakeup := make(chan struct{}, 1)
	var wait sync.WaitGroup
	operationIndex := 0
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			for _, operation := range operations {
				for _, item := range queues[operation] {
					resultsMu.Lock()
					results[item.index].Err = err
					resultsMu.Unlock()
					remaining--
				}
				queues[operation] = nil
			}
			break
		}
		started := false
		activeBefore := scheduler.active()
		available := scheduler.limits.Global - activeBefore
		if available < 1 {
			available = 1
		}
		for checked := 0; checked < len(operations) && available > 0; checked++ {
			operation := operations[operationIndex%len(operations)]
			operationIndex++
			queue := queues[operation]
			if len(queue) == 0 {
				continue
			}
			item := queue[0]
			permit, acquired, acquireErr := scheduler.acquire(ctx, item.work)
			if acquireErr != nil {
				if errors.Is(acquireErr, context.DeadlineExceeded) && ctx.Err() == nil {
					continue
				}
				if isTransientPermitError(acquireErr) {
					continue
				}
				queues[operation] = queue[1:]
				remaining--
				resultsMu.Lock()
				results[item.index].Err = fmt.Errorf("acquire scheduler permit: %w", acquireErr)
				resultsMu.Unlock()
				started = true
				continue
			}
			if !acquired {
				continue
			}
			queues[operation] = queue[1:]
			remaining--
			started = true
			available--
			wait.Add(1)
			go func(permit Permit) {
				defer wait.Done()
				err := scheduler.run(ctx, item.work, permit)
				resultsMu.Lock()
				results[item.index].Err = err
				resultsMu.Unlock()
				select {
				case wakeup <- struct{}{}:
				default:
				}
			}(permit)
		}
		if remaining > 0 && !started {
			scheduler.waitForWakeup(ctx, wakeup)
		}
	}
	wait.Wait()
	return results
}

func (scheduler *Scheduler) active() int {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.global
}

func (scheduler *Scheduler) acquire(ctx context.Context, work Work) (Permit, bool, error) {
	if !scheduler.acquireLocal(work) {
		return Permit{}, false, nil
	}
	if scheduler.permitStore == nil {
		return Permit{}, true, nil
	}
	acquireCtx, cancelAcquire := context.WithTimeout(ctx, scheduler.permitOperationTimeout)
	permit, acquired, err := scheduler.permitStore.TryAcquire(acquireCtx, PermitRequest{
		Owner: scheduler.owner, Operation: work.Operation, Host: work.Host, Sensitive: work.Sensitive,
		Limits: scheduler.limits, LeaseDuration: scheduler.leaseDuration, Now: scheduler.now(),
	})
	cancelAcquire()
	if err != nil || !acquired {
		scheduler.releaseLocal(work)
	}
	return permit, acquired, err
}

func (scheduler *Scheduler) acquireLocal(work Work) bool {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.global >= scheduler.limits.Global || scheduler.hosts[work.Host] >= scheduler.limits.PerHost {
		return false
	}
	operationLimit := scheduler.limits.PerOperation[work.Operation]
	if operationLimit <= 0 {
		operationLimit = scheduler.limits.Global
	}
	if scheduler.operations[work.Operation] >= operationLimit {
		return false
	}
	if work.Sensitive && scheduler.sensitive >= scheduler.limits.Sensitive {
		return false
	}
	scheduler.global++
	scheduler.operations[work.Operation]++
	scheduler.hosts[work.Host]++
	if work.Sensitive {
		scheduler.sensitive++
	}
	return true
}

func (scheduler *Scheduler) releaseLocal(work Work) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.global--
	scheduler.operations[work.Operation]--
	scheduler.hosts[work.Host]--
	if work.Sensitive {
		scheduler.sensitive--
	}
}

func (scheduler *Scheduler) run(ctx context.Context, work Work, permit Permit) (runErr error) {
	defer scheduler.releaseLocal(work)
	if scheduler.permitStore == nil {
		return work.Run(ctx)
	}
	runContext, cancelRun := context.WithCancel(ctx)
	stopRenewal := make(chan struct{})
	renewalDone := make(chan error, 1)
	go scheduler.renewPermit(cancelRun, permit, stopRenewal, renewalDone)
	defer func() {
		close(stopRenewal)
		renewalErr := <-renewalDone
		cancelRun()
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), scheduler.permitOperationTimeout)
		releaseErr := scheduler.permitStore.Release(releaseCtx, permit)
		cancelRelease()
		if renewalErr != nil {
			runErr = errors.Join(runErr, renewalErr)
		}
		if releaseErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("release scheduler permit: %w", releaseErr))
		}
	}()
	return work.Run(runContext)
}

func (scheduler *Scheduler) renewPermit(
	cancelRun context.CancelFunc,
	permit Permit,
	stop <-chan struct{},
	done chan<- error,
) {
	ticker := time.NewTicker(scheduler.renewalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			done <- nil
			return
		case <-ticker.C:
			renewCtx, cancelRenew := context.WithTimeout(context.Background(), scheduler.permitOperationTimeout)
			renewed, err := scheduler.permitStore.Renew(renewCtx, permit, scheduler.now(), scheduler.leaseDuration)
			cancelRenew()
			if err != nil {
				cancelRun()
				done <- fmt.Errorf("renew scheduler permit: %w", err)
				return
			}
			if !renewed {
				cancelRun()
				done <- errors.New("scheduler permit lease was lost")
				return
			}
		}
	}
}

func isTransientPermitError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "database is busy") ||
		strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "sqlite_locked")
}

func (scheduler *Scheduler) waitForWakeup(ctx context.Context, wakeup <-chan struct{}) {
	if scheduler.permitStore == nil {
		select {
		case <-ctx.Done():
		case <-wakeup:
		}
		return
	}
	timer := time.NewTimer(scheduler.pollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-wakeup:
	case <-timer.C:
	}
}

func SummarizeResults(results []error) error {
	var failed int
	for _, err := range results {
		if err != nil {
			failed++
		}
	}
	if failed == 0 {
		return nil
	}
	if failed == len(results) {
		return fmt.Errorf("all %d scheduled items failed: %w", failed, errors.Join(results...))
	}
	return fmt.Errorf("%d of %d scheduled items failed: %w", failed, len(results), errors.Join(results...))
}
