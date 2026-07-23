package library

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	_ "modernc.org/sqlite"
)

type RestoreConflictPolicy string

const (
	RestoreRefuseConflicts RestoreConflictPolicy = "refuse"
	RestoreRenameConflicts RestoreConflictPolicy = "rename"
)

type RestoreOptions struct {
	ArchivePath    string
	DatabasePath   string
	ObjectStore    *objects.FileStore
	ConfigPath     string
	ConflictPolicy RestoreConflictPolicy
	TargetProfile  domain.ProfileID
	TargetName     string
	Now            time.Time
	BeforeCommit   func() error
	fileOperations *restoreFileOperations
}

type RestoreReport struct {
	Manifest       BackupManifest             `json:"manifest"`
	Profiles       []RestoreProfileResolution `json:"profiles"`
	RestoredFiles  int                        `json:"restoredFiles"`
	RestoredBytes  int64                      `json:"restoredBytes"`
	RollbackBackup string                     `json:"rollbackBackup,omitempty"`
}

type RestoreProfileResolution struct {
	SourceID   domain.ProfileID `json:"sourceId"`
	SourceName string           `json:"sourceName"`
	TargetID   domain.ProfileID `json:"targetId"`
	TargetName string           `json:"targetName"`
	Resolution string           `json:"resolution"`
}

type stagedRestore struct {
	root      string
	database  string
	objects   string
	config    string
	manifest  BackupManifest
	profiles  []RestoreProfileResolution
	fileCount int
	byteCount int64
}

type restoreFileOperations struct {
	rename    func(string, string) error
	removeAll func(string) error
}

func defaultRestoreFileOperations() *restoreFileOperations {
	return &restoreFileOperations{
		rename:    os.Rename,
		removeAll: os.RemoveAll,
	}
}

