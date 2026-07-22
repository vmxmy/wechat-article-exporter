package migration

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sort"
	"time"
)

var ErrArchiveChanged = errors.New("legacy archive changed after validation")

type ConflictPolicy string

const (
	PreserveNewer   ConflictPolicy = "preserve-newer"
	PreferArchive   ConflictPolicy = "prefer-archive"
	RejectConflicts ConflictPolicy = "reject-conflicts"
)

type ImportOptions struct {
	ConflictPolicy ConflictPolicy
}

type Inventory struct {
	Records []RecordIdentity
	Objects []string
}

type LocalRecord struct {
	UpdatedAt   time.Time
	Fingerprint string
}

type LocalObject struct {
	Size int64
}

type TargetState struct {
	Records map[RecordIdentity]LocalRecord
	Objects map[string]LocalObject
}

type ReconciliationAction string

const (
	ActionInsert        ReconciliationAction = "insert"
	ActionReplace       ReconciliationAction = "replace"
	ActionSkipIdentical ReconciliationAction = "skip-identical"
	ActionPreserveLocal ReconciliationAction = "preserve-local"
)

type ReconciledRecord struct {
	Record Record
	Action ReconciliationAction
}

type ObjectSource interface {
	Open(context.Context) (io.ReadCloser, error)
}

type ImportObject struct {
	Digest    string
	Size      int64
	MediaType string
	Source    ObjectSource
}

type ImportBatch struct {
	Records []ReconciledRecord
	Objects []ImportObject
}

type Target interface {
	Inspect(context.Context, Inventory) (TargetState, error)
	Apply(context.Context, ImportBatch) error
}

type ConflictDecision struct {
	Identity RecordIdentity       `json:"identity"`
	Action   ReconciliationAction `json:"action"`
	Reason   string               `json:"reason"`
}

type ImportReport struct {
	SourceRecords      int                `json:"sourceRecords"`
	RecordsInserted    int                `json:"recordsInserted"`
	RecordsReplaced    int                `json:"recordsReplaced"`
	RecordsUnchanged   int                `json:"recordsUnchanged"`
	PreservedLocal     int                `json:"preservedLocal"`
	ObjectsWritten     int                `json:"objectsWritten"`
	ObjectsReused      int                `json:"objectsReused"`
	ObjectBytesWritten int64              `json:"objectBytesWritten"`
	MissingResources   int                `json:"missingResources"`
	ArchiveDuplicates  int                `json:"archiveDuplicates"`
	Decisions          []ConflictDecision `json:"decisions,omitempty"`
	Warnings           []string           `json:"warnings,omitempty"`
}

