package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
)

type JobStore struct {
	database *Database
	now      func() time.Time
	admit    func(context.Context) (func() error, error)
	admitMu  sync.RWMutex
}

type admissionContextKey struct{}

type JobItem = jobs.Item

type JobAttempt = jobs.Attempt

type JobLog struct {
	ID        int64          `json:"id"`
	JobID     domain.JobID   `json:"jobId"`
	ItemID    string         `json:"itemId,omitempty"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type JobLogBudget struct {
	MaximumRows       int
	MaximumRawBytes   int
	MaximumEntryBytes int
}

type RegisteredJobObject struct {
	Object    objects.Object
	CreatedAt time.Time
}

type JobLease struct {
	Owner     string    `json:"owner,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	Active    bool      `json:"active"`
}

type RestoreBlocker struct {
	JobID        domain.JobID    `json:"jobId"`
	Kind         string          `json:"kind"`
	State        domain.JobState `json:"state"`
	LeaseOwner   string          `json:"leaseOwner,omitempty"`
	LeaseExpires time.Time       `json:"leaseExpires,omitempty"`
}

func NewJobStore(database *Database) *JobStore {
	return &JobStore{database: database, now: time.Now}
}

// SetAdmissionGuard installs the profile maintenance admission barrier used by
// process-composed runtimes. Tests and package-local stores may leave it nil.
// The returned release function is held across the state mutation that admits
// new work, so restore cannot pass its blocker check concurrently.
func (store *JobStore) SetAdmissionGuard(guard func(context.Context) (func() error, error)) {
	if store != nil {
		store.admitMu.Lock()
		defer store.admitMu.Unlock()
		store.admit = guard
	}
}

func (store *JobStore) withAdmission(ctx context.Context, operation func() error) (resultErr error) {
	if admitted, _ := ctx.Value(admissionContextKey{}).(bool); admitted {
		return operation()
	}
	if store == nil {
		return operation()
	}
	store.admitMu.RLock()
	guard := store.admit
	store.admitMu.RUnlock()
	if guard == nil {
		return operation()
	}
	release, err := guard(ctx)
	if err != nil {
		return err
	}
	defer func() {
		releaseErr := release()
		if resultErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		}
	}()
	resultErr = operation()
	// The guarded mutation may already be durably committed when releasing the
	// maintenance lock fails. Reporting that successful mutation as failed can
	// cause callers to repeat non-idempotent work, so release errors are only
	// joined to an operation error. A later maintenance acquisition still
	// performs its own authoritative lock check.
	return resultErr
}

// WithAdmission holds the profile maintenance admission barrier across a
// compound operation. Calls back into JobStore with the supplied context do
// not reacquire the same cross-process lock.
func (store *JobStore) WithAdmission(ctx context.Context, operation func(context.Context) error) error {
	if operation == nil {
		return errors.New("admitted operation is required")
	}
	return store.withAdmission(ctx, func() error {
		return operation(context.WithValue(ctx, admissionContextKey{}, true))
	})
}

func (store *JobStore) Create(ctx context.Context, spec jobs.Spec) (domain.Job, error) {
	var created domain.Job
	err := store.withAdmission(ctx, func() error {
		var err error
		created, err = store.create(ctx, spec)
		return err
	})
	return created, err
}

func (store *JobStore) create(ctx context.Context, spec jobs.Spec) (domain.Job, error) {
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
	var created domain.Job
	err := store.withAdmission(ctx, func() error {
		var err error
		created, err = store.createWithItems(ctx, spec, itemKeys)
		return err
	})
	return created, err
}

