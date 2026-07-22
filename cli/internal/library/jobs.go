package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

type JobStore struct {
	database *Database
	now      func() time.Time
}

type JobItem = jobs.Item

type JobAttempt = jobs.Attempt

func NewJobStore(database *Database) *JobStore {
	return &JobStore{database: database, now: time.Now}
}

func (store *JobStore) Create(ctx context.Context, spec jobs.Spec) (domain.Job, error) {
	profile := spec.Profile
	if profile == "" {
		profile = store.database.profileID
	}
	payload, err := marshalRedacted(spec.Payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode job payload: %w", err)
	}
	now := store.now()
	job := domain.Job{ID: domain.JobID(uuid.NewString()), Kind: spec.Kind, Profile: profile, State: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	if spec.IdempotencyKey != "" {
		var existing string
		err := store.database.db.QueryRowContext(ctx, `SELECT id FROM jobs WHERE profile_id=? AND kind=? AND idempotency_key=?`,
			profile, spec.Kind, spec.IdempotencyKey).Scan(&existing)
		if err == nil {
			return store.Get(ctx, domain.JobID(existing))
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, err
		}
	}
	storedKey := spec.IdempotencyKey
	if storedKey == "" {
		storedKey = "job:" + string(job.ID)
	}
	_, err = store.database.db.ExecContext(ctx, `INSERT INTO jobs(
id, profile_id, kind, state, idempotency_key, payload_json, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, job.ID, profile, job.Kind, job.State, storedKey, string(payload), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return domain.Job{}, fmt.Errorf("create job: %w", err)
	}
	return job, nil
}

func (store *JobStore) CreateWithItems(ctx context.Context, spec jobs.Spec, itemKeys []string) (domain.Job, error) {
	var job domain.Job
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		profile := spec.Profile
		if profile == "" {
			profile = store.database.profileID
		}
		payload, err := marshalRedacted(spec.Payload)
		if err != nil {
			return err
		}
		now := store.now()
		job = domain.Job{ID: domain.JobID(uuid.NewString()), Kind: spec.Kind, Profile: profile, State: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
		if spec.IdempotencyKey != "" {
			var existing string
			err := transaction.QueryRowContext(ctx, `SELECT id FROM jobs WHERE profile_id=? AND kind=? AND idempotency_key=?`,
				profile, spec.Kind, spec.IdempotencyKey).Scan(&existing)
			if err == nil {
				job, err = scanJobRow(transaction.QueryRowContext(ctx, `SELECT id, kind, state, profile_id, created_at, updated_at FROM jobs WHERE id=?`, existing))
				if err != nil {
					return err
				}
				return insertMissingJobItems(ctx, transaction, job.ID, itemKeys, now)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		storedKey := spec.IdempotencyKey
		if storedKey == "" {
			storedKey = "job:" + string(job.ID)
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO jobs(
id, profile_id, kind, state, idempotency_key, payload_json, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, job.ID, profile, job.Kind, job.State, storedKey, string(payload), now.UnixMilli(), now.UnixMilli()); err != nil {
			return err
		}
		return insertMissingJobItems(ctx, transaction, job.ID, itemKeys, now)
	})
	return job, err
}

func insertMissingJobItems(ctx context.Context, transaction *sql.Tx, jobID domain.JobID, itemKeys []string, now time.Time) error {
	seen := make(map[string]struct{}, len(itemKeys))
	for _, key := range itemKeys {
		if key == "" {
			return errors.New("job item key must not be empty")
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO job_items(
id, job_id, item_key, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(job_id, item_key) DO NOTHING`,
			uuid.NewString(), jobID, key, domain.JobQueued, now.UnixMilli(), now.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

func (store *JobStore) ListItems(ctx context.Context, id domain.JobID) ([]JobItem, error) {
	rows, err := store.database.db.QueryContext(ctx, `SELECT id, job_id, item_key, state, attempt_count,
checkpoint_json, error_class, error_message, created_at, updated_at FROM job_items
WHERE job_id=? ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]JobItem, 0)
	for rows.Next() {
		var item JobItem
		var checkpoint string
		var created, updated int64
		if err := rows.Scan(&item.ID, &item.JobID, &item.Key, &item.State, &item.AttemptCount,
			&checkpoint, &item.ErrorClass, &item.ErrorMessage, &created, &updated); err != nil {
			return nil, err
		}
		item.Checkpoint = json.RawMessage(checkpoint)
		item.CreatedAt = time.UnixMilli(created)
		item.UpdatedAt = time.UnixMilli(updated)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *JobStore) UpdateItem(
	ctx context.Context,
	itemID string,
	from, to domain.JobState,
	checkpoint any,
	failureClass jobs.FailureClass,
	errorMessage string,
) error {
	var jobID domain.JobID
	if err := store.database.db.QueryRowContext(ctx, `SELECT job_id FROM job_items WHERE id=?`, itemID).Scan(&jobID); err != nil {
		return err
	}
	_, err := store.transitionItem(ctx, jobID, itemID, "", from, to, checkpoint, failureClass, errorMessage, false)
	return err
}

func (store *JobStore) BeginAttempt(ctx context.Context, item jobs.Item, routeID, requestID string) (jobs.Attempt, error) {
	now := store.now()
	attempt := jobs.Attempt{JobID: item.JobID, ItemID: item.ID, Number: item.AttemptCount + 1, RouteID: routeID, RequestID: requestID, StartedAt: now}
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE job_items SET attempt_count=?, updated_at=? WHERE id=? AND job_id=? AND attempt_count=?`,
			attempt.Number, now.UnixMilli(), item.ID, item.JobID, item.AttemptCount)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		_, err = transaction.ExecContext(ctx, `INSERT INTO job_attempts(
job_id, item_id, attempt_number, route_id, request_id, started_at) VALUES(?, ?, ?, NULLIF(?, ''), ?, ?)`,
			item.JobID, item.ID, attempt.Number, redactString(routeID), redactString(requestID), now.UnixMilli())
		return err
	})
	return attempt, err
}

func (store *JobStore) FinishAttempt(ctx context.Context, attempt jobs.Attempt) error {
	// Redact again at the durable boundary even when the engine already
	// supplied a sanitized attempt.
	_, err := store.database.db.ExecContext(ctx, `UPDATE job_attempts SET failure_class=?, error_message=?, completed_at=?
WHERE job_id=? AND item_id=? AND attempt_number=?`, attempt.FailureClass, redactString(attempt.ErrorMessage),
		store.now().UnixMilli(), attempt.JobID, attempt.ItemID, attempt.Number)
	return err
}

func (store *JobStore) AppendLog(ctx context.Context, jobID domain.JobID, itemID, level, message string, fields any) error {
	encoded, err := marshalRedacted(fields)
	if err != nil {
		return err
	}
	_, err = store.database.db.ExecContext(ctx, `INSERT INTO job_logs(job_id, item_id, level, message, fields_json, created_at)
VALUES(?, NULLIF(?, ''), ?, ?, ?, ?)`, jobID, itemID, level, redactString(message), string(encoded), store.now().UnixMilli())
	return err
}

func (store *JobStore) RecoverStale(ctx context.Context) (int64, error) {
	now := store.now().UnixMilli()
	var recovered int64
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		rows, err := transaction.QueryContext(ctx, `SELECT id FROM jobs
WHERE profile_id=? AND state=? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?`,
			store.database.profileID, domain.JobRunning, now)
		if err != nil {
			return err
		}
		var ids []domain.JobID
		for rows.Next() {
			var id domain.JobID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := transaction.ExecContext(ctx, `UPDATE job_attempts SET failure_class=?, error_message=?, completed_at=?
WHERE job_id=? AND completed_at IS NULL`, jobs.FailureInterrupted, "executor lease expired", now, id); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class=?, error_message=?,
updated_at=?, completed_at=NULL WHERE job_id=? AND state=?`, domain.JobQueued, jobs.FailureInterrupted,
				"executor lease expired; resume from checkpoint", now, id, domain.JobRunning); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=NULL WHERE id=? AND state=?`, domain.JobQueued, now, id, domain.JobRunning); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

func (store *JobStore) Get(ctx context.Context, id domain.JobID) (domain.Job, error) {
	return store.scanJob(store.database.db.QueryRowContext(ctx, `SELECT id, kind, state, profile_id, created_at, updated_at
FROM jobs WHERE id=? AND profile_id=?`, id, store.database.profileID))
}

func (store *JobStore) Query(ctx context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	limit, offset := normalizePage(query.Limit, query.Offset)
	where := []string{"profile_id = ?"}
	arguments := []any{store.database.profileID}
	if query.Kind != "" {
		where = append(where, "kind = ?")
		arguments = append(arguments, query.Kind)
	}
	if len(query.States) > 0 {
		placeholders := make([]string, len(query.States))
		for index, state := range query.States {
			placeholders[index] = "?"
			arguments = append(arguments, state)
		}
		where = append(where, "state IN ("+strings.Join(placeholders, ",")+")")
	}
	predicate := strings.Join(where, " AND ")
	var total int
	if err := store.database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE "+predicate, arguments...).Scan(&total); err != nil {
		return domain.Page[domain.Job]{}, err
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT id, kind, state, profile_id, created_at, updated_at
FROM jobs WHERE `+predicate+` ORDER BY created_at DESC, id LIMIT ? OFFSET ?`, append(arguments, limit, offset)...)
	if err != nil {
		return domain.Page[domain.Job]{}, err
	}
	defer rows.Close()
	items := make([]domain.Job, 0)
	for rows.Next() {
		job, err := scanJobRow(rows)
		if err != nil {
			return domain.Page[domain.Job]{}, err
		}
		items = append(items, job)
	}
	return domain.Page[domain.Job]{Items: items, Total: total, Offset: offset, Limit: limit}, rows.Err()
}

func (store *JobStore) Cancel(ctx context.Context, id domain.JobID) (domain.Job, error) {
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var current domain.JobState
		if err := transaction.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND profile_id=?`, id, store.database.profileID).Scan(&current); err != nil {
			return err
		}
		if err := jobs.ValidateTransition(current, domain.JobCancelled); err != nil {
			return err
		}
		result, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=? WHERE id=? AND profile_id=? AND state=?`,
			domain.JobCancelled, now, now, id, store.database.profileID, current)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		_, err = transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class=?, error_message=?,
updated_at=?, completed_at=? WHERE job_id=? AND state IN (?, ?)`, domain.JobCancelled, jobs.FailureInterrupted,
			"job cancelled", now, now, id, domain.JobQueued, domain.JobRunning)
		return err
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) Pause(ctx context.Context, id domain.JobID) (domain.Job, error) {
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var current domain.JobState
		if err := transaction.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND profile_id=?`, id, store.database.profileID).Scan(&current); err != nil {
			return err
		}
		if err := jobs.ValidateTransition(current, domain.JobPaused); err != nil {
			return err
		}
		result, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=NULL WHERE id=? AND profile_id=? AND state=?`,
			domain.JobPaused, now, id, store.database.profileID, current)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class=?, error_message=?,
