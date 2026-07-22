package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

type Scheduler struct {
	limits     Limits
	mu         sync.Mutex
	global     int
	operations map[string]int
	hosts      map[string]int
	sensitive  int
}

func NewScheduler(limits Limits) *Scheduler {
	if limits.Global <= 0 {
		limits.Global = 4
	}
	if limits.PerHost <= 0 {
		limits.PerHost = limits.Global
	}
	if limits.Sensitive <= 0 {
		limits.Sensitive = 1
	}
	return &Scheduler{limits: limits, operations: make(map[string]int), hosts: make(map[string]int)}
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
			if !scheduler.acquire(item.work) {
				continue
			}
			queues[operation] = queue[1:]
			remaining--
			started = true
			available--
			wait.Add(1)
			go func() {
				defer wait.Done()
				err := item.work.Run(ctx)
				scheduler.release(item.work)
				resultsMu.Lock()
				results[item.index].Err = err
				resultsMu.Unlock()
				select {
				case wakeup <- struct{}{}:
				default:
				}
			}()
		}
		if remaining > 0 && !started {
			select {
			case <-ctx.Done():
			case <-wakeup:
			}
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

func (scheduler *Scheduler) acquire(work Work) bool {
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

func (scheduler *Scheduler) release(work Work) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.global--
	scheduler.operations[work.Operation]--
	scheduler.hosts[work.Host]--
	if work.Sensitive {
		scheduler.sensitive--
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
