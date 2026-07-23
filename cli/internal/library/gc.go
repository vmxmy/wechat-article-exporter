package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

const defaultMetadataRetention = 30 * 24 * time.Hour

type GarbageCollectionOptions struct {
	ObjectStore           *objects.FileStore
	ObjectRetention       time.Duration
	TemporaryRetention    time.Duration
	DebugRetention        time.Duration
	CompletedJobRetention time.Duration
	Now                   time.Time
}

type MetadataGCPlan struct {
	ExpiredDebug     objects.Reclaimable `json:"expiredDebugCaptures"`
	CompletedJobLogs objects.Reclaimable `json:"completedJobLogs"`
}

type GarbageCollectionPlan struct {
	GeneratedAt              time.Time      `json:"generatedAt"`
	DebugDeleteBefore        time.Time      `json:"debugDeleteBefore"`
	CompletedLogDeleteBefore time.Time      `json:"completedLogDeleteBefore"`
	Objects                  objects.GCPlan `json:"objects"`
	Metadata                 MetadataGCPlan `json:"metadata"`
	Confirmation             string         `json:"confirmation"`
	debugIDs                 []string
	jobLogIDs                []int64
}

type GarbageCollectionResult struct {
	Objects              objects.GCResult    `json:"objects"`
	DeletedDebug         objects.Reclaimable `json:"deletedDebugCaptures"`
	DeletedCompletedLogs objects.Reclaimable `json:"deletedCompletedJobLogs"`
}

func (database *Database) PlanGarbageCollection(ctx context.Context, options GarbageCollectionOptions) (GarbageCollectionPlan, error) {
	if options.ObjectStore == nil {
		return GarbageCollectionPlan{}, errors.New("object store is required")
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	if options.DebugRetention <= 0 {
		options.DebugRetention = defaultMetadataRetention
	}
	if options.CompletedJobRetention <= 0 {
		options.CompletedJobRetention = defaultMetadataRetention
	}
	referenced, err := database.referencedObjects(ctx)
	if err != nil {
		return GarbageCollectionPlan{}, err
	}
	objectPlan, err := options.ObjectStore.PlanGarbageCollection(ctx, referenced, objects.RetentionPolicy{
		UnreferencedObjects: options.ObjectRetention,
		TemporaryFiles:      options.TemporaryRetention,
		Now:                 now,
	})
	if err != nil {
		return GarbageCollectionPlan{}, err
	}
	plan := GarbageCollectionPlan{
		GeneratedAt: now, DebugDeleteBefore: now.Add(-options.DebugRetention),
		CompletedLogDeleteBefore: now.Add(-options.CompletedJobRetention),
		Objects:                  objectPlan, debugIDs: []string{}, jobLogIDs: []int64{},
	}
	debugRows, err := database.db.QueryContext(ctx, `SELECT id FROM debug_incidents
WHERE (expires_at IS NOT NULL AND expires_at <= ?) OR created_at <= ? ORDER BY id`,
		now.UnixMilli(), plan.DebugDeleteBefore.UnixMilli())
	if err != nil {
		return GarbageCollectionPlan{}, err
	}
	for debugRows.Next() {
		var id string
		if err := debugRows.Scan(&id); err != nil {
			debugRows.Close()
			return GarbageCollectionPlan{}, err
		}
		plan.debugIDs = append(plan.debugIDs, id)
	}
	if err := debugRows.Close(); err != nil {
		return GarbageCollectionPlan{}, err
	}
	plan.Metadata.ExpiredDebug.Count = len(plan.debugIDs)
	logRows, err := database.db.QueryContext(ctx, `SELECT jl.id, length(jl.message) + length(jl.fields_json)
FROM job_logs jl JOIN jobs j ON j.id=jl.job_id
WHERE j.state IN ('completed', 'partial', 'failed', 'cancelled')
  AND COALESCE(j.completed_at, j.updated_at) <= ?
	ORDER BY jl.id`, plan.CompletedLogDeleteBefore.UnixMilli())
	if err != nil {
		return GarbageCollectionPlan{}, err
	}
	for logRows.Next() {
		var id int64
		var size sql.NullInt64
		if err := logRows.Scan(&id, &size); err != nil {
			logRows.Close()
			return GarbageCollectionPlan{}, err
		}
		plan.jobLogIDs = append(plan.jobLogIDs, id)
		plan.Metadata.CompletedJobLogs.Bytes += size.Int64
	}
	if err := logRows.Close(); err != nil {
		return GarbageCollectionPlan{}, err
	}
	plan.Metadata.CompletedJobLogs.Count = len(plan.jobLogIDs)
	plan.Confirmation = garbageCollectionConfirmation(plan)
	return plan, nil
}

func garbageCollectionConfirmation(plan GarbageCollectionPlan) string {
	hasher := sha256.New()
	for _, candidate := range plan.Objects.Candidates {
		_, _ = fmt.Fprintf(hasher, "object\x00%s\x00%s\x00%d\x00%d\n", candidate.Kind, candidate.Path, candidate.Size,
			candidate.ModifiedAt.UnixNano())
	}
	for _, id := range plan.debugIDs {
		_, _ = fmt.Fprintf(hasher, "debug\x00%s\n", id)
	}
	for _, id := range plan.jobLogIDs {
		_, _ = fmt.Fprintf(hasher, "job-log\x00%s\n", strconv.FormatInt(id, 10))
	}
	digest := hex.EncodeToString(hasher.Sum(nil))[:24]
	return strings.Join([]string{"garbage-collect", strconv.Itoa(plan.Objects.Unreferenced.Count),
		strconv.Itoa(plan.Objects.Temporary.Count), strconv.Itoa(plan.Metadata.ExpiredDebug.Count),
		strconv.Itoa(plan.Metadata.CompletedJobLogs.Count), digest}, ":")
}

func (database *Database) ApplyGarbageCollection(
	ctx context.Context,
	options GarbageCollectionOptions,
	plan GarbageCollectionPlan,
	confirmation string,
) (GarbageCollectionResult, error) {
	if confirmation == "" || confirmation != plan.Confirmation {
		return GarbageCollectionResult{}, fmt.Errorf("garbage collection confirmation is required: use %q", plan.Confirmation)
	}
	if options.ObjectStore == nil {
		return GarbageCollectionResult{}, errors.New("object store is required")
	}
	result := GarbageCollectionResult{}
	if err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		for _, id := range plan.debugIDs {
			deleted, err := deleteExpiredDebug(ctx, transaction, id, plan.GeneratedAt, plan.DebugDeleteBefore)
			if err != nil {
				return err
			}
			if deleted {
				result.DeletedDebug.Count++
			}
		}
		for _, id := range plan.jobLogIDs {
			deleted, bytes, err := deleteCompletedJobLog(ctx, transaction, id, plan.CompletedLogDeleteBefore)
			if err != nil {
				return err
			}
			if deleted {
				result.DeletedCompletedLogs.Count++
				result.DeletedCompletedLogs.Bytes += bytes
			}
		}
		return nil
	}); err != nil {
		return GarbageCollectionResult{}, err
	}
	objectResult, err := options.ObjectStore.ApplyGarbageCollection(ctx, plan.Objects, plan.Objects.Confirmation, database.objectReferenced)
	if err != nil {
		return result, err
	}
	result.Objects = objectResult
	return result, nil
}

