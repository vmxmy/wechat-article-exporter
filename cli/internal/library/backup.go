package library

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	_ "modernc.org/sqlite"
)

const (
	BackupFormatVersion = 1
	backupManifestPath  = "manifest.json"
	backupDatabasePath  = "library.sqlite3"
	backupConfigPath    = "config/profile.json"
)

type BackupOptions struct {
	Destination string
	ObjectStore *objects.FileStore
	ConfigPath  string
	Now         time.Time
}

type BackupManifest struct {
	FormatVersion int                   `json:"formatVersion"`
	CreatedAt     time.Time             `json:"createdAt"`
	SchemaVersion int                   `json:"schemaVersion"`
	Profiles      []BackupProfile       `json:"profiles"`
	Counts        map[string]int        `json:"counts"`
	Files         map[string]BackupFile `json:"files"`
	Objects       []BackupObject        `json:"objects"`
	Omitted       []string              `json:"omitted"`
	TotalBytes    int64                 `json:"totalBytes"`
}

type BackupProfile struct {
	ID   domain.ProfileID `json:"id"`
	Name string           `json:"name"`
}

type BackupFile struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type BackupObject struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType,omitempty"`
	Path      string `json:"path"`
}

type BackupVerification struct {
	Valid      bool           `json:"valid"`
	Manifest   BackupManifest `json:"manifest"`
	Failures   []string       `json:"failures"`
	ArchiveSHA string         `json:"archiveSha256"`
}

type backupEntry struct {
	path       string
	sourcePath string
	digest     string
	size       int64
}

