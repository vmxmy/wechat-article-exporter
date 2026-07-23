package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestEngineLogsItemOutcomeAfterPersistentTransition(t *testing.T) {
	tests := []struct {
		name       string
		runErr     error
		transition domain.JobState
		message    string
	}{
		{name: "completed", transition: domain.JobCompleted, message: "job item attempt completed"},
		{name: "partial", runErr: &PartialError{Class: FailureStorage, Err: errors.New("partial")}, transition: domain.JobPartial, message: "job item completed partially"},
		{name: "authentication", runErr: &ClassifiedError{Class: FailureAuthentication, Err: errors.New("login required")}, transition: domain.JobBlockedAuth, message: "job blocked on authentication"},
		{name: "interrupted", runErr: context.Canceled, transition: domain.JobPaused, message: "job item interrupted"},
		{name: "failed", runErr: errors.New("failed"), transition: domain.JobFailed, message: "job item attempt failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newEngineEventStore()
			store.job.State = domain.JobRunning
			store.activeOwner = "worker/run"
			engine := newTestEngine(t, store)
			runErr := engine.runItem(context.Background(), store.job.ID, store.item, store.activeOwner,
				func(context.Context, Item, CheckpointFunc) error { return test.runErr }, func() {})
			if test.runErr == context.Canceled {
				if !errors.Is(runErr, context.Canceled) {
					t.Fatalf("runItem() error = %v", runErr)
				}
			} else if runErr != nil {
				t.Fatalf("runItem() error = %v", runErr)
			}
			assertEventBefore(t, store.eventsSnapshot(), "transition:"+string(test.transition), "log:"+test.message)
		})
	}
}

func TestEngineDoesNotLogOutcomeWhenTransitionFails(t *testing.T) {
	tests := []struct {
		name       string
		runErr     error
		transition domain.JobState
		message    string
	}{
		{name: "authentication", runErr: &ClassifiedError{Class: FailureAuthentication, Err: errors.New("login required")}, transition: domain.JobBlockedAuth, message: "job blocked on authentication"},
		{name: "interrupted", runErr: context.Canceled, transition: domain.JobPaused, message: "job item interrupted"},
		{name: "failed", runErr: errors.New("failed"), transition: domain.JobFailed, message: "job item attempt failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newEngineEventStore()
			store.job.State = domain.JobRunning
			store.activeOwner = "worker/run"
			store.transitionErrors[test.transition] = errors.New("transition failed")
			engine := newTestEngine(t, store)
			_ = engine.runItem(context.Background(), store.job.ID, store.item, store.activeOwner,
				func(context.Context, Item, CheckpointFunc) error { return test.runErr }, func() {})
			if eventIndex(store.eventsSnapshot(), "log:"+test.message) >= 0 {
				t.Fatalf("outcome was logged despite failed transition: %#v", store.eventsSnapshot())
			}
		})
	}
}

func TestEngineLifecycleLogsFollowStartAndFinalizeTransitions(t *testing.T) {
	store := newEngineEventStore()
	store.items = nil
	engine := newTestEngine(t, store)
	result, err := engine.Run(context.Background(), store.job.ID, func(context.Context, Item, CheckpointFunc) error {
		return nil
	})
	if err != nil || result.State != domain.JobCompleted {
		t.Fatalf("Run() = %#v, %v", result, err)
	}
	events := store.eventsSnapshot()
	assertEventBefore(t, events, "transition:running", "log:job worker started")
	assertEventBefore(t, events, "transition:completed", "log:job finalized")
}

