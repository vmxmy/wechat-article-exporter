package jobs

import (
	"context"
	"errors"
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

func updateMaximum(maximum *atomic.Int32, current int32) {
	for {
		previous := maximum.Load()
		if current <= previous || maximum.CompareAndSwap(previous, current) {
			return
		}
	}
}
