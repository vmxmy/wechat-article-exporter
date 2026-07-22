package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
)

func TestJobStoreCreatesIdempotentlyTransitionsAndLeases(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	ctx := context.Background()
	first, err := store.Create(ctx, jobs.Spec{Kind: "route_probe", Profile: "profile-a", IdempotencyKey: "probe-all", Payload: map[string]any{"all": true}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, jobs.Spec{Kind: "route_probe", Profile: "profile-a", IdempotencyKey: "probe-all"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent job IDs differ: %s, %s", first.ID, second.ID)
	}
	acquired, err := store.AcquireLease(ctx, first.ID, "worker-a", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("AcquireLease(worker-a) = %v, %v", acquired, err)
	}
	acquired, err = store.AcquireLease(ctx, first.ID, "worker-b", time.Minute)
	if err != nil || acquired {
		t.Fatalf("AcquireLease(worker-b) = %v, %v", acquired, err)
	}
	running, err := store.Transition(ctx, first.ID, domain.JobRunning)
	if err != nil || running.State != domain.JobRunning {
		t.Fatalf("Transition(running) = %#v, %v", running, err)
	}
	cancelled, err := store.Cancel(ctx, first.ID)
	if err != nil || cancelled.State != domain.JobCancelled {
		t.Fatalf("Cancel() = %#v, %v", cancelled, err)
	}
	page, err := store.Query(ctx, domain.JobQuery{States: []domain.JobState{domain.JobCancelled}})
	if err != nil || page.Total != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("Query() = %#v, %v", page, err)
	}
}

func TestJobStorePersistsItemsAttemptsCheckpointsLogsAndRecovery(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", Profile: "profile-a", IdempotencyKey: "batch-a"}, []string{"a", "b", "a"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListItems() = %#v, %v", items, err)
	}
	attempt, err := store.BeginAttempt(ctx, items[0], "", "request-a")
	if err != nil {
		t.Fatal(err)
	}
	attempt.FailureClass = jobs.FailureNetwork
	attempt.ErrorMessage = "timeout"
	if err := store.FinishAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateItem(ctx, items[0].ID, domain.JobQueued, domain.JobRunning, map[string]any{"offset": 1}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateItem(ctx, items[0].ID, domain.JobRunning, domain.JobCompleted, map[string]any{"offset": 2}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLog(ctx, job.ID, items[0].ID, "info", "completed", map[string]any{"route": "direct"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(ctx, job.ID, domain.JobRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.Exec(`UPDATE jobs SET lease_owner='dead', lease_expires_at=? WHERE id=?`, now.Add(-time.Second).UnixMilli(), job.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverStale(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverStale() = %d, %v", recovered, err)
	}
	got, err := store.Get(ctx, job.ID)
	if err != nil || got.State != domain.JobQueued {
		t.Fatalf("Get(recovered) = %#v, %v", got, err)
	}
	items, err = store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].State != domain.JobCompleted || items[0].AttemptCount != 1 {
		t.Fatalf("completed item after recovery = %#v", items[0])
	}
}

func TestJobStoreRedactsEverySerializedPersistenceBoundary(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	payload := map[string]any{
		"access_token": "payload-secret",
		"url":          "https://mp.weixin.qq.com/s/example?pass_ticket=payload-query-secret&keep=yes",
		"raw":          json.RawMessage(`{"refresh_token":"payload-raw-secret","visible":"payload-visible"}`),
	}
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", Payload: payload}, []string{"item"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListItems() = %#v, %v", items, err)
	}
	claimed, err := store.ClaimItem(ctx, job.ID, items[0].ID, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	route, err := database.AddRoute(ctx, network.RouteConfig{
		ID: "route-redaction", Name: "redaction", Kind: network.RouteURLWrapper,
		Endpoint: "https://proxy.example/route?authorization=route-secret", Trust: network.TrustPublicOnly,
		Classes: []network.RequestClass{network.PublicContent}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.BeginAttempt(ctx, claimed, route.ID, "request access_token=request-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(ctx, job.ID, claimed.ID, "worker-a", map[string]any{
		"appmsg_token": "checkpoint-secret",
		"cursor":       "checkpoint-visible",
	}); err != nil {
		t.Fatal(err)
	}
	attempt.FailureClass = jobs.FailureNetwork
	attempt.ErrorMessage = "request failed: https://mp.weixin.qq.com/s/example?key=attempt-secret&keep=yes"
	if err := store.FinishAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLog(ctx, job.ID, claimed.ID, "error",
		"Cookie: sid=log-message-secret; bizuin=log-cookie-secret", map[string]any{
			"proxy_authorization": "log-field-secret",
			"diagnostic":          "log-visible",
		}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionItem(ctx, job.ID, claimed.ID, "worker-a", domain.JobRunning, domain.JobFailed,
		map[string]any{"session_token": "transition-secret", "offset": 40}, jobs.FailureNetwork,
		"Authorization: Bearer item-error-secret"); err != nil {
		t.Fatal(err)
	}

	var payloadJSON, checkpointJSON, itemError string
	if err := database.db.QueryRow(`SELECT payload_json FROM jobs WHERE id=?`, job.ID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow(`SELECT checkpoint_json, error_message FROM job_items WHERE id=?`, claimed.ID).
		Scan(&checkpointJSON, &itemError); err != nil {
		t.Fatal(err)
	}
	var routeID, requestID, attemptError string
	if err := database.db.QueryRow(`SELECT route_id, request_id, error_message FROM job_attempts
WHERE job_id=? AND item_id=?`, job.ID, claimed.ID).Scan(&routeID, &requestID, &attemptError); err != nil {
		t.Fatal(err)
	}
	var logMessage, logFields string
	if err := database.db.QueryRow(`SELECT message, fields_json FROM job_logs WHERE job_id=? AND item_id=?`, job.ID, claimed.ID).
		Scan(&logMessage, &logFields); err != nil {
		t.Fatal(err)
	}

	persisted := strings.Join([]string{payloadJSON, checkpointJSON, itemError, routeID, requestID, attemptError, logMessage, logFields}, "\n")
	for _, forbidden := range []string{
		"payload-secret", "payload-query-secret", "payload-raw-secret", "route-secret", "request-secret",
		"checkpoint-secret", "attempt-secret", "log-message-secret", "log-cookie-secret", "log-field-secret",
		"transition-secret", "item-error-secret",
	} {
		if strings.Contains(persisted, forbidden) {
			t.Fatalf("SQLite persistence leaked %q:\n%s", forbidden, persisted)
		}
	}
	for _, retained := range []string{
		"payload-visible", "keep=yes", "offset", "40", "log-visible",
	} {
		if !strings.Contains(persisted, retained) {
			t.Fatalf("SQLite persistence removed diagnostic value %q:\n%s", retained, persisted)
		}
	}
}

func TestJobStoreRedactsCreatePayloadOutsideItemTransactions(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	job, err := store.Create(context.Background(), jobs.Spec{
		Kind: "diagnostic",
		Payload: map[string]any{
			"cookie": "create-secret",
			"detail": "create-visible",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := database.db.QueryRow(`SELECT payload_json FROM jobs WHERE id=?`, job.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "create-secret") || !strings.Contains(payload, "create-visible") {
		t.Fatalf("Create payload = %s", payload)
	}
}

func TestJobStoreControlsPreserveCompletedItemsAndRetryOnlyFailures(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "controls"}, []string{"done", "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		claimed, claimErr := store.ClaimItem(ctx, job.ID, item.ID, "worker-a")
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		if item.Key == "done" {
			if _, err := store.TransitionItem(ctx, job.ID, claimed.ID, "worker-a", domain.JobRunning,
				domain.JobCompleted, map[string]any{"offset": 9}, "", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	paused, err := store.Pause(ctx, job.ID)
	if err != nil || paused.State != domain.JobPaused {
		t.Fatalf("Pause() = %#v, %v", paused, err)
	}
	items, err = store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := itemStates(items)
	if states["done"] != domain.JobCompleted || states["active"] != domain.JobPaused {
		t.Fatalf("paused item states = %#v", states)
	}
	resumed, err := store.Resume(ctx, job.ID)
	if err != nil || resumed.State != domain.JobQueued {
		t.Fatalf("Resume() = %#v, %v", resumed, err)
	}
	items, err = store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	states = itemStates(items)
	if states["done"] != domain.JobCompleted || states["active"] != domain.JobQueued {
		t.Fatalf("resumed item states = %#v", states)
	}

	if _, err := store.StartJob(ctx, job.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	items, _ = store.ListItems(ctx, job.ID)
	for _, item := range items {
		if item.Key != "active" {
			continue
		}
		claimed, err := store.ClaimItem(ctx, job.ID, item.ID, "worker-a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.TransitionItem(ctx, job.ID, claimed.ID, "worker-a", domain.JobRunning,
			domain.JobFailed, nil, jobs.FailureParsing, "bad payload"); err != nil {
			t.Fatal(err)
		}
	}
	partial, err := store.FinalizeJob(ctx, job.ID, "worker-a")
	if err != nil || partial.State != domain.JobPartial {
		t.Fatalf("FinalizeJob() = %#v, %v", partial, err)
	}
	retried, err := store.Retry(ctx, job.ID)
	if err != nil || retried.State != domain.JobQueued {
		t.Fatalf("Retry() = %#v, %v", retried, err)
	}
	items, _ = store.ListItems(ctx, job.ID)
	states = itemStates(items)
	if states["done"] != domain.JobCompleted || states["active"] != domain.JobQueued {
		t.Fatalf("retried item states = %#v", states)
	}
}

func TestJobStoreRecoversStaleRunningItemsFromCheckpointAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	ctx := context.Background()
	database, err := Open(ctx, OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewJobStore(database)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "sync", IdempotencyKey: "restart"}, []string{"page-1", "page-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "dead-worker", time.Second); err != nil {
		t.Fatal(err)
	}
	items, _ := store.ListItems(ctx, job.ID)
	for _, item := range items {
		claimed, err := store.ClaimItem(ctx, job.ID, item.ID, "dead-worker")
		if err != nil {
			t.Fatal(err)
		}
		if item.Key == "page-1" {
			if err := store.SaveCheckpoint(ctx, job.ID, item.ID, "dead-worker", map[string]any{"offset": 40}); err != nil {
				t.Fatal(err)
			}
			attempt, err := store.BeginAttempt(ctx, claimed, "", "request-1")
			if err != nil {
				t.Fatal(err)
			}
			_ = attempt
		} else if _, err := store.TransitionItem(ctx, job.ID, item.ID, "dead-worker", domain.JobRunning,
			domain.JobCompleted, map[string]any{"offset": 80}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	recoveredStore := NewJobStore(reopened)
	recoveredStore.now = func() time.Time { return now.Add(2 * time.Second) }
	recovered, err := recoveredStore.RecoverStale(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverStale() = %d, %v", recovered, err)
	}
	items, err = recoveredStore.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	byKey := itemsByKey(items)
	if byKey["page-1"].State != domain.JobQueued || byKey["page-2"].State != domain.JobCompleted {
		t.Fatalf("recovered states = %#v", itemStates(items))
	}
	var checkpoint map[string]int
	if err := json.Unmarshal(byKey["page-1"].Checkpoint, &checkpoint); err != nil || checkpoint["offset"] != 40 {
		t.Fatalf("checkpoint = %s, %v", byKey["page-1"].Checkpoint, err)
	}
	var openAttempts int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM job_attempts WHERE job_id=? AND completed_at IS NULL`, job.ID).Scan(&openAttempts); err != nil {
		t.Fatal(err)
	}
	if openAttempts != 0 {
		t.Fatalf("open attempts after recovery = %d", openAttempts)
	}
}

func TestEngineCompletesPartialWorkAndSkipsCompletedItemsOnResume(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "partial"}, []string{"done", "good", "bad"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "setup", time.Minute); err != nil {
		t.Fatal(err)
	}
	items, _ := store.ListItems(ctx, job.ID)
	for _, item := range items {
		if item.Key == "done" {
			claimed, err := store.ClaimItem(ctx, job.ID, item.ID, "setup")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.TransitionItem(ctx, job.ID, claimed.ID, "setup", domain.JobRunning,
				domain.JobCompleted, map[string]any{"committed": true}, "", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := store.Pause(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resume(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	engine, err := jobs.NewEngine(store, jobs.EngineOptions{
		Owner: "worker-a", Scheduler: jobs.NewScheduler(jobs.Limits{Global: 2}), MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	called := map[string]int{}
	result, err := engine.Run(ctx, job.ID, func(_ context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
		mu.Lock()
		called[item.Key]++
		mu.Unlock()
		if item.Key == "good" {
			return checkpoint(map[string]any{"committed": true})
		}
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: errors.New("bad payload")}
	})
	if err != nil || result.State != domain.JobPartial {
		t.Fatalf("Run() = %#v, %v", result, err)
	}
	if called["done"] != 0 || called["good"] != 1 || called["bad"] != 1 {
		t.Fatalf("execution counts = %#v", called)
	}
}

func TestEngineBlocksAuthenticationAndResumeAvoidsDuplicateCompletedWork(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "comments", IdempotencyKey: "auth"}, []string{"first", "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "setup", time.Minute); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Key != "first" {
			continue
		}
		claimed, err := store.ClaimItem(ctx, job.ID, item.ID, "setup")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.TransitionItem(ctx, job.ID, claimed.ID, "setup", domain.JobRunning,
			domain.JobCompleted, map[string]any{"committed": true}, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Pause(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resume(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	engine, err := jobs.NewEngine(store, jobs.EngineOptions{
		Owner: "worker-a", Scheduler: jobs.NewScheduler(jobs.Limits{Global: 1}), MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var firstCalls atomic.Int32
	result, err := engine.Run(ctx, job.ID, func(_ context.Context, item jobs.Item, _ jobs.CheckpointFunc) error {
		if item.Key == "first" {
			firstCalls.Add(1)
			return nil
		}
		return &jobs.ClassifiedError{Class: jobs.FailureAuthentication, Err: errors.New("login required")}
	})
	if err != nil || result.State != domain.JobBlockedAuth {
		t.Fatalf("blocked Run() = %#v, %v", result, err)
	}
	items, _ = store.ListItems(ctx, job.ID)
	states := itemStates(items)
	if states["first"] != domain.JobCompleted || states["auth"] != domain.JobBlockedAuth {
		t.Fatalf("blocked item states = %#v", states)
	}
	if _, err := store.Resume(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	resumedEngine, err := jobs.NewEngine(store, jobs.EngineOptions{
		Owner: "worker-b", Scheduler: jobs.NewScheduler(jobs.Limits{Global: 1}), MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = resumedEngine.Run(ctx, job.ID, func(_ context.Context, item jobs.Item, _ jobs.CheckpointFunc) error {
		if item.Key == "first" {
			firstCalls.Add(1)
		}
		return nil
	})
	if err != nil || result.State != domain.JobCompleted {
		t.Fatalf("resumed Run() = %#v, %v", result, err)
	}
	if firstCalls.Load() != 0 {
		t.Fatalf("completed item calls = %d", firstCalls.Load())
	}
}

func TestEngineCancellationStopsNewItemsAndPersistsCancelledState(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "cancel"}, []string{"one", "two", "three"})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := jobs.NewEngine(store, jobs.EngineOptions{
		Owner: "worker-a", Scheduler: jobs.NewScheduler(jobs.Limits{Global: 1}),
		LeaseDuration: time.Second, PollInterval: time.Millisecond, MaxAttempts: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	go func() {
		defer close(done)
		result, runErr := engine.Run(ctx, job.ID, func(runContext context.Context, _ jobs.Item, _ jobs.CheckpointFunc) error {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-runContext.Done()
			return runContext.Err()
		})
		if runErr != nil || result.State != domain.JobCancelled {
			t.Errorf("Run() = %#v, %v", result, runErr)
		}
	}()
	<-started
	if _, err := store.Cancel(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("engine did not stop after cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("started items after cancellation = %d", calls.Load())
	}
	items, _ := store.ListItems(ctx, job.ID)
	for _, item := range items {
		if item.State != domain.JobCancelled {
			t.Fatalf("cancelled item = %#v", item)
		}
	}
}

func TestJobCreationAddsMissingTargetsWithoutDuplicatingCompletedItems(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	first, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "same-target"}, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "same-target"}, []string{"b", "c", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("job IDs differ: %s, %s", first.ID, second.ID)
	}
	items, err := store.ListItems(ctx, first.ID)
	if err != nil || len(items) != 3 {
		t.Fatalf("ListItems() = %#v, %v", items, err)
	}
	seen := map[string]int{}
	for _, item := range items {
		seen[item.Key]++
	}
	if seen["a"] != 1 || seen["b"] != 1 || seen["c"] != 1 {
		t.Fatalf("item keys = %#v", seen)
	}
}

func itemStates(items []JobItem) map[string]domain.JobState {
	states := make(map[string]domain.JobState, len(items))
	for _, item := range items {
		states[item.Key] = item.State
	}
	return states
}

func itemsByKey(items []JobItem) map[string]JobItem {
	byKey := make(map[string]JobItem, len(items))
	for _, item := range items {
		byKey[item.Key] = item
	}
	return byKey
}
