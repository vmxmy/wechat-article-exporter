package library

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
)

func TestSchedulerPermitsEnforceLimitsAcrossConnections(t *testing.T) {
	tests := []struct {
		name   string
		limits jobs.Limits
		first  jobs.Work
		second jobs.Work
	}{
		{
			name: "global", limits: jobs.Limits{Global: 1, PerHost: 2, Sensitive: 2},
			first:  jobs.Work{Operation: "article", Host: "mp.weixin.qq.com"},
			second: jobs.Work{Operation: "resource", Host: "res.wx.qq.com"},
		},
		{
			name: "operation", limits: jobs.Limits{Global: 2, PerHost: 2, Sensitive: 2, PerOperation: map[string]int{"export": 1}},
			first:  jobs.Work{Operation: "export", Host: "host-a"},
			second: jobs.Work{Operation: "export", Host: "host-b"},
		},
		{
			name: "host", limits: jobs.Limits{Global: 2, PerHost: 1, Sensitive: 2},
			first:  jobs.Work{Operation: "article", Host: "mp.weixin.qq.com"},
			second: jobs.Work{Operation: "resource", Host: "mp.weixin.qq.com"},
		},
		{
			name: "sensitive", limits: jobs.Limits{Global: 2, PerHost: 2, Sensitive: 1},
			first:  jobs.Work{Operation: "comments", Host: "host-a", Sensitive: true},
			second: jobs.Work{Operation: "metadata", Host: "host-b", Sensitive: true},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shared.sqlite3")
			firstDatabase := openPath(t, path, "profile-a")
			secondDatabase := openPath(t, path, "profile-a")
			options := func(database *Database, owner string) jobs.SchedulerOptions {
				return jobs.SchedulerOptions{
					PermitStore: NewSchedulerPermitStore(database), Owner: owner,
					LeaseDuration: time.Second, RenewalInterval: 100 * time.Millisecond, PollInterval: 5 * time.Millisecond,
				}
			}
			firstScheduler := jobs.NewScheduler(test.limits, options(firstDatabase, "scheduler-a"))
			secondScheduler := jobs.NewScheduler(test.limits, options(secondDatabase, "scheduler-b"))

			firstStarted := make(chan struct{})
			firstRelease := make(chan struct{})
			secondStarted := make(chan struct{})
			first := test.first
			first.ID = "first"
			first.Run = func(context.Context) error {
				close(firstStarted)
				<-firstRelease
				return nil
			}
			second := test.second
			second.ID = "second"
			second.Run = func(context.Context) error {
				close(secondStarted)
				return nil
			}

			firstDone := make(chan []jobs.Result, 1)
			go func() { firstDone <- firstScheduler.RunResults(context.Background(), []jobs.Work{first}) }()
			select {
			case <-firstStarted:
			case <-time.After(time.Second):
				t.Fatal("first scheduler did not start")
			}
			secondDone := make(chan []jobs.Result, 1)
			go func() { secondDone <- secondScheduler.RunResults(context.Background(), []jobs.Work{second}) }()
			select {
			case <-secondStarted:
				t.Fatal("second scheduler exceeded the shared permit limit")
			case <-time.After(75 * time.Millisecond):
			}
			close(firstRelease)
			assertSchedulerResults(t, <-firstDone)
			select {
			case <-secondStarted:
			case <-time.After(time.Second):
				t.Fatal("second scheduler did not start after permit release")
			}
			assertSchedulerResults(t, <-secondDone)
		})
	}
}

