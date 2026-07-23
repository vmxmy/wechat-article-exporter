package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerEnforcesGlobalHostOperationAndSensitiveLimits(t *testing.T) {
	scheduler := NewScheduler(Limits{Global: 3, PerHost: 2, Sensitive: 1, PerOperation: map[string]int{"comments": 2}})
	var global, maximumGlobal atomic.Int32
	var sensitive, maximumSensitive atomic.Int32
	var host sync.Map
	work := make([]Work, 0, 12)
	for index := range 12 {
		item := Work{ID: string(rune(index)), Operation: "comments", Host: "mp.weixin.qq.com", Sensitive: true}
		item.Run = func(context.Context) error {
			currentGlobal := global.Add(1)
			updateMaximum(&maximumGlobal, currentGlobal)
			currentSensitive := sensitive.Add(1)
			updateMaximum(&maximumSensitive, currentSensitive)
			value, _ := host.LoadOrStore(item.Host, new(atomic.Int32))
			counter := value.(*atomic.Int32)
			currentHost := counter.Add(1)
			if currentHost > 2 {
				t.Errorf("host concurrency = %d", currentHost)
			}
			time.Sleep(time.Millisecond)
			counter.Add(-1)
			sensitive.Add(-1)
			global.Add(-1)
			return nil
		}
		work = append(work, item)
	}
	results := scheduler.Run(context.Background(), work)
	if len(results) != len(work) || maximumGlobal.Load() > 3 || maximumSensitive.Load() > 1 {
		t.Fatalf("results=%d maxGlobal=%d maxSensitive=%d", len(results), maximumGlobal.Load(), maximumSensitive.Load())
	}
}

func TestSchedulerRoundRobinsOperationQueues(t *testing.T) {
	scheduler := NewScheduler(Limits{Global: 1})
	var mu sync.Mutex
	var order []string
	work := []Work{}
	for _, operation := range []string{"article", "article", "article", "resource", "resource"} {
		name := operation
		work = append(work, Work{Operation: operation, Host: operation, Run: func(context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}})
	}
	scheduler.Run(context.Background(), work)
	if len(order) != 5 || order[0] != "article" || order[1] != "resource" {
		t.Fatalf("execution order = %#v", order)
	}
}

func TestSchedulerFairnessContinuesAcrossManyRounds(t *testing.T) {
	scheduler := NewScheduler(Limits{Global: 1})
	var mu sync.Mutex
	var order []string
	work := make([]Work, 0, 8)
	for index := range 6 {
		work = append(work, Work{ID: "article", Operation: "article", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			mu.Lock()
			order = append(order, "article")
			mu.Unlock()
			return nil
		}})
		if index < 2 {
			work = append(work, Work{ID: "resource", Operation: "resource", Host: "res.wx.qq.com", Run: func(context.Context) error {
				mu.Lock()
				order = append(order, "resource")
				mu.Unlock()
				return nil
			}})
		}
	}
	results := scheduler.RunResults(context.Background(), work)
	if len(results) != len(work) {
		t.Fatalf("result count = %d", len(results))
	}
	for index, result := range results {
		if result.Err != nil {
			t.Fatalf("result[%d] = %v", index, result.Err)
		}
	}
	if len(order) < 4 || order[0] != "article" || order[1] != "resource" || order[2] != "article" || order[3] != "resource" {
		t.Fatalf("execution order = %#v", order)
	}
}

func TestSchedulerCancellationDoesNotLaunchQueuedWork(t *testing.T) {
	scheduler := NewScheduler(Limits{Global: 1})
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	work := []Work{
		{ID: "first", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			calls.Add(1)
			close(firstStarted)
			<-firstRelease
			return nil
		}},
		{ID: "second", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			calls.Add(1)
			return nil
		}},
	}
	done := make(chan []Result, 1)
	go func() { done <- scheduler.RunResults(ctx, work) }()
	<-firstStarted
	cancel()
	close(firstRelease)
	results := <-done
	if calls.Load() != 1 {
		t.Fatalf("started work count = %d", calls.Load())
	}
	if results[0].Err != nil || !errors.Is(results[1].Err, context.Canceled) {
		t.Fatalf("results = %#v", results)
	}
}

