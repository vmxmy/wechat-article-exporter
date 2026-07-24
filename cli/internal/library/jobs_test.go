package library

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
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

func TestRestoreBlockersIncludeRunningJobsAndActiveLeases(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	now := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return now }
	running, err := store.Create(context.Background(), jobs.Spec{Kind: "download", Profile: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(context.Background(), running.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	queued, err := store.Create(context.Background(), jobs.Spec{Kind: "export", Profile: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.AcquireLease(context.Background(), queued.ID, "worker-b", time.Minute); err != nil || !ok {
		t.Fatalf("queued lease = %v, %v", ok, err)
	}
	blockers, err := store.RestoreBlockers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[domain.JobID]bool{}
	for _, blocker := range blockers {
		seen[blocker.JobID] = true
	}
	if len(blockers) != 2 || !seen[running.ID] || !seen[queued.ID] {
		t.Fatalf("restore blockers = %#v", blockers)
	}
	store.now = func() time.Time { return now.Add(2 * time.Minute) }
	blockers, err = store.RestoreBlockers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 || blockers[0].JobID != running.ID {
		t.Fatalf("expired restore blockers = %#v", blockers)
	}
}

func TestJobAdmissionGuardCoversCreationAndExecutionStart(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	store.SetAdmissionGuard(func(context.Context) (func() error, error) {
		entered <- struct{}{}
		<-release
		return func() error { return nil }, nil
	})

	created := make(chan domain.Job, 1)
	errorsChannel := make(chan error, 1)
	go func() {
		job, err := store.Create(context.Background(), jobs.Spec{Kind: "download", Profile: "profile-a"})
		created <- job
		errorsChannel <- err
	}()
	<-entered
	var count int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("job was created before admission release: %d", count)
	}
	close(release)
	job := <-created
	if err := <-errorsChannel; err != nil {
		t.Fatal(err)
	}

	startEntered := make(chan struct{}, 1)
	startRelease := make(chan struct{})
	store.SetAdmissionGuard(func(context.Context) (func() error, error) {
		startEntered <- struct{}{}
		<-startRelease
		return func() error { return nil }, nil
	})
	startResult := make(chan error, 1)
	go func() {
		_, err := store.StartJob(context.Background(), job.ID, "worker-a", time.Minute)
		startResult <- err
	}()
	<-startEntered
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.JobQueued {
		t.Fatalf("job started before admission release: %s", got.State)
	}
	close(startRelease)
	if err := <-startResult; err != nil {
		t.Fatal(err)
	}
}

func TestJobAdmissionGuardCoversEveryRequeuePath(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(context.Context, *JobStore) domain.Job
		requeue func(context.Context, *JobStore, domain.JobID) error
	}{
		{
			name: "resume",
			prepare: func(ctx context.Context, store *JobStore) domain.Job {
				job, err := store.Create(ctx, jobs.Spec{Kind: "download", Profile: "profile-a"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Pause(ctx, job.ID); err != nil {
					t.Fatal(err)
				}
				return job
			},
			requeue: func(ctx context.Context, store *JobStore, id domain.JobID) error {
				_, err := store.Resume(ctx, id)
				return err
			},
		},
		{
			name: "retry",
			prepare: func(ctx context.Context, store *JobStore) domain.Job {
				job, err := store.Create(ctx, jobs.Spec{Kind: "download", Profile: "profile-a"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Transition(ctx, job.ID, domain.JobRunning); err != nil {
					t.Fatal(err)
				}
				if _, err := store.Transition(ctx, job.ID, domain.JobFailed); err != nil {
					t.Fatal(err)
				}
				return job
			},
			requeue: func(ctx context.Context, store *JobStore, id domain.JobID) error {
				_, err := store.Retry(ctx, id)
				return err
			},
		},
		{
			name: "generic transition",
			prepare: func(ctx context.Context, store *JobStore) domain.Job {
				job, err := store.Create(ctx, jobs.Spec{Kind: "download", Profile: "profile-a"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.Transition(ctx, job.ID, domain.JobRunning); err != nil {
					t.Fatal(err)
				}
				if _, err := store.Transition(ctx, job.ID, domain.JobFailed); err != nil {
					t.Fatal(err)
				}
				return job
			},
			requeue: func(ctx context.Context, store *JobStore, id domain.JobID) error {
				_, err := store.Transition(ctx, id, domain.JobQueued)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openTestDatabase(t, "profile-a")
			store := NewJobStore(database)
			ctx := context.Background()
			job := test.prepare(ctx, store)
			before, err := store.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}

			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			store.SetAdmissionGuard(func(context.Context) (func() error, error) {
				entered <- struct{}{}
				<-release
				return func() error { return nil }, nil
			})
			done := make(chan error, 1)
			go func() { done <- test.requeue(ctx, store, job.ID) }()
			<-entered
			blocked, err := store.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if blocked.State != before.State {
				t.Fatalf("state changed before admission release: got %s want %s", blocked.State, before.State)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			after, err := store.Get(ctx, job.ID)
			if err != nil || after.State != domain.JobQueued {
				t.Fatalf("requeued job=%#v err=%v", after, err)
			}
		})
	}
}

func TestRecoverStaleUsesAdmissionGuardAndReleaseFailureDoesNotHideCommittedMutation(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	ctx := context.Background()
	job, err := store.Create(ctx, jobs.Spec{Kind: "download", Profile: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "dead-worker", time.Second); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now.Add(2 * time.Second) }
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	store.SetAdmissionGuard(func(context.Context) (func() error, error) {
		entered <- struct{}{}
		<-release
		return func() error { return errors.New("maintenance unlock failed after commit") }, nil
	})
	type result struct {
		count int64
		err   error
	}
	done := make(chan result, 1)
	go func() {
		count, err := store.RecoverStale(ctx)
		done <- result{count: count, err: err}
	}()
	<-entered
	blocked, err := store.Get(ctx, job.ID)
	if err != nil || blocked.State != domain.JobRunning {
		t.Fatalf("job changed before admission release: %#v err=%v", blocked, err)
	}
	close(release)
	got := <-done
	if got.err != nil || got.count != 1 {
		t.Fatalf("RecoverStale() count=%d err=%v", got.count, got.err)
	}
	recovered, err := store.Get(ctx, job.ID)
	if err != nil || recovered.State != domain.JobQueued {
		t.Fatalf("recovered job=%#v err=%v", recovered, err)
	}
}

func TestRetryExportRollsBackWhenProvenanceIsBeingWritten(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "export", Profile: "profile-a"}, []string{"article-a"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if err := store.UpdateItem(ctx, items[0].ID, domain.JobQueued, domain.JobRunning, nil, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateItem(ctx, items[0].ID, domain.JobRunning, domain.JobFailed, nil, "storage", "failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(ctx, job.ID, domain.JobRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(ctx, job.ID, domain.JobFailed); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertExport(ctx, ExportRecord{
		ID: "export-a", JobID: job.ID, Format: "markdown", Manifest: map[string]any{"articleIds": []string{"article-a"}},
		State: string(domain.JobFailed), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE exports SET provenance_state='writing', provenance_claimed_at=? WHERE id=?`,
		time.Now().UnixMilli(), "export-a"); err != nil {
		t.Fatal(err)
	}

	if _, err := store.RetryExport(ctx, job.ID); !errors.Is(err, jobs.ErrStateChanged) {
		t.Fatalf("RetryExport() error=%v", err)
	}
	got, err := store.Get(ctx, job.ID)
	if err != nil || got.State != domain.JobFailed {
		t.Fatalf("job=%#v err=%v", got, err)
	}
	items, err = store.ListItems(ctx, job.ID)
	if err != nil || items[0].State != domain.JobFailed {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	record, err := database.GetExport(ctx, "export-a")
	if err != nil || record.ProvenanceState != "writing" || record.ProvenanceGeneration != 1 {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}

func TestExportProvenanceClaimsAreFencedRecoverStaleWritersAndRedactFailures(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	ctx := context.Background()
	if err := database.UpsertExport(ctx, ExportRecord{
		ID: "export-a", Format: "markdown", Manifest: map[string]any{"articleIds": []string{"article-a"}},
		State: string(domain.JobCompleted), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	generation, claimed, err := database.ClaimExportProvenance(ctx, "export-a", 1, time.Now().Add(-time.Minute))
	if err != nil || !claimed || generation != 1 {
		t.Fatalf("first claim generation=%d claimed=%v err=%v", generation, claimed, err)
	}
	if _, claimed, err := database.ClaimExportProvenance(ctx, "export-a", 1, time.Now().Add(-time.Minute)); err != nil || claimed {
		t.Fatalf("second claim claimed=%v err=%v", claimed, err)
	}
	if _, err := database.db.ExecContext(ctx, `UPDATE exports SET provenance_claimed_at=? WHERE id=?`,
		time.Now().Add(-10*time.Minute).UnixMilli(), "export-a"); err != nil {
		t.Fatal(err)
	}
	staleGeneration, claimed, err := database.ClaimExportProvenance(ctx, "export-a", 1, time.Now().Add(-5*time.Minute))
	if err != nil || !claimed || staleGeneration != generation+1 {
		t.Fatalf("stale claim generation=%d claimed=%v err=%v", staleGeneration, claimed, err)
	}
	if err := database.FailExportProvenance(ctx, "export-a", generation,
		"abandoned worker must be fenced"); !errors.Is(err, jobs.ErrStateChanged) {
		t.Fatalf("old generation error=%v", err)
	}
	if err := database.FailExportProvenance(ctx, "export-a", staleGeneration,
		"Authorization: Bearer secret-token access_token=another-secret failed"); err != nil {
		t.Fatal(err)
	}
	record, err := database.GetExport(ctx, "export-a")
	if err != nil {
		t.Fatal(err)
	}
	if record.ProvenanceState != "failed" || strings.Contains(record.ProvenanceError, "secret-token") ||
		strings.Contains(record.ProvenanceError, "another-secret") || !strings.Contains(record.ProvenanceError, "[REDACTED]") {
		t.Fatalf("provenance error=%q", record.ProvenanceError)
	}
	reclaimedGeneration, claimed, err := database.ClaimExportProvenance(ctx, "export-a", staleGeneration, time.Now())
	if err != nil || !claimed || reclaimedGeneration != staleGeneration+1 {
		t.Fatalf("failed reclaim generation=%d claimed=%v err=%v", reclaimedGeneration, claimed, err)
	}
	if err := database.CompleteExportProvenance(ctx, "export-a", staleGeneration, map[string]any{"stale": true},
		"stale.json", strings.Repeat("0", 64)); !errors.Is(err, jobs.ErrStateChanged) {
		t.Fatalf("old failed claimant completion error=%v", err)
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
	if _, err := store.StartJob(ctx, job.ID, "worker-a", time.Minute); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimItem(ctx, job.ID, items[0].ID, "worker-a")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.BeginAttempt(ctx, claimed, "worker-a", "", "request-a")
	if err != nil {
		t.Fatal(err)
	}
	attempt.FailureClass = jobs.FailureNetwork
	attempt.ErrorMessage = "timeout"
	if err := store.FinishAttempt(ctx, attempt, "worker-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionItem(ctx, job.ID, items[0].ID, "worker-a", domain.JobRunning,
		domain.JobCompleted, map[string]any{"offset": 2}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLog(ctx, job.ID, items[0].ID, "info", "completed", map[string]any{"route": "direct"}); err != nil {
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

func TestAppendLogOnceIsProfileScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.sqlite3")
	firstDatabase := openPath(t, path, "profile-a")
	secondDatabase := openPath(t, path, "profile-b")
	firstStore := NewJobStore(firstDatabase)
	secondStore := NewJobStore(secondDatabase)
	job, err := firstStore.CreateWithItems(context.Background(), jobs.Spec{Kind: "download", Profile: "profile-a"}, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := secondStore.AppendLogOnce(context.Background(), job.ID, "", "info", "cross-profile", nil, "once"); err != nil {
		t.Fatal(err)
	}
	logs, err := firstStore.ListLogs(context.Background(), job.ID, 10)
	if err != nil || len(logs) != 0 {
		t.Fatalf("cross-profile logs=%#v error=%v", logs, err)
	}
}

func TestJobStoreRejectsWhitespaceLeaseOwners(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	job, err := store.CreateWithItems(context.Background(), jobs.Spec{Kind: "download", Profile: "profile-a"}, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(context.Background(), job.ID, "   ", time.Minute); err == nil {
		t.Fatal("whitespace StartJob owner was accepted")
	}
	if acquired, err := store.AcquireLease(context.Background(), job.ID, "\t", time.Minute); err == nil || acquired {
		t.Fatalf("whitespace AcquireLease acquired=%v error=%v", acquired, err)
	}
}

func TestJobStoreFencesAttemptWritesAfterLeaseTakeover(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	ctx := context.Background()
	job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", Profile: "profile-a"}, []string{"article-a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartJob(ctx, job.ID, "worker-old", time.Minute); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListItems(ctx, job.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	claimed, err := store.ClaimItem(ctx, job.ID, items[0].ID, "worker-old")
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.BeginAttempt(ctx, claimed, "worker-old", "", "request-old")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := database.db.ExecContext(ctx, `UPDATE jobs SET lease_owner=?, lease_expires_at=? WHERE id=?`,
		"worker-new", now.Add(time.Minute).UnixMilli(), job.ID); err != nil {
		t.Fatal(err)
	}
	attempt.ErrorMessage = "stale completion"
	if err := store.FinishAttempt(ctx, attempt, "worker-old"); !errors.Is(err, jobs.ErrStateChanged) {
		t.Fatalf("stale FinishAttempt() error=%v", err)
	}
	if _, err := store.BeginAttempt(ctx, claimed, "worker-old", "direct", "request-stale"); !errors.Is(err, jobs.ErrStateChanged) {
		t.Fatalf("stale BeginAttempt() error=%v", err)
	}

	var completedAt any
	if err := database.db.QueryRowContext(ctx, `SELECT completed_at FROM job_attempts
WHERE job_id=? AND item_id=? AND attempt_number=?`, job.ID, claimed.ID, attempt.Number).Scan(&completedAt); err != nil {
		t.Fatal(err)
	}
	if completedAt != nil {
		t.Fatalf("stale owner completed attempt: %#v", completedAt)
	}
}

func TestJobStoreFencesOwnerControlWritesAfterLeaseTakeover(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation func(context.Context, *JobStore, domain.JobID, string, string) error
	}{
		{name: "pause", operation: func(ctx context.Context, store *JobStore, jobID domain.JobID, _ string, oldOwner string) error {
			_, err := store.PauseOwned(ctx, jobID, oldOwner)
			return err
		}},
		{name: "block authentication", operation: func(ctx context.Context, store *JobStore, jobID domain.JobID, itemID, oldOwner string) error {
			_, err := store.BlockAuthentication(ctx, jobID, itemID, oldOwner, "stale authentication failure")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := openTestDatabase(t, "profile-a")
			store := NewJobStore(database)
			now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
			store.now = func() time.Time { return now }
			ctx := context.Background()
			job, err := store.CreateWithItems(ctx, jobs.Spec{Kind: "download", Profile: "profile-a"}, []string{"article-a"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.StartJob(ctx, job.ID, "worker-old", time.Minute); err != nil {
				t.Fatal(err)
			}
			items, err := store.ListItems(ctx, job.ID)
			if err != nil || len(items) != 1 {
				t.Fatalf("items=%#v err=%v", items, err)
			}
			claimed, err := store.ClaimItem(ctx, job.ID, items[0].ID, "worker-old")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.db.ExecContext(ctx, `UPDATE jobs SET lease_owner=?, lease_expires_at=? WHERE id=?`,
				"worker-new", now.Add(time.Minute).UnixMilli(), job.ID); err != nil {
				t.Fatal(err)
			}
			if err := test.operation(ctx, store, job.ID, claimed.ID, "worker-old"); !errors.Is(err, jobs.ErrStateChanged) {
				t.Fatalf("stale owner operation error=%v", err)
			}
			current, err := store.Get(ctx, job.ID)
			if err != nil || current.State != domain.JobRunning {
				t.Fatalf("job after stale operation=%#v err=%v", current, err)
			}
			currentItems, err := store.ListItems(ctx, job.ID)
			if err != nil || currentItems[0].State != domain.JobRunning {
				t.Fatalf("item after stale operation=%#v err=%v", currentItems, err)
			}
		})
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
	attempt, err := store.BeginAttempt(ctx, claimed, "worker-a", route.ID, "request access_token=request-secret")
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
	if err := store.FinishAttempt(ctx, attempt, "worker-a"); err != nil {
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

func TestListLogsBoundedUsesUTF8ByteBudgets(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	job, err := store.Create(context.Background(), jobs.Spec{Kind: "diagnostic"})
	if err != nil {
		t.Fatal(err)
	}
	message := strings.Repeat("界", 100)
	if _, err := database.db.Exec(`INSERT INTO job_logs(job_id, level, message, fields_json, created_at)
VALUES(?, 'info', ?, '{}', ?)`, job.ID, message, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	logs, err := store.ListLogsBounded(context.Background(), job.ID, JobLogBudget{
		MaximumRows: 10, MaximumRawBytes: 128, MaximumEntryBytes: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message == message || len(logs[0].Message) > 128 {
		t.Fatalf("bounded multibyte logs=%#v", logs)
	}
	if truncated, _ := logs[0].Fields["truncated"].(bool); !truncated {
		t.Fatalf("bounded multibyte log was not marked truncated: %#v", logs[0])
	}
}

func TestCreateWithItemsAndObjectsRollsBackMetadataWithoutDurablePin(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	object := objects.Object{Digest: strings.Repeat("a", 64), Size: 12, MediaType: "application/json"}
	if _, err := store.CreateWithItemsAndObjects(context.Background(), jobs.Spec{Kind: "export"}, []string{""},
		[]RegisteredJobObject{{Object: object, CreatedAt: time.Now()}}); err == nil {
		t.Fatal("CreateWithItemsAndObjects() error = nil")
	}
	var objectCount, jobCount int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM objects WHERE digest=?", object.Digest).Scan(&objectCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM jobs WHERE kind='export'").Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if objectCount != 0 || jobCount != 0 {
		t.Fatalf("rolled-back object/job counts=%d/%d", objectCount, jobCount)
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
			attempt, err := store.BeginAttempt(ctx, claimed, "dead-worker", "", "request-1")
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
		LeaseDuration: time.Second, PollInterval: 100 * time.Millisecond, MaxAttempts: 1,
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

func TestJobStoreCreateOrGetWithItemsDoesNotExpandExistingIntent(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	ctx := context.Background()
	first, existed, err := store.CreateOrGetWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "fixed-targets"}, []string{"a", "b"})
	if err != nil || existed {
		t.Fatalf("first=%#v existed=%t err=%v", first, existed, err)
	}
	second, existed, err := store.CreateOrGetWithItems(ctx, jobs.Spec{Kind: "download", IdempotencyKey: "fixed-targets"}, []string{"b", "c", "a"})
	if err != nil || !existed || first.ID != second.ID {
		t.Fatalf("first=%#v second=%#v existed=%t err=%v", first, second, existed, err)
	}
	items, err := store.ListItems(ctx, first.ID)
	keys := map[string]bool{}
	for _, item := range items {
		keys[item.Key] = true
	}
	if err != nil || len(items) != 2 || !keys["a"] || !keys["b"] || keys["c"] {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestJobStoreCreateOrGetWithItemsConcurrentSameKeyReturnsOneJob(t *testing.T) {
	database := openTestDatabase(t, "profile-a")
	store := NewJobStore(database)
	const callers = 16
	start := make(chan struct{})
	type result struct {
		job     domain.Job
		existed bool
		err     error
	}
	results := make(chan result, callers)
	var group sync.WaitGroup
	for index := 0; index < callers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			job, existed, err := store.CreateOrGetWithItems(context.Background(), jobs.Spec{
				Kind: "download", Profile: "profile-a", IdempotencyKey: "concurrent-fixed-targets",
			}, []string{"item-a", "item-b", "caller-" + strconv.Itoa(index)})
			results <- result{job: job, existed: existed, err: err}
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	var first domain.JobID
	created := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("CreateOrGetWithItems() error: %v", result.err)
		}
		if first == "" {
			first = result.job.ID
		}
		if result.job.ID != first {
			t.Fatalf("job IDs differ: first=%s got=%s", first, result.job.ID)
		}
		if !result.existed {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created callers=%d, want 1", created)
	}
	items, err := store.ListItems(context.Background(), first)
	if err != nil || len(items) != 3 {
		t.Fatalf("items=%#v err=%v", items, err)
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