func Import(ctx context.Context, plan ImportPlan, target Target, options ImportOptions) (ImportReport, error) {
	if target == nil {
		return ImportReport{}, errors.New("migration target is required")
	}
	policy := options.ConflictPolicy
	if policy == "" {
		policy = PreserveNewer
	}
	if policy != PreserveNewer && policy != PreferArchive && policy != RejectConflicts {
		return ImportReport{}, fmt.Errorf("unsupported conflict policy %q", policy)
	}
	fingerprint, err := archiveFingerprint(ctx, plan.Archive.Path)
	if err != nil {
		return ImportReport{}, fmt.Errorf("recheck archive: %w", err)
	}
	if fingerprint != plan.Archive.Fingerprint {
		return ImportReport{}, ErrArchiveChanged
	}
	inventory := Inventory{Records: make([]RecordIdentity, 0, len(plan.Records)), Objects: make([]string, 0, len(plan.Objects))}
	for _, record := range plan.Records {
		inventory.Records = append(inventory.Records, record.Identity())
	}
	for _, object := range plan.Objects {
		inventory.Objects = append(inventory.Objects, object.Digest)
	}
	state, err := target.Inspect(ctx, inventory)
	if err != nil {
		return ImportReport{}, fmt.Errorf("inspect migration target: %w", err)
	}
	if state.Records == nil {
		state.Records = map[RecordIdentity]LocalRecord{}
	}
	if state.Objects == nil {
		state.Objects = map[string]LocalObject{}
	}
	report := ImportReport{SourceRecords: plan.Report.SourceRecords, MissingResources: plan.Report.MissingResources,
		ArchiveDuplicates: plan.Report.ArchiveDuplicates, Warnings: append([]string(nil), plan.Report.Warnings...)}
	batch := ImportBatch{Records: make([]ReconciledRecord, 0, len(plan.Records)), Objects: make([]ImportObject, 0, len(plan.Objects))}
	for _, record := range plan.Records {
		local, exists := state.Records[record.Identity()]
		if !exists {
			report.RecordsInserted++
			batch.Records = append(batch.Records, ReconciledRecord{Record: record, Action: ActionInsert})
			continue
		}
		if local.Fingerprint != "" && local.Fingerprint == record.Fingerprint {
			report.RecordsUnchanged++
			report.Decisions = append(report.Decisions, ConflictDecision{Identity: record.Identity(), Action: ActionSkipIdentical, Reason: "same fingerprint"})
			continue
		}
		if policy == RejectConflicts {
			return ImportReport{}, fmt.Errorf("record conflict for %s", record.Identity().String())
		}
		if policy == PreserveNewer && local.UpdatedAt.After(record.UpdatedAt) {
			report.PreservedLocal++
			report.Decisions = append(report.Decisions, ConflictDecision{Identity: record.Identity(), Action: ActionPreserveLocal, Reason: "local record is newer"})
			continue
		}
		report.RecordsReplaced++
		batch.Records = append(batch.Records, ReconciledRecord{Record: record, Action: ActionReplace})
		report.Decisions = append(report.Decisions, ConflictDecision{Identity: record.Identity(), Action: ActionReplace, Reason: "archive record selected by conflict policy"})
	}

	file, reader, err := openValidatedZIP(plan.Archive)
	if err != nil {
		return ImportReport{}, fmt.Errorf("open staged archive: %w", err)
	}
	defer file.Close()
	for _, object := range plan.Objects {
		if local, exists := state.Objects[object.Digest]; exists {
			if local.Size != object.Size {
				return ImportReport{}, fmt.Errorf("local object %s has size %d, archive expects %d", object.Digest, local.Size, object.Size)
			}
			report.ObjectsReused++
			continue
		}
		entry := entryByName(reader, object.EntryPath)
		if entry == nil {
			return ImportReport{}, fmt.Errorf("validated object entry %q disappeared", object.EntryPath)
		}
		batch.Objects = append(batch.Objects, ImportObject{Digest: object.Digest, Size: object.Size, MediaType: object.MediaType,
			Source: zipObjectSource{archivePath: plan.Archive.Path, entryPath: object.EntryPath, digest: object.Digest, size: object.Size}})
		report.ObjectsWritten++
		report.ObjectBytesWritten += object.Size
	}
	sort.Slice(batch.Records, func(i, j int) bool {
		return batch.Records[i].Record.Identity().String() < batch.Records[j].Record.Identity().String()
	})
	sort.Slice(batch.Objects, func(i, j int) bool { return batch.Objects[i].Digest < batch.Objects[j].Digest })
	if err := target.Apply(ctx, batch); err != nil {
		return ImportReport{}, fmt.Errorf("apply migration batch: %w", err)
	}
	return report, nil
}

type zipObjectSource struct {
	archivePath string
	entryPath   string
	digest      string
	size        int64
}

func (source zipObjectSource) Open(ctx context.Context) (io.ReadCloser, error) {
	file, err := os.Open(source.archivePath)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		file.Close()
		return nil, err
	}
	entry := entryByName(reader, source.entryPath)
	if entry == nil {
		file.Close()
		return nil, errors.New("object entry is absent")
	}
	entryReader, err := entry.Open()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &verifiedObjectReader{ctx: ctx, reader: entryReader, archive: file, expectedDigest: source.digest,
		expectedSize: source.size, hash: sha256.New()}, nil
}

type verifiedObjectReader struct {
	ctx            context.Context
	reader         io.ReadCloser
	archive        *os.File
	expectedDigest string
	expectedSize   int64
	hash           hash.Hash
	read           int64
	finished       bool
}

func (reader *verifiedObjectReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
	}
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.read += int64(count)
		_, _ = reader.hash.Write(buffer[:count])
	}
	if errors.Is(err, io.EOF) {
		reader.finished = true
		if reader.read != reader.expectedSize {
			return count, fmt.Errorf("object size changed: got %d, want %d", reader.read, reader.expectedSize)
		}
		if hex.EncodeToString(reader.hash.Sum(nil)) != reader.expectedDigest {
			return count, errors.New("object checksum changed")
		}
	}
	return count, err
}

func (reader *verifiedObjectReader) Close() error {
	if !reader.finished {
		buffer := make([]byte, 128*1024)
		for {
			_, err := reader.Read(buffer)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				_ = reader.reader.Close()
				_ = reader.archive.Close()
				return err
			}
		}
	}
	readErr := reader.reader.Close()
	fileErr := reader.archive.Close()
	if readErr != nil {
		return readErr
	}
	return fileErr
}