// CreateWithItemsAndObjects atomically makes object metadata reachable from
// durable job-item pins. Callers may write immutable object bytes before this
// transaction; garbage collection cannot collect an object before both its
// metadata row and the job reference become visible together.
func (store *JobStore) CreateWithItemsAndObjects(
	ctx context.Context,
	spec jobs.Spec,
	itemKeys []string,
	registered []RegisteredJobObject,
) (domain.Job, error) {
	var created domain.Job
	err := store.withAdmission(ctx, func() error {
		return store.database.WithTx(ctx, func(transaction *sql.Tx) error {
			for _, registeredObject := range registered {
				object := registeredObject.Object
				if object.Digest == "" || object.Size < 0 {
					return errors.New("registered job object requires a digest and non-negative size")
				}
				createdAt := registeredObject.CreatedAt
				if createdAt.IsZero() {
					createdAt = store.now()
				}
				if _, err := transaction.ExecContext(ctx, `INSERT INTO objects(digest, size_bytes, media_type, created_at)
VALUES(?, ?, ?, ?) ON CONFLICT(digest) DO UPDATE SET
size_bytes=excluded.size_bytes,
media_type=CASE WHEN excluded.media_type<>'' THEN excluded.media_type ELSE objects.media_type END`,
					object.Digest, object.Size, object.MediaType, createdAt.UnixMilli()); err != nil {
					return fmt.Errorf("register job object: %w", err)
				}
			}
			var err error
			created, err = store.createWithItemsTx(ctx, transaction, spec, itemKeys)
			return err
		})
	})
	return created, err
}

func (store *JobStore) createWithItems(ctx context.Context, spec jobs.Spec, itemKeys []string) (domain.Job, error) {
	var job domain.Job
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var err error
		job, err = store.createWithItemsTx(ctx, transaction, spec, itemKeys)
		return err
	})
	return job, err
}

func (store *JobStore) createWithItemsTx(
	ctx context.Context,
	transaction *sql.Tx,
	spec jobs.Spec,
	itemKeys []string,
) (domain.Job, error) {
	profile := spec.Profile
	if profile == "" {
		profile = store.database.profileID
	}
	payload, err := marshalRedacted(spec.Payload)
	if err != nil {
		return domain.Job{}, err
	}
	now := store.now()
	job := domain.Job{ID: domain.JobID(uuid.NewString()), Kind: spec.Kind, Profile: profile, State: domain.JobQueued, CreatedAt: now, UpdatedAt: now}
	if spec.IdempotencyKey != "" {
		var existing string
		err := transaction.QueryRowContext(ctx, `SELECT id FROM jobs WHERE profile_id=? AND kind=? AND idempotency_key=?`,
			profile, spec.Kind, spec.IdempotencyKey).Scan(&existing)
		if err == nil {
			job, err = scanJobRow(transaction.QueryRowContext(ctx, `SELECT id, kind, state, profile_id, created_at, updated_at FROM jobs WHERE id=?`, existing))
			if err != nil {
				return domain.Job{}, err
			}
			return job, insertMissingJobItems(ctx, transaction, job.ID, itemKeys, now)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.Job{}, err
		}
	}
	storedKey := spec.IdempotencyKey
	if storedKey == "" {
		storedKey = "job:" + string(job.ID)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO jobs(
id, profile_id, kind, state, idempotency_key, payload_json, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, job.ID, profile, job.Kind, job.State, storedKey, string(payload), now.UnixMilli(), now.UnixMilli()); err != nil {
		return domain.Job{}, err
	}
	return job, insertMissingJobItems(ctx, transaction, job.ID, itemKeys, now)
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

func (store *JobStore) BeginAttempt(ctx context.Context, item jobs.Item, owner, routeID, requestID string) (jobs.Attempt, error) {
	if strings.TrimSpace(owner) == "" {
		return jobs.Attempt{}, errors.New("attempt owner is required")
	}
	now := store.now()
	attempt := jobs.Attempt{JobID: item.JobID, ItemID: item.ID, Number: item.AttemptCount + 1, RouteID: routeID, RequestID: requestID, StartedAt: now}
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `UPDATE job_items SET attempt_count=?, updated_at=?
WHERE id=? AND job_id=? AND state=? AND attempt_count=? AND EXISTS (
  SELECT 1 FROM jobs WHERE id=? AND profile_id=? AND state=? AND lease_owner=? AND lease_expires_at>=?
)`, attempt.Number, now.UnixMilli(), item.ID, item.JobID, domain.JobRunning, item.AttemptCount,
			item.JobID, store.database.profileID, domain.JobRunning, owner, now.UnixMilli())
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

func (store *JobStore) FinishAttempt(ctx context.Context, attempt jobs.Attempt, owner string) error {
	if strings.TrimSpace(owner) == "" {
		return errors.New("attempt owner is required")
	}
	// Redact again at the durable boundary even when the engine already
	// supplied a sanitized attempt.
	now := store.now().UnixMilli()
	result, err := store.database.db.ExecContext(ctx, `UPDATE job_attempts SET failure_class=?, error_message=?, completed_at=?
WHERE job_id=? AND item_id=? AND attempt_number=? AND completed_at IS NULL AND EXISTS (
  SELECT 1 FROM jobs WHERE id=? AND profile_id=? AND state=? AND lease_owner=? AND lease_expires_at>=?
) AND EXISTS (
  SELECT 1 FROM job_items WHERE id=? AND job_id=? AND state=?
)`, attempt.FailureClass, redactString(attempt.ErrorMessage), now, attempt.JobID, attempt.ItemID, attempt.Number,
		attempt.JobID, store.database.profileID, domain.JobRunning, owner, now,
		attempt.ItemID, attempt.JobID, domain.JobRunning)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return jobs.ErrStateChanged
	}
	return nil
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

// AppendLogOnce records replayable job output without duplicating it when a
// worker resumes from a durable checkpoint after a crash or database error.
// The idempotency key is stored inside the already-redacted JSON object, so no
// schema migration is required and older readers continue to see a normal log.
func (store *JobStore) AppendLogOnce(
	ctx context.Context,
	jobID domain.JobID,
	itemID, level, message string,
	fields map[string]any,
	idempotencyKey string,
) error {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return errors.New("job log idempotency key is required")
	}
	redacted, ok := safety.Redact(fields, "").(map[string]any)
	if !ok || redacted == nil {
		redacted = map[string]any{}
	}
	redacted["idempotencyKey"] = idempotencyKey
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return err
	}
	_, err = store.database.db.ExecContext(ctx, `INSERT INTO job_logs(job_id, item_id, level, message, fields_json, created_at)
SELECT ?, NULLIF(?, ''), ?, ?, ?, ?
WHERE EXISTS (SELECT 1 FROM jobs WHERE id=? AND profile_id=?)
  AND NOT EXISTS (
  SELECT 1 FROM job_logs
  WHERE job_id=? AND COALESCE(item_id, '')=?
    AND json_extract(fields_json, '$.idempotencyKey')=?
)`, jobID, itemID, level, redactString(message), string(encoded), store.now().UnixMilli(), jobID, store.database.profileID,
		jobID, itemID, idempotencyKey)
	return err
}