func RestoreBackup(ctx context.Context, options RestoreOptions) (RestoreReport, error) {
	if options.ArchivePath == "" || options.DatabasePath == "" {
		return RestoreReport{}, errors.New("restore archive and database destination are required")
	}
	if options.ObjectStore == nil {
		return RestoreReport{}, errors.New("object store is required")
	}
	if filepath.Clean(options.DatabasePath) == filepath.Clean(options.ObjectStore.Root()) {
		return RestoreReport{}, errors.New("database destination cannot be the object-store root")
	}
	if options.ConflictPolicy == "" {
		options.ConflictPolicy = RestoreRefuseConflicts
	}
	if options.ConflictPolicy != RestoreRefuseConflicts && options.ConflictPolicy != RestoreRenameConflicts {
		return RestoreReport{}, fmt.Errorf("unsupported restore conflict policy %q", options.ConflictPolicy)
	}
	fileOperations := options.fileOperations
	if fileOperations == nil {
		fileOperations = defaultRestoreFileOperations()
	}
	if fileOperations.rename == nil || fileOperations.removeAll == nil {
		return RestoreReport{}, errors.New("restore file operations are incomplete")
	}
	verification, err := VerifyBackup(ctx, options.ArchivePath)
	if err != nil {
		return RestoreReport{}, err
	}
	if !verification.Valid {
		return RestoreReport{}, fmt.Errorf("backup validation failed: %v", verification.Failures)
	}
	staged, err := stageRestore(ctx, options, verification.Manifest)
	if err != nil {
		return RestoreReport{}, err
	}
	defer os.RemoveAll(staged.root)
	if options.BeforeCommit != nil {
		if err := options.BeforeCommit(); err != nil {
			return RestoreReport{}, fmt.Errorf("restore stopped before commit: %w", err)
		}
	}
	report := RestoreReport{
		Manifest:      staged.manifest,
		Profiles:      staged.profiles,
		RestoredFiles: staged.fileCount,
		RestoredBytes: staged.byteCount,
	}
	if err := ensureParent(options.DatabasePath); err != nil {
		return RestoreReport{}, err
	}
	rollbackRoot, err := os.MkdirTemp(filepath.Dir(options.DatabasePath), ".restore-rollback-*")
	if err != nil {
		return RestoreReport{}, fmt.Errorf("create restore rollback directory: %w", err)
	}
	removeRollback := true
	defer func() {
		if removeRollback {
			_ = os.RemoveAll(rollbackRoot)
		}
	}()
	report.RollbackBackup = rollbackRoot
	committed := []restoreCommit{}
	if err := commitRestorePath(staged.database, options.DatabasePath, filepath.Join(rollbackRoot, "database"), &committed, fileOperations); err != nil {
		if rollbackErr := rollbackRestore(committed, fileOperations); rollbackErr != nil {
			removeRollback = false
			return report, errors.Join(err, fmt.Errorf("restore rollback failed; preserve %s: %w", rollbackRoot, rollbackErr))
		}
		return RestoreReport{}, err
	}
	if err := commitRestorePath(staged.objects, options.ObjectStore.Root(), filepath.Join(rollbackRoot, "objects"), &committed, fileOperations); err != nil {
		if rollbackErr := rollbackRestore(committed, fileOperations); rollbackErr != nil {
			removeRollback = false
			return report, errors.Join(err, fmt.Errorf("restore rollback failed; preserve %s: %w", rollbackRoot, rollbackErr))
		}
		return RestoreReport{}, err
	}
	if staged.config != "" && options.ConfigPath != "" {
		if err := commitRestorePath(staged.config, options.ConfigPath, filepath.Join(rollbackRoot, "config"), &committed, fileOperations); err != nil {
			if rollbackErr := rollbackRestore(committed, fileOperations); rollbackErr != nil {
				removeRollback = false
				return report, errors.Join(err, fmt.Errorf("restore rollback failed; preserve %s: %w", rollbackRoot, rollbackErr))
			}
			return RestoreReport{}, err
		}
	}
	if err := verifyCommittedRestore(ctx, options.DatabasePath, options.ObjectStore.Root(), staged.manifest); err != nil {
		if rollbackErr := rollbackRestore(committed, fileOperations); rollbackErr != nil {
			removeRollback = false
			return report, errors.Join(fmt.Errorf("verify committed restore: %w", err),
				fmt.Errorf("restore rollback failed; preserve %s: %w", rollbackRoot, rollbackErr))
		}
		return RestoreReport{}, fmt.Errorf("verify committed restore: %w", err)
	}
	for _, commit := range committed {
		if commit.backup != "" {
			_ = os.RemoveAll(commit.backup)
		}
	}
	report.RollbackBackup = ""
	return report, nil
}

