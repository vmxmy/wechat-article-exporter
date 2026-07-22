package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type PlanOptions struct {
	Limits Limits
}

type PlanReport struct {
	SourceRecords     int                `json:"sourceRecords"`
	PlannedRecords    int                `json:"plannedRecords"`
	ArchiveDuplicates int                `json:"archiveDuplicates"`
	Objects           int                `json:"objects"`
	ObjectBytes       int64              `json:"objectBytes"`
	MissingResources  int                `json:"missingResources"`
	Counts            map[RecordKind]int `json:"counts"`
	Warnings          []string           `json:"warnings,omitempty"`
}

type ObjectPlan struct {
	Digest    string
	Size      int64
	MediaType string
	EntryPath string
}

type ImportPlan struct {
	Archive  ValidatedArchive
	Manifest Manifest
	Records  []Record
	Objects  []ObjectPlan
	Report   PlanReport
}

type recordEnvelope struct {
	SchemaVersion int               `json:"schemaVersion"`
	Records       []json.RawMessage `json:"records"`
}

func Plan(ctx context.Context, archivePath string, options PlanOptions) (ImportPlan, error) {
	archive, err := Validate(ctx, archivePath, options.Limits)
	if err != nil {
		return ImportPlan{}, err
	}
	file, reader, err := openValidatedZIP(archive)
	if err != nil {
		return ImportPlan{}, fmt.Errorf("open validated archive: %w", err)
	}
	defer file.Close()

	report := PlanReport{Counts: map[RecordKind]int{}, Warnings: []string{}}
	records := make([]Record, 0)
	objects := make([]ObjectPlan, 0)
	for _, manifestFile := range archive.Manifest.Files {
		select {
		case <-ctx.Done():
			return ImportPlan{}, ctx.Err()
		default:
		}
		if manifestFile.Kind == FileObject {
			objects = append(objects, ObjectPlan{Digest: manifestFile.SHA256, Size: manifestFile.Size,
				MediaType: manifestFile.MediaType, EntryPath: manifestFile.Path})
			report.ObjectBytes += manifestFile.Size
			continue
		}
		entry := entryByName(reader, manifestFile.Path)
		if entry == nil {
			return ImportPlan{}, fmt.Errorf("validated record entry %q disappeared", manifestFile.Path)
		}
		body, _, err := readZipEntry(ctx, entry, manifestFile.Size)
		if err != nil {
			return ImportPlan{}, fmt.Errorf("read %s: %w", manifestFile.Path, err)
		}
		parsed, warnings, sourceCount, err := parseDataset(manifestFile.Dataset, body, archive.Manifest.Source.SchemaVersion())
		if err != nil {
			return ImportPlan{}, fmt.Errorf("parse %s: %w", manifestFile.Path, err)
		}
		records = append(records, parsed...)
		report.SourceRecords += sourceCount
		report.Warnings = append(report.Warnings, warnings...)
	}

	records = reconcileURLRelations(records)
	records, duplicates := dedupeArchiveRecords(records)
	report.ArchiveDuplicates = duplicates
	report.PlannedRecords = len(records)
	report.Objects = len(objects)
	for _, record := range records {
		report.Counts[record.Kind]++
	}
	report.MissingResources = countMissingResources(records)
	if len(archive.Manifest.MissingResources) > report.MissingResources {
		report.MissingResources = len(archive.Manifest.MissingResources)
	}
	for _, warning := range archive.Manifest.Warnings {
		report.Warnings = append(report.Warnings, warning.Message)
	}
	if report.MissingResources > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d referenced resource(s) have no archived bytes", report.MissingResources))
	}
	if len(archive.Manifest.Files) == 0 {
		report.Warnings = append(report.Warnings, "archive contains no data files")
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Identity().String() < records[j].Identity().String()
	})
	sort.Slice(objects, func(i, j int) bool { return objects[i].Digest < objects[j].Digest })
	return ImportPlan{Archive: archive, Manifest: archive.Manifest, Records: records, Objects: objects, Report: report}, nil
}

