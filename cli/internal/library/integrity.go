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

func (database *Database) CheckIntegrity(ctx context.Context, objectStore *objects.FileStore) (IntegrityReport, error) {
	report := IntegrityReport{CheckedAt: time.Now(), Issues: []IntegrityIssue{}}
	var sqliteResult string
	if err := database.db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&sqliteResult); err != nil {
		return report, err
	}
	if sqliteResult != "ok" {
		report.Issues = append(report.Issues, IntegrityIssue{Kind: "sqlite", Message: sqliteResult, Repairable: false})
	}
	rows, err := database.db.QueryContext(ctx, `SELECT ar.article_id, ar.resource_id, r.object_digest
FROM article_resources ar JOIN resources r ON r.id=ar.resource_id WHERE r.object_digest IS NOT NULL AND r.object_digest <> ''`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var articleID, resourceID, digest string
		if err := rows.Scan(&articleID, &resourceID, &digest); err != nil {
			return report, err
		}
		if objectStore == nil {
			continue
		}
		if err := objectStore.Validate(ctx, digest); err != nil {
			kind := "corrupt-object"
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, filepath.ErrBadPattern) {
				kind = "missing-object"
			}
			report.Issues = append(report.Issues, IntegrityIssue{
				Kind: kind, ArticleID: articleID, ResourceID: resourceID, ObjectDigest: digest,
				Message: err.Error(), Repairable: true, Recommendation: "redownload the missing or corrupt resource",
			})
			if _, updateErr := database.db.ExecContext(ctx, `UPDATE articles SET content_status='incomplete', updated_at=? WHERE id=?`, time.Now().UnixMilli(), articleID); updateErr != nil {
				return report, fmt.Errorf("mark article incomplete: %w", updateErr)
			}
		}
	}
	return report, rows.Err()
}
