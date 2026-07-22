package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
)

// LocalTarget applies a validated legacy archive to the active local profile.
// Object bytes are committed before SQLite references are created; records are
// then applied in dependency order so foreign-key relationships remain valid.
type LocalTarget struct {
	Library *library.Database
	Objects *objects.FileStore
}

func (target LocalTarget) Inspect(ctx context.Context, inventory Inventory) (TargetState, error) {
	if target.Library == nil || target.Objects == nil {
		return TargetState{}, errors.New("local migration target requires library and object store")
	}
	state := TargetState{Records: map[RecordIdentity]LocalRecord{}, Objects: map[string]LocalObject{}}
	for _, identity := range inventory.Records {
		local, found, err := target.inspectRecord(ctx, identity)
		if err != nil {
			return TargetState{}, err
		}
		if found {
			state.Records[identity] = local
		}
	}
	for _, digest := range inventory.Objects {
		object, err := target.Objects.Stat(digest)
		if err == nil {
			state.Objects[digest] = LocalObject{Size: object.Size}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return TargetState{}, fmt.Errorf("inspect local object %s: %w", digest, err)
		}
	}
	return state, nil
}

func (target LocalTarget) inspectRecord(ctx context.Context, identity RecordIdentity) (LocalRecord, bool, error) {
	parts := strings.Split(identity.Key, "\x00")
	switch identity.Kind {
	case RecordAccount:
		account, err := target.Library.GetAccountByFakeID(ctx, identity.Key)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, fmt.Errorf("inspect local account: %w", err)
		}
		return LocalRecord{UpdatedAt: account.LastSyncAt}, true, nil
	case RecordArticle, RecordHTML, RecordMetric, RecordComment, RecordReply, RecordResourceMap:
		rawURL := recordIdentityURL(identity)
		if rawURL == "" {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, rawURL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, fmt.Errorf("inspect local article for %s: %w", identity.String(), err)
		}
		switch identity.Kind {
		case RecordArticle:
			return LocalRecord{UpdatedAt: article.UpdatedAt}, true, nil
		case RecordHTML:
			content, contentErr := target.Library.CurrentContent(ctx, article.ID, "html")
			if errors.Is(contentErr, sql.ErrNoRows) {
				return LocalRecord{}, false, nil
			}
			return LocalRecord{UpdatedAt: content.CapturedAt}, contentErr == nil, contentErr
		case RecordMetric:
			metric, metricErr := target.Library.LatestMetricSnapshot(ctx, article.ID)
			if errors.Is(metricErr, sql.ErrNoRows) {
				return LocalRecord{}, false, nil
			}
			return LocalRecord{UpdatedAt: metric.CapturedAt}, metricErr == nil, metricErr
		case RecordComment:
			comments, commentErr := target.Library.CommentsForArticle(ctx, article.ID)
			if commentErr != nil {
				return LocalRecord{}, false, commentErr
			}
			upstreamID := parts[len(parts)-1]
			for _, comment := range comments {
				if comment.UpstreamID == upstreamID {
					return LocalRecord{UpdatedAt: comment.FetchedAt}, true, nil
				}
			}
			return LocalRecord{}, false, nil
		case RecordReply:
			if len(parts) < 3 {
				return LocalRecord{}, false, nil
			}
			replies, replyErr := target.Library.RepliesForComment(ctx, article.ID, parts[len(parts)-2])
			if errors.Is(replyErr, sql.ErrNoRows) {
				return LocalRecord{}, false, nil
			}
			if replyErr != nil {
				return LocalRecord{}, false, replyErr
			}
			for _, reply := range replies {
				if reply.UpstreamID == parts[len(parts)-1] {
					return LocalRecord{UpdatedAt: reply.FetchedAt}, true, nil
				}
			}
			return LocalRecord{}, false, nil
		case RecordResourceMap:
			resources, resourceErr := target.Library.ListArticleResources(ctx, article.ID)
			return LocalRecord{UpdatedAt: article.UpdatedAt}, resourceErr == nil && len(resources) > 0, resourceErr
		}
	case RecordResource:
		_, err := target.Library.ResourceByURL(ctx, identity.Key)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		return LocalRecord{}, err == nil, err
	}
	return LocalRecord{}, false, nil
}

