package exporter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

const ProvenanceManifestVersion = 1

var ErrInvalidProvenance = errors.New("invalid export provenance manifest")

type ExportCompletionStatus string

const (
	ExportCompleted ExportCompletionStatus = "completed"
	ExportPartial   ExportCompletionStatus = "partial"
	ExportFailed    ExportCompletionStatus = "failed"
	ExportCancelled ExportCompletionStatus = "cancelled"
)

type SourceArticle struct {
	ArticleID      domain.ArticleID `json:"articleId"`
	SHA256         string           `json:"sha256"`
	SnapshotSHA256 string           `json:"snapshotSha256,omitempty"`
}

type MissingResource struct {
	ArticleID domain.ArticleID `json:"articleId"`
	Resource  string           `json:"resource"`
	Reason    string           `json:"reason,omitempty"`
}

type Warning struct {
	Code       string             `json:"code"`
	Message    string             `json:"message"`
	ArticleIDs []domain.ArticleID `json:"articleIds,omitempty"`
}

type ProvenanceManifest struct {
	SchemaVersion      int                    `json:"schemaVersion"`
	ApplicationVersion string                 `json:"applicationVersion"`
	ExportID           domain.ExportID        `json:"exportId"`
	Format             string                 `json:"format"`
	Status             ExportCompletionStatus `json:"status"`
	Selection          SelectionManifest      `json:"selection"`
	Sources            []SourceArticle        `json:"sources"`
	Outputs            []OutputFile           `json:"outputs"`
	MissingResources   []MissingResource      `json:"missingResources,omitempty"`
	Warnings           []Warning              `json:"warnings,omitempty"`
	StartedAt          time.Time              `json:"startedAt"`
	CompletedAt        time.Time              `json:"completedAt"`
}

type WarningCollector struct {
	mu      sync.Mutex
	entries map[string]*warningEntry
}

type warningEntry struct {
	code       string
	message    string
	articleIDs map[domain.ArticleID]struct{}
}

func NewWarningCollector() *WarningCollector {
	return &WarningCollector{entries: make(map[string]*warningEntry)}
}

func (collector *WarningCollector) Add(code, message string, articleIDs ...domain.ArticleID) {
	if collector == nil {
		return
	}
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if code == "" || message == "" {
		return
	}
	key := code + "\x00" + message
	collector.mu.Lock()
	defer collector.mu.Unlock()
	if collector.entries == nil {
		collector.entries = make(map[string]*warningEntry)
	}
	entry := collector.entries[key]
	if entry == nil {
		entry = &warningEntry{code: code, message: message, articleIDs: make(map[domain.ArticleID]struct{})}
		collector.entries[key] = entry
	}
	for _, articleID := range articleIDs {
		if strings.TrimSpace(string(articleID)) != "" {
			entry.articleIDs[articleID] = struct{}{}
		}
	}
}

func (collector *WarningCollector) Warnings() []Warning {
	if collector == nil {
		return []Warning{}
	}
	collector.mu.Lock()
	defer collector.mu.Unlock()
	warnings := make([]Warning, 0, len(collector.entries))
	for _, entry := range collector.entries {
		articleIDs := make([]domain.ArticleID, 0, len(entry.articleIDs))
		for articleID := range entry.articleIDs {
			articleIDs = append(articleIDs, articleID)
		}
		sort.Slice(articleIDs, func(i, j int) bool { return articleIDs[i] < articleIDs[j] })
		warnings = append(warnings, Warning{Code: entry.code, Message: entry.message, ArticleIDs: articleIDs})
	}
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Code == warnings[j].Code {
			return warnings[i].Message < warnings[j].Message
		}
		return warnings[i].Code < warnings[j].Code
	})
	return warnings
}

type ProvenanceBuilder struct {
	mu                 sync.Mutex
	applicationVersion string
	exportID           domain.ExportID
	format             string
	selection          SelectionManifest
	startedAt          time.Time
	sources            map[domain.ArticleID]SourceArticle
	outputs            []OutputFile
	missingResources   []MissingResource
	warnings           *WarningCollector
}