func TestSchedulerRenewsSharedPermitWhileWorkIsRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite3")
	firstDatabase := openPath(t, path, "profile-a")
	secondDatabase := openPath(t, path, "profile-a")
	limits := jobs.Limits{Global: 1, PerHost: 1, Sensitive: 1}
	options := func(database *Database, owner string) jobs.SchedulerOptions {
		return jobs.SchedulerOptions{
			PermitStore: NewSchedulerPermitStore(database), Owner: owner,
			LeaseDuration: 80 * time.Millisecond, RenewalInterval: 20 * time.Millisecond, PollInterval: 5 * time.Millisecond,
		}
	}
	firstScheduler := jobs.NewScheduler(limits, options(firstDatabase, "scheduler-a"))
	secondScheduler := jobs.NewScheduler(limits, options(secondDatabase, "scheduler-b"))
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan []jobs.Result, 1)
	go func() {
		firstDone <- firstScheduler.RunResults(context.Background(), []jobs.Work{{
			ID: "first", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
				close(started)
				<-release
				return nil
			},
		}})
	}()
	<-started
	secondStarted := make(chan struct{})
	secondDone := make(chan []jobs.Result, 1)
	go func() {
		secondDone <- secondScheduler.RunResults(context.Background(), []jobs.Work{{
			ID: "second", Operation: "download", Host: "mp.weixin.qq.com", Run: func(context.Context) error {
				close(secondStarted)
				return nil
			},
		}})
	}()
	select {
	case <-secondStarted:
		t.Fatal("second scheduler started before the original lease duration")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	assertSchedulerResults(t, <-firstDone)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second scheduler did not start after renewed permit release")
	}
	assertSchedulerResults(t, <-secondDone)
}

func TestSchedulerPermitExpiresAfterOwnerCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite3")
	firstDatabase := openPath(t, path, "profile-a")
	secondDatabase := openPath(t, path, "profile-a")
	firstStore := NewSchedulerPermitStore(firstDatabase)
	secondStore := NewSchedulerPermitStore(secondDatabase)
	request := jobs.PermitRequest{
		Owner: "crashed-worker", Operation: "export", Host: "local", LeaseDuration: 80 * time.Millisecond,
		Limits: jobs.Limits{Global: 1, PerHost: 1, Sensitive: 1},
	}
	if _, acquired, err := firstStore.TryAcquire(context.Background(), request); err != nil || !acquired {
		t.Fatalf("first TryAcquire() acquired=%v error=%v", acquired, err)
	}
	request.Owner = "replacement-worker"
	if _, acquired, err := secondStore.TryAcquire(context.Background(), request); err != nil || acquired {
		t.Fatalf("second TryAcquire() before expiry acquired=%v error=%v", acquired, err)
	}
	time.Sleep(130 * time.Millisecond)
	if _, acquired, err := secondStore.TryAcquire(context.Background(), request); err != nil || !acquired {
		t.Fatalf("second TryAcquire() after expiry acquired=%v error=%v", acquired, err)
	}
}

func TestSchedulerPermitIgnoresSeverelySkewedClientClocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite3")
	firstDatabase := openPath(t, path, "profile-a")
	secondDatabase := openPath(t, path, "profile-a")
	firstStore := NewSchedulerPermitStore(firstDatabase)
	secondStore := NewSchedulerPermitStore(secondDatabase)
	limits := jobs.Limits{Global: 1, PerHost: 1, Sensitive: 1}
	first, acquired, err := firstStore.TryAcquire(context.Background(), jobs.PermitRequest{
		Owner: "clock-far-future", Operation: "export", Host: "local", Limits: limits,
		LeaseDuration: 80 * time.Millisecond, Now: time.Now().Add(100 * 365 * 24 * time.Hour),
	})
	if err != nil || !acquired {
		t.Fatalf("future-clock acquire=%v error=%v", acquired, err)
	}
	if _, acquired, err := secondStore.TryAcquire(context.Background(), jobs.PermitRequest{
		Owner: "clock-far-past", Operation: "export", Host: "local", Limits: limits,
		LeaseDuration: 80 * time.Millisecond, Now: time.Now().Add(-100 * 365 * 24 * time.Hour),
	}); err != nil || acquired {
		t.Fatalf("past-clock acquire before expiry=%v error=%v", acquired, err)
	}
	if renewed, err := firstStore.Renew(context.Background(), first, time.Now().Add(-100*365*24*time.Hour), 80*time.Millisecond); err != nil || !renewed {
		t.Fatalf("skewed renewal=%v error=%v", renewed, err)
	}
	if err := firstStore.Release(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := secondStore.TryAcquire(context.Background(), jobs.PermitRequest{
		Owner: "clock-far-past", Operation: "export", Host: "local", Limits: limits,
		LeaseDuration: 80 * time.Millisecond, Now: time.Now().Add(-100 * 365 * 24 * time.Hour),
	}); err != nil || !acquired {
		t.Fatalf("past-clock acquire after release=%v error=%v", acquired, err)
	}
}