func (target LocalTarget) inspectPlannedRecord(ctx context.Context, record Record) (LocalRecord, bool, error) {
	switch record.Kind {
	case RecordAccount:
		if record.Data.Account == nil {
			return LocalRecord{}, false, nil
		}
		account, err := target.Library.GetAccountByFakeID(ctx, record.Data.Account.FakeID)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		return LocalRecord{UpdatedAt: account.LastSyncAt}, err == nil, err
	case RecordArticle:
		if record.Data.Article == nil {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, record.Data.Article.CanonicalURL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		return LocalRecord{UpdatedAt: article.UpdatedAt}, err == nil, err
	case RecordHTML:
		if record.Data.HTML == nil {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, record.Data.HTML.URL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, err
		}
		content, err := target.Library.CurrentContent(ctx, article.ID, "html")
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		return LocalRecord{UpdatedAt: content.CapturedAt}, err == nil, err
	case RecordMetric:
		if record.Data.Metric == nil {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, record.Data.Metric.URL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, err
		}
		metric, err := target.Library.LatestMetricSnapshot(ctx, article.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		return LocalRecord{UpdatedAt: metric.CapturedAt}, err == nil, err
	case RecordComment:
		if record.Data.Comment == nil {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, record.Data.Comment.URL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, err
		}
		comments, err := target.Library.CommentsForArticle(ctx, article.ID)
		if err != nil {
			return LocalRecord{}, false, err
		}
		for _, comment := range comments {
			if comment.UpstreamID == record.Data.Comment.UpstreamID {
				return LocalRecord{UpdatedAt: comment.FetchedAt}, true, nil
			}
		}
		return LocalRecord{}, false, nil
	case RecordReply:
		if record.Data.Reply == nil {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, record.Data.Reply.URL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, err
		}
		replies, err := target.Library.RepliesForComment(ctx, article.ID, record.Data.Reply.CommentUpstreamID)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, err
		}
		for _, reply := range replies {
			if reply.UpstreamID == record.Data.Reply.UpstreamID {
				return LocalRecord{UpdatedAt: reply.FetchedAt}, true, nil
			}
		}
		return LocalRecord{}, false, nil
	case RecordResourceMap:
		if record.Data.ResourceMap == nil {
			return LocalRecord{}, false, nil
		}
		article, err := target.Library.GetArticleByCanonicalURL(ctx, record.Data.ResourceMap.URL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		if err != nil {
			return LocalRecord{}, false, err
		}
		resources, err := target.Library.ListArticleResources(ctx, article.ID)
		return LocalRecord{UpdatedAt: article.UpdatedAt}, err == nil && len(resources) > 0, err
	case RecordResource:
		if record.Data.Resource == nil {
			return LocalRecord{}, false, nil
		}
		_, err := target.Library.ResourceByURL(ctx, record.Data.Resource.URL)
		if errors.Is(err, sql.ErrNoRows) {
			return LocalRecord{}, false, nil
		}
		return LocalRecord{}, err == nil, err
	default:
		return LocalRecord{}, false, nil
	}
}

func recordIdentityURL(identity RecordIdentity) string {
	parts := strings.Split(identity.Key, "\x00")
	switch identity.Kind {
	case RecordMetric:
		if len(parts) < 2 {
			return ""
		}
		return strings.Join(parts[:len(parts)-1], "\x00")
	case RecordComment:
		if len(parts) < 2 {
			return ""
		}
		return strings.Join(parts[:len(parts)-1], "\x00")
	case RecordReply:
		if len(parts) < 3 {
			return ""
		}
		return strings.Join(parts[:len(parts)-2], "\x00")
	default:
		return identity.Key
	}
}

func (target LocalTarget) Apply(ctx context.Context, batch ImportBatch) error {
	if target.Library == nil || target.Objects == nil {
		return errors.New("local migration target requires library and object store")
	}
	for _, staged := range batch.Objects {
		reader, err := staged.Source.Open(ctx)
		if err != nil {
			return fmt.Errorf("open staged object %s: %w", staged.Digest, err)
		}
		written, putErr := target.Objects.Put(ctx, reader, staged.MediaType)
		closeErr := reader.Close()
		if putErr != nil {
			return fmt.Errorf("store staged object %s: %w", staged.Digest, putErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close staged object %s: %w", staged.Digest, closeErr)
		}
		if written.Digest != staged.Digest || written.Size != staged.Size {
			return fmt.Errorf("stored object %s does not match staged checksum/size", staged.Digest)
		}
	}

	records := append([]ReconciledRecord(nil), batch.Records...)
	sort.SliceStable(records, func(i, j int) bool {
		return recordApplyOrder(records[i].Record.Kind) < recordApplyOrder(records[j].Record.Kind)
	})
	accountIDs := map[string]domain.AccountID{}
	articleIDs := map[string]domain.ArticleID{}
	resourceMaps := map[string]ResourceMap{}
	for _, reconciled := range records {
		record := reconciled.Record
		switch record.Kind {
		case RecordAccount:
			value := record.Data.Account
			account, err := target.Library.ImportLegacyAccount(ctx, domain.Account{
				FakeID: value.FakeID, Name: value.Name, AvatarURL: value.AvatarURL, LastSyncAt: value.UpdatedAt,
				MessageCount: value.MessageCount, ArticleCount: value.ArticleCount, UpstreamTotal: value.UpstreamTotal,
				SyncCompleted: value.Completed,
			})
			if err != nil {
				return fmt.Errorf("import legacy account %s: %w", value.FakeID, err)
			}
			accountIDs[value.FakeID] = account.ID
		case RecordArticle:
			value := record.Data.Article
			accountID, err := target.resolveAccount(ctx, accountIDs, value.FakeID)
			if err != nil {
				return err
			}
			local, lookupErr := target.Library.GetArticleByCanonicalURL(ctx, value.CanonicalURL)
			articleID := domain.ArticleID("")
			if lookupErr == nil {
				articleID = local.ID
			} else if !errors.Is(lookupErr, sql.ErrNoRows) {
				return lookupErr
			}
			if articleID == "" {
				articleID = stableImportedArticleID(value.CanonicalURL)
			}
			if err := target.Library.ImportLegacyArticleRecord(ctx, library.ArticleRecord{ID: articleID, AccountID: accountID,
				Aid: value.Aid, Title: value.Title, Author: value.Author, Digest: value.Digest,
				CanonicalURL: value.CanonicalURL, CoverURL: value.CoverURL, PublishedAt: value.PublishedAt,
				UpdatedAt: value.UpdatedAt, MessageType: value.MessageType, State: value.State,
				Deleted: value.Deleted, Paid: value.Paid, Single: value.Single, ContentStatus: "missing",
			}, value.AppMsgID, value.ItemIndex); err != nil {
				return fmt.Errorf("import legacy article %s: %w", value.CanonicalURL, err)
			}
			stored, err := target.Library.GetArticleByCanonicalURL(ctx, value.CanonicalURL)
			if err != nil {
				return err
			}
			articleIDs[value.CanonicalURL] = stored.ID
		case RecordHTML:
			value := record.Data.HTML
			if value.ObjectDigest == "" {
				continue
			}
			articleID, err := target.resolveArticle(ctx, articleIDs, value.URL)
			if err != nil {
				return err
			}
			object, err := target.localObject(ctx, value.ObjectDigest, value.MediaType)
			if err != nil {
				return err
			}
			if _, err := target.Library.CommitContent(ctx, articleID, object, "html", value.URL, "valid", value.CommentID, value.UpdatedAt); err != nil {
				return fmt.Errorf("import HTML %s: %w", value.URL, err)
			}
		case RecordMetric:
			value := record.Data.Metric
			articleID, err := target.resolveArticle(ctx, articleIDs, value.URL)
			if err != nil {
				return err
			}
			_, err = target.Library.ImportLegacyMetricSnapshot(ctx, library.MetricSnapshot{ArticleID: articleID,
				ReadCount: value.ReadCount, OldLikeCount: value.OldLikeCount, ShareCount: value.ShareCount,
				LikeCount: value.LikeCount, CommentCount: value.CommentCount,
				CapturedAt: value.CapturedAt})
			if err != nil {
				return fmt.Errorf("import metric %s: %w", value.URL, err)
			}
		case RecordComment:
			value := record.Data.Comment
			articleID, err := target.resolveArticle(ctx, articleIDs, value.URL)
			if err != nil {
				return err
			}
			_, err = target.Library.CommitCommentPage(ctx, articleID, library.CommentPageCommit{Comments: []library.CommentRecord{{
				UpstreamID: value.UpstreamID, AuthorName: value.AuthorName, Content: value.Content,
				LikeCount: value.LikeCount, CreatedAt: value.CreatedAt, ReplyTotal: value.ReplyTotal,
				ReplyMaxID: value.ReplyMaxID,
			}}, Buffer: value.Buffer, Complete: value.Complete, FetchedAt: value.FetchedAt})
			if err != nil {
				return fmt.Errorf("import comment %s: %w", value.UpstreamID, err)
			}
		case RecordReply:
			value := record.Data.Reply
			articleID, err := target.resolveArticle(ctx, articleIDs, value.URL)
			if err != nil {
				return err
			}
			_, err = target.Library.CommitReplyPage(ctx, articleID, value.CommentUpstreamID, library.ReplyPageCommit{
				Replies: []library.ReplyRecord{{UpstreamID: value.UpstreamID, AuthorName: value.AuthorName,
					Content: value.Content, LikeCount: value.LikeCount, CreatedAt: value.CreatedAt, FetchedAt: value.FetchedAt}},
				MaxReplyID: value.MaxReplyID, FetchedAt: value.FetchedAt,
			})
			if err != nil {
				return fmt.Errorf("import reply %s: %w", value.UpstreamID, err)
			}
		case RecordResourceMap:
			resourceMaps[record.Data.ResourceMap.URL] = *record.Data.ResourceMap
		case RecordResource:
			value := record.Data.Resource
			if value.ObjectDigest == "" {
				continue
			}
			for articleURL, mapping := range resourceMaps {
				ordinal := stringIndex(mapping.Resources, value.URL)
				if ordinal < 0 {
					continue
				}
				articleID, err := target.resolveArticle(ctx, articleIDs, articleURL)
				if err != nil {
					return err
				}
				object, err := target.localObject(ctx, value.ObjectDigest, value.MediaType)
				if err != nil {
					return err
				}
				if _, err := target.Library.CommitResource(ctx, articleID, value.URL, "legacy", ordinal, object); err != nil {
					return fmt.Errorf("import resource %s: %w", value.URL, err)
				}
			}
		}
	}
	for articleURL, mapping := range resourceMaps {
		articleID, err := target.resolveArticle(ctx, articleIDs, articleURL)
		if err != nil {
			return err
		}
		for ordinal, resourceURL := range mapping.Resources {
			if _, err := target.Library.ResourceByURL(ctx, resourceURL); err == nil {
				continue
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err := target.Library.MarkResourceMissing(ctx, articleID, resourceURL, "legacy", ordinal); err != nil {
				return fmt.Errorf("mark missing resource %s: %w", resourceURL, err)
			}
		}
	}
	return nil
}

func (target LocalTarget) resolveAccount(ctx context.Context, cache map[string]domain.AccountID, fakeID string) (domain.AccountID, error) {
	if fakeID == "" {
		return "", nil
	}
	if id := cache[fakeID]; id != "" {
		return id, nil
	}
	account, err := target.Library.GetAccountByFakeID(ctx, fakeID)
	if err != nil {
		return "", fmt.Errorf("resolve imported account %s: %w", fakeID, err)
	}
	cache[fakeID] = account.ID
	return account.ID, nil
}

func (target LocalTarget) resolveArticle(ctx context.Context, cache map[string]domain.ArticleID, rawURL string) (domain.ArticleID, error) {
	if id := cache[rawURL]; id != "" {
		return id, nil
	}
	article, err := target.Library.GetArticleByCanonicalURL(ctx, rawURL)
	if err != nil {
		return "", fmt.Errorf("resolve imported article %s: %w", rawURL, err)
	}
	cache[rawURL] = article.ID
	return article.ID, nil
}

func (target LocalTarget) localObject(ctx context.Context, digest, mediaType string) (objects.Object, error) {
	reader, object, err := target.Objects.Open(ctx, digest)
	if err != nil {
		return objects.Object{}, err
	}
	_, copyErr := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return objects.Object{}, copyErr
	}
	if closeErr != nil {
		return objects.Object{}, closeErr
	}
	object.MediaType = mediaType
	return object, nil
}

func recordApplyOrder(kind RecordKind) int {
	switch kind {
	case RecordAccount:
		return 0
	case RecordArticle:
		return 1
	case RecordHTML:
		return 2
	case RecordMetric:
		return 3
	case RecordComment:
		return 4
	case RecordReply:
		return 5
	case RecordResourceMap:
		return 6
	case RecordResource:
		return 7
	default:
		return 100
	}
}

func stringIndex(values []string, wanted string) int {
	for index, value := range values {
		if value == wanted {
			return index
		}
	}
	return -1
}

func stableImportedArticleID(rawURL string) domain.ArticleID {
	digest := sha256.Sum256([]byte(rawURL))
	return domain.ArticleID("article:" + hex.EncodeToString(digest[:16]))
}

// VerifyReport compares the source archive with records and object bytes that
// are visible through the active local profile after import.
type VerifyReport struct {
	ArchiveFingerprint string         `json:"archiveFingerprint"`
	SourceCounts       map[string]int `json:"sourceCounts"`
	VerifiedCounts     map[string]int `json:"verifiedCounts"`
	MissingRecords     []string       `json:"missingRecords,omitempty"`
	MissingObjects     []string       `json:"missingObjects,omitempty"`
	CorruptObjects     []string       `json:"corruptObjects,omitempty"`
	MissingResources   int            `json:"missingResources"`
	Success            bool           `json:"success"`
}

func Verify(ctx context.Context, plan ImportPlan, target LocalTarget) (VerifyReport, error) {
	state, err := target.Inspect(ctx, Inventory{Objects: objectDigests(plan.Objects)})
	if err != nil {
		return VerifyReport{}, err
	}
	report := VerifyReport{ArchiveFingerprint: plan.Archive.Fingerprint, SourceCounts: map[string]int{},
		VerifiedCounts: map[string]int{}, MissingRecords: []string{}, MissingObjects: []string{}, CorruptObjects: []string{},
		MissingResources: plan.Report.MissingResources}
	for _, record := range plan.Records {
		kind := string(record.Kind)
		report.SourceCounts[kind]++
		_, ok, inspectErr := target.inspectPlannedRecord(ctx, record)
		if inspectErr != nil {
			return VerifyReport{}, inspectErr
		}
		if ok {
			report.VerifiedCounts[kind]++
		} else {
			report.MissingRecords = append(report.MissingRecords, record.Identity().String())
		}
	}
	for _, object := range plan.Objects {
		if _, ok := state.Objects[object.Digest]; !ok {
			report.MissingObjects = append(report.MissingObjects, object.Digest)
			continue
		}
		if err := target.Objects.Validate(ctx, object.Digest); err != nil {
			report.CorruptObjects = append(report.CorruptObjects, object.Digest)
		}
	}
	sort.Strings(report.MissingRecords)
	sort.Strings(report.MissingObjects)
	sort.Strings(report.CorruptObjects)
	report.Success = len(report.MissingRecords) == 0 && len(report.MissingObjects) == 0 && len(report.CorruptObjects) == 0
	return report, nil
}

func objectDigests(objects []ObjectPlan) []string {
	result := make([]string, len(objects))
	for index, object := range objects {
		result[index] = object.Digest
	}
	return result
}

var _ Target = LocalTarget{}