func parseDataset(dataset Dataset, body []byte, dexieVersion int) ([]Record, []string, int, error) {
	var rawRecords []json.RawMessage
	var envelope recordEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.SchemaVersion != 0 {
		if envelope.SchemaVersion != CurrentSchemaVersion {
			return nil, nil, 0, fmt.Errorf("unsupported record schema version %d", envelope.SchemaVersion)
		}
		rawRecords = envelope.Records
	} else if err := json.Unmarshal(body, &rawRecords); err != nil {
		return nil, nil, 0, err
	}
	records := make([]Record, 0, len(rawRecords))
	warnings := make([]string, 0)
	for index, raw := range rawRecords {
		var wrapped struct {
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Value) > 0 {
			raw = wrapped.Value
		}
		parsed, parseWarnings, err := parseRecord(dataset, raw, dexieVersion)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("record %d: %w", index, err)
		}
		records = append(records, parsed...)
		warnings = append(warnings, parseWarnings...)
	}
	return records, warnings, len(rawRecords), nil
}

func parseRecord(dataset Dataset, raw json.RawMessage, dexieVersion int) ([]Record, []string, error) {
	switch dataset {
	case DatasetAccounts:
		var wire wireAccount
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(wire.FakeID) == "" {
			return nil, nil, errors.New("account fakeid is required")
		}
		updatedAt := parseUnix(maxInt64(wire.UpdateTime, wire.LastUpdateTime))
		account := Account{FakeID: wire.FakeID, Name: wire.Nickname, AvatarURL: wire.AvatarURL, Completed: wire.Completed,
			MessageCount: wire.MessageCount, ArticleCount: wire.ArticleCount, UpstreamTotal: wire.UpstreamTotal, UpdatedAt: updatedAt}
		return []Record{newRecord(RecordAccount, recordKey(RecordAccount, wire.FakeID), updatedAt, RecordData{Account: &account})}, nil, nil
	case DatasetArticles:
		var wire wireArticle
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(wire.Link) == "" {
			return nil, nil, errors.New("article link is required")
		}
		updatedAt := parseUnix(wire.UpdateTime)
		article := Article{FakeID: wire.FakeID, Aid: wire.Aid, AppMsgID: wire.AppMsgID, ItemIndex: wire.ItemIndex,
			Title: wire.Title, Author: wire.Author, Digest: wire.Digest, CanonicalURL: canonicalURLKey(wire.Link), CoverURL: wire.Cover,
			PublishedAt: parseUnix(wire.CreateTime), UpdatedAt: updatedAt, MessageType: wire.MessageType, State: wire.State,
			Deleted: wire.Deleted, Paid: wire.Paid != 0, Single: wire.Single}
		return []Record{newRecord(RecordArticle, recordKey(RecordArticle, article.CanonicalURL), updatedAt, RecordData{Article: &article})}, nil, nil
	case DatasetHTML:
		var wire wireHTML
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, nil, err
		}
		warnings := []string{}
		if dexieVersion == 1 && strings.TrimSpace(wire.FakeID) == "" {
			wire.FakeID = ""
			warnings = append(warnings, "Dexie v1 HTML record has no fakeid; it will be reconciled by article URL")
		}
		digest := defaultString(wire.ObjectDigest, wire.Content.SHA256)
		mediaType := defaultString(wire.MediaType, wire.Content.MediaType)
		html := HTML{FakeID: wire.FakeID, URL: canonicalURLKey(wire.URL), Title: wire.Title, CommentID: wire.CommentID,
			ObjectDigest: digest, MediaType: defaultString(mediaType, "text/html"), UpdatedAt: parseUnix(wire.UpdatedAt)}
		return []Record{newRecord(RecordHTML, recordKey(RecordHTML, html.URL), html.UpdatedAt, RecordData{HTML: &html})}, warnings, nil
	case DatasetMetadata, DatasetMetrics:
		var wire wireMetric
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, nil, err
		}
		capturedAt := parseUnix(maxInt64(wire.CapturedAt, wire.UpdatedAt))
		metric := Metric{FakeID: wire.FakeID, URL: canonicalURLKey(wire.URL), ReadCount: chooseInt(wire.ReadCount, wire.ReadNum),
			OldLikeCount: chooseInt(wire.OldLikeCount, wire.OldLikeNum), ShareCount: chooseInt(wire.ShareCount, wire.ShareNum),
			LikeCount: chooseInt(wire.LikeCount, wire.LikeNum), CommentCount: chooseInt(wire.CommentCount, wire.CommentNum), CapturedAt: capturedAt}
		key := recordKey(RecordMetric, metric.URL, capturedAt.UTC().Format(time.RFC3339Nano))
		return []Record{newRecord(RecordMetric, key, capturedAt, RecordData{Metric: &metric})}, nil, nil
	case DatasetComments:
		return parseComments(raw)
	case DatasetReplies:
		return parseReplies(raw)
	case DatasetResourceMaps:
		var wire wireResourceMap
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, nil, err
		}
		mapping := ResourceMap{FakeID: wire.FakeID, URL: canonicalURLKey(wire.URL), Resources: uniqueStrings(wire.Resources), UpdatedAt: parseUnix(wire.UpdatedAt)}
		return []Record{newRecord(RecordResourceMap, recordKey(RecordResourceMap, mapping.URL), mapping.UpdatedAt, RecordData{ResourceMap: &mapping})}, nil, nil
	case DatasetResources, DatasetAssets:
		var wire wireResource
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, nil, err
		}
		resource := Resource{FakeID: wire.FakeID, URL: canonicalURLKey(wire.URL), ObjectDigest: defaultString(wire.ObjectDigest, wire.Content.SHA256),
			MediaType: defaultString(wire.MediaType, wire.Content.MediaType), UpdatedAt: parseUnix(wire.UpdatedAt)}
		return []Record{newRecord(RecordResource, recordKey(RecordResource, resource.URL), resource.UpdatedAt, RecordData{Resource: &resource})}, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported dataset %q", dataset)
	}
}

