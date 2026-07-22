package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func (database *Database) UpsertCredential(ctx context.Context, metadata credentials.Metadata) (credentials.Metadata, error) {
	if strings.TrimSpace(metadata.ID) == "" || metadata.AccountID == "" || strings.TrimSpace(metadata.SecretRef) == "" {
		return credentials.Metadata{}, errors.New("credential ID, account ID, and secret reference are required")
	}
	if metadata.Kind == "" {
		metadata.Kind = credentials.ArticleKind
	}
	if metadata.Status == "" {
		metadata.Status = credentials.StatusUnknown
	}
	if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = time.Now()
	}
	if metadata.UpdatedAt.IsZero() {
		metadata.UpdatedAt = metadata.CreatedAt
	}
	_, err := database.db.ExecContext(ctx, `INSERT INTO credential_refs(
id, profile_id, account_id, kind, secret_ref, status, validated_at, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(profile_id, kind, secret_ref) DO UPDATE SET
account_id=excluded.account_id, status=excluded.status, validated_at=excluded.validated_at, updated_at=excluded.updated_at`,
		metadata.ID, database.profileID, metadata.AccountID, metadata.Kind, metadata.SecretRef, metadata.Status,
		nullableTime(metadata.ValidatedAt), metadata.CreatedAt.UnixMilli(), metadata.UpdatedAt.UnixMilli())
	if err != nil {
		return credentials.Metadata{}, fmt.Errorf("upsert credential metadata: %w", err)
	}
	return database.CredentialByID(ctx, metadata.ID)
}

func (database *Database) CredentialByID(ctx context.Context, id string) (credentials.Metadata, error) {
	metadata, err := scanCredential(database.db.QueryRowContext(ctx, `SELECT id, account_id, kind, secret_ref, status,
validated_at, created_at, updated_at FROM credential_refs WHERE profile_id=? AND id=?`, database.profileID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return credentials.Metadata{}, credentials.ErrCredentialMissing
	}
	return metadata, err
}

func (database *Database) CredentialForAccount(ctx context.Context, accountID domain.AccountID, kind string) (credentials.Metadata, error) {
	metadata, err := scanCredential(database.db.QueryRowContext(ctx, `SELECT id, account_id, kind, secret_ref, status,
validated_at, created_at, updated_at FROM credential_refs
WHERE profile_id=? AND account_id=? AND kind=? ORDER BY updated_at DESC, id DESC LIMIT 1`, database.profileID, accountID, kind))
	if errors.Is(err, sql.ErrNoRows) {
		return credentials.Metadata{}, credentials.ErrCredentialMissing
	}
	return metadata, err
}

func (database *Database) ListCredentials(ctx context.Context) ([]credentials.Metadata, error) {
	rows, err := database.db.QueryContext(ctx, `SELECT id, account_id, kind, secret_ref, status,
validated_at, created_at, updated_at FROM credential_refs WHERE profile_id=? ORDER BY updated_at DESC, id`, database.profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]credentials.Metadata, 0)
	for rows.Next() {
		metadata, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, metadata)
	}
	return items, rows.Err()
}

func (database *Database) UpdateCredentialStatus(ctx context.Context, id string, status credentials.Status, validatedAt time.Time) (credentials.Metadata, error) {
	if status != credentials.StatusUnknown && status != credentials.StatusValid && status != credentials.StatusInvalid {
		return credentials.Metadata{}, fmt.Errorf("unsupported credential status %q", status)
	}
	if validatedAt.IsZero() {
		validatedAt = time.Now()
	}
	result, err := database.db.ExecContext(ctx, `UPDATE credential_refs SET status=?, validated_at=?, updated_at=?
WHERE profile_id=? AND id=?`, status, validatedAt.UnixMilli(), validatedAt.UnixMilli(), database.profileID, id)
	if err != nil {
		return credentials.Metadata{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return credentials.Metadata{}, credentials.ErrCredentialMissing
	}
	return database.CredentialByID(ctx, id)
}

func (database *Database) RemoveCredential(ctx context.Context, id string) error {
	result, err := database.db.ExecContext(ctx, "DELETE FROM credential_refs WHERE profile_id=? AND id=?", database.profileID, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return credentials.ErrCredentialMissing
	}
	return nil
}

type credentialScanner interface {
	Scan(...any) error
}

func scanCredential(scanner credentialScanner) (credentials.Metadata, error) {
	var metadata credentials.Metadata
	var validated sql.NullInt64
	var createdAt, updatedAt int64
	if err := scanner.Scan(&metadata.ID, &metadata.AccountID, &metadata.Kind, &metadata.SecretRef, &metadata.Status,
		&validated, &createdAt, &updatedAt); err != nil {
		return credentials.Metadata{}, err
	}
	metadata.ValidatedAt = unixMillis(validated)
	metadata.CreatedAt = time.UnixMilli(createdAt)
	metadata.UpdatedAt = time.UnixMilli(updatedAt)
	return metadata, nil
}