func (database *Database) CreateBackup(ctx context.Context, options BackupOptions) (BackupManifest, error) {
	if options.Destination == "" {
		return BackupManifest{}, errors.New("backup destination is required")
	}
	if options.ObjectStore == nil {
		return BackupManifest{}, errors.New("object store is required")
	}
	createdAt := options.Now
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if err := ensureParent(options.Destination); err != nil {
		return BackupManifest{}, err
	}
	workingDirectory, err := os.MkdirTemp(filepath.Dir(options.Destination), ".backup-staging-*")
	if err != nil {
		return BackupManifest{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(workingDirectory)

	databasePath := filepath.Join(workingDirectory, backupDatabasePath)
	if err := database.Backup(ctx, databasePath); err != nil {
		return BackupManifest{}, err
	}
	manifest, entries, err := buildBackupManifest(ctx, databasePath, options.ObjectStore, options.ConfigPath, createdAt)
	if err != nil {
		return BackupManifest{}, err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BackupManifest{}, fmt.Errorf("encode backup manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')

	temporary := options.Destination + ".tmp"
	_ = os.Remove(temporary)
	archive, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("create backup archive: %w", err)
	}
	committed := false
	defer func() {
		_ = archive.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	writer := zip.NewWriter(archive)
	if err := writeZipBytes(writer, backupManifestPath, manifestBytes); err != nil {
		return BackupManifest{}, err
	}
	for _, entry := range entries {
		if err := writeZipFile(ctx, writer, entry.path, entry.sourcePath); err != nil {
			return BackupManifest{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return BackupManifest{}, fmt.Errorf("close backup archive: %w", err)
	}
	if err := archive.Sync(); err != nil {
		return BackupManifest{}, fmt.Errorf("sync backup archive: %w", err)
	}
	if err := archive.Close(); err != nil {
		return BackupManifest{}, fmt.Errorf("close backup file: %w", err)
	}
	verification, err := VerifyBackup(ctx, temporary)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("verify created backup: %w", err)
	}
	if !verification.Valid {
		return BackupManifest{}, fmt.Errorf("verify created backup: %s", strings.Join(verification.Failures, "; "))
	}
	if err := commitBackupFile(temporary, options.Destination); err != nil {
		return BackupManifest{}, fmt.Errorf("commit backup archive: %w", err)
	}
	committed = true
	return manifest, nil
}

func VerifyBackup(ctx context.Context, archivePath string) (BackupVerification, error) {
	verification := BackupVerification{Failures: []string{}}
	archiveSHA, _, err := hashFile(ctx, archivePath)
	if err != nil {
		return verification, fmt.Errorf("checksum backup archive: %w", err)
	}
	verification.ArchiveSHA = archiveSHA
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return verification, fmt.Errorf("open backup archive: %w", err)
	}
	defer reader.Close()
	entries, failures := indexBackupEntries(reader.File)
	verification.Failures = append(verification.Failures, failures...)
	manifestFile := entries[backupManifestPath]
	if manifestFile == nil {
		verification.Failures = append(verification.Failures, "missing manifest.json")
		return verification, nil
	}
	manifestBytes, err := readZipFile(ctx, manifestFile, 16<<20)
	if err != nil {
		verification.Failures = append(verification.Failures, "read manifest.json: "+err.Error())
		return verification, nil
	}
	if err := json.Unmarshal(manifestBytes, &verification.Manifest); err != nil {
		verification.Failures = append(verification.Failures, "decode manifest.json: "+err.Error())
		return verification, nil
	}
	manifest := verification.Manifest
	if manifest.FormatVersion != BackupFormatVersion {
		verification.Failures = append(verification.Failures, fmt.Sprintf("unsupported backup format version %d", manifest.FormatVersion))
	}
	if manifest.SchemaVersion < MinimumSchemaVersion || manifest.SchemaVersion > CurrentSchemaVersion {
		verification.Failures = append(verification.Failures, fmt.Sprintf("unsupported schema version %d", manifest.SchemaVersion))
	}
	for path, expected := range manifest.Files {
		entry := entries[path]
		if entry == nil {
			verification.Failures = append(verification.Failures, "missing "+path)
			continue
		}
		actualDigest, actualSize, err := hashZipFile(ctx, entry)
		if err != nil {
			verification.Failures = append(verification.Failures, fmt.Sprintf("verify %s: %v", path, err))
			continue
		}
		if actualSize != expected.Size {
			verification.Failures = append(verification.Failures, fmt.Sprintf("size mismatch for %s: got %d, want %d", path, actualSize, expected.Size))
		}
		if actualDigest != expected.SHA256 {
			verification.Failures = append(verification.Failures, fmt.Sprintf("checksum mismatch for %s", path))
		}
	}
	for path := range entries {
		if path == backupManifestPath {
			continue
		}
		if _, expected := manifest.Files[path]; expected {
			continue
		}
		if isManifestObjectPath(manifest.Objects, path) {
			continue
		}
		verification.Failures = append(verification.Failures, "unlisted archive entry "+path)
	}
	seenObjects := make(map[string]struct{}, len(manifest.Objects))
	for _, object := range manifest.Objects {
		if _, exists := seenObjects[object.Digest]; exists {
			verification.Failures = append(verification.Failures, "duplicate object digest "+object.Digest)
			continue
		}
		seenObjects[object.Digest] = struct{}{}
		entry := entries[object.Path]
		if entry == nil {
			verification.Failures = append(verification.Failures, "missing "+object.Path)
			continue
		}
		actualDigest, actualSize, err := hashZipFile(ctx, entry)
		if err != nil {
			verification.Failures = append(verification.Failures, fmt.Sprintf("verify %s: %v", object.Path, err))
			continue
		}
		if actualSize != object.Size {
			verification.Failures = append(verification.Failures, fmt.Sprintf("size mismatch for %s: got %d, want %d", object.Path, actualSize, object.Size))
		}
		if actualDigest != object.Digest {
			verification.Failures = append(verification.Failures, fmt.Sprintf("object checksum mismatch for %s", object.Path))
		}
	}
	if databaseFile := entries[backupDatabasePath]; databaseFile != nil {
		temporaryDirectory, err := os.MkdirTemp("", ".backup-verify-*")
		if err != nil {
			return verification, err
		}
		defer os.RemoveAll(temporaryDirectory)
		databasePath := filepath.Join(temporaryDirectory, backupDatabasePath)
		if err := extractZipEntry(ctx, databaseFile, databasePath); err != nil {
			verification.Failures = append(verification.Failures, "extract database for verification: "+err.Error())
		} else if err := verifyBackupDatabase(ctx, databasePath, manifest, seenObjects); err != nil {
			verification.Failures = append(verification.Failures, err.Error())
		}
	}
	sort.Strings(verification.Failures)
	verification.Valid = len(verification.Failures) == 0
	return verification, nil
}

func buildBackupManifest(
	ctx context.Context,
	databasePath string,
	objectStore *objects.FileStore,
	configPath string,
	createdAt time.Time,
) (BackupManifest, []backupEntry, error) {
	database, err := sql.Open("sqlite", databasePath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return BackupManifest{}, nil, fmt.Errorf("open backup snapshot: %w", err)
	}
	defer database.Close()
	manifest := BackupManifest{
		FormatVersion: BackupFormatVersion,
		CreatedAt:     createdAt,
		SchemaVersion: CurrentSchemaVersion,
		Counts:        map[string]int{},
		Files:         map[string]BackupFile{},
		Objects:       []BackupObject{},
		Omitted:       []string{"OS credential-store secrets", "encrypted vault secret bytes"},
	}
	rows, err := database.QueryContext(ctx, "SELECT id, name FROM profiles ORDER BY id")
	if err != nil {
		return BackupManifest{}, nil, fmt.Errorf("list backup profiles: %w", err)
	}
	for rows.Next() {
		var profile BackupProfile
		if err := rows.Scan(&profile.ID, &profile.Name); err != nil {
			rows.Close()
			return BackupManifest{}, nil, err
		}
		manifest.Profiles = append(manifest.Profiles, profile)
	}
	if err := rows.Close(); err != nil {
		return BackupManifest{}, nil, err
	}
	for _, table := range []string{
		"profiles", "accounts", "articles", "albums", "content_versions", "metric_snapshots", "comments", "replies",
		"resources", "jobs", "job_items", "job_logs", "exports", "debug_incidents",
	} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return BackupManifest{}, nil, fmt.Errorf("count %s: %w", table, err)
		}
		manifest.Counts[table] = count
	}
	databaseDigest, databaseSize, err := hashFile(ctx, databasePath)
	if err != nil {
		return BackupManifest{}, nil, err
	}
	manifest.Files[backupDatabasePath] = BackupFile{Size: databaseSize, SHA256: databaseDigest}
	entries := []backupEntry{{path: backupDatabasePath, sourcePath: databasePath, digest: databaseDigest, size: databaseSize}}
	if configPath != "" {
		info, statErr := os.Stat(configPath)
		if errors.Is(statErr, os.ErrNotExist) {
			configPath = ""
		} else if statErr != nil {
			return BackupManifest{}, nil, fmt.Errorf("inspect profile configuration: %w", statErr)
		} else if !info.Mode().IsRegular() {
			return BackupManifest{}, nil, errors.New("profile configuration must be a regular file")
		}
	}
	if configPath != "" {
		sanitizedConfig := filepath.Join(filepath.Dir(databasePath), "profile.backup.json")
		if err := writeBackupConfiguration(configPath, sanitizedConfig); err != nil {
			return BackupManifest{}, nil, err
		}
		configDigest, configSize, err := hashFile(ctx, sanitizedConfig)
		if err != nil {
			return BackupManifest{}, nil, err
		}
		manifest.Files[backupConfigPath] = BackupFile{Size: configSize, SHA256: configDigest}
		entries = append(entries, backupEntry{path: backupConfigPath, sourcePath: sanitizedConfig, digest: configDigest, size: configSize})
	}
	objectRows, err := database.QueryContext(ctx, `SELECT digest, size_bytes, media_type FROM objects WHERE digest IN (`+referencedObjectUnion+`) ORDER BY digest`)
	if err != nil {
		return BackupManifest{}, nil, fmt.Errorf("list referenced objects: %w", err)
	}
	defer objectRows.Close()
	for objectRows.Next() {
		var object BackupObject
		var recordedSize int64
		if err := objectRows.Scan(&object.Digest, &recordedSize, &object.MediaType); err != nil {
			return BackupManifest{}, nil, err
		}
		reader, stored, err := objectStore.Open(ctx, object.Digest)
		if err != nil {
			return BackupManifest{}, nil, fmt.Errorf("open referenced object %s: %w", object.Digest, err)
		}
		actualDigest, actualSize, err := hashReader(ctx, reader)
		reader.Close()
		if err != nil {
			return BackupManifest{}, nil, fmt.Errorf("hash referenced object %s: %w", object.Digest, err)
		}
		if actualDigest != object.Digest || actualSize != stored.Size || actualSize != recordedSize {
			return BackupManifest{}, nil, fmt.Errorf("referenced object %s failed integrity validation", object.Digest)
		}
		object.Size = actualSize
		object.Path = backupObjectPath(object.Digest)
		manifest.Objects = append(manifest.Objects, object)
		entries = append(entries, backupEntry{path: object.Path, sourcePath: objectStorePath(objectStore, object.Digest), digest: object.Digest, size: actualSize})
	}
	if err := objectRows.Err(); err != nil {
		return BackupManifest{}, nil, err
	}
	manifest.TotalBytes = databaseSize
	if file, ok := manifest.Files[backupConfigPath]; ok {
		manifest.TotalBytes += file.Size
	}
	for _, object := range manifest.Objects {
		manifest.TotalBytes += object.Size
	}
	return manifest, entries, nil
}

func writeBackupConfiguration(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read profile configuration for backup: %w", err)
	}
	var configuration struct {
		SchemaVersion int             `json:"schemaVersion"`
		ProfileID     string          `json:"profileId"`
		Preferences   json.RawMessage `json:"preferences"`
		MCP           json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(data, &configuration); err != nil {
		return fmt.Errorf("decode profile configuration for backup: %w", err)
	}
	if len(configuration.Preferences) == 0 {
		configuration.Preferences = json.RawMessage(`{}`)
	}
	if len(configuration.MCP) == 0 {
		configuration.MCP = json.RawMessage(`{}`)
	}
	encoded, err := json.MarshalIndent(configuration, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(destination, encoded, 0o600)
}

const referencedObjectRows = `
SELECT cv.object_digest AS digest, cv.article_id, '' AS resource_id, 'content' AS reference_kind
FROM content_versions AS cv
WHERE cv.object_digest <> ''
UNION ALL
SELECT r.object_digest AS digest, COALESCE(ar.article_id, '') AS article_id, r.id AS resource_id,
  'resource' AS reference_kind
FROM resources AS r
LEFT JOIN article_resources AS ar ON ar.resource_id=r.id
WHERE r.object_digest IS NOT NULL AND r.object_digest <> ''
UNION ALL
SELECT c.raw_object_digest AS digest, c.article_id, '' AS resource_id, 'comment' AS reference_kind
FROM comments AS c
WHERE c.raw_object_digest <> ''
UNION ALL
SELECT rp.raw_object_digest AS digest, c.article_id, '' AS resource_id, 'reply' AS reference_kind
FROM replies AS rp
JOIN comments AS c ON c.id=rp.comment_id
WHERE rp.raw_object_digest <> ''
UNION ALL
SELECT object_digest AS digest, '' AS article_id, '' AS resource_id, 'debug' AS reference_kind
FROM debug_incidents WHERE object_digest IS NOT NULL AND object_digest <> ''
UNION ALL
SELECT CAST(pinned.value AS TEXT) AS digest, '' AS article_id, '' AS resource_id, 'job-pin' AS reference_kind
FROM job_items AS ji
JOIN jobs AS j ON j.id=ji.job_id
JOIN json_each(CASE WHEN json_valid(ji.item_key) THEN ji.item_key ELSE '{}' END, '$.pinnedDigests') AS pinned
  ON pinned.type='text'
WHERE CAST(pinned.value AS TEXT) <> ''
  AND j.state <> 'completed'
  AND ji.state <> 'completed'`

const referencedObjectUnion = `SELECT digest FROM (` + referencedObjectRows + `)`

func verifyBackupDatabase(ctx context.Context, path string, manifest BackupManifest, archiveObjects map[string]struct{}) error {
	database, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=query_only(1)")
	if err != nil {
		return fmt.Errorf("open backup database: %w", err)
	}
	defer database.Close()
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("check backup database integrity: %w", err)
	}
	if integrity != "ok" {
		return errors.New("backup database integrity check failed: " + integrity)
	}
	var foreignKeyFailure int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&foreignKeyFailure); err != nil {
		return fmt.Errorf("check backup database foreign keys: %w", err)
	}
	if foreignKeyFailure != 0 {
		return fmt.Errorf("backup database has %d foreign-key violation(s)", foreignKeyFailure)
	}
	var version sql.NullInt64
	if err := database.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read backup database schema version: %w", err)
	}
	if !version.Valid || int(version.Int64) != manifest.SchemaVersion {
		return fmt.Errorf("backup database schema version mismatch: got %d, want %d", version.Int64, manifest.SchemaVersion)
	}
	rows, err := database.QueryContext(ctx, `SELECT digest FROM objects WHERE digest IN (`+referencedObjectUnion+`) ORDER BY digest`)
	if err != nil {
		return fmt.Errorf("read referenced backup objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			return err
		}
		if _, exists := archiveObjects[digest]; !exists {
			return errors.New("backup database references missing object " + digest)
		}
	}
	return rows.Err()
}