func (store *JobStore) ListLogs(ctx context.Context, jobID domain.JobID, limit int) ([]JobLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT jl.id, jl.job_id, COALESCE(jl.item_id, ''), jl.level,
jl.message, jl.fields_json, jl.created_at FROM job_logs jl JOIN jobs j ON j.id=jl.job_id
WHERE jl.job_id=? AND j.profile_id=? ORDER BY jl.id DESC LIMIT ?`, jobID, store.database.profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]JobLog, 0)
	for rows.Next() {
		var item JobLog
		var fields string
		var created int64
		if err := rows.Scan(&item.ID, &item.JobID, &item.ItemID, &item.Level, &item.Message, &fields, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = time.UnixMilli(created)
		if strings.TrimSpace(fields) != "" {
			if err := json.Unmarshal([]byte(fields), &item.Fields); err != nil {
				item.Fields = map[string]any{"decodeError": "invalid redacted log fields"}
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *JobStore) ListLogsBounded(ctx context.Context, jobID domain.JobID, budget JobLogBudget) ([]JobLog, error) {
	if budget.MaximumRows <= 0 || budget.MaximumRows > 500 {
		budget.MaximumRows = 100
	}
	if budget.MaximumRawBytes <= 0 {
		return []JobLog{}, nil
	}
	if budget.MaximumEntryBytes <= 0 || budget.MaximumEntryBytes > budget.MaximumRawBytes {
		budget.MaximumEntryBytes = budget.MaximumRawBytes
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT jl.id, jl.job_id, COALESCE(jl.item_id, ''), jl.level,
CAST(substr(CAST(jl.message AS BLOB), 1, ?) AS TEXT),
CAST(substr(CAST(jl.fields_json AS BLOB), 1, ?) AS TEXT), jl.created_at,
length(CAST(jl.message AS BLOB)) + length(CAST(jl.fields_json AS BLOB))
FROM job_logs jl JOIN jobs j ON j.id=jl.job_id
WHERE jl.job_id=? AND j.profile_id=? ORDER BY jl.id DESC LIMIT ?`, budget.MaximumEntryBytes+1, budget.MaximumEntryBytes+1,
		jobID, store.database.profileID, budget.MaximumRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]JobLog, 0)
	remaining := budget.MaximumRawBytes
	for rows.Next() {
		var item JobLog
		var fields string
		var created int64
		var rawBytes int
		if err := rows.Scan(&item.ID, &item.JobID, &item.ItemID, &item.Level, &item.Message, &fields, &created, &rawBytes); err != nil {
			return nil, err
		}
		if remaining <= 0 {
			break
		}
		if rawBytes > remaining {
			item.Message = "diagnostic log entry omitted because it exceeds the remaining byte budget"
			item.Fields = map[string]any{"truncated": true, "rawBytes": rawBytes}
			remaining = 0
		} else {
			remaining -= rawBytes
			if rawBytes > budget.MaximumEntryBytes || len(item.Message) > budget.MaximumEntryBytes || len(fields) > budget.MaximumEntryBytes {
				item.Message = truncateLogText(item.Message, budget.MaximumEntryBytes/4)
				item.Fields = map[string]any{"truncated": true, "rawBytes": rawBytes}
			} else if strings.TrimSpace(fields) != "" {
				if err := json.Unmarshal([]byte(fields), &item.Fields); err != nil {
					item.Fields = map[string]any{"decodeError": "invalid redacted log fields"}
				}
			}
		}
		item.CreatedAt = time.UnixMilli(created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func truncateLogText(value string, maximum int) string {
	if maximum <= 0 || len(value) <= maximum {
		return value
	}
	return value[:maximum] + "...[truncated]"
}

func (store *JobStore) ListAllLogs(ctx context.Context, jobID domain.JobID) ([]JobLog, error) {
	rows, err := store.database.db.QueryContext(ctx, `SELECT jl.id, jl.job_id, COALESCE(jl.item_id, ''), jl.level,
jl.message, jl.fields_json, jl.created_at FROM job_logs jl JOIN jobs j ON j.id=jl.job_id
WHERE jl.job_id=? AND j.profile_id=? ORDER BY jl.id`, jobID, store.database.profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]JobLog, 0)
	for rows.Next() {
		var item JobLog
		var fields string
		var created int64
		if err := rows.Scan(&item.ID, &item.JobID, &item.ItemID, &item.Level, &item.Message, &fields, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = time.UnixMilli(created)
		if strings.TrimSpace(fields) != "" {
			if err := json.Unmarshal([]byte(fields), &item.Fields); err != nil {
				item.Fields = map[string]any{"decodeError": "invalid redacted log fields"}
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *JobStore) Lease(ctx context.Context, jobID domain.JobID) (JobLease, error) {
	var owner string
	var expires sql.NullInt64
	if err := store.database.db.QueryRowContext(ctx, `SELECT lease_owner, lease_expires_at FROM jobs
WHERE id=? AND profile_id=?`, jobID, store.database.profileID).Scan(&owner, &expires); err != nil {
		return JobLease{}, err
	}
	lease := JobLease{Owner: owner}
	if expires.Valid {
		lease.ExpiresAt = time.UnixMilli(expires.Int64)
		lease.Active = !lease.ExpiresAt.Before(store.now())
	}
	return lease, nil
}

func (store *JobStore) RestoreBlockers(ctx context.Context) ([]RestoreBlocker, error) {
	now := store.now().UnixMilli()
	rows, err := store.database.db.QueryContext(ctx, `SELECT id, kind, state, lease_owner, lease_expires_at
FROM jobs WHERE profile_id=? AND (state=? OR (lease_expires_at IS NOT NULL AND lease_expires_at>=?))
ORDER BY created_at, id`, store.database.profileID, domain.JobRunning, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RestoreBlocker, 0)
	for rows.Next() {
		var item RestoreBlocker
		var expires sql.NullInt64
		if err := rows.Scan(&item.JobID, &item.Kind, &item.State, &item.LeaseOwner, &expires); err != nil {
			return nil, err
		}
		if expires.Valid {
			item.LeaseExpires = time.UnixMilli(expires.Int64)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *JobStore) RecoverStale(ctx context.Context) (int64, error) {
	var recovered int64
	err := store.withAdmission(ctx, func() error {
		var err error
		recovered, err = store.recoverStale(ctx)
		return err
	})
	return recovered, err
}

func (store *JobStore) recoverStale(ctx context.Context) (int64, error) {
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
	job, err := store.scanJob(store.database.db.QueryRowContext(ctx, `SELECT id, kind, state, profile_id, created_at, updated_at
FROM jobs WHERE id=? AND profile_id=?`, id, store.database.profileID))
	if err != nil {
		return domain.Job{}, err
	}
	job.Counts, err = store.jobCounts(ctx, job.ID)
	return job, err
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
	if err := rows.Err(); err != nil {
		return domain.Page[domain.Job]{}, err
	}
	counts, err := store.jobCountsForPage(ctx, items)
	if err != nil {
		return domain.Page[domain.Job]{}, err
	}
	for index := range items {
		items[index].Counts = counts[items[index].ID]
	}
	return domain.Page[domain.Job]{Items: items, Total: total, Offset: offset, Limit: limit}, nil
}

func (store *JobStore) jobCountsForPage(ctx context.Context, jobsPage []domain.Job) (map[domain.JobID]map[string]int, error) {
	result := make(map[domain.JobID]map[string]int, len(jobsPage))
	if len(jobsPage) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(jobsPage))
	arguments := make([]any, len(jobsPage))
	for index, job := range jobsPage {
		placeholders[index] = "?"
		arguments[index] = job.ID
		result[job.ID] = map[string]int{"total": 0}
	}
	rows, err := store.database.db.QueryContext(ctx, `SELECT job_id, state, COUNT(*) FROM job_items
WHERE job_id IN (`+strings.Join(placeholders, ",")+`) GROUP BY job_id, state`, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var jobID domain.JobID
		var state string
		var count int
		if err := rows.Scan(&jobID, &state, &count); err != nil {
			return nil, err
		}
		result[jobID][state] = count
		result[jobID]["total"] += count
	}
	return result, rows.Err()
}

func (store *JobStore) jobCounts(ctx context.Context, id domain.JobID) (map[string]int, error) {
	rows, err := store.database.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM job_items WHERE job_id=? GROUP BY state`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{"total": 0}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		counts[state] = count
		counts["total"] += count
	}
	return counts, rows.Err()
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
	return store.pause(ctx, id, "")
}

func (store *JobStore) PauseOwned(ctx context.Context, id domain.JobID, owner string) (domain.Job, error) {
	if strings.TrimSpace(owner) == "" {
		return domain.Job{}, errors.New("pause owner is required")
	}
	return store.pause(ctx, id, owner)
}

func (store *JobStore) pause(ctx context.Context, id domain.JobID, owner string) (domain.Job, error) {
	now := store.now().UnixMilli()
	err := store.database.WithTx(ctx, func(transaction *sql.Tx) error {
		var current domain.JobState
		var leaseOwner string
		var leaseExpires sql.NullInt64
		if err := transaction.QueryRowContext(ctx, `SELECT state, lease_owner, lease_expires_at FROM jobs WHERE id=? AND profile_id=?`,
			id, store.database.profileID).Scan(&current, &leaseOwner, &leaseExpires); err != nil {
			return err
		}
		if owner != "" && (leaseOwner != owner || !leaseExpires.Valid || leaseExpires.Int64 < now) {
			return jobs.ErrStateChanged
		}
		if err := jobs.ValidateTransition(current, domain.JobPaused); err != nil {
			return err
		}
		result, err := transaction.ExecContext(ctx, `UPDATE jobs SET state=?, lease_owner='', lease_expires_at=NULL,
updated_at=?, completed_at=NULL WHERE id=? AND profile_id=? AND state=? AND
(?='' OR (lease_owner=? AND lease_expires_at>=?))`,
			domain.JobPaused, now, id, store.database.profileID, current, owner, owner, now)
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
	var resumed domain.Job
	err := store.withAdmission(ctx, func() error {
		var err error
		resumed, err = store.resume(ctx, id)
		return err
	})
	return resumed, err
}

func (store *JobStore) resume(ctx context.Context, id domain.JobID) (domain.Job, error) {
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
	return store.retryWithAdmission(ctx, id, false)
}

func (store *JobStore) RetryExport(ctx context.Context, id domain.JobID) (domain.Job, error) {
	return store.retryWithAdmission(ctx, id, true)
}

func (store *JobStore) retryWithAdmission(ctx context.Context, id domain.JobID, resetExport bool) (domain.Job, error) {
	var retried domain.Job
	err := store.withAdmission(ctx, func() error {
		var err error
		retried, err = store.retry(ctx, id, resetExport)
		return err
	})
	return retried, err
}

func (store *JobStore) retry(ctx context.Context, id domain.JobID, resetExport bool) (domain.Job, error) {
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
		if resetExport {
			result, err = transaction.ExecContext(ctx, `UPDATE exports SET state=?, completed_at=NULL,
provenance_json='{}', provenance_path='', provenance_sha256='', provenance_state='pending', provenance_error='',
provenance_generation=provenance_generation+1, provenance_claimed_at=NULL
WHERE profile_id=? AND job_id=? AND provenance_state<>'writing'`, domain.JobQueued, store.database.profileID, id)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return jobs.ErrStateChanged
			}
		}
		return nil
	})
	if err != nil {
		return domain.Job{}, err
	}
	return store.Get(ctx, id)
}

func (store *JobStore) Transition(ctx context.Context, id domain.JobID, to domain.JobState) (domain.Job, error) {
	if to == domain.JobQueued {
		var transitioned domain.Job
		err := store.withAdmission(ctx, func() error {
			var err error
			transitioned, err = store.transition(ctx, id, to)
			return err
		})
		return transitioned, err
	}
	return store.transition(ctx, id, to)
}

func (store *JobStore) transition(ctx context.Context, id domain.JobID, to domain.JobState) (domain.Job, error) {
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
	owner = strings.TrimSpace(owner)
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
	var started domain.Job
	err := store.withAdmission(ctx, func() error {
		var err error
		started, err = store.startJob(ctx, id, owner, duration)
		return err
	})
	return started, err
}

func (store *JobStore) startJob(ctx context.Context, id domain.JobID, owner string, duration time.Duration) (domain.Job, error) {
	owner = strings.TrimSpace(owner)
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
	owner = strings.TrimSpace(owner)
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
updated_at=?, completed_at=NULL WHERE id=? AND job_id=? AND state=? AND EXISTS (
  SELECT 1 FROM jobs WHERE id=? AND profile_id=? AND state=? AND lease_owner=? AND lease_expires_at>=?
)`, domain.JobBlockedAuth, jobs.FailureAuthentication, redactString(message), now, itemID, id, domain.JobRunning,
			id, store.database.profileID, domain.JobRunning, owner, now)
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
		var total, queued, running, completed, partial, failed int
		if err := transaction.QueryRowContext(ctx, `SELECT
COUNT(*),
COALESCE(SUM(CASE WHEN state=? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN state=? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN state=? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN state=? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN state=? THEN 1 ELSE 0 END), 0)
FROM job_items WHERE job_id=?`, domain.JobQueued, domain.JobRunning, domain.JobCompleted,
			domain.JobPartial, domain.JobFailed, id).Scan(&total, &queued, &running, &completed, &partial, &failed); err != nil {
			return err
		}
		if queued > 0 || running > 0 {
			return errors.New("cannot finalize job with unfinished items")
		}
		switch {
		case total == 0 || partial == 0 && failed == 0:
			finalized = domain.JobCompleted
		case completed > 0 || partial > 0:
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
