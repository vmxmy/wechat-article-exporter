package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

type IntegrityIssue struct {
	Kind           string `json:"kind"`
	ArticleID      string `json:"articleId,omitempty"`
	ResourceID     string `json:"resourceId,omitempty"`
	ObjectDigest   string `json:"objectDigest,omitempty"`
	Message        string `json:"message"`
	Repairable     bool   `json:"repairable"`
	Recommendation string `json:"recommendation,omitempty"`
}

type IntegrityReport struct {
	CheckedAt time.Time        `json:"checkedAt"`
	Issues    []IntegrityIssue `json:"issues"`
}

type IntegrityOptions struct {
	FullObjectValidation bool
	MarkIncomplete       bool
}

func (database *Database) CheckIntegrity(ctx context.Context, objectStore *objects.FileStore) (IntegrityReport, error) {
	return database.CheckIntegrityWithOptions(ctx, objectStore, IntegrityOptions{
		FullObjectValidation: true,
		MarkIncomplete:       true,
	})
}

func (database *Database) CheckIntegrityWithOptions(
	ctx context.Context,
	objectStore *objects.FileStore,
	options IntegrityOptions,
) (IntegrityReport, error) {
	report := IntegrityReport{CheckedAt: time.Now(), Issues: []IntegrityIssue{}}
	pragma := "PRAGMA quick_check"
	if options.FullObjectValidation {
		pragma = "PRAGMA integrity_check"
	}
	sqliteRows, err := database.db.QueryContext(ctx, pragma)
	if err != nil {
		return report, err
	}
	defer sqliteRows.Close()
	for sqliteRows.Next() {
		var sqliteResult string
		if err := sqliteRows.Scan(&sqliteResult); err != nil {
			return report, err
		}
		if sqliteResult != "ok" {
			report.Issues = append(report.Issues, IntegrityIssue{Kind: "sqlite", Message: sqliteResult, Repairable: false})
		}
	}
	if err := sqliteRows.Err(); err != nil {
		return report, err
	}
	foreignRows, err := database.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return report, err
	}
	for foreignRows.Next() {
		var table string
		var rowID, parent, constraint any
		if err := foreignRows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			foreignRows.Close()
			return report, err
		}
		report.Issues = append(report.Issues, IntegrityIssue{Kind: "foreign-key", Message: fmt.Sprintf("table %s row %v references %v", table, rowID, parent), Repairable: false})
	}
	if err := foreignRows.Err(); err != nil {
		foreignRows.Close()
		return report, err
	}
	if err := foreignRows.Close(); err != nil {
		return report, err
	}
	var schemaMin, schemaVersion, schemaCount int
	if err := database.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(version), 0), COALESCE(MAX(version), 0), COUNT(*)
FROM schema_migrations`).Scan(&schemaMin, &schemaVersion, &schemaCount); err != nil {
		return report, err
	}
	if schemaMin != 1 || schemaVersion != CurrentSchemaVersion || schemaCount != CurrentSchemaVersion {
		report.Issues = append(report.Issues, IntegrityIssue{Kind: "migration-state",
			Message: fmt.Sprintf("database migration set min=%d max=%d count=%d does not match contiguous versions 1-%d",
				schemaMin, schemaVersion, schemaCount, CurrentSchemaVersion), Repairable: false})
	}
	type objectReference struct {
		articleID, resourceID, digest, referenceKind string
	}
	// Integrity, backup, and garbage collection must agree on the complete
	// reachable-object set. The shared rows query also carries the best
	// available article/resource context for actionable repair diagnostics.
	rows, err := database.db.QueryContext(ctx, `SELECT article_id, resource_id, digest, reference_kind
FROM (`+referencedObjectRows+`)
ORDER BY digest, reference_kind, article_id, resource_id`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	validatedDigests := make(map[string]error)
	for rows.Next() {
		var reference objectReference
		if err := rows.Scan(&reference.articleID, &reference.resourceID, &reference.digest, &reference.referenceKind); err != nil {
			return report, err
		}
		if objectStore == nil {
			continue
		}
		validationErr, checked := validatedDigests[reference.digest]
		if !checked {
			if options.FullObjectValidation {
				validationErr = objectStore.Validate(ctx, reference.digest)
			} else {
				_, validationErr = objectStore.Stat(reference.digest)
			}
			validatedDigests[reference.digest] = validationErr
		}
		if validationErr != nil {
			kind := "corrupt-object"
			if errors.Is(validationErr, os.ErrNotExist) || errors.Is(validationErr, filepath.ErrBadPattern) {
				kind = "missing-object"
			}
			recommendation := "redownload the missing or corrupt resource"
			if reference.referenceKind == "content" {
				recommendation = "redownload the article HTML content"
			} else if reference.referenceKind == "comment" || reference.referenceKind == "reply" {
				recommendation = "redownload article comments and replies"
			} else if reference.referenceKind == "debug" {
				recommendation = "remove or recreate the diagnostic capture"
			} else if reference.referenceKind == "job-pin" {
				recommendation = "retry or recreate the queued job snapshot before garbage collection or backup"
			}
			report.Issues = append(report.Issues, IntegrityIssue{
				Kind: kind, ArticleID: reference.articleID, ResourceID: reference.resourceID, ObjectDigest: reference.digest,
				Message: validationErr.Error(), Repairable: true, Recommendation: recommendation,
			})
			if options.MarkIncomplete && reference.articleID != "" {
				if _, updateErr := database.db.ExecContext(ctx, `UPDATE articles SET content_status='incomplete', updated_at=? WHERE id=?`, time.Now().UnixMilli(), reference.articleID); updateErr != nil {
					return report, fmt.Errorf("mark article incomplete: %w", updateErr)
				}
			}
		}
	}
	return report, rows.Err()
}
