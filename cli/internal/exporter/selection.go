package exporter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

const SelectionManifestVersion = 1

var ErrInvalidSelection = errors.New("invalid export selection")

type SelectionSource interface {
	ResolveArticleURL(context.Context, string) (domain.ArticleID, error)
	LoadSavedArticleQuery(context.Context, string) (domain.ArticleQuery, error)
	QueryArticleIDs(context.Context, domain.ArticleQuery) ([]domain.ArticleID, error)
}

type SelectionManifest struct {
	SchemaVersion int                        `json:"schemaVersion"`
	ID            string                     `json:"id"`
	DigestSHA256  string                     `json:"digestSha256"`
	Kind          domain.ExportSelectionKind `json:"kind"`
	Selection     domain.ExportSelection     `json:"selection"`
	ArticleIDs    []domain.ArticleID         `json:"articleIds"`
	FilterSummary string                     `json:"filterSummary"`
	Format        string                     `json:"format"`
	Options       domain.ExportOptions       `json:"options"`
	CreatedAt     time.Time                  `json:"createdAt"`
}

func BuildSelectionManifest(
	ctx context.Context,
	source SelectionSource,
	request domain.ExportRequest,
	createdAt time.Time,
) (SelectionManifest, error) {
	format := strings.TrimSpace(request.Format)
	if format == "" {
		return SelectionManifest{}, fmt.Errorf("format is required: %w", ErrInvalidSelection)
	}
	selection, err := normalizeRequestedSelection(request)
	if err != nil {
		return SelectionManifest{}, err
	}
	articleIDs, resolvedSelection, err := resolveSelection(ctx, source, selection)
	if err != nil {
		return SelectionManifest{}, err
	}
	if err := validateArticleIDs(articleIDs); err != nil {
		return SelectionManifest{}, err
	}
	filterSummary, err := canonicalJSON(resolvedSelection)
	if err != nil {
		return SelectionManifest{}, fmt.Errorf("encode filter summary: %w", err)
	}
	options, err := cloneExportOptions(request.Options)
	if err != nil {
		return SelectionManifest{}, fmt.Errorf("copy export options: %w", err)
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	createdAt = createdAt.UTC()
	manifest := SelectionManifest{
		SchemaVersion: SelectionManifestVersion,
		Kind:          resolvedSelection.Kind,
		Selection:     resolvedSelection,
		ArticleIDs:    append([]domain.ArticleID(nil), articleIDs...),
		FilterSummary: filterSummary,
		Format:        format,
		Options:       options,
		CreatedAt:     createdAt,
	}
	digest, err := selectionManifestDigest(manifest)
	if err != nil {
		return SelectionManifest{}, err
	}
	manifest.DigestSHA256 = digest
	manifest.ID = "selection-" + digest
	return manifest, nil
}

func normalizeRequestedSelection(request domain.ExportRequest) (domain.ExportSelection, error) {
	hasLegacyIDs := len(request.ArticleIDs) > 0
	hasSelection := request.Selection.Kind != "" || selectionHasValues(request.Selection)
	if hasLegacyIDs && hasSelection {
		return domain.ExportSelection{}, fmt.Errorf("legacy articleIds and selection cannot both be set: %w", ErrInvalidSelection)
	}
	if hasLegacyIDs {
		return domain.ExportSelection{
			Kind:       domain.ExportSelectionExplicitIDs,
			ArticleIDs: append([]domain.ArticleID(nil), request.ArticleIDs...),
		}, nil
	}
	if !hasSelection || request.Selection.Kind == "" {
		return domain.ExportSelection{}, fmt.Errorf("selection is required: %w", ErrInvalidSelection)
	}
	selection := request.Selection
	selection.URLs = append([]string(nil), selection.URLs...)
	selection.ArticleIDs = append([]domain.ArticleID(nil), selection.ArticleIDs...)
	return selection, nil
}

func selectionHasValues(selection domain.ExportSelection) bool {
	return len(selection.URLs) > 0 || selection.AccountID != "" || selection.AlbumID != "" ||
		selection.SavedQueryID != "" || len(selection.ArticleIDs) > 0 || articleQueryHasValues(selection.Query)
}

func articleQueryHasValues(query domain.ArticleQuery) bool {
	return query.AccountID != "" || query.AlbumID != "" || query.Keyword != "" || query.Author != "" || query.State != "" ||
		!query.PublishedFrom.IsZero() || !query.PublishedTo.IsZero() || query.Deleted != nil || query.HasContent != nil ||
		query.HasComments != nil || query.Original != nil || query.Paid != nil || len(query.MessageTypes) > 0 ||
		query.ReadMin != nil || query.ReadMax != nil || query.OldLikeMin != nil || query.OldLikeMax != nil ||
		query.ShareMin != nil || query.ShareMax != nil || query.LikeMin != nil || query.LikeMax != nil ||
		query.CommentMin != nil || query.CommentMax != nil || query.WeCoinMin != nil || query.WeCoinMax != nil ||
		query.MediaSecondsMin != nil || query.MediaSecondsMax != nil || query.Sort != "" || len(query.Sorts) > 0 ||
		query.Offset != 0 || query.Limit != 0
}

func resolveSelection(
	ctx context.Context,
	source SelectionSource,
	selection domain.ExportSelection,
) ([]domain.ArticleID, domain.ExportSelection, error) {
	switch selection.Kind {
	case domain.ExportSelectionURLs:
		if len(selection.URLs) == 0 || selectionHasUnexpectedFields(selection, "urls") {
			return nil, selection, fmt.Errorf("URL selection requires only one or more URLs: %w", ErrInvalidSelection)
		}
		if source == nil {
			return nil, selection, errors.New("URL selection source is required")
		}
		ids := make([]domain.ArticleID, 0, len(selection.URLs))
		for index, rawURL := range selection.URLs {
			if strings.TrimSpace(rawURL) == "" {
				return nil, selection, fmt.Errorf("URL %d is empty: %w", index, ErrInvalidSelection)
			}
			id, err := source.ResolveArticleURL(ctx, rawURL)
			if err != nil {
				return nil, selection, fmt.Errorf("resolve URL %d: %w", index, err)
			}
			ids = append(ids, id)
		}
		return ids, selection, nil
	case domain.ExportSelectionAccount:
		if selection.AccountID == "" || selectionHasUnexpectedFields(selection, "account") {
			return nil, selection, fmt.Errorf("account selection requires only accountId: %w", ErrInvalidSelection)
		}
		query := normalizeSelectionQuery(domain.ArticleQuery{AccountID: selection.AccountID})
		ids, err := querySelectionIDs(ctx, source, query)
		return ids, selection, err
	case domain.ExportSelectionAlbum:
		if selection.AlbumID == "" || selectionHasUnexpectedFields(selection, "album") {
			return nil, selection, fmt.Errorf("album selection requires only albumId: %w", ErrInvalidSelection)
		}
		query := normalizeSelectionQuery(domain.ArticleQuery{AlbumID: selection.AlbumID})
		ids, err := querySelectionIDs(ctx, source, query)
		return ids, selection, err
	case domain.ExportSelectionSavedQuery:
		if strings.TrimSpace(selection.SavedQueryID) == "" || selectionHasUnexpectedFields(selection, "saved_query") {
			return nil, selection, fmt.Errorf("saved query selection requires only savedQueryId: %w", ErrInvalidSelection)
		}
		if source == nil {
			return nil, selection, errors.New("saved query selection source is required")
		}
		query, err := source.LoadSavedArticleQuery(ctx, selection.SavedQueryID)
		if err != nil {
			return nil, selection, fmt.Errorf("load saved query %q: %w", selection.SavedQueryID, err)
		}
		query = normalizeSelectionQuery(query)
		selection.Query = query
		ids, err := querySelectionIDs(ctx, source, query)
		return ids, selection, err
	case domain.ExportSelectionExplicitIDs:
		if len(selection.ArticleIDs) == 0 || selectionHasUnexpectedFields(selection, "explicit_ids") {
			return nil, selection, fmt.Errorf("explicit ID selection requires only articleIds: %w", ErrInvalidSelection)
		}
		return append([]domain.ArticleID(nil), selection.ArticleIDs...), selection, nil
	case domain.ExportSelectionAllMatching:
		if selectionHasUnexpectedFields(selection, "all_matching") {
			return nil, selection, fmt.Errorf("all-matching selection accepts only query filters: %w", ErrInvalidSelection)
		}
		selection.Query = normalizeSelectionQuery(selection.Query)
		ids, err := querySelectionIDs(ctx, source, selection.Query)
		return ids, selection, err
	default:
		return nil, selection, fmt.Errorf("unsupported selection kind %q: %w", selection.Kind, ErrInvalidSelection)
	}
}

func selectionHasUnexpectedFields(selection domain.ExportSelection, allowed string) bool {
	if allowed != "urls" && len(selection.URLs) > 0 {
		return true
	}
	if allowed != "account" && selection.AccountID != "" {
		return true
	}
	if allowed != "album" && selection.AlbumID != "" {
		return true
	}
	if allowed != "saved_query" && selection.SavedQueryID != "" {
		return true
	}
	if allowed != "explicit_ids" && len(selection.ArticleIDs) > 0 {
		return true
	}
	if allowed != "all_matching" && articleQueryHasValues(selection.Query) {
		return true
	}
	return false
}

func normalizeSelectionQuery(query domain.ArticleQuery) domain.ArticleQuery {
	query.Offset = 0
	query.Limit = 0
	if strings.TrimSpace(query.Sort) == "" {
		query.Sort = "published_desc"
	}
	return query
}

func querySelectionIDs(ctx context.Context, source SelectionSource, query domain.ArticleQuery) ([]domain.ArticleID, error) {
	if source == nil {
		return nil, errors.New("article query selection source is required")
	}
	ids, err := source.QueryArticleIDs(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query article IDs: %w", err)
	}
	return append([]domain.ArticleID(nil), ids...), nil
}

func validateArticleIDs(ids []domain.ArticleID) error {
	if len(ids) == 0 {
		return fmt.Errorf("selection resolved to no articles: %w", ErrInvalidSelection)
	}
	seen := make(map[domain.ArticleID]struct{}, len(ids))
	for index, id := range ids {
		if strings.TrimSpace(string(id)) == "" {
			return fmt.Errorf("article ID %d is empty: %w", index, ErrInvalidSelection)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("duplicate article ID %q: %w", id, ErrInvalidSelection)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func cloneExportOptions(options domain.ExportOptions) (domain.ExportOptions, error) {
	data, err := json.Marshal(options)
	if err != nil {
		return domain.ExportOptions{}, err
	}
	var clone domain.ExportOptions
	if err := json.Unmarshal(data, &clone); err != nil {
		return domain.ExportOptions{}, err
	}
	return clone, nil
}

func canonicalJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var canonical any
	if err := json.Unmarshal(data, &canonical); err != nil {
		return "", err
	}
	canonical = omitCanonicalEmptyValues(canonical)
	data, err = json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func omitCanonicalEmptyValues(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			child = omitCanonicalEmptyValues(child)
			if canonicalValueEmpty(child) {
				continue
			}
			result[key] = child
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = omitCanonicalEmptyValues(child)
		}
		return result
	default:
		return value
	}
}

func canonicalValueEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == "" || typed == "0001-01-01T00:00:00Z"
	case float64:
		return typed == 0
	case bool:
		return !typed
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}

func selectionManifestDigest(manifest SelectionManifest) (string, error) {
	identity := struct {
		SchemaVersion int                        `json:"schemaVersion"`
		Kind          domain.ExportSelectionKind `json:"kind"`
		Selection     domain.ExportSelection     `json:"selection"`
		ArticleIDs    []domain.ArticleID         `json:"articleIds"`
		FilterSummary string                     `json:"filterSummary"`
		Format        string                     `json:"format"`
		Options       domain.ExportOptions       `json:"options"`
		CreatedAt     time.Time                  `json:"createdAt"`
	}{
		SchemaVersion: manifest.SchemaVersion,
		Kind:          manifest.Kind,
		Selection:     manifest.Selection,
		ArticleIDs:    manifest.ArticleIDs,
		FilterSummary: manifest.FilterSummary,
		Format:        manifest.Format,
		Options:       manifest.Options,
		CreatedAt:     manifest.CreatedAt,
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode selection manifest identity: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