func NewProvenanceBuilder(
	applicationVersion string,
	exportID domain.ExportID,
	format string,
	selection SelectionManifest,
	startedAt time.Time,
) *ProvenanceBuilder {
	return &ProvenanceBuilder{
		applicationVersion: strings.TrimSpace(applicationVersion),
		exportID:           exportID,
		format:             strings.TrimSpace(format),
		selection:          cloneSelectionManifest(selection),
		startedAt:          startedAt.UTC(),
		sources:            make(map[domain.ArticleID]SourceArticle),
		outputs:            []OutputFile{},
		missingResources:   []MissingResource{},
		warnings:           NewWarningCollector(),
	}
}

func (builder *ProvenanceBuilder) AddSource(source SourceArticle) error {
	if builder == nil {
		return errors.New("provenance builder is nil")
	}
	if strings.TrimSpace(string(source.ArticleID)) == "" {
		return fmt.Errorf("source article ID is required: %w", ErrInvalidProvenance)
	}
	if !validSHA256(source.SHA256) {
		return fmt.Errorf("source article %s has an invalid SHA-256 digest: %w", source.ArticleID, ErrInvalidProvenance)
	}
	if source.SnapshotSHA256 != "" && !validSHA256(source.SnapshotSHA256) {
		return fmt.Errorf("source article %s has an invalid snapshot SHA-256 digest: %w", source.ArticleID, ErrInvalidProvenance)
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if existing, exists := builder.sources[source.ArticleID]; exists && existing != source {
		return fmt.Errorf("source article %s has conflicting hashes: %w", source.ArticleID, ErrInvalidProvenance)
	}
	builder.sources[source.ArticleID] = source
	return nil
}

func (builder *ProvenanceBuilder) AddOutput(output OutputFile) error {
	if builder == nil {
		return errors.New("provenance builder is nil")
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if err := validateOutputArticles(output, selectionArticleSet(builder.selection)); err != nil {
		return err
	}
	builder.outputs = append(builder.outputs, output)
	return nil
}

func (builder *ProvenanceBuilder) AddMissingResource(resource MissingResource) {
	if builder == nil {
		return
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	builder.missingResources = append(builder.missingResources, resource)
}

func (builder *ProvenanceBuilder) Warn(code, message string, articleIDs ...domain.ArticleID) {
	if builder == nil {
		return
	}
	builder.warnings.Add(code, message, articleIDs...)
}

func (builder *ProvenanceBuilder) Complete(
	status ExportCompletionStatus,
	completedAt time.Time,
) (ProvenanceManifest, error) {
	if builder == nil {
		return ProvenanceManifest{}, errors.New("provenance builder is nil")
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if err := validateCompletionStatus(status); err != nil {
		return ProvenanceManifest{}, err
	}
	if builder.applicationVersion == "" || builder.exportID == "" || builder.format == "" {
		return ProvenanceManifest{}, fmt.Errorf("application version, export ID, and format are required: %w", ErrInvalidProvenance)
	}
	if err := ValidateSelectionManifest(builder.selection); err != nil {
		return ProvenanceManifest{}, fmt.Errorf("a valid selection manifest is required: %w", ErrInvalidProvenance)
	}
	if builder.startedAt.IsZero() {
		return ProvenanceManifest{}, fmt.Errorf("export start time is required: %w", ErrInvalidProvenance)
	}
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	completedAt = completedAt.UTC()
	if completedAt.Before(builder.startedAt) {
		return ProvenanceManifest{}, fmt.Errorf("completion time precedes start time: %w", ErrInvalidProvenance)
	}

	sources := make([]SourceArticle, 0, len(builder.selection.ArticleIDs))
	for _, articleID := range builder.selection.ArticleIDs {
		source, exists := builder.sources[articleID]
		if !exists {
			return ProvenanceManifest{}, fmt.Errorf("source hash missing for article %s: %w", articleID, ErrInvalidProvenance)
		}
		sources = append(sources, source)
	}
	if len(builder.sources) != len(sources) {
		return ProvenanceManifest{}, fmt.Errorf("source hashes include articles outside the selection: %w", ErrInvalidProvenance)
	}
	outputs := append([]OutputFile(nil), builder.outputs...)
	selectionSet := selectionArticleSet(builder.selection)
	for index, output := range outputs {
		if err := validateOutputFile(output); err != nil {
			return ProvenanceManifest{}, fmt.Errorf("output %d: %w", index, err)
		}
		if err := validateOutputArticles(output, selectionSet); err != nil {
			return ProvenanceManifest{}, fmt.Errorf("output %d: %w", index, err)
		}
	}
	missing := append([]MissingResource(nil), builder.missingResources...)
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].ArticleID == missing[j].ArticleID {
			if missing[i].Resource == missing[j].Resource {
				return missing[i].Reason < missing[j].Reason
			}
			return missing[i].Resource < missing[j].Resource
		}
		return missing[i].ArticleID < missing[j].ArticleID
	})
	return ProvenanceManifest{
		SchemaVersion:      ProvenanceManifestVersion,
		ApplicationVersion: builder.applicationVersion,
		ExportID:           builder.exportID,
		Format:             builder.format,
		Status:             status,
		Selection:          cloneSelectionManifest(builder.selection),
		Sources:            sources,
		Outputs:            outputs,
		MissingResources:   missing,
		Warnings:           builder.warnings.Warnings(),
		StartedAt:          builder.startedAt,
		CompletedAt:        completedAt,
	}, nil
}

func WriteProvenanceManifest(
	ctx context.Context,
	manager *OutputManager,
	relativePath string,
	manifest ProvenanceManifest,
	policy CollisionPolicy,
) (OutputFile, error) {
	if err := validateProvenanceManifest(manifest); err != nil {
		return OutputFile{}, err
	}
	data, err := marshalManifest(manifest)
	if err != nil {
		return OutputFile{}, err
	}
	return manager.WriteFile(ctx, relativePath, policy, func(writer io.Writer) error {
		_, err := writer.Write(data)
		return err
	})
}

type VerificationIssueKind string

const (
	VerificationMissingFile      VerificationIssueKind = "missing_file"
	VerificationSizeMismatch     VerificationIssueKind = "size_mismatch"
	VerificationChecksumMismatch VerificationIssueKind = "checksum_mismatch"
	VerificationUnsafePath       VerificationIssueKind = "unsafe_path"
	VerificationInvalidManifest  VerificationIssueKind = "invalid_manifest"
)

type VerificationIssue struct {
	Kind      VerificationIssueKind `json:"kind"`
	ArticleID domain.ArticleID      `json:"articleId,omitempty"`
	Path      string                `json:"path,omitempty"`
	Expected  string                `json:"expected,omitempty"`
	Actual    string                `json:"actual,omitempty"`
	Message   string                `json:"message,omitempty"`
}

type VerificationReport struct {
	Valid              bool                `json:"valid"`
	Manifest           ProvenanceManifest  `json:"manifest"`
	VerifiedOutputs    int                 `json:"verifiedOutputs"`
	Issues             []VerificationIssue `json:"issues"`
	AffectedArticleIDs []domain.ArticleID  `json:"affectedArticleIds,omitempty"`
}

func VerifyProvenanceManifest(ctx context.Context, root, manifestPath string) (VerificationReport, error) {
	report := VerificationReport{Issues: []VerificationIssue{}, AffectedArticleIDs: []domain.ArticleID{}}
	manager, err := NewOutputManager(root)
	if err != nil {
		return report, err
	}
	defer manager.Close()
	canonicalManifestPath, _, err := manager.resolveSafeOutputPath(manifestPath, false)
	if err != nil {
		return report, err
	}
	data, err := manager.readBoundedFile(ctx, canonicalManifestPath, 16<<20)
	if err != nil {
		return report, fmt.Errorf("read provenance manifest %q: %w", canonicalManifestPath, err)
	}
	if err := json.Unmarshal(data, &report.Manifest); err != nil {
		return report, fmt.Errorf("decode provenance manifest %q: %w", canonicalManifestPath, err)
	}
	if err := validateProvenanceManifest(report.Manifest); err != nil {
		// A structurally valid envelope can still contain an unsafe output path.
		// Report that path with the more actionable verification classification
		// instead of collapsing it into a generic invalid-manifest issue.
		for _, output := range report.Manifest.Outputs {
			if _, normalizeErr := normalizeRelativeOutputPath(output.Path); normalizeErr != nil {
				report.Issues = append(report.Issues, VerificationIssue{
					Kind: VerificationUnsafePath, ArticleID: output.ArticleID, Path: output.Path, Message: normalizeErr.Error(),
				})
				markAffectedOutputIDs(&report, output)
			}
		}
		report.Issues = append(report.Issues, VerificationIssue{Kind: VerificationInvalidManifest, Message: err.Error()})
		markAffectedSelectionIDs(&report)
		report.Valid = false
		return report, nil
	}
	affected := make(map[domain.ArticleID]struct{})
	for _, output := range report.Manifest.Outputs {
		canonical, _, err := manager.resolveSafeOutputPath(output.Path, false)
		if err != nil {
			report.Issues = append(report.Issues, VerificationIssue{
				Kind: VerificationUnsafePath, ArticleID: output.ArticleID, Path: output.Path, Message: err.Error(),
			})
			markAffectedOutput(affected, output)
			continue
		}
		digest, size, err := manager.hashRegularFile(ctx, canonical)
		if errors.Is(err, os.ErrNotExist) {
			report.Issues = append(report.Issues, VerificationIssue{
				Kind: VerificationMissingFile, ArticleID: output.ArticleID, Path: canonical, Message: "output file is missing",
			})
			markAffectedOutput(affected, output)
			continue
		}
		if err != nil {
			report.Issues = append(report.Issues, VerificationIssue{
				Kind: VerificationUnsafePath, ArticleID: output.ArticleID, Path: canonical, Message: err.Error(),
			})
			markAffectedOutput(affected, output)
			continue
		}
		mismatched := false
		if size != output.Size {
			report.Issues = append(report.Issues, VerificationIssue{
				Kind: VerificationSizeMismatch, ArticleID: output.ArticleID, Path: canonical,
				Expected: fmt.Sprintf("%d", output.Size), Actual: fmt.Sprintf("%d", size),
			})
			mismatched = true
		}
		if digest != output.SHA256 {
			report.Issues = append(report.Issues, VerificationIssue{
				Kind: VerificationChecksumMismatch, ArticleID: output.ArticleID, Path: canonical,
				Expected: output.SHA256, Actual: digest,
			})
			mismatched = true
		}
		if mismatched {
			markAffectedOutput(affected, output)
		}
		if !mismatched {
			report.VerifiedOutputs++
		}
	}
	report.AffectedArticleIDs = sortedAffectedArticleIDs(affected)
	report.Valid = len(report.Issues) == 0
	return report, nil
}

func markAffectedOutputIDs(report *VerificationReport, output OutputFile) {
	affected := make(map[domain.ArticleID]struct{}, len(report.AffectedArticleIDs)+1+len(output.ArticleIDs))
	for _, articleID := range report.AffectedArticleIDs {
		affected[articleID] = struct{}{}
	}
	markAffectedOutput(affected, output)
	report.AffectedArticleIDs = sortedAffectedArticleIDs(affected)
}

func markAffectedSelectionIDs(report *VerificationReport) {
	if report == nil {
		return
	}
	affected := make(map[domain.ArticleID]struct{}, len(report.Manifest.Selection.ArticleIDs))
	for _, articleID := range report.Manifest.Selection.ArticleIDs {
		if articleID != "" {
			affected[articleID] = struct{}{}
		}
	}
	report.AffectedArticleIDs = sortedAffectedArticleIDs(affected)
}

func sortedAffectedArticleIDs(affected map[domain.ArticleID]struct{}) []domain.ArticleID {
	result := make([]domain.ArticleID, 0, len(affected))
	for articleID := range affected {
		result = append(result, articleID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func markAffectedOutput(affected map[domain.ArticleID]struct{}, output OutputFile) {
	if output.ArticleID != "" {
		affected[output.ArticleID] = struct{}{}
	}
	for _, articleID := range output.ArticleIDs {
		if articleID != "" {
			affected[articleID] = struct{}{}
		}
	}
}

func validateCompletionStatus(status ExportCompletionStatus) error {
	switch status {
	case ExportCompleted, ExportPartial, ExportFailed, ExportCancelled:
		return nil
	default:
		return fmt.Errorf("unsupported export completion status %q: %w", status, ErrInvalidProvenance)
	}
}

func validateOutputFile(output OutputFile) error {
	if strings.TrimSpace(output.Path) == "" {
		return fmt.Errorf("output path is empty: %w", ErrInvalidProvenance)
	}
	if output.Size < 0 || !validSHA256(output.SHA256) {
		return fmt.Errorf("output %q has invalid size or checksum: %w", output.Path, ErrInvalidProvenance)
	}
	if output.ArticleID != "" && len(output.ArticleIDs) > 0 {
		return fmt.Errorf("output %q mixes singular and batch article IDs: %w", output.Path, ErrInvalidProvenance)
	}
	if _, err := normalizeRelativeOutputPath(output.Path); err != nil {
		return err
	}
	if len(output.ArticleIDs) > 0 {
		if err := validateArticleIDs(output.ArticleIDs); err != nil {
			return fmt.Errorf("output %q batch article IDs are invalid: %w", output.Path, err)
		}
	}
	switch output.Status {
	case OutputWritten, OutputSkipped, OutputReplaced:
	default:
		return fmt.Errorf("output %q has invalid status %q: %w", output.Path, output.Status, ErrInvalidProvenance)
	}
	return nil
}

func validateProvenanceManifest(manifest ProvenanceManifest) error {
	if err := validateProvenanceEnvelope(manifest); err != nil {
		return err
	}
	if manifest.StartedAt.IsZero() || manifest.CompletedAt.IsZero() || manifest.CompletedAt.Before(manifest.StartedAt) {
		return fmt.Errorf("manifest timestamps are invalid: %w", ErrInvalidProvenance)
	}
	if len(manifest.Sources) != len(manifest.Selection.ArticleIDs) {
		return fmt.Errorf("source hash count does not match selection: %w", ErrInvalidProvenance)
	}
	for index, source := range manifest.Sources {
		if source.ArticleID != manifest.Selection.ArticleIDs[index] || !validSHA256(source.SHA256) ||
			(source.SnapshotSHA256 != "" && !validSHA256(source.SnapshotSHA256)) {
			return fmt.Errorf("source %d does not match selection or has an invalid hash: %w", index, ErrInvalidProvenance)
		}
	}
	return nil
}

func validateProvenanceEnvelope(manifest ProvenanceManifest) error {
	if manifest.SchemaVersion != ProvenanceManifestVersion {
		return fmt.Errorf("unsupported provenance manifest version %d: %w", manifest.SchemaVersion, ErrInvalidProvenance)
	}
	if strings.TrimSpace(manifest.ApplicationVersion) == "" || manifest.ExportID == "" || strings.TrimSpace(manifest.Format) == "" {
		return fmt.Errorf("application version, export ID, and format are required: %w", ErrInvalidProvenance)
	}
	if err := validateCompletionStatus(manifest.Status); err != nil {
		return err
	}
	if err := ValidateSelectionManifest(manifest.Selection); err != nil {
		return fmt.Errorf("selection manifest is invalid: %w", err)
	}
	if manifest.Format != manifest.Selection.Format {
		return fmt.Errorf("manifest format does not match selection format: %w", ErrInvalidProvenance)
	}
	if len(manifest.Sources) > 0 && len(manifest.Sources) != len(manifest.Selection.ArticleIDs) {
		return fmt.Errorf("source hash count does not match selection: %w", ErrInvalidProvenance)
	}
	for index, source := range manifest.Sources {
		if source.ArticleID != manifest.Selection.ArticleIDs[index] || !validSHA256(source.SHA256) ||
			(source.SnapshotSHA256 != "" && !validSHA256(source.SnapshotSHA256)) {
			return fmt.Errorf("source %d does not match selection or has an invalid hash: %w", index, ErrInvalidProvenance)
		}
	}
	selectionSet := selectionArticleSet(manifest.Selection)
	for index, output := range manifest.Outputs {
		if output.Size < 0 || !validSHA256(output.SHA256) {
			return fmt.Errorf("output %d has an invalid size or checksum: %w", index, ErrInvalidProvenance)
		}
		switch output.Status {
		case OutputWritten, OutputSkipped, OutputReplaced:
		default:
			return fmt.Errorf("output %d has invalid status %q: %w", index, output.Status, ErrInvalidProvenance)
		}
		if strings.TrimSpace(output.Path) == "" {
			return fmt.Errorf("output %d has an empty path: %w", index, ErrInvalidProvenance)
		}
		if _, err := normalizeRelativeOutputPath(output.Path); err != nil {
			return fmt.Errorf("output %d has an unsafe path: %w", index, err)
		}
		if output.ArticleID != "" && len(output.ArticleIDs) > 0 {
			return fmt.Errorf("output %d mixes singular and batch article IDs: %w", index, ErrInvalidProvenance)
		}
		if len(output.ArticleIDs) > 0 {
			if err := validateArticleIDs(output.ArticleIDs); err != nil {
				return fmt.Errorf("output %d batch article IDs are invalid: %w", index, err)
			}
		}
		if err := validateOutputArticles(output, selectionSet); err != nil {
			return fmt.Errorf("output %d: %w", index, err)
		}
	}
	return nil
}

func selectionArticleSet(selection SelectionManifest) map[domain.ArticleID]struct{} {
	result := make(map[domain.ArticleID]struct{}, len(selection.ArticleIDs))
	for _, articleID := range selection.ArticleIDs {
		result[articleID] = struct{}{}
	}
	return result
}

func validateOutputArticles(output OutputFile, selection map[domain.ArticleID]struct{}) error {
	if output.ArticleID != "" {
		if _, ok := selection[output.ArticleID]; !ok {
			return fmt.Errorf("output article %s is outside the selected articles: %w", output.ArticleID, ErrInvalidProvenance)
		}
	}
	for _, articleID := range output.ArticleIDs {
		if _, ok := selection[articleID]; !ok {
			return fmt.Errorf("output article %s is outside the selected articles: %w", articleID, ErrInvalidProvenance)
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func cloneSelectionManifest(manifest SelectionManifest) SelectionManifest {
	data, err := json.Marshal(manifest)
	if err != nil {
		return manifest
	}
	var clone SelectionManifest
	if err := json.Unmarshal(data, &clone); err != nil {
		return manifest
	}
	return clone
}

func marshalManifest(manifest ProvenanceManifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode provenance manifest: %w", err)
	}
	return append(data, '\n'), nil
}

func (manager *OutputManager) readBoundedFile(ctx context.Context, path string, maximum int64) ([]byte, error) {
	if manager == nil {
		return nil, errors.New("export output manager is closed")
	}
	manager.lifecycle.RLock()
	defer manager.lifecycle.RUnlock()
	if manager.handle == nil {
		return nil, errors.New("export output manager is closed")
	}
	file, err := manager.handle.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnsafePath
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	reader := io.LimitReader(file, maximum+1)
	data, err := io.ReadAll(&contextReader{ctx: ctx, reader: reader})
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(data []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(data)
	}
}
