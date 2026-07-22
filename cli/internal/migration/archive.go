package migration

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Limits struct {
	MaxEntries          int
	MaxManifestBytes    int64
	MaxEntryBytes       int64
	MaxTotalBytes       int64
	MaxCompressionRatio uint64
}

func DefaultLimits() Limits {
	return Limits{MaxEntries: 100_000, MaxManifestBytes: 4 << 20, MaxEntryBytes: 512 << 20,
		MaxTotalBytes: 8 << 30, MaxCompressionRatio: 1_000}
}

func normalizeLimits(limits Limits) Limits {
	defaults := DefaultLimits()
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = defaults.MaxEntries
	}
	if limits.MaxManifestBytes <= 0 {
		limits.MaxManifestBytes = defaults.MaxManifestBytes
	}
	if limits.MaxEntryBytes <= 0 {
		limits.MaxEntryBytes = defaults.MaxEntryBytes
	}
	if limits.MaxTotalBytes <= 0 {
		limits.MaxTotalBytes = defaults.MaxTotalBytes
	}
	if limits.MaxCompressionRatio == 0 {
		limits.MaxCompressionRatio = defaults.MaxCompressionRatio
	}
	return limits
}

type ProblemCode string

const (
	ProblemUnsafePath       ProblemCode = "unsafe_path"
	ProblemDuplicateEntry   ProblemCode = "duplicate_entry"
	ProblemTooManyEntries   ProblemCode = "too_many_entries"
	ProblemEntryTooLarge    ProblemCode = "entry_too_large"
	ProblemArchiveTooLarge  ProblemCode = "archive_too_large"
	ProblemCompressionRatio ProblemCode = "compression_ratio"
	ProblemMissingManifest  ProblemCode = "missing_manifest"
	ProblemInvalidManifest  ProblemCode = "invalid_manifest"
	ProblemUnexpectedEntry  ProblemCode = "unexpected_entry"
	ProblemMissingEntry     ProblemCode = "missing_entry"
	ProblemSizeMismatch     ProblemCode = "size_mismatch"
	ProblemChecksumMismatch ProblemCode = "checksum_mismatch"
)

type ValidationProblem struct {
	Code ProblemCode
	Path string
	Err  error
}

type ValidationError struct {
	Problems []ValidationProblem
}

func (err *ValidationError) Error() string {
	if len(err.Problems) == 0 {
		return "archive validation failed"
	}
	problem := err.Problems[0]
	if problem.Path != "" {
		return fmt.Sprintf("archive validation failed: %s (%s): %v", problem.Path, problem.Code, problem.Err)
	}
	return fmt.Sprintf("archive validation failed: %s: %v", problem.Code, problem.Err)
}

func (err *ValidationError) Has(code ProblemCode) bool {
	for _, problem := range err.Problems {
		if problem.Code == code {
			return true
		}
	}
	return false
}

type ValidatedArchive struct {
	Path        string
	Size        int64
	Fingerprint string
	Manifest    Manifest
	Entries     map[string]ValidatedEntry
}

type ValidatedEntry struct {
	Path           string
	Size           int64
	CompressedSize uint64
	SHA256         string
	Kind           FileKind
	Dataset        Dataset
	MediaType      string
}

type checksumFile struct {
	Algorithm string          `json:"algorithm"`
	Scope     string          `json:"scope"`
	Files     []checksumEntry `json:"files"`
}

type checksumEntry struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

var windowsVolumePattern = regexp.MustCompile(`^[A-Za-z]:`)