func TestSchedulerCancelsWorkWhenSharedPermitLeaseIsLost(t *testing.T) {
	store := &losingPermitStore{}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 10 * time.Millisecond, PollInterval: time.Millisecond,
	})
	workCancelled := make(chan struct{})
	results := scheduler.RunResults(context.Background(), []Work{{
		ID: "lost", Operation: "download", Host: "mp.weixin.qq.com", Run: func(ctx context.Context) error {
			<-ctx.Done()
			close(workCancelled)
			return ctx.Err()
		},
	}})
	select {
	case <-workCancelled:
	default:
		t.Fatal("work was not cancelled after its permit lease was lost")
	}
	if len(results) != 1 || !errors.Is(results[0].Err, context.Canceled) ||
		!strings.Contains(results[0].Err.Error(), "permit lease was lost") {
		t.Fatalf("results = %#v", results)
	}
	if store.released.Load() != 1 {
		t.Fatalf("release count = %d", store.released.Load())
	}
}

func TestSchedulerKeepsRenewingUntilWorkReturnsAfterCallerCancellation(t *testing.T) {
	store := &recordingPermitStore{}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 10 * time.Millisecond, PollInterval: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	workCancelled := make(chan struct{})
	release := make(chan struct{})
	done := make(chan []Result, 1)
	go func() {
		done <- scheduler.RunResults(ctx, []Work{{
			ID: "work", Operation: "download", Host: "mp.weixin.qq.com", Run: func(workContext context.Context) error {
				close(started)
				<-workContext.Done()
				close(workCancelled)
				<-release
				return nil
			},
		}})
	}()
	<-started
	cancel()
	select {
	case <-workCancelled:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("caller cancellation did not reach running work")
	}
	waitForAtomicAtLeast(t, &store.renewed, 2, 250*time.Millisecond)
	close(release)
	results := <-done
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("results = %#v", results)
	}
	if store.released.Load() != 1 {
		t.Fatalf("release count = %d", store.released.Load())
	}
}

func TestSchedulerBoundsRenewAndReleaseStoreCalls(t *testing.T) {
	store := &blockingPermitStore{}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 5 * time.Millisecond, PollInterval: time.Millisecond, PermitOperationTimeout: 10 * time.Millisecond,
	})
	started := time.Now()
	results := scheduler.RunResults(context.Background(), []Work{{
		ID: "bounded", Operation: "download", Host: "mp.weixin.qq.com", Run: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}})
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("bounded permit calls took %s", elapsed)
	}
	if len(results) != 1 || results[0].Err == nil || !store.renewDeadline.Load() || !store.releaseDeadline.Load() {
		t.Fatalf("results=%#v renewDeadline=%v releaseDeadline=%v", results, store.renewDeadline.Load(), store.releaseDeadline.Load())
	}
}

func TestSchedulerBoundsPermitAcquisitionAndRetriesAfterOperationTimeout(t *testing.T) {
	store := &blockingAcquirePermitStore{}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 80 * time.Millisecond,
		RenewalInterval: 20 * time.Millisecond, PollInterval: time.Millisecond, PermitOperationTimeout: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
	defer cancel()
	started := time.Now()
	results := scheduler.RunResults(ctx, []Work{{
		ID: "bounded-acquire", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			t.Fatal("work ran without a distributed permit")
			return nil
		},
	}})
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("bounded permit acquisition took %s", elapsed)
	}
	if len(results) != 1 || !errors.Is(results[0].Err, context.DeadlineExceeded) || store.calls.Load() < 2 || !store.sawDeadline.Load() {
		t.Fatalf("results=%#v calls=%d deadline=%v", results, store.calls.Load(), store.sawDeadline.Load())
	}
}

func TestSchedulerDefersLocalReleaseWhenPermitReleasePanics(t *testing.T) {
	store := &panicReleasePermitStore{}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 10 * time.Millisecond, PollInterval: time.Millisecond,
	})
	work := Work{ID: "panic", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error { return nil }}
	if !scheduler.acquireLocal(work) {
		t.Fatal("local permit was not acquired")
	}
	func() {
		defer func() { _ = recover() }()
		_ = scheduler.run(context.Background(), work, Permit{ID: "permit", Owner: "scheduler-a"})
	}()
	if active := scheduler.active(); active != 0 {
		t.Fatalf("active local permits = %d", active)
	}
}