func TestEngineLoggingUsesBoundedContext(t *testing.T) {
	store := newEngineEventStore()
	store.blockLogs = true
	engine, err := NewEngine(store, EngineOptions{Owner: "worker", LogTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	engine.log(context.Background(), store.job.ID, store.item.ID, "info", "bounded", nil)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("bounded log call took %s", elapsed)
	}
	if !store.logDeadline.Load() {
		t.Fatal("AppendLog context did not have a deadline")
	}
}

func TestEngineRunUsesUniqueExecutionOwnerWithConfiguredPrefix(t *testing.T) {
	store := newEngineEventStore()
	store.items = nil
	engine := newTestEngine(t, store)
	for range 2 {
		store.resetQueued()
		result, err := engine.Run(context.Background(), store.job.ID, func(context.Context, Item, CheckpointFunc) error {
			return nil
		})
		if err != nil || result.State != domain.JobCompleted {
			t.Fatalf("Run() = %#v, %v", result, err)
		}
	}
	owners := store.startedOwnersSnapshot()
	if len(owners) != 2 || owners[0] == owners[1] {
		t.Fatalf("execution owners = %#v", owners)
	}
	for _, owner := range owners {
		if !strings.HasPrefix(owner, "worker/") {
			t.Fatalf("execution owner %q does not preserve configured prefix", owner)
		}
	}
}

func TestEngineOldRunCannotWriteAfterSamePrefixLeaseTakeover(t *testing.T) {
	store := newEngineEventStore()
	checkpointReady := make(chan CheckpointFunc, 1)
	releaseOldRun := make(chan struct{})
	oldEngine := newTestEngine(t, store)
	oldDone := make(chan error, 1)
	go func() {
		_, err := oldEngine.Run(context.Background(), store.job.ID, func(_ context.Context, _ Item, checkpoint CheckpointFunc) error {
			checkpointReady <- checkpoint
			<-releaseOldRun
			return nil
		})
		oldDone <- err
	}()
	checkpoint := <-checkpointReady
	oldOwner := store.activeOwnerSnapshot()
	store.takeOverWithSamePrefix("worker")
	newOwner := store.activeOwnerSnapshot()
	if oldOwner == newOwner || !strings.HasPrefix(oldOwner, "worker/") || !strings.HasPrefix(newOwner, "worker/") {
		t.Fatalf("old owner=%q new owner=%q", oldOwner, newOwner)
	}
	if err := checkpoint(map[string]any{"offset": 1}); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("stale checkpoint error = %v", err)
	}
	close(releaseOldRun)
	if err := <-oldDone; !errors.Is(err, ErrStateChanged) {
		t.Fatalf("stale Run() error = %v", err)
	}
	if got := store.transitionOwnerSnapshot(); got != "" {
		t.Fatalf("stale execution reached item transition with owner %q", got)
	}
	if store.finishCalls.Load() != 0 {
		t.Fatalf("stale worker finished %d attempts", store.finishCalls.Load())
	}
	if store.finalizeCalls.Load() != 0 {
		t.Fatalf("stale worker finalized %d times", store.finalizeCalls.Load())
	}
	if store.pauseCalls.Load() != 0 {
		t.Fatalf("stale worker paused replacement execution %d times", store.pauseCalls.Load())
	}
	if state := store.jobStateSnapshot(); state != domain.JobRunning {
		t.Fatalf("replacement execution state = %s", state)
	}
}

func TestEngineCancelsSiblingWhenOwnershipChanges(t *testing.T) {
	store := newEngineEventStore()
	store.items = []Item{
		{ID: "item-stale", JobID: store.job.ID, Key: "stale", State: domain.JobQueued},
		{ID: "item-sibling", JobID: store.job.ID, Key: "sibling", State: domain.JobQueued},
	}
	engine, err := NewEngine(store, EngineOptions{
		Owner: "worker", MaxAttempts: 1, PollInterval: time.Hour, LogTimeout: 20 * time.Millisecond,
		Scheduler: NewScheduler(Limits{Global: 2, PerHost: 2, Sensitive: 2}),
	})
	if err != nil {
		t.Fatal(err)
	}
	siblingStarted := make(chan struct{})
	siblingCancelled := make(chan struct{})
	_, runErr := engine.Run(context.Background(), store.job.ID, func(ctx context.Context, item Item, _ CheckpointFunc) error {
		switch item.ID {
		case "item-stale":
			<-siblingStarted
			return ErrStateChanged
		case "item-sibling":
			close(siblingStarted)
			<-ctx.Done()
			close(siblingCancelled)
			return ctx.Err()
		default:
			return nil
		}
	})
	if !errors.Is(runErr, ErrStateChanged) {
		t.Fatalf("Run error=%v", runErr)
	}
	select {
	case <-siblingCancelled:
	case <-time.After(time.Second):
		t.Fatal("sibling was not cancelled after ownership loss")
	}
}

func newTestEngine(t *testing.T, store *engineEventStore) *Engine {
	t.Helper()
	engine, err := NewEngine(store, EngineOptions{
		Owner: "worker", MaxAttempts: 1, PollInterval: time.Hour, LogTimeout: 20 * time.Millisecond,
		Scheduler: NewScheduler(Limits{Global: 1}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

type engineEventStore struct {
	mu               sync.Mutex
	events           []string
	startedOwners    []string
	activeOwner      string
	transitionOwner  string
	job              domain.Job
	item             Item
	items            []Item
	transitionErrors map[domain.JobState]error
	blockLogs        bool
	logDeadline      atomic.Bool
	finalizeCalls    atomic.Int32
	pauseCalls       atomic.Int32
	finishCalls      atomic.Int32
}

func newEngineEventStore() *engineEventStore {
	job := domain.Job{ID: "job", Kind: "download", State: domain.JobQueued}
	item := Item{ID: "item", JobID: job.ID, Key: "item", State: domain.JobQueued}
	return &engineEventStore{
		job: job, item: item, items: []Item{item}, transitionErrors: make(map[domain.JobState]error),
	}
}

func (store *engineEventStore) record(event string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.events = append(store.events, event)
}

func (store *engineEventStore) eventsSnapshot() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]string(nil), store.events...)
}

func (store *engineEventStore) Get(context.Context, domain.JobID) (domain.Job, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.job, nil
}

func (store *engineEventStore) ListItems(context.Context, domain.JobID) ([]Item, error) {
	return append([]Item(nil), store.items...), nil
}

func (store *engineEventStore) StartJob(_ context.Context, _ domain.JobID, owner string, _ time.Duration) (domain.Job, error) {
	store.mu.Lock()
	store.job.State = domain.JobRunning
	store.activeOwner = owner
	store.startedOwners = append(store.startedOwners, owner)
	job := store.job
	store.events = append(store.events, "transition:"+string(domain.JobRunning))
	store.mu.Unlock()
	return job, nil
}

func (store *engineEventStore) RenewLease(_ context.Context, _ domain.JobID, owner string, _ time.Duration) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.job.State == domain.JobRunning && store.activeOwner == owner, nil
}