func Validate(ctx context.Context, archivePath string, limits Limits) (ValidatedArchive, error) {
	limits = normalizeLimits(limits)
	file, err := os.Open(archivePath)
	if err != nil {
		return ValidatedArchive{}, fmt.Errorf("open legacy archive: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ValidatedArchive{}, fmt.Errorf("stat legacy archive: %w", err)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return ValidatedArchive{}, fmt.Errorf("open legacy archive zip: %w", err)
	}
	problems := make([]ValidationProblem, 0)
	if len(reader.File) > limits.MaxEntries {
		problems = append(problems, ValidationProblem{Code: ProblemTooManyEntries, Err: fmt.Errorf("%d entries exceeds %d", len(reader.File), limits.MaxEntries)})
	}
	zipEntries := make(map[string]*zip.File, len(reader.File))
	var total uint64
	for _, entry := range reader.File {
		name := entry.Name
		if err := safeArchivePath(name); err != nil {
			problems = append(problems, ValidationProblem{Code: ProblemUnsafePath, Path: name, Err: err})
		}
		if _, duplicate := zipEntries[name]; duplicate {
			problems = append(problems, ValidationProblem{Code: ProblemDuplicateEntry, Path: name, Err: errors.New("duplicate ZIP entry")})
		} else {
			zipEntries[name] = entry
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		size := entry.UncompressedSize64
		total += size
		if size > uint64(limits.MaxEntryBytes) || (name == ManifestPath && size > uint64(limits.MaxManifestBytes)) {
			problems = append(problems, ValidationProblem{Code: ProblemEntryTooLarge, Path: name, Err: fmt.Errorf("entry size %d exceeds limit", size)})
		}
		if entry.CompressedSize64 == 0 {
			if size > 0 {
				problems = append(problems, ValidationProblem{Code: ProblemCompressionRatio, Path: name, Err: errors.New("non-empty entry has zero compressed size")})
			}
		} else if size/entry.CompressedSize64 > limits.MaxCompressionRatio {
			problems = append(problems, ValidationProblem{Code: ProblemCompressionRatio, Path: name, Err: errors.New("compression ratio exceeds limit")})
		}
	}
	if total > uint64(limits.MaxTotalBytes) {
		problems = append(problems, ValidationProblem{Code: ProblemArchiveTooLarge, Err: fmt.Errorf("uncompressed size %d exceeds limit", total)})
	}
	manifestEntry := zipEntries[ManifestPath]
	if manifestEntry == nil {
		problems = append(problems, ValidationProblem{Code: ProblemMissingManifest, Path: ManifestPath, Err: errors.New("manifest is required")})
	}
	if len(problems) > 0 {
		return ValidatedArchive{}, &ValidationError{Problems: problems}
	}
	manifestBody, _, err := readZipEntry(ctx, manifestEntry, limits.MaxManifestBytes)
	if err != nil {
		return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: ManifestPath, Err: err}}}
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBody)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: ManifestPath, Err: err}}}
	}
	if err := manifest.Validate(); err != nil {
		return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: ManifestPath, Err: err}}}
	}
	if len(manifest.Files) == 0 && manifest.ChecksumFile != "" {
		checksumZIPEntry := zipEntries[manifest.ChecksumFile]
		if checksumZIPEntry == nil {
			return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemMissingEntry, Path: manifest.ChecksumFile, Err: errors.New("checksum file is absent")}}}
		}
		checksumBody, _, checksumErr := readZipEntry(ctx, checksumZIPEntry, limits.MaxManifestBytes)
		if checksumErr != nil {
			return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: manifest.ChecksumFile, Err: checksumErr}}}
		}
		var checksums checksumFile
		checksumDecoder := json.NewDecoder(strings.NewReader(string(checksumBody)))
		checksumDecoder.DisallowUnknownFields()
		if checksumErr := checksumDecoder.Decode(&checksums); checksumErr != nil {
			return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: manifest.ChecksumFile, Err: checksumErr}}}
		}
		if checksums.Algorithm != "sha256" {
			return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: manifest.ChecksumFile, Err: errors.New("unsupported checksum algorithm")}}}
		}
		derived, deriveErr := deriveManifestFiles(manifest, checksums)
		if deriveErr != nil {
			return ValidatedArchive{}, &ValidationError{Problems: []ValidationProblem{{Code: ProblemInvalidManifest, Path: manifest.ChecksumFile, Err: deriveErr}}}
		}
		manifest.Files = derived
	}
	declared := make(map[string]ManifestFile, len(manifest.Files))
	for _, expected := range manifest.Files {
		if err := safeArchivePath(expected.Path); err != nil {
			problems = append(problems, ValidationProblem{Code: ProblemUnsafePath, Path: expected.Path, Err: err})
			continue
		}
		if expected.Path == ManifestPath {
			problems = append(problems, ValidationProblem{Code: ProblemInvalidManifest, Path: expected.Path, Err: errors.New("manifest cannot declare itself")})
			continue
		}
		if _, duplicate := declared[expected.Path]; duplicate {
			problems = append(problems, ValidationProblem{Code: ProblemDuplicateEntry, Path: expected.Path, Err: errors.New("duplicate manifest path")})
			continue
		}
		declared[expected.Path] = expected
	}
	for name, entry := range zipEntries {
		if name == ManifestPath || name == manifest.ChecksumFile || entry.FileInfo().IsDir() {
			continue
		}
		if _, ok := declared[name]; !ok {
			problems = append(problems, ValidationProblem{Code: ProblemUnexpectedEntry, Path: name, Err: errors.New("entry is not declared by manifest")})
		}
	}
	validatedEntries := make(map[string]ValidatedEntry, len(declared))
	for name, expected := range declared {
		entry := zipEntries[name]
		if entry == nil {
			problems = append(problems, ValidationProblem{Code: ProblemMissingEntry, Path: name, Err: errors.New("declared entry is absent")})
			continue
		}
		if entry.UncompressedSize64 != uint64(expected.Size) {
			problems = append(problems, ValidationProblem{Code: ProblemSizeMismatch, Path: name, Err: fmt.Errorf("ZIP size %d does not match manifest %d", entry.UncompressedSize64, expected.Size)})
			continue
		}
		_, digest, readErr := readZipEntry(ctx, entry, limits.MaxEntryBytes)
		if readErr != nil {
			problems = append(problems, ValidationProblem{Code: ProblemChecksumMismatch, Path: name, Err: readErr})
			continue
		}
		if digest != expected.SHA256 {
			problems = append(problems, ValidationProblem{Code: ProblemChecksumMismatch, Path: name, Err: fmt.Errorf("got %s, want %s", digest, expected.SHA256)})
			continue
		}
		if expected.Kind == FileObject && !objectPathMatchesDigest(name, expected.SHA256) {
			problems = append(problems, ValidationProblem{Code: ProblemChecksumMismatch, Path: name, Err: errors.New("object path does not match digest")})
			continue
		}
		validatedEntries[name] = ValidatedEntry{Path: name, Size: expected.Size, CompressedSize: entry.CompressedSize64,
			SHA256: expected.SHA256, Kind: expected.Kind, Dataset: expected.Dataset, MediaType: expected.MediaType}
	}
	if len(problems) > 0 {
		return ValidatedArchive{}, &ValidationError{Problems: problems}
	}
	fingerprint, err := archiveFingerprint(ctx, archivePath)
	if err != nil {
		return ValidatedArchive{}, err
	}
	absolute, err := filepath.Abs(archivePath)
	if err != nil {
		return ValidatedArchive{}, err
	}
	return ValidatedArchive{Path: absolute, Size: info.Size(), Fingerprint: fingerprint, Manifest: manifest, Entries: validatedEntries}, nil
}