func TestSchedulerRetriesTransientAcquireUntilSuccess(t *testing.T) {
	store := &retryingPermitStore{failures: 2}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 10 * time.Millisecond, PollInterval: time.Millisecond,
	})
	var calls atomic.Int32
	results := scheduler.RunResults(context.Background(), []Work{{
		ID: "retry", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}})
	if len(results) != 1 || results[0].Err != nil || calls.Load() != 1 || store.attempts.Load() != 3 {
		t.Fatalf("results=%#v calls=%d attempts=%d", results, calls.Load(), store.attempts.Load())
	}
}

func TestSchedulerTransientAcquireRetryIsCancellable(t *testing.T) {
	store := &retryingPermitStore{failures: -1}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 10 * time.Millisecond, PollInterval: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []Result, 1)
	var calls atomic.Int32
	go func() {
		done <- scheduler.RunResults(ctx, []Work{{
			ID: "retry", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
				calls.Add(1)
				return nil
			},
		}})
	}()
	waitForAtomicAtLeast(t, &store.attempts, 2, 250*time.Millisecond)
	cancel()
	results := <-done
	if len(results) != 1 || !errors.Is(results[0].Err, context.Canceled) {
		t.Fatalf("results = %#v", results)
	}
	if calls.Load() != 0 {
		t.Fatalf("work started %d times while permit acquisition kept failing", calls.Load())
	}
}

func TestSchedulerDoesNotRetryPermanentAcquireError(t *testing.T) {
	store := &retryingPermitStore{failures: -1, err: errors.New("permission denied")}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: 60 * time.Millisecond,
		RenewalInterval: 10 * time.Millisecond, PollInterval: time.Millisecond,
	})
	var calls atomic.Int32
	results := scheduler.RunResults(context.Background(), []Work{{
		ID: "permanent", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}})
	if len(results) != 1 || results[0].Err == nil ||
		!strings.Contains(results[0].Err.Error(), "acquire scheduler permit") {
		t.Fatalf("results = %#v", results)
	}
	if calls.Load() != 0 || store.attempts.Load() != 1 {
		t.Fatalf("calls=%d attempts=%d", calls.Load(), store.attempts.Load())
	}
}

func TestSchedulerRejectsLeaseTooShortForSafeRenewal(t *testing.T) {
	store := &recordingPermitStore{}
	scheduler := NewScheduler(Limits{Global: 1}, SchedulerOptions{
		PermitStore: store, Owner: "scheduler-a", LeaseDuration: time.Millisecond,
	})
	var calls atomic.Int32
	results := scheduler.RunResults(context.Background(), []Work{{
		ID: "short", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}})
	if len(results) != 1 || results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "lease duration") ||
		calls.Load() != 0 || store.acquired.Load() != 0 {
		t.Fatalf("results=%#v calls=%d acquireAttempts=%d", results, calls.Load(), store.acquired.Load())
	}
}

func TestSchedulerRejectsUnsafeRenewalTiming(t *testing.T) {
	tests := []struct {
		name    string
		options SchedulerOptions
		want    string
	}{
		{name: "interval equals lease", options: SchedulerOptions{
			LeaseDuration: 60 * time.Millisecond, RenewalInterval: 60 * time.Millisecond,
			PermitOperationTimeout: 10 * time.Millisecond,
		}, want: "renewal interval"},
		{name: "operation timeout consumes margin", options: SchedulerOptions{
			LeaseDuration: 60 * time.Millisecond, RenewalInterval: 40 * time.Millisecond,
			PermitOperationTimeout: 20 * time.Millisecond,
		}, want: "operation timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingPermitStore{}
			test.options.PermitStore = store
			test.options.Owner = "scheduler-a"
			scheduler := NewScheduler(Limits{Global: 1}, test.options)
			results := scheduler.RunResults(context.Background(), []Work{{
				ID: "unsafe", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
					return nil
				},
			}})
			if len(results) != 1 || results[0].Err == nil || !strings.Contains(results[0].Err.Error(), test.want) {
				t.Fatalf("results = %#v", results)
			}
			if store.acquired.Load() != 0 {
				t.Fatalf("acquire attempts = %d", store.acquired.Load())
			}
		})
	}
}

type losingPermitStore struct {
	released atomic.Int32
}

func (*losingPermitStore) TryAcquire(context.Context, PermitRequest) (Permit, bool, error) {
	return Permit{ID: "permit-a", Owner: "scheduler-a"}, true, nil
}

