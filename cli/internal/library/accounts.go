package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/identity"
)

const AccountManifestVersion = 1

func (database *Database) SaveAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	account, err := validateLocalAccount(account)
	if err != nil {
		return domain.Account{}, err
	}
	if account.ID == "" {
		account.ID = domain.AccountID(identity.AccountID(account.FakeID))
	}
	err = database.WithTx(ctx, func(transaction *sql.Tx) error {
		existing, exists, err := accountByFakeIDTx(ctx, transaction, database.profileID, account.FakeID)
		if err != nil {
			return err
		}
		if !exists {
			return insertAccountTx(ctx, transaction, database.profileID, account)
		}
		merged := mergeAccount(existing, account)
		if accountsEqual(existing, merged) {
			return nil
		}
		return updateAccountTx(ctx, transaction, database.profileID, merged)
	})
	if err != nil {
		return domain.Account{}, fmt.Errorf("save account: %w", err)
	}
	return database.GetAccountByFakeID(ctx, account.FakeID)
}

// UpdateAccount explicitly replaces editable account details while preserving
// synchronization state. SaveAccount and ImportAccounts intentionally use the
// conservative merge policy for upstream/discovered records instead.
func (database *Database) UpdateAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	if strings.TrimSpace(string(account.ID)) == "" {
		return domain.Account{}, errors.New("account ID is required for update")
	}
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		existing, err := accountByIDTx(ctx, transaction, database.profileID, account.ID)
		if err != nil {
			return err
		}
		if fakeID := strings.TrimSpace(account.FakeID); fakeID != "" && fakeID != existing.FakeID {
			return errors.New("account fakeid cannot be changed")
		}
		updated := existing
		updated.Name = account.Name
		updated.Alias = account.Alias
		updated.Description = account.Description
		updated.AvatarURL = account.AvatarURL
		updated.ServiceType = account.ServiceType
		updated, err = validateLocalAccount(updated)
		if err != nil {
			return err
		}
		return updateAccountTx(ctx, transaction, database.profileID, updated)
	})
	if err != nil {
		return domain.Account{}, fmt.Errorf("update account: %w", err)
	}
	return database.GetAccount(ctx, account.ID)
}

func (database *Database) GetAccount(ctx context.Context, id domain.AccountID) (domain.Account, error) {
	return database.scanAccount(database.db.QueryRowContext(ctx, `SELECT id, fakeid, nickname, alias, signature,
avatar_url, service_type, last_sync_at, article_count, message_count, upstream_total, sync_cursor, completed
FROM accounts WHERE profile_id=? AND id=?`, database.profileID, id))
}

func (database *Database) GetAccountByFakeID(ctx context.Context, fakeID string) (domain.Account, error) {
	return database.scanAccount(database.db.QueryRowContext(ctx, `SELECT id, fakeid, nickname, alias, signature,
avatar_url, service_type, last_sync_at, article_count, message_count, upstream_total, sync_cursor, completed
FROM accounts WHERE profile_id=? AND fakeid=?`,
		database.profileID, strings.TrimSpace(fakeID)))
}