func TestSchedulerPermitSkewCannotCreatePermanentLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite3")
	firstDatabase := openPath(t, path, "profile-a")
	secondDatabase := openPath(t, path, "profile-a")
	firstStore := NewSchedulerPermitStore(firstDatabase)
	secondStore := NewSchedulerPermitStore(secondDatabase)
	request := jobs.PermitRequest{
		Owner: "crashed-future-clock", Operation: "export", Host: "local", LeaseDuration: 80 * time.Millisecond,
		Limits: jobs.Limits{Global: 1, PerHost: 1, Sensitive: 1}, Now: time.Now().Add(100 * 365 * 24 * time.Hour),
	}
	if _, acquired, err := firstStore.TryAcquire(context.Background(), request); err != nil || !acquired {
		t.Fatalf("future-clock TryAcquire() acquired=%v error=%v", acquired, err)
	}
	time.Sleep(130 * time.Millisecond)
	request.Owner = "replacement-past-clock"
	request.Now = time.Now().Add(-100 * 365 * 24 * time.Hour)
	if _, acquired, err := secondStore.TryAcquire(context.Background(), request); err != nil || !acquired {
		t.Fatalf("replacement TryAcquire() acquired=%v error=%v", acquired, err)
	}
}

func TestSchedulerPermitRejectsSubMillisecondLeases(t *testing.T) {
	database := openPath(t, filepath.Join(t.TempDir(), "library.sqlite3"), "profile-a")
	store := NewSchedulerPermitStore(database)
	request := jobs.PermitRequest{
		Owner: "worker", Operation: "export", Host: "local", LeaseDuration: time.Nanosecond,
		Limits: jobs.Limits{Global: 1, PerHost: 1, Sensitive: 1},
	}
	if _, acquired, err := store.TryAcquire(context.Background(), request); err == nil || acquired {
		t.Fatalf("sub-millisecond acquire=%v error=%v", acquired, err)
	}
	if renewed, err := store.Renew(context.Background(), jobs.Permit{ID: "permit", Owner: "worker"}, time.Now(), time.Nanosecond); err == nil || renewed {
		t.Fatalf("sub-millisecond renew=%v error=%v", renewed, err)
	}
}

func TestSchedulerPermitsNeverOverAcquireDuringConcurrentConnectionRaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite3")
	firstDatabase := openPath(t, path, "profile-a")
	secondDatabase := openPath(t, path, "profile-a")
	firstStore := NewSchedulerPermitStore(firstDatabase)
	secondStore := NewSchedulerPermitStore(secondDatabase)
	limits := jobs.Limits{Global: 1, PerHost: 1, Sensitive: 1}
	for round := range 50 {
		start := make(chan struct{})
		var acquired atomic.Int32
		var permitsMu sync.Mutex
		permits := make([]struct {
			store  *SchedulerPermitStore
			permit jobs.Permit
		}, 0, 2)
		errorsFound := make(chan error, 2)
		var wait sync.WaitGroup
		for index, store := range []*SchedulerPermitStore{firstStore, secondStore} {
			wait.Add(1)
			go func(index int, store *SchedulerPermitStore) {
				defer wait.Done()
				<-start
				permit, ok, err := store.TryAcquire(context.Background(), jobs.PermitRequest{
					Owner: fmt.Sprintf("worker-%d", index), Operation: "export", Host: "local",
					Limits: limits, LeaseDuration: time.Second,
				})
				if err != nil {
					errorsFound <- err
					return
				}
				if ok {
					acquired.Add(1)
					permitsMu.Lock()
					permits = append(permits, struct {
						store  *SchedulerPermitStore
						permit jobs.Permit
					}{store: store, permit: permit})
					permitsMu.Unlock()
				}
			}(index, store)
		}
		close(start)
		wait.Wait()
		close(errorsFound)
		for err := range errorsFound {
			t.Fatalf("round %d concurrent TryAcquire() error = %v", round, err)
		}
		if acquired.Load() != 1 {
			t.Fatalf("round %d acquired permits = %d, want 1", round, acquired.Load())
		}
		for _, held := range permits {
			if err := held.store.Release(context.Background(), held.permit); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func assertSchedulerResults(t *testing.T, results []jobs.Result) {
	t.Helper()
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("scheduler results = %#v", results)
	}
}