func parseComments(raw json.RawMessage) ([]Record, []string, error) {
	var asset wireCommentAsset
	if err := json.Unmarshal(raw, &asset); err != nil {
		return nil, nil, err
	}
	responses, err := decodeCommentResponses(asset.Data)
	if err != nil {
		return nil, nil, err
	}
	fetchedAt := parseUnix(asset.UpdatedAt)
	records := make([]Record, 0)
	for _, response := range responses {
		for _, wire := range response.ElectedComments {
			comment := Comment{FakeID: asset.FakeID, URL: canonicalURLKey(asset.URL), UpstreamID: wire.ContentID,
				AuthorName: wire.AuthorName, Content: wire.Content, LikeCount: wire.LikeCount, CreatedAt: parseUnix(wire.CreatedAt),
				FetchedAt: fetchedAt, ReplyTotal: wire.ReplyNew.ReplyTotal, ReplyMaxID: wire.ReplyNew.MaxReplyID,
				Complete: !response.ContinueFlag, Buffer: response.Buffer}
			records = append(records, newRecord(RecordComment, recordKey(RecordComment, comment.URL, comment.UpstreamID),
				maxTime(fetchedAt, comment.CreatedAt), RecordData{Comment: &comment}))
			for index, wireReply := range wire.ReplyNew.Replies {
				reply := Reply{FakeID: asset.FakeID, URL: comment.URL, CommentUpstreamID: comment.UpstreamID,
					UpstreamID: replyFallbackID(wireReply, index), AuthorName: wireReply.AuthorName, Content: wireReply.Content,
					LikeCount: wireReply.LikeCount, CreatedAt: parseUnix(wireReply.CreatedAt), FetchedAt: fetchedAt,
					MaxReplyID: wire.ReplyNew.MaxReplyID}
				records = append(records, newRecord(RecordReply, recordKey(RecordReply, reply.URL, reply.CommentUpstreamID, reply.UpstreamID),
					maxTime(fetchedAt, reply.CreatedAt), RecordData{Reply: &reply}))
			}
		}
	}
	return records, nil, nil
}

func parseReplies(raw json.RawMessage) ([]Record, []string, error) {
	var asset wireReplyAsset
	if err := json.Unmarshal(raw, &asset); err != nil {
		return nil, nil, err
	}
	var response wireReplyResponse
	if err := json.Unmarshal(asset.Data, &response); err != nil {
		return nil, nil, err
	}
	fetchedAt := parseUnix(asset.UpdatedAt)
	records := make([]Record, 0, len(response.ReplyList.Replies))
	for index, wire := range response.ReplyList.Replies {
		reply := Reply{FakeID: asset.FakeID, URL: canonicalURLKey(asset.URL), CommentUpstreamID: asset.ContentID,
			UpstreamID: replyFallbackID(wire, index), AuthorName: wire.AuthorName, Content: wire.Content,
			LikeCount: wire.LikeCount, CreatedAt: parseUnix(wire.CreatedAt), FetchedAt: fetchedAt,
			MaxReplyID: response.ReplyList.MaxReplyID}
		records = append(records, newRecord(RecordReply, recordKey(RecordReply, reply.URL, reply.CommentUpstreamID, reply.UpstreamID),
			maxTime(fetchedAt, reply.CreatedAt), RecordData{Reply: &reply}))
	}
	return records, nil, nil
}