// AccountNames resolves saved account display names in one bounded local query.
// Missing IDs are deliberately omitted so presentation adapters can render an
// unavailable name without revealing an internal stable identifier.
func (database *Database) AccountNames(ctx context.Context, ids []domain.AccountID) (map[domain.AccountID]string, error) {
	unique := make(map[domain.AccountID]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			unique[id] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return map[domain.AccountID]string{}, nil
	}
	placeholders := make([]string, 0, len(unique))
	arguments := make([]any, 0, len(unique)+1)
	arguments = append(arguments, database.profileID)
	for id := range unique {
		placeholders = append(placeholders, "?")
		arguments = append(arguments, id)
	}
	rows, err := database.db.QueryContext(ctx, `SELECT id, nickname FROM accounts WHERE profile_id=? AND id IN (`+strings.Join(placeholders, ",")+`)`, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := make(map[domain.AccountID]string, len(unique))
	for rows.Next() {
		var id domain.AccountID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		if name = strings.TrimSpace(name); name != "" {
			names[id] = name
		}
	}
	return names, rows.Err()
}

func (database *Database) ExportAccounts(ctx context.Context, query domain.AccountQuery) (domain.AccountManifest, error) {
	query.Offset = 0
	query.Limit = 500
	manifest := domain.AccountManifest{SchemaVersion: AccountManifestVersion, ExportedAt: time.Now(), Accounts: []domain.Account{}}
	for {
		page, err := database.QueryAccounts(ctx, query)
		if err != nil {
			return domain.AccountManifest{}, err
		}
		manifest.Accounts = append(manifest.Accounts, page.Items...)
		if len(manifest.Accounts) >= page.Total || len(page.Items) == 0 {
			break
		}
		query.Offset += len(page.Items)
	}
	return manifest, nil
}

func (database *Database) ImportAccounts(ctx context.Context, manifest domain.AccountManifest) (domain.AccountImportReport, error) {
	if manifest.SchemaVersion != AccountManifestVersion {
		return domain.AccountImportReport{}, fmt.Errorf("unsupported account manifest version %d", manifest.SchemaVersion)
	}
	validated := make([]domain.Account, len(manifest.Accounts))
	seen := map[string]struct{}{}
	for index, account := range manifest.Accounts {
		value, err := validateLocalAccount(account)
		if err != nil {
			return domain.AccountImportReport{}, fmt.Errorf("account manifest item %d: %w", index, err)
		}
		if _, exists := seen[value.FakeID]; exists {
			return domain.AccountImportReport{}, fmt.Errorf("account manifest contains duplicate fakeid %q", value.FakeID)
		}
		seen[value.FakeID] = struct{}{}
		if value.ID == "" {
			value.ID = domain.AccountID(identity.AccountID(value.FakeID))
		}
		validated[index] = value
	}
	report := domain.AccountImportReport{}
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		for _, account := range validated {
			existing, exists, err := accountByFakeIDTx(ctx, transaction, database.profileID, account.FakeID)
			if err != nil {
				return err
			}
			if !exists {
				if err := insertAccountTx(ctx, transaction, database.profileID, account); err != nil {
					return err
				}
				report.Added++
				continue
			}
			merged := mergeAccount(existing, account)
			if accountsEqual(existing, merged) {
				report.Unchanged++
				continue
			}
			if err := updateAccountTx(ctx, transaction, database.profileID, merged); err != nil {
				return err
			}
			report.Merged++
		}
		return nil
	})
	if err != nil {
		return domain.AccountImportReport{}, fmt.Errorf("import account manifest: %w", err)
	}
	return report, nil
}