func (database *Database) referencedObjects(ctx context.Context) (map[string]struct{}, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT digest FROM objects WHERE digest IN (`+referencedObjectUnion+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	referenced := map[string]struct{}{}
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return nil, err
		}
		referenced[digest] = struct{}{}
	}
	return referenced, rows.Err()
}

func (database *Database) objectReferenced(ctx context.Context, digest string) (bool, error) {
	var count int
	err := database.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects WHERE digest=? AND digest IN (`+referencedObjectUnion+`)`, digest).Scan(&count)
	return count > 0, err
}

func deleteExpiredDebug(ctx context.Context, transaction *sql.Tx, id string, now, deleteBefore time.Time) (bool, error) {
	result, err := transaction.ExecContext(ctx, `DELETE FROM debug_incidents
WHERE id=? AND ((expires_at IS NOT NULL AND expires_at <= ?) OR created_at <= ?)`, id, now.UnixMilli(), deleteBefore.UnixMilli())
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func deleteCompletedJobLog(ctx context.Context, transaction *sql.Tx, id int64, deleteBefore time.Time) (bool, int64, error) {
	var bytes int64
	err := transaction.QueryRowContext(ctx, `SELECT length(jl.message) + length(jl.fields_json)
FROM job_logs jl JOIN jobs j ON j.id=jl.job_id
WHERE jl.id=? AND j.state IN ('completed', 'partial', 'failed', 'cancelled')
	  AND COALESCE(j.completed_at, j.updated_at) <= ?`, id, deleteBefore.UnixMilli()).Scan(&bytes)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	result, err := transaction.ExecContext(ctx, "DELETE FROM job_logs WHERE id=?", id)
	if err != nil {
		return false, 0, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, bytes, err
}