func deriveManifestFiles(manifest Manifest, checksums checksumFile) ([]ManifestFile, error) {
	tableFiles := make(map[string]Dataset, len(manifest.Tables))
	for logicalName, table := range manifest.Tables {
		dataset, ok := datasetForLogicalTable(logicalName)
		if !ok {
			return nil, fmt.Errorf("unsupported logical table %q", logicalName)
		}
		tableFiles[table.Path] = dataset
	}
	files := make([]ManifestFile, 0, len(checksums.Files)-1)
	seen := make(map[string]struct{}, len(checksums.Files))
	foundManifest := false
	for _, checksum := range checksums.Files {
		if err := safeArchivePath(checksum.Path); err != nil {
			return nil, fmt.Errorf("unsafe checksum path %q: %w", checksum.Path, err)
		}
		if _, duplicate := seen[checksum.Path]; duplicate {
			return nil, fmt.Errorf("duplicate checksum path %q", checksum.Path)
		}
		seen[checksum.Path] = struct{}{}
		if checksum.Path == ManifestPath {
			foundManifest = true
			continue
		}
		entry := ManifestFile{Path: checksum.Path, Size: checksum.Bytes, SHA256: checksum.SHA256}
		if dataset, ok := tableFiles[checksum.Path]; ok {
			entry.Kind = FileRecords
			entry.Dataset = dataset
		} else if strings.HasPrefix(checksum.Path, "objects/") {
			entry.Kind = FileObject
		} else {
			return nil, fmt.Errorf("checksum path %q is neither a table nor object", checksum.Path)
		}
		files = append(files, entry)
	}
	if !foundManifest {
		return nil, errors.New("checksums do not cover manifest.json")
	}
	for path := range tableFiles {
		if _, ok := seen[path]; !ok {
			return nil, fmt.Errorf("checksums do not cover table %q", path)
		}
	}
	return files, nil
}