updated_at=?, completed_at=NULL WHERE job_id=? AND state=?`, domain.JobPaused, jobs.FailureInterrupted,
			"job paused; resume from checkpoint", now, id, domain.JobRunning); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) Resume(ctx context.Context, id domain.JobID) (domain.Job, error) {
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var current domain.JobState
		if err := transaction.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND profile_id=?`, id, store.database.profileID).Scan(&current); err != nil {
			return err
		}
		if current != domain.JobPaused && current != domain.JobBlockedAuth {
			return fmt.Errorf("%s -> %s: %w", current, domain.JobQueued, jobs.ErrInvalidTransition)
		}
		result, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=NULL WHERE id=? AND profile_id=? AND state=?`, domain.JobQueued, now, id,
			store.database.profileID, current)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		_, err = transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class='', error_message='',
updated_at=?, completed_at=NULL WHERE job_id=? AND state=?`, domain.JobQueued, now, id, current)
		return err
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) Retry(ctx context.Context, id domain.JobID) (domain.Job, error) {
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var current domain.JobState
		if err := transaction.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=? AND profile_id=?`, id, store.database.profileID).Scan(&current); err != nil {
			return err
		}
		if err := jobs.ValidateTransition(current, domain.JobQueued); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class='', error_message='',
updated_at=?, completed_at=NULL WHERE job_id=? AND state IN (?, ?, ?)`, domain.JobQueued, now, id,
			domain.JobFailed, domain.JobPartial, domain.JobCancelled); err != nil {
			return err
		}
		result, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=NULL WHERE id=? AND profile_id=? AND state=?`,
			domain.JobQueued, now, id, store.database.profileID, current)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		return nil
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) Transition(ctx context.Context, id domain.JobID, to domain.JobState) (domain.Job, error) {
	current, err := store.Get(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if err := jobs.ValidateTransition(current.State, to); err != nil {
		return domain.Job{}, err
	}
	now := store.now().UnixMilli()
	completed := any(nil)
	if to == domain.JobCompleted || to == domain.JobPartial || to == domain.JobFailed || to == domain.JobCancelled {
		completed = now
	}
	result, err := store.database.db.ExecContext(ctx, `UPDATE jobs SET state=?, updated_at=?, completed_at=?