func stageRestore(ctx context.Context, options RestoreOptions, manifest BackupManifest) (stagedRestore, error) {
	if err := ensureParent(options.DatabasePath); err != nil {
		return stagedRestore{}, err
	}
	root, err := os.MkdirTemp(filepath.Dir(options.DatabasePath), ".restore-staging-*")
	if err != nil {
		return stagedRestore{}, fmt.Errorf("create restore staging directory: %w", err)
	}
	staged := stagedRestore{
		root: root, database: filepath.Join(root, backupDatabasePath), objects: filepath.Join(root, "objects"),
		manifest: manifest, profiles: []RestoreProfileResolution{},
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	reader, err := zip.OpenReader(options.ArchivePath)
	if err != nil {
		return stagedRestore{}, err
	}
	defer reader.Close()
	entries, failures := indexBackupEntries(reader.File)
	if len(failures) != 0 {
		return stagedRestore{}, fmt.Errorf("unsafe restore archive: %v", failures)
	}
	if err := extractZipEntry(ctx, entries[backupDatabasePath], staged.database); err != nil {
		return stagedRestore{}, fmt.Errorf("stage backup database: %w", err)
	}
	staged.fileCount++
	staged.byteCount += manifest.Files[backupDatabasePath].Size
	if configEntry := entries[backupConfigPath]; configEntry != nil && options.ConfigPath != "" {
		staged.config = filepath.Join(root, backupConfigPath)
		if err := extractZipEntry(ctx, configEntry, staged.config); err != nil {
			return stagedRestore{}, fmt.Errorf("stage profile configuration: %w", err)
		}
		staged.fileCount++
		staged.byteCount += manifest.Files[backupConfigPath].Size
	}
	if err := os.MkdirAll(filepath.Join(staged.objects, "sha256"), 0o700); err != nil {
		return stagedRestore{}, err
	}
	for _, object := range manifest.Objects {
		entry := entries[object.Path]
		destination := filepath.Join(staged.objects, "sha256", object.Digest[:2], object.Digest[2:4], object.Digest)
		if err := extractZipEntry(ctx, entry, destination); err != nil {
			return stagedRestore{}, fmt.Errorf("stage object %s: %w", object.Digest, err)
		}
		staged.fileCount++
		staged.byteCount += object.Size
	}
	if err := applyRestoreProfilePolicy(ctx, staged.database, options.DatabasePath, options.ConflictPolicy,
		options.TargetProfile, options.TargetName, staged.config, options.Now, &staged.profiles); err != nil {
		return stagedRestore{}, err
	}
	objectSet := make(map[string]struct{}, len(manifest.Objects))
	for _, object := range manifest.Objects {
		objectSet[object.Digest] = struct{}{}
	}
	if err := verifyBackupDatabase(ctx, staged.database, manifest, objectSet); err != nil {
		return stagedRestore{}, fmt.Errorf("verify staged restore database: %w", err)
	}
	cleanup = false
	return staged, nil
}

func applyRestoreProfilePolicy(
	ctx context.Context,
	stagedPath string,
	livePath string,
	policy RestoreConflictPolicy,
	targetProfile domain.ProfileID,
	targetName string,
	configPath string,
	now time.Time,
	resolutions *[]RestoreProfileResolution,
) error {
	if now.IsZero() {
		now = time.Now()
	}
	liveProfiles, err := readProfiles(ctx, livePath)
	if err != nil {
		return err
	}
	staged, err := sql.Open("sqlite", stagedPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer staged.Close()
	rows, err := staged.QueryContext(ctx, "SELECT id, name FROM profiles ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()
	type profilePair struct{ id, name string }
	profiles := []profilePair{}
	for rows.Next() {
		var pair profilePair
		if err := rows.Scan(&pair.id, &pair.name); err != nil {
			return err
		}
		profiles = append(profiles, pair)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if targetProfile != "" {
		if len(profiles) != 1 {
			return errors.New("restore to an active profile requires an archive containing exactly one profile")
		}
		profile := profiles[0]
		resolvedName := strings.TrimSpace(targetName)
		if resolvedName == "" {
			resolvedName = profile.name
		}
		if profile.id != string(targetProfile) {
			if err := rewriteProfileID(ctx, staged, profile.id, string(targetProfile)); err != nil {
				return err
			}
		}
		if _, err := staged.ExecContext(ctx, "UPDATE profiles SET name=?, updated_at=? WHERE id=?",
			resolvedName, now.UnixMilli(), targetProfile); err != nil {
			return fmt.Errorf("rewrite restored profile name: %w", err)
		}
		if configPath != "" {
			if err := rewriteRestoredConfigProfile(configPath, string(targetProfile)); err != nil {
				return err
			}
		}
		resolution := "unchanged"
		if profile.id != string(targetProfile) || profile.name != resolvedName {
			resolution = "remapped"
		}
		*resolutions = append(*resolutions, RestoreProfileResolution{SourceID: domain.ProfileID(profile.id), SourceName: profile.name,
			TargetID: targetProfile, TargetName: resolvedName, Resolution: resolution})
		return nil
	}
	usedIDs := map[string]struct{}{}
	usedNames := map[string]struct{}{}
	for id, name := range liveProfiles {
		usedIDs[id] = struct{}{}
		usedNames[name] = struct{}{}
	}
	for _, profile := range profiles {
		targetID := profile.id
		targetName := profile.name
		resolution := "unchanged"
		conflict := false
		if liveName, ok := liveProfiles[profile.id]; ok && liveName != profile.name {
			conflict = true
		}
		if existingID := profileIDByName(liveProfiles, profile.name); existingID != "" && existingID != profile.id {
			conflict = true
		}
		if conflict {
			if policy == RestoreRefuseConflicts {
				return fmt.Errorf("restore profile conflict for id %q and name %q", profile.id, profile.name)
			}
			targetID = uniqueProfileValue(profile.id+"-restored", usedIDs)
			targetName = uniqueProfileValue(profile.name+"-restored", usedNames)
			if err := rewriteProfileID(ctx, staged, profile.id, targetID); err != nil {
				return err
			}
			if _, err := staged.ExecContext(ctx, "UPDATE profiles SET name=?, updated_at=? WHERE id=?", targetName, now.UnixMilli(), targetID); err != nil {
				return fmt.Errorf("rename restored profile: %w", err)
			}
			resolution = "renamed"
		}
		usedIDs[targetID] = struct{}{}
		usedNames[targetName] = struct{}{}
		*resolutions = append(*resolutions, RestoreProfileResolution{
			SourceID: domain.ProfileID(profile.id), SourceName: profile.name,
			TargetID: domain.ProfileID(targetID), TargetName: targetName, Resolution: resolution,
		})
	}
	return nil
}

func rewriteProfileID(ctx context.Context, database *sql.DB, sourceID, targetID string) error {
	if sourceID == targetID {
		return nil
	}
	return withSQLTx(ctx, database, func(transaction *sql.Tx) error {
		if _, err := transaction.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO profiles(id, name, created_at, updated_at)
SELECT ?, name || '-staging', created_at, updated_at FROM profiles WHERE id=?`, targetID, sourceID); err != nil {
			return fmt.Errorf("create restored profile identity: %w", err)
		}
		tables, err := profileOwnedTables(ctx, transaction)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if _, err := transaction.ExecContext(ctx, "UPDATE "+quoteSQLiteIdentifier(table)+" SET profile_id=? WHERE profile_id=?", targetID, sourceID); err != nil {
				return fmt.Errorf("rewrite %s profile identity: %w", table, err)
			}
		}
		if _, err := transaction.ExecContext(ctx, "DELETE FROM profiles WHERE id=?", sourceID); err != nil {
			return fmt.Errorf("remove old restored profile identity: %w", err)
		}
		return nil
	})
}

func profileOwnedTables(ctx context.Context, transaction *sql.Tx) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT name FROM sqlite_schema
WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list restored profile-owned tables: %w", err)
	}
	var candidates []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	owned := make([]string, 0)
	for _, table := range candidates {
		foreignRows, err := transaction.QueryContext(ctx, "PRAGMA foreign_key_list("+quoteSQLiteIdentifier(table)+")")
		if err != nil {
			return nil, fmt.Errorf("inspect restored table %s foreign keys: %w", table, err)
		}
		profileOwned := false
		for foreignRows.Next() {
			var id, sequence int
			var referencedTable, fromColumn, toColumn, onUpdate, onDelete, match string
			if err := foreignRows.Scan(&id, &sequence, &referencedTable, &fromColumn, &toColumn, &onUpdate, &onDelete, &match); err != nil {
				foreignRows.Close()
				return nil, err
			}
			if referencedTable == "profiles" && fromColumn == "profile_id" && toColumn == "id" {
				profileOwned = true
			}
		}
		if err := foreignRows.Err(); err != nil {
			foreignRows.Close()
			return nil, err
		}
		if err := foreignRows.Close(); err != nil {
			return nil, err
		}
		if profileOwned {
			owned = append(owned, table)
		}
	}
	return owned, nil
}

func quoteSQLiteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func readProfiles(ctx context.Context, path string) (map[string]string, error) {
	profiles := map[string]string{}
	if path == "" {
		return profiles, nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return profiles, nil
	} else if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, "SELECT id, name FROM profiles")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		profiles[id] = name
	}
	return profiles, rows.Err()
}

func profileIDByName(profiles map[string]string, name string) string {
	for id, candidate := range profiles {
		if candidate == name {
			return id
		}
	}
	return ""
}

func uniqueProfileValue(base string, used map[string]struct{}) string {
	for suffix := 0; ; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", base, suffix)
		}
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

type restoreCommit struct {
	live   string
	backup string
}

func commitRestorePath(
	staged string,
	live string,
	backup string,
	committed *[]restoreCommit,
	fileOperations *restoreFileOperations,
) error {
	if staged == "" || live == "" {
		return nil
	}
	if err := ensureParent(live); err != nil {
		return err
	}
	if _, err := os.Stat(live); err == nil {
		if err := ensureParent(backup); err != nil {
			return err
		}
		if err := fileOperations.rename(live, backup); err != nil {
			return fmt.Errorf("stage live path for rollback %s: %w", live, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else {
		backup = ""
	}
	if err := fileOperations.rename(staged, live); err != nil {
		if backup != "" {
			if restoreErr := fileOperations.rename(backup, live); restoreErr != nil {
				// The rollback copy is now the only known-good live data. Track it so
				// the outer rollback can retry, and preserve the rollback root if that
				// retry also fails.
				*committed = append(*committed, restoreCommit{live: live, backup: backup})
				return errors.Join(
					fmt.Errorf("commit restored path %s: %w", live, err),
					fmt.Errorf("immediate restore rollback path %s: %w", live, restoreErr),
				)
			}
		}
		return fmt.Errorf("commit restored path %s: %w", live, err)
	}
	*committed = append(*committed, restoreCommit{live: live, backup: backup})
	return nil
}

func rollbackRestore(committed []restoreCommit, fileOperations *restoreFileOperations) error {
	var result error
	for index := len(committed) - 1; index >= 0; index-- {
		commit := committed[index]
		if err := fileOperations.removeAll(commit.live); err != nil {
			result = errors.Join(result, fmt.Errorf("remove failed restored path %s: %w", commit.live, err))
			continue
		}
		if commit.backup != "" {
			if err := fileOperations.rename(commit.backup, commit.live); err != nil {
				result = errors.Join(result, fmt.Errorf("restore rollback path %s: %w", commit.live, err))
			}
		}
	}
	return result
}

func rewriteRestoredConfigProfile(path, profileID string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read staged restored configuration: %w", err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode staged restored configuration: %w", err)
	}
	if value == nil {
		return errors.New("decode staged restored configuration: expected a JSON object")
	}
	encodedProfileID, err := json.Marshal(profileID)
	if err != nil {
		return fmt.Errorf("encode restored profile identity: %w", err)
	}
	value["profileId"] = encodedProfileID
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func verifyCommittedRestore(ctx context.Context, databasePath, objectRoot string, manifest BackupManifest) error {
	objectsSet := make(map[string]struct{}, len(manifest.Objects))
	for _, object := range manifest.Objects {
		objectsSet[object.Digest] = struct{}{}
		path := filepath.Join(objectRoot, "sha256", object.Digest[:2], object.Digest[2:4], object.Digest)
		digest, size, err := hashFile(ctx, path)
		if err != nil {
			return err
		}
		if digest != object.Digest || size != object.Size {
			return fmt.Errorf("restored object %s failed verification", object.Digest)
		}
	}
	return verifyBackupDatabase(ctx, databasePath, manifest, objectsSet)
}

func withSQLTx(ctx context.Context, database *sql.DB, operation func(*sql.Tx) error) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := operation(transaction); err != nil {
		_ = transaction.Rollback()
		return err
	}
	return transaction.Commit()
}