func decodeCommentResponses(raw json.RawMessage) ([]wireCommentResponse, error) {
	var responses []wireCommentResponse
	if err := json.Unmarshal(raw, &responses); err == nil {
		return responses, nil
	}
	var response wireCommentResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	return []wireCommentResponse{response}, nil
}

func newRecord(kind RecordKind, key string, updatedAt time.Time, data RecordData) Record {
	fingerprintBody, _ := json.Marshal(data)
	digest := sha256.Sum256(fingerprintBody)
	return Record{Kind: kind, Key: key, UpdatedAt: updatedAt, Fingerprint: hex.EncodeToString(digest[:]), Data: data}
}

func dedupeArchiveRecords(records []Record) ([]Record, int) {
	unique := make(map[RecordIdentity]Record, len(records))
	duplicates := 0
	for _, record := range records {
		identity := record.Identity()
		current, exists := unique[identity]
		if !exists {
			unique[identity] = record
			continue
		}
		duplicates++
		if current.UpdatedAt.Before(record.UpdatedAt) || (current.UpdatedAt.Equal(record.UpdatedAt) && record.Fingerprint < current.Fingerprint) {
			unique[identity] = record
		}
	}
	result := make([]Record, 0, len(unique))
	for _, record := range unique {
		result = append(result, record)
	}
	return result, duplicates
}

func reconcileURLRelations(records []Record) []Record {
	articleFakeIDs := make(map[string]string)
	for _, record := range records {
		if record.Data.Article != nil && record.Data.Article.FakeID != "" {
			articleFakeIDs[canonicalURLKey(record.Data.Article.CanonicalURL)] = record.Data.Article.FakeID
		}
	}
	for index, record := range records {
		var rawURL string
		var fakeID *string
		switch {
		case record.Data.HTML != nil:
			rawURL, fakeID = record.Data.HTML.URL, &record.Data.HTML.FakeID
		case record.Data.Metric != nil:
			rawURL, fakeID = record.Data.Metric.URL, &record.Data.Metric.FakeID
		case record.Data.Comment != nil:
			rawURL, fakeID = record.Data.Comment.URL, &record.Data.Comment.FakeID
		case record.Data.Reply != nil:
			rawURL, fakeID = record.Data.Reply.URL, &record.Data.Reply.FakeID
		case record.Data.ResourceMap != nil:
			rawURL, fakeID = record.Data.ResourceMap.URL, &record.Data.ResourceMap.FakeID
		}
		if fakeID != nil && *fakeID == "" {
			*fakeID = articleFakeIDs[canonicalURLKey(rawURL)]
			if *fakeID != "" {
				records[index] = newRecord(record.Kind, record.Key, record.UpdatedAt, record.Data)
			}
		}
	}
	return records
}

func countMissingResources(records []Record) int {
	resources := map[string]struct{}{}
	mapped := map[string]struct{}{}
	for _, record := range records {
		if record.Data.Resource != nil && strings.TrimSpace(record.Data.Resource.ObjectDigest) != "" {
			resources[canonicalURLKey(record.Data.Resource.URL)] = struct{}{}
		}
		if record.Data.ResourceMap != nil {
			for _, resourceURL := range record.Data.ResourceMap.Resources {
				mapped[canonicalURLKey(resourceURL)] = struct{}{}
			}
		}
	}
	missing := 0
	for resourceURL := range mapped {
		if _, ok := resources[resourceURL]; !ok {
			missing++
		}
	}
	return missing
}

func chooseInt(primary, fallback int) int {
	if primary != 0 {
		return primary
	}
	return fallback
}

func maxInt64(values ...int64) int64 {
	var maximum int64
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}

func maxTime(values ...time.Time) time.Time {
	var maximum time.Time
	for _, value := range values {
		if value.After(maximum) {
			maximum = value
		}
	}
	return maximum
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = canonicalURLKey(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