WHERE id=? AND profile_id=? AND state=?`, to, now, completed, id, store.database.profileID, current.State)
	if err != nil {
		return domain.Job{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.Job{}, jobs.ErrStateChanged
	}
	return store.Get(ctx, id)
}

func (store *JobStore) AcquireLease(ctx context.Context, id domain.JobID, owner string, duration time.Duration) (bool, error) {
	if owner == "" || duration <= 0 {
		return false, errors.New("lease owner and positive duration are required")
	}
	currentTime := store.now()
	now := currentTime.UnixMilli()
	expires := currentTime.Add(duration).UnixMilli()
	result, err := store.database.db.ExecContext(ctx, `UPDATE jobs SET lease_owner=?, lease_expires_at=?, updated_at=?
WHERE id=? AND profile_id=? AND (lease_expires_at IS NULL OR lease_expires_at < ? OR lease_owner=?)`,
		owner, expires, now, id, store.database.profileID, now, owner)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (store *JobStore) StartJob(ctx context.Context, id domain.JobID, owner string, duration time.Duration) (domain.Job, error) {
	if owner == "" || duration <= 0 {
		return domain.Job{}, errors.New("lease owner and positive duration are required")
	}
	currentTime := store.now()
	now := currentTime.UnixMilli()
	expires := currentTime.Add(duration).UnixMilli()
	result, err := store.database.db.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner=?, lease_expires_at=?,
started_at=COALESCE(started_at, ?), updated_at=?, completed_at=NULL
WHERE id=? AND profile_id=? AND state=? AND (lease_expires_at IS NULL OR lease_expires_at < ? OR lease_owner=?)`,
		domain.JobRunning, owner, expires, now, now, id, store.database.profileID, domain.JobQueued, now, owner)
	if err != nil {
		return domain.Job{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return domain.Job{}, jobs.ErrLeaseUnavailable
	}
	return store.Get(ctx, id)
}