func indexBackupEntries(files []*zip.File) (map[string]*zip.File, []string) {
	entries := make(map[string]*zip.File, len(files))
	failures := []string{}
	for _, file := range files {
		if !validArchivePath(file.Name) {
			failures = append(failures, "unsafe archive path "+file.Name)
			continue
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 || !file.Mode().IsRegular() {
			failures = append(failures, "unsupported archive entry "+file.Name)
			continue
		}
		if _, exists := entries[file.Name]; exists {
			failures = append(failures, "duplicate archive entry "+file.Name)
			continue
		}
		entries[file.Name] = file
	}
	return entries, failures
}

func backupObjectPath(digest string) string {
	return filepath.ToSlash(filepath.Join("objects", "sha256", digest[:2], digest[2:4], digest))
}

func isManifestObjectPath(manifest []BackupObject, path string) bool {
	for _, object := range manifest {
		if object.Path == path {
			return true
		}
	}
	return false
}

func objectStorePath(store *objects.FileStore, digest string) string {
	return filepath.Join(store.Root(), "sha256", digest[:2], digest[2:4], digest)
}

func validArchivePath(path string) bool {
	if path == "" || strings.Contains(path, "\\") || strings.HasPrefix(path, "/") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == path && clean != "." && !strings.HasPrefix(clean, "../")
}

func commitBackupFile(source, destination string) error {
	backup := destination + ".previous"
	_ = os.Remove(backup)
	if _, err := os.Stat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			_ = os.Rename(backup, destination)
		}
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func writeZipBytes(writer *zip.Writer, path string, contents []byte) error {
	header := &zip.FileHeader{Name: path, Method: zip.Deflate}
	header.SetMode(0o600)
	header.Modified = time.Unix(0, 0).UTC()
	destination, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create backup entry %s: %w", path, err)
	}
	if _, err := destination.Write(contents); err != nil {
		return fmt.Errorf("write backup entry %s: %w", path, err)
	}
	return nil
}