func (*losingPermitStore) Renew(context.Context, Permit, time.Time, time.Duration) (bool, error) {
	return false, nil
}

func (store *losingPermitStore) Release(context.Context, Permit) error {
	store.released.Add(1)
	return nil
}

type recordingPermitStore struct {
	acquired atomic.Int32
	renewed  atomic.Int32
	released atomic.Int32
}

func (store *recordingPermitStore) TryAcquire(context.Context, PermitRequest) (Permit, bool, error) {
	store.acquired.Add(1)
	return Permit{ID: "permit-a", Owner: "scheduler-a"}, true, nil
}

func (store *recordingPermitStore) Renew(context.Context, Permit, time.Time, time.Duration) (bool, error) {
	store.renewed.Add(1)
	return true, nil
}

func (store *recordingPermitStore) Release(context.Context, Permit) error {
	store.released.Add(1)
	return nil
}

type blockingPermitStore struct {
	renewDeadline   atomic.Bool
	releaseDeadline atomic.Bool
}

type blockingAcquirePermitStore struct {
	calls       atomic.Int32
	sawDeadline atomic.Bool
}

func (store *blockingAcquirePermitStore) TryAcquire(ctx context.Context, _ PermitRequest) (Permit, bool, error) {
	store.calls.Add(1)
	_, hasDeadline := ctx.Deadline()
	store.sawDeadline.Store(hasDeadline)
	<-ctx.Done()
	return Permit{}, false, ctx.Err()
}

func (*blockingAcquirePermitStore) Renew(context.Context, Permit, time.Time, time.Duration) (bool, error) {
	return true, nil
}

func (*blockingAcquirePermitStore) Release(context.Context, Permit) error { return nil }

func (*blockingPermitStore) TryAcquire(context.Context, PermitRequest) (Permit, bool, error) {
	return Permit{ID: "permit-a", Owner: "scheduler-a"}, true, nil
}

func (store *blockingPermitStore) Renew(ctx context.Context, _ Permit, _ time.Time, _ time.Duration) (bool, error) {
	_, hasDeadline := ctx.Deadline()
	store.renewDeadline.Store(hasDeadline)
	<-ctx.Done()
	return false, ctx.Err()
}

func (store *blockingPermitStore) Release(ctx context.Context, _ Permit) error {
	_, hasDeadline := ctx.Deadline()
	store.releaseDeadline.Store(hasDeadline)
	<-ctx.Done()
	return ctx.Err()
}

type panicReleasePermitStore struct{}

func (*panicReleasePermitStore) TryAcquire(context.Context, PermitRequest) (Permit, bool, error) {
	return Permit{ID: "permit", Owner: "scheduler-a"}, true, nil
}
func (*panicReleasePermitStore) Renew(context.Context, Permit, time.Time, time.Duration) (bool, error) {
	return true, nil
}
func (*panicReleasePermitStore) Release(context.Context, Permit) error { panic("release panic") }

type retryingPermitStore struct {
	failures int32
	attempts atomic.Int32
	err      error
}

func (store *retryingPermitStore) TryAcquire(context.Context, PermitRequest) (Permit, bool, error) {
	attempt := store.attempts.Add(1)
	if store.failures < 0 || attempt <= store.failures {
		if store.err != nil {
			return Permit{}, false, store.err
		}
		return Permit{}, false, temporaryPermitError{err: errors.New("transient permit store failure")}
	}
	return Permit{ID: "permit-a", Owner: "scheduler-a"}, true, nil
}
func (*retryingPermitStore) Renew(context.Context, Permit, time.Time, time.Duration) (bool, error) {
	return true, nil
}
func (*retryingPermitStore) Release(context.Context, Permit) error { return nil }

type temporaryPermitError struct{ err error }

func (err temporaryPermitError) Error() string { return err.err.Error() }
func (err temporaryPermitError) Unwrap() error { return err.err }
func (temporaryPermitError) Temporary() bool   { return true }

func waitForAtomicAtLeast(t *testing.T, value *atomic.Int32, want int32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for value.Load() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := value.Load(); got < want {
		t.Fatalf("atomic value = %d, want at least %d", got, want)
	}
}

func updateMaximum(maximum *atomic.Int32, current int32) {
	for {
		previous := maximum.Load()
		if current <= previous || maximum.CompareAndSwap(previous, current) {
			return
		}
	}
}