func (store *JobStore) RenewLease(ctx context.Context, id domain.JobID, owner string, duration time.Duration) (bool, error) {
	if owner == "" || duration <= 0 {
		return false, errors.New("lease owner and positive duration are required")
	}
	currentTime := store.now()
	result, err := store.database.db.ExecContext(ctx, `UPDATE jobs SET lease_expires_at=?, updated_at=?
WHERE id=? AND profile_id=? AND state=? AND lease_owner=?`, currentTime.Add(duration).UnixMilli(), currentTime.UnixMilli(),
		id, store.database.profileID, domain.JobRunning, owner)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (store *JobStore) ClaimItem(ctx context.Context, jobID domain.JobID, itemID, owner string) (jobs.Item, error) {
	now := store.now().UnixMilli()
	result, err := store.database.db.ExecContext(ctx, `UPDATE job_items SET state=?, started_at=COALESCE(started_at, ?),
updated_at=?, completed_at=NULL, error_class='', error_message=''
WHERE id=? AND job_id=? AND state=? AND EXISTS (
  SELECT 1 FROM jobs WHERE id=? AND profile_id=? AND state=? AND lease_owner=? AND lease_expires_at>=?
)`, domain.JobRunning, now, now, itemID, jobID, domain.JobQueued, jobID, store.database.profileID,
		domain.JobRunning, owner, now)
	if err != nil {
		return jobs.Item{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.Item{}, jobs.ErrStateChanged
	}
	return store.getItem(ctx, itemID)
}

func (store *JobStore) SaveCheckpoint(ctx context.Context, jobID domain.JobID, itemID, owner string, checkpoint any) error {
	checkpointBytes, err := marshalRedacted(checkpoint)
	if err != nil {
		return err
	}
	now := store.now().UnixMilli()
	result, err := store.database.db.ExecContext(ctx, `UPDATE job_items SET checkpoint_json=?, updated_at=?
WHERE id=? AND job_id=? AND state=? AND EXISTS (
  SELECT 1 FROM jobs WHERE id=? AND profile_id=? AND state=? AND lease_owner=? AND lease_expires_at>=?
)`, string(checkpointBytes), now, itemID, jobID, domain.JobRunning, jobID, store.database.profileID,
		domain.JobRunning, owner, now)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.ErrStateChanged
	}
	return nil
}

func (store *JobStore) TransitionItem(
	ctx context.Context,
	jobID domain.JobID,
	itemID, owner string,
	from, to domain.JobState,
	checkpoint any,
	failureClass jobs.FailureClass,
	errorMessage string,
) (jobs.Item, error) {
	return store.transitionItem(ctx, jobID, itemID, owner, from, to, checkpoint, failureClass, errorMessage, true)
}

func (store *JobStore) transitionItem(
	ctx context.Context,
	jobID domain.JobID,
	itemID, owner string,
	from, to domain.JobState,
	checkpoint any,
	failureClass jobs.FailureClass,
	errorMessage string,
	requireLease bool,
) (jobs.Item, error) {
	if err := jobs.ValidateTransition(from, to); err != nil {
		return jobs.Item{}, err
	}
	checkpointBytes, err := marshalRedacted(checkpoint)
	if err != nil {
		return jobs.Item{}, err
	}
	now := store.now().UnixMilli()
	completed := any(nil)
	if isTerminalItemState(to) {
		completed = now
	}
	query := `UPDATE job_items SET state=?, checkpoint_json=CASE WHEN ?='null' THEN checkpoint_json ELSE ? END,
error_class=?, error_message=?, updated_at=?, completed_at=? WHERE id=? AND job_id=? AND state=?`
	arguments := []any{to, string(checkpointBytes), string(checkpointBytes), failureClass, redactString(errorMessage), now, completed, itemID, jobID, from}
	if requireLease {
		query += ` AND EXISTS (SELECT 1 FROM jobs WHERE id=? AND profile_id=? AND state=? AND lease_owner=? AND lease_expires_at>=?)`
		arguments = append(arguments, jobID, store.database.profileID, domain.JobRunning, owner, now)
	}
	result, err := store.database.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		return jobs.Item{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.Item{}, jobs.ErrStateChanged
	}
	return store.getItem(ctx, itemID)
}

func (store *JobStore) BlockAuthentication(ctx context.Context, id domain.JobID, itemID, owner, message string) (domain.Job, error) {
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class=?, error_message=?,
updated_at=?, completed_at=NULL WHERE id=? AND job_id=? AND state=?`, domain.JobBlockedAuth, jobs.FailureAuthentication,
			redactString(message), now, itemID, id, domain.JobRunning)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE job_items SET state=?, error_class=?, error_message=?,
updated_at=?, completed_at=NULL WHERE job_id=? AND state=?`, domain.JobBlockedAuth, jobs.FailureAuthentication,
			"job blocked until authentication is restored", now, id, domain.JobQueued); err != nil {
			return err
		}
		result, err = transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=NULL WHERE id=? AND profile_id=? AND state=? AND lease_owner=?`,
			domain.JobBlockedAuth, now, id, store.database.profileID, domain.JobRunning, owner)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		return nil
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) FinalizeJob(ctx context.Context, id domain.JobID, owner string) (domain.Job, error) {
	var finalized domain.JobState
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var total, queued, running, completed, failed int
		if err := transaction.QueryRowContext(ctx, `SELECT
COUNT(*),
SUM(CASE WHEN state=? THEN 1 ELSE 0 END),
SUM(CASE WHEN state=? THEN 1 ELSE 0 END),
SUM(CASE WHEN state=? THEN 1 ELSE 0 END),
SUM(CASE WHEN state IN (?, ?) THEN 1 ELSE 0 END)
FROM job_items WHERE job_id=?`, domain.JobQueued, domain.JobRunning, domain.JobCompleted,
			domain.JobFailed, domain.JobPartial, id).Scan(&total, &queued, &running, &completed, &failed); err != nil {
			return err
		}
		if queued > 0 || running > 0 {
			return errors.New("cannot finalize job with unfinished items")
		}
		switch {
		case total == 0 || failed == 0:
			finalized = domain.JobCompleted
		case completed > 0:
			finalized = domain.JobPartial
		default:
			finalized = domain.JobFailed
		}
		result, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=? WHERE id=? AND profile_id=? AND state=? AND lease_owner=?`, finalized, now, now,
			id, store.database.profileID, domain.JobRunning, owner)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return jobs.ErrStateChanged
		}
		return nil
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) getItem(ctx context.Context, itemID string) (jobs.Item, error) {
	var item jobs.Item
	var checkpoint string
	var created, updated int64
	err := store.database.db.QueryRowContext(ctx, `SELECT id, job_id, item_key, state, attempt_count,
checkpoint_json, error_class, error_message, created_at, updated_at FROM job_items WHERE id=?`, itemID).Scan(
		&item.ID, &item.JobID, &item.Key, &item.State, &item.AttemptCount, &checkpoint, &item.ErrorClass,
		&item.ErrorMessage, &created, &updated)
	if err != nil {
		return jobs.Item{}, err
	}
	item.Checkpoint = json.RawMessage(checkpoint)
	item.CreatedAt = time.UnixMilli(created)
	item.UpdatedAt = time.UnixMilli(updated)
	return item, nil
}

func isTerminalItemState(state domain.JobState) bool {
	return state == domain.JobCompleted || state == domain.JobPartial || state == domain.JobFailed || state == domain.JobCancelled
}

func (store *JobStore) scanJob(row *sql.Row) (domain.Job, error) {
	var job domain.Job
	var created, updated int64
	if err := row.Scan(&job.ID, &job.Kind, &job.State, &job.Profile, &created, &updated); err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt = time.UnixMilli(created)
	job.UpdatedAt = time.UnixMilli(updated)
	return job, nil
}

type rowScanner interface{ Scan(...any) error }

func scanJobRow(row rowScanner) (domain.Job, error) {
	var job domain.Job
	var created, updated int64
	if err := row.Scan(&job.ID, &job.Kind, &job.State, &job.Profile, &created, &updated); err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt = time.UnixMilli(created)
	job.UpdatedAt = time.UnixMilli(updated)
	return job, nil
}

var _ jobs.Manager = (*JobStore)(nil)
var _ jobs.EngineStore = (*JobStore)(nil)

func marshalRedacted(value any) ([]byte, error) {
	return json.Marshal(safety.Redact(value, ""))
}

func redactString(value string) string {
	redacted, _ := safety.Redact(value, "").(string)
	return redacted
}