func (store *engineEventStore) PauseOwned(_ context.Context, _ domain.JobID, owner string) (domain.Job, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.activeOwner != owner {
		return store.job, ErrStateChanged
	}
	store.pauseCalls.Add(1)
	store.job.State = domain.JobPaused
	return store.job, nil
}

func (store *engineEventStore) ClaimItem(_ context.Context, _ domain.JobID, itemID string, owner string) (Item, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.activeOwner != owner {
		return Item{}, ErrStateChanged
	}
	for index := range store.items {
		if store.items[index].ID == itemID {
			store.items[index].State = domain.JobRunning
			return store.items[index], nil
		}
	}
	store.item.State = domain.JobRunning
	return store.item, nil
}

func (store *engineEventStore) SaveCheckpoint(_ context.Context, _ domain.JobID, _ string, owner string, _ any) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.activeOwner != owner {
		return ErrStateChanged
	}
	return nil
}

func (store *engineEventStore) TransitionItem(
	_ context.Context,
	_ domain.JobID,
	itemID string,
	owner string,
	_ domain.JobState,
	to domain.JobState,
	_ any,
	_ FailureClass,
	_ string,
) (Item, error) {
	store.mu.Lock()
	store.transitionOwner = owner
	activeOwner := store.activeOwner
	store.mu.Unlock()
	store.record("transition:" + string(to))
	if owner != activeOwner {
		return Item{}, ErrStateChanged
	}
	if err := store.transitionErrors[to]; err != nil {
		return Item{}, err
	}
	for index := range store.items {
		if store.items[index].ID == itemID {
			store.items[index].State = to
			return store.items[index], nil
		}
	}
	store.item.State = to
	return store.item, nil
}

func (store *engineEventStore) BeginAttempt(_ context.Context, item Item, owner, _, _ string) (Attempt, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.activeOwner != owner {
		return Attempt{}, ErrStateChanged
	}
	return Attempt{JobID: store.job.ID, ItemID: item.ID, Number: 1}, nil
}

func (store *engineEventStore) FinishAttempt(_ context.Context, _ Attempt, owner string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.activeOwner != owner {
		return ErrStateChanged
	}
	store.finishCalls.Add(1)
	return nil
}

func (store *engineEventStore) BlockAuthentication(_ context.Context, _ domain.JobID, _ string, owner string, _ string) (domain.Job, error) {
	store.mu.Lock()
	activeOwner := store.activeOwner
	store.mu.Unlock()
	store.record("transition:" + string(domain.JobBlockedAuth))
	if owner != activeOwner {
		return domain.Job{}, ErrStateChanged
	}
	if err := store.transitionErrors[domain.JobBlockedAuth]; err != nil {
		return domain.Job{}, err
	}
	store.job.State = domain.JobBlockedAuth
	store.item.State = domain.JobBlockedAuth
	return store.job, nil
}

func (store *engineEventStore) FinalizeJob(_ context.Context, _ domain.JobID, owner string) (domain.Job, error) {
	store.mu.Lock()
	if store.activeOwner != owner {
		store.mu.Unlock()
		return domain.Job{}, ErrStateChanged
	}
	store.finalizeCalls.Add(1)
	store.job.State = domain.JobCompleted
	job := store.job
	store.events = append(store.events, "transition:"+string(domain.JobCompleted))
	store.mu.Unlock()
	return job, nil
}

func (store *engineEventStore) resetQueued() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.job.State = domain.JobQueued
	store.item.State = domain.JobQueued
	store.activeOwner = ""
}

func (store *engineEventStore) takeOverWithSamePrefix(prefix string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.job.State = domain.JobRunning
	store.activeOwner = prefix + "/replacement"
}

func (store *engineEventStore) startedOwnersSnapshot() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]string(nil), store.startedOwners...)
}

func (store *engineEventStore) activeOwnerSnapshot() string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.activeOwner
}

func (store *engineEventStore) transitionOwnerSnapshot() string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.transitionOwner
}

func (store *engineEventStore) jobStateSnapshot() domain.JobState {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.job.State
}

func (store *engineEventStore) AppendLog(ctx context.Context, _ domain.JobID, _ string, _ string, message string, _ any) error {
	_, hasDeadline := ctx.Deadline()
	store.logDeadline.Store(hasDeadline)
	store.record("log:" + message)
	if store.blockLogs {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func assertEventBefore(t *testing.T, events []string, before, after string) {
	t.Helper()
	beforeIndex := eventIndex(events, before)
	afterIndex := eventIndex(events, after)
	if beforeIndex < 0 || afterIndex < 0 || beforeIndex >= afterIndex {
		t.Fatalf("expected %q before %q, events = %#v", before, after, events)
	}
}

func eventIndex(events []string, target string) int {
	for index, event := range events {
		if event == target {
			return index
		}
	}
	return -1
}