func (database *Database) DeleteAccounts(ctx context.Context, ids []domain.AccountID) (domain.AccountDeleteReport, error) {
	unique := map[domain.AccountID]struct{}{}
	for _, id := range ids {
		if strings.TrimSpace(string(id)) != "" {
			unique[id] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return domain.AccountDeleteReport{}, errors.New("at least one account ID is required")
	}
	report := domain.AccountDeleteReport{}
	err := database.WithTx(ctx, func(transaction *sql.Tx) error {
		candidateDigests := map[string]struct{}{}
		for id := range unique {
			var accountExists int
			if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE profile_id=? AND id=?",
				database.profileID, id).Scan(&accountExists); err != nil {
				return err
			}
			if accountExists == 0 {
				continue
			}
			var articleCount int
			if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM articles WHERE profile_id=? AND account_id=?",
				database.profileID, id).Scan(&articleCount); err != nil {
				return err
			}
			var albumCount int
			if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM albums WHERE profile_id=? AND account_id=?",
				database.profileID, id).Scan(&albumCount); err != nil {
				return err
			}
			digestRows, err := transaction.QueryContext(ctx, `SELECT digest FROM objects WHERE digest IN (
SELECT cv.object_digest FROM content_versions cv JOIN articles a ON a.id=cv.article_id WHERE a.profile_id=? AND a.account_id=?
UNION SELECT r.object_digest FROM resources r JOIN article_resources ar ON ar.resource_id=r.id JOIN articles a ON a.id=ar.article_id
  WHERE a.profile_id=? AND a.account_id=? AND r.object_digest IS NOT NULL AND r.object_digest <> ''
UNION SELECT c.raw_object_digest FROM comments c JOIN articles a ON a.id=c.article_id
  WHERE a.profile_id=? AND a.account_id=? AND c.raw_object_digest <> ''
UNION SELECT rp.raw_object_digest FROM replies rp JOIN comments c ON c.id=rp.comment_id JOIN articles a ON a.id=c.article_id
  WHERE a.profile_id=? AND a.account_id=? AND rp.raw_object_digest <> ''
			)`, database.profileID, id, database.profileID, id, database.profileID, id, database.profileID, id)
			if err != nil {
				return err
			}
			for digestRows.Next() {
				var digest string
				if err := digestRows.Scan(&digest); err != nil {
					digestRows.Close()
					return err
				}
				candidateDigests[digest] = struct{}{}
			}
			if err := digestRows.Close(); err != nil {
				return err
			}
			var resourceIDs []string
			resourceRows, err := transaction.QueryContext(ctx, `SELECT DISTINCT r.id FROM resources r
JOIN article_resources ar ON ar.resource_id=r.id JOIN articles a ON a.id=ar.article_id
WHERE a.profile_id=? AND a.account_id=?`, database.profileID, id)
			if err != nil {
				return err
			}
			for resourceRows.Next() {
				var resourceID string
				if err := resourceRows.Scan(&resourceID); err != nil {
					resourceRows.Close()
					return err
				}
				resourceIDs = append(resourceIDs, resourceID)
			}
			if err := resourceRows.Close(); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, "DELETE FROM articles WHERE profile_id=? AND account_id=?", database.profileID, id); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, "DELETE FROM albums WHERE profile_id=? AND account_id=?", database.profileID, id); err != nil {
				return err
			}
			for _, resourceID := range resourceIDs {
				if _, err := transaction.ExecContext(ctx, `DELETE FROM resources WHERE id=? AND profile_id=?
AND NOT EXISTS (SELECT 1 FROM article_resources WHERE resource_id=?)`, resourceID, database.profileID, resourceID); err != nil {
					return err
				}
			}
			result, err := transaction.ExecContext(ctx, "DELETE FROM accounts WHERE profile_id=? AND id=?", database.profileID, id)
			if err != nil {
				return err
			}
			deleted, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if deleted > 0 {
				report.AccountsDeleted += int(deleted)
				report.ArticlesDeleted += articleCount
				report.AlbumsDeleted += albumCount
			}
		}
		for digest := range candidateDigests {
			var referenced int
			if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM objects WHERE digest=? AND digest IN (`+referencedObjectUnion+`)`,
				digest).Scan(&referenced); err != nil {
				return err
			}
			if referenced == 0 {
				report.ObjectsGarbageEligible++
			}
		}
		return nil
	})
	if err != nil {
		return domain.AccountDeleteReport{}, fmt.Errorf("delete accounts: %w", err)
	}
	return report, nil
}

func (database *Database) scanAccount(row *sql.Row) (domain.Account, error) {
	var account domain.Account
	var lastSync sql.NullInt64
	var cursor string
	if err := row.Scan(&account.ID, &account.FakeID, &account.Name, &account.Alias, &account.Description,
		&account.AvatarURL, &account.ServiceType, &lastSync, &account.ArticleCount, &account.MessageCount,
		&account.UpstreamTotal, &cursor, &account.SyncCompleted); err != nil {
		return domain.Account{}, err
	}
	account.LastSyncAt = unixMillis(lastSync)
	account.SyncCursor, _ = strconv.Atoi(cursor)
	return account, nil
}

func validateLocalAccount(account domain.Account) (domain.Account, error) {
	account.FakeID = strings.TrimSpace(account.FakeID)
	account.Name = strings.TrimSpace(account.Name)
	account.Alias = strings.TrimSpace(account.Alias)
	account.Description = strings.TrimSpace(account.Description)
	account.AvatarURL = strings.TrimSpace(account.AvatarURL)
	if account.FakeID == "" || account.Name == "" {
		return domain.Account{}, errors.New("account fakeid and name are required")
	}
	if len(account.FakeID) > 512 || len(account.Name) > 512 || len(account.Alias) > 512 || len(account.Description) > 4096 {
		return domain.Account{}, errors.New("account fields exceed supported limits")
	}
	if account.ArticleCount < 0 || account.ServiceType < 0 {
		return domain.Account{}, errors.New("account counts and service type cannot be negative")
	}
	return account, nil
}

func mergeAccount(local, incoming domain.Account) domain.Account {
	merged := local
	if merged.Name == "" {
		merged.Name = incoming.Name
	}
	if merged.Alias == "" {
		merged.Alias = incoming.Alias
	}
	if merged.Description == "" {
		merged.Description = incoming.Description
	}
	if merged.AvatarURL == "" {
		merged.AvatarURL = incoming.AvatarURL
	}
	if merged.ServiceType == 0 {
		merged.ServiceType = incoming.ServiceType
	}
	if incoming.ArticleCount > merged.ArticleCount {
		merged.ArticleCount = incoming.ArticleCount
	}
	if incoming.LastSyncAt.After(merged.LastSyncAt) {
		merged.LastSyncAt = incoming.LastSyncAt
	}
	return merged
}

func accountByFakeIDTx(ctx context.Context, tx *sql.Tx, profileID domain.ProfileID, fakeID string) (domain.Account, bool, error) {
	var account domain.Account
	var lastSync sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, fakeid, nickname, alias, signature, avatar_url, service_type, last_sync_at, article_count
FROM accounts WHERE profile_id=? AND fakeid=?`, profileID, fakeID).Scan(&account.ID, &account.FakeID, &account.Name,
		&account.Alias, &account.Description, &account.AvatarURL, &account.ServiceType, &lastSync, &account.ArticleCount)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, false, nil
	}
	if err != nil {
		return domain.Account{}, false, err
	}
	account.LastSyncAt = unixMillis(lastSync)
	return account, true, nil
}