func writeZipFile(ctx context.Context, writer *zip.Writer, archivePath, sourcePath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open backup source %s: %w", sourcePath, err)
	}
	defer source.Close()
	header := &zip.FileHeader{Name: archivePath, Method: zip.Deflate}
	header.SetMode(0o600)
	header.Modified = time.Unix(0, 0).UTC()
	destination, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create backup entry %s: %w", archivePath, err)
	}
	if _, err := copyWithContext(ctx, destination, source); err != nil {
		return fmt.Errorf("write backup entry %s: %w", archivePath, err)
	}
	return nil
}

func readZipFile(ctx context.Context, file *zip.File, maximum int64) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	limited := io.LimitReader(reader, maximum+1)
	contents, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > maximum {
		return nil, fmt.Errorf("entry exceeds %d bytes", maximum)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return contents, nil
}

func extractZipEntry(ctx context.Context, file *zip.File, destination string) error {
	if err := ensureParent(destination); err != nil {
		return err
	}
	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(destination)
		}
	}()
	if _, err := copyWithContext(ctx, output, reader); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	committed = true
	return nil
}

func hashZipFile(ctx context.Context, file *zip.File) (string, int64, error) {
	reader, err := file.Open()
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()
	return hashReader(ctx, reader)
}

func hashFile(ctx context.Context, path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	return hashReader(ctx, file)
}

func hashReader(ctx context.Context, reader io.Reader) (string, int64, error) {
	hash := sha256.New()
	written, err := copyWithContext(ctx, hash, reader)
	if err != nil {
		return "", written, err
	}
	return hex.EncodeToString(hash.Sum(nil)), written, nil
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, readErr
		}
	}
}
