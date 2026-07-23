package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
)

type SchedulerPermitStore struct {
	database *Database
}

func NewSchedulerPermitStore(database *Database) *SchedulerPermitStore {
	return &SchedulerPermitStore{database: database}
}

func (store *SchedulerPermitStore) TryAcquire(ctx context.Context, request jobs.PermitRequest) (jobs.Permit, bool, error) {
	if store == nil || store.database == nil {
		return jobs.Permit{}, false, errors.New("scheduler permit database is required")
	}
	if strings.TrimSpace(request.Owner) == "" {
		return jobs.Permit{}, false, errors.New("scheduler permit owner is required")
	}
	if request.LeaseDuration < time.Millisecond {
		return jobs.Permit{}, false, errors.New("scheduler permit lease duration must be at least one millisecond")
	}
	permit := jobs.Permit{ID: uuid.NewString(), Owner: request.Owner}
	acquired := false
	connection, err := store.database.db.Conn(ctx)
	if err != nil {
		return jobs.Permit{}, false, fmt.Errorf("acquire scheduler connection: %w", err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return jobs.Permit{}, false, fmt.Errorf("begin scheduler permit transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	now, err := schedulerDatabaseNow(ctx, connection)
	if err != nil {
		return jobs.Permit{}, false, err
	}
	if _, err := connection.ExecContext(ctx, `DELETE FROM scheduler_permits
WHERE profile_id=? AND expires_at<=?`, store.database.profileID, now.UnixMilli()); err != nil {
		return jobs.Permit{}, false, err
	}
	var global, operation, host, sensitive int
	if err := connection.QueryRowContext(ctx, `SELECT
COUNT(*),
COALESCE(SUM(CASE WHEN operation=? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN host=? THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN sensitive=1 THEN 1 ELSE 0 END), 0)
FROM scheduler_permits WHERE profile_id=? AND expires_at>?`, request.Operation, request.Host,
		store.database.profileID, now.UnixMilli()).Scan(&global, &operation, &host, &sensitive); err != nil {
		return jobs.Permit{}, false, err
	}
	operationLimit := request.Limits.PerOperation[request.Operation]
	if operationLimit <= 0 {
		operationLimit = request.Limits.Global
	}
	if global < request.Limits.Global && operation < operationLimit && host < request.Limits.PerHost &&
		(!request.Sensitive || sensitive < request.Limits.Sensitive) {
		if _, err := connection.ExecContext(ctx, `INSERT INTO scheduler_permits(
id, profile_id, owner, operation, host, sensitive, acquired_at, renewed_at, expires_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, permit.ID, store.database.profileID, permit.Owner,
			request.Operation, request.Host, request.Sensitive, now.UnixMilli(), now.UnixMilli(), now.Add(request.LeaseDuration).UnixMilli()); err != nil {
			return jobs.Permit{}, false, err
		}
		acquired = true
	}
	if _, err := connection.ExecContext(ctx, "COMMIT"); err != nil {
		return jobs.Permit{}, false, err
	}
	committed = true
	// BEGIN IMMEDIATE serializes count-and-insert across separate SQLite
	// connections. A deferred transaction would let two readers observe the
	// same capacity and both insert.
	if !acquired {
		return jobs.Permit{}, false, nil
	}
	return permit, true, nil
}

func (store *SchedulerPermitStore) Renew(ctx context.Context, permit jobs.Permit, _ time.Time, duration time.Duration) (bool, error) {
	if store == nil || store.database == nil {
		return false, errors.New("scheduler permit database is required")
	}
	if permit.ID == "" || permit.Owner == "" || duration < time.Millisecond {
		return false, errors.New("scheduler permit ID, owner, and duration of at least one millisecond are required")
	}
	connection, err := store.database.db.Conn(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire scheduler connection: %w", err)
	}
	defer connection.Close()
	result, err := connection.ExecContext(ctx, `UPDATE scheduler_permits
SET renewed_at=CAST(unixepoch('subsec') * 1000 AS INTEGER),
    expires_at=CAST(unixepoch('subsec') * 1000 AS INTEGER) + ?
WHERE id=? AND profile_id=? AND owner=?
  AND expires_at>CAST(unixepoch('subsec') * 1000 AS INTEGER)`, duration.Milliseconds(),
		permit.ID, store.database.profileID, permit.Owner)
	if err != nil {
		return false, fmt.Errorf("renew scheduler permit: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

type schedulerTimeQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func schedulerDatabaseNow(ctx context.Context, querier schedulerTimeQuerier) (time.Time, error) {
	var milliseconds int64
	if err := querier.QueryRowContext(ctx, `SELECT CAST(unixepoch('subsec') * 1000 AS INTEGER)`).Scan(&milliseconds); err != nil {
		return time.Time{}, fmt.Errorf("read scheduler database time: %w", err)
	}
	return time.UnixMilli(milliseconds), nil
}

func (store *SchedulerPermitStore) Release(ctx context.Context, permit jobs.Permit) error {
	if store == nil || store.database == nil {
		return errors.New("scheduler permit database is required")
	}
	if permit.ID == "" || permit.Owner == "" {
		return errors.New("scheduler permit ID and owner are required")
	}
	_, err := store.database.db.ExecContext(ctx, `DELETE FROM scheduler_permits
WHERE id=? AND profile_id=? AND owner=?`, permit.ID, store.database.profileID, permit.Owner)
	if err != nil {
		return fmt.Errorf("release scheduler permit: %w", err)
	}
	return nil
}

var _ jobs.PermitStore = (*SchedulerPermitStore)(nil)