func accountByIDTx(ctx context.Context, tx *sql.Tx, profileID domain.ProfileID, id domain.AccountID) (domain.Account, error) {
	var account domain.Account
	var lastSync sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, fakeid, nickname, alias, signature, avatar_url, service_type, last_sync_at, article_count
FROM accounts WHERE profile_id=? AND id=?`, profileID, id).Scan(&account.ID, &account.FakeID, &account.Name,
		&account.Alias, &account.Description, &account.AvatarURL, &account.ServiceType, &lastSync, &account.ArticleCount)
	if err != nil {
		return domain.Account{}, err
	}
	account.LastSyncAt = unixMillis(lastSync)
	return account, nil
}

func insertAccountTx(ctx context.Context, tx *sql.Tx, profileID domain.ProfileID, account domain.Account) error {
	now := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `INSERT INTO accounts(id, profile_id, fakeid, nickname, alias, signature, avatar_url,
service_type, article_count, last_sync_at, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		account.ID, profileID, account.FakeID, account.Name, account.Alias, account.Description, account.AvatarURL,
		account.ServiceType, account.ArticleCount, nullableTime(account.LastSyncAt), now, now)
	return err
}

func updateAccountTx(ctx context.Context, tx *sql.Tx, profileID domain.ProfileID, account domain.Account) error {
	_, err := tx.ExecContext(ctx, `UPDATE accounts SET nickname=?, alias=?, signature=?, avatar_url=?, service_type=?,
article_count=?, last_sync_at=?, updated_at=? WHERE profile_id=? AND id=?`, account.Name, account.Alias, account.Description,
		account.AvatarURL, account.ServiceType, account.ArticleCount, nullableTime(account.LastSyncAt), time.Now().UnixMilli(),
		profileID, account.ID)
	return err
}

func accountsEqual(left, right domain.Account) bool {
	return left.ID == right.ID && left.FakeID == right.FakeID && left.Name == right.Name && left.Alias == right.Alias &&
		left.Description == right.Description && left.AvatarURL == right.AvatarURL && left.ServiceType == right.ServiceType &&
		left.ArticleCount == right.ArticleCount && left.LastSyncAt.Equal(right.LastSyncAt)
}