func datasetForLogicalTable(name string) (Dataset, bool) {
	switch name {
	case "accounts":
		return DatasetAccounts, true
	case "articles":
		return DatasetArticles, true
	case "html":
		return DatasetHTML, true
	case "metadata":
		return DatasetMetadata, true
	case "comments":
		return DatasetComments, true
	case "replies":
		return DatasetReplies, true
	case "resourceMaps":
		return DatasetResourceMaps, true
	case "resources":
		return DatasetResources, true
	case "assets":
		return DatasetAssets, true
	default:
		return "", false
	}
}

func safeArchivePath(name string) error {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return errors.New("entry path is empty or uses an unsafe separator")
	}
	if strings.HasPrefix(name, "/") || windowsVolumePattern.MatchString(name) {
		return errors.New("absolute entry paths are forbidden")
	}
	cleaned := path.Clean(name)
	if cleaned != name || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("entry path is not normalized or traverses outside the archive")
	}
	return nil
}

func readZipEntry(ctx context.Context, entry *zip.File, limit int64) ([]byte, string, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, "", err
	}
	defer reader.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	var body []byte
	var total int64
	for {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			total += int64(count)
			if total > limit {
				return nil, "", errors.New("entry exceeded configured size limit while reading")
			}
			body = append(body, buffer[:count]...)
			_, _ = hash.Write(buffer[:count])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, "", readErr
		}
	}
	return body, hex.EncodeToString(hash.Sum(nil)), nil
}

func archiveFingerprint(ctx context.Context, archivePath string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func objectPathMatchesDigest(name, digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	return name == "objects/sha256/"+digest || name == "objects/sha256/"+digest[:2]+"/"+digest[2:4]+"/"+digest ||
		strings.HasPrefix(name, "objects/html/") || strings.HasPrefix(name, "objects/resources/") || strings.HasPrefix(name, "objects/assets/")
}

func openValidatedZIP(archive ValidatedArchive) (*os.File, *zip.Reader, error) {
	file, err := os.Open(archive.Path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, reader, nil
}

func entryByName(reader *zip.Reader, name string) *zip.File {
	index := sort.Search(len(reader.File), func(index int) bool { return reader.File[index].Name >= name })
	if index < len(reader.File) && reader.File[index].Name == name {
		return reader.File[index]
	}
	for _, entry := range reader.File {
		if entry.Name == name {
			return entry
		}
	}
	return nil
}
