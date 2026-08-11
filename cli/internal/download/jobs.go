package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

// PersistentJobStore is the durable job boundary used by download operations.
// Implementations persist intent and item keys before any network side effect.
type PersistentJobStore interface {
	jobs.EngineStore
	CreateWithItems(context.Context, jobs.Spec, []string) (domain.Job, error)
	CreateOrGetWithItems(context.Context, jobs.Spec, []string) (domain.Job, bool, error)
	GetByIdempotency(context.Context, domain.ProfileID, string, string) (domain.Job, bool, error)
	RecoverStale(context.Context) (int64, error)
}

type JobKind string

const (
	JobArticle  JobKind = "article_download"
	JobResource JobKind = "resource_download"
	JobMetadata JobKind = "metadata_download"
	JobComments JobKind = "comments_download"
	JobPaid     JobKind = "paid_content_download"
)

type JobRequest struct {
	Kind           JobKind                 `json:"kind"`
	Profile        domain.ProfileID        `json:"profile"`
	IdempotencyKey string                  `json:"idempotencyKey,omitempty"`
	Articles       []ArticleRequest        `json:"articles,omitempty"`
	Resources      []ResourceRequest       `json:"resources,omitempty"`
	Metadata       []MetadataRequest       `json:"metadata,omitempty"`
	Comments       []CommentsRequest       `json:"comments,omitempty"`
	Paid           []PaidContentJobRequest `json:"paid,omitempty"`
}

// PaidContentJobRequest adds durable article identity to a paid fetch so the
// fetched body can pass through the normal processor/object-store commit path.
type PaidContentJobRequest struct {
	ArticleID domain.ArticleID `json:"articleId"`
	AccountID domain.AccountID `json:"accountId"`
	URL       string           `json:"url"`
	Force     bool             `json:"force,omitempty"`
}

type JobService struct {
	Store     PersistentJobStore
	Engine    jobs.EngineOptions
	Articles  ArticleDownloader
	Resources ResourceDownloader
	Metadata  MetadataDownloader
	Comments  CommentsDownloader
	Paid      PaidArticleDownloader
}

// Start persists a deterministic item set without starting external work.
func (service JobService) Start(ctx context.Context, request JobRequest) (domain.Job, error) {
	if service.Store == nil {
		return domain.Job{}, errors.New("persistent download job store is required")
	}
	items, err := encodeJobItems(request)
	if err != nil {
		return domain.Job{}, err
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.key)
	}
	spec := jobs.Spec{
		Kind:           string(request.Kind),
		Profile:        request.Profile,
		IdempotencyKey: strings.TrimSpace(request.IdempotencyKey),
		Payload: map[string]any{
			"kind":      request.Kind,
			"itemCount": len(keys),
		},
	}
	if spec.IdempotencyKey != "" {
		job, _, err := service.Store.CreateOrGetWithItems(ctx, spec, keys)
		return job, err
	}
	return service.Store.CreateWithItems(ctx, spec, keys)
}

// GetByIdempotency returns an existing job in the request's profile and kind.
// Parent handoffs use it before resolving mutable selections so a retry keeps
// the already-durable child intent intact.
func (service JobService) GetByIdempotency(ctx context.Context, profile domain.ProfileID, kind JobKind, key string) (domain.Job, bool, error) {
	if service.Store == nil {
		return domain.Job{}, false, errors.New("persistent download job store is required")
	}
	return service.Store.GetByIdempotency(ctx, profile, string(kind), key)
}

// Run attaches a job executor. Completed items are omitted by the persistent
// engine; stale running work can be recovered and resumed without duplicate
// committed records.
func (service JobService) Run(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if service.Store == nil {
		return domain.Job{}, errors.New("persistent download job store is required")
	}
	options := service.Engine
	if strings.TrimSpace(options.Owner) == "" {
		options.Owner = "download-worker"
	}
	if options.Metadata == nil {
		options.Metadata = metadataForJobItem
	}
	engine, err := jobs.NewEngine(service.Store, options)
	if err != nil {
		return domain.Job{}, err
	}
	return engine.Run(ctx, id, service.execute)
}

// Recover marks abandoned running items resumable before a later Run call.
func (service JobService) Recover(ctx context.Context) (int64, error) {
	if service.Store == nil {
		return 0, errors.New("persistent download job store is required")
	}
	return service.Store.RecoverStale(ctx)
}

type encodedJobItem struct {
	key string
}

type itemEnvelope struct {
	Version  int             `json:"version"`
	Kind     JobKind         `json:"kind"`
	TargetID string          `json:"targetId"`
	Payload  json.RawMessage `json:"payload"`
}

func encodeJobItems(request JobRequest) ([]encodedJobItem, error) {
	if strings.TrimSpace(string(request.Kind)) == "" {
		return nil, errors.New("download job kind is required")
	}
	var values []any
	switch request.Kind {
	case JobArticle:
		values = toAnySlice(request.Articles)
	case JobResource:
		values = toAnySlice(request.Resources)
	case JobMetadata:
		values = toAnySlice(request.Metadata)
	case JobComments:
		values = toAnySlice(request.Comments)
	case JobPaid:
		values = toAnySlice(request.Paid)
	default:
		return nil, fmt.Errorf("unsupported download job kind %q", request.Kind)
	}
	if len(values) == 0 {
		return nil, errors.New("download job requires at least one item")
	}
	items := make([]encodedJobItem, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		targetID, err := validateJobPayload(request.Kind, value)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", index, err)
		}
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode item %d: %w", index, err)
		}
		envelope, err := json.Marshal(itemEnvelope{Version: 1, Kind: request.Kind, TargetID: targetID, Payload: payload})
		if err != nil {
			return nil, fmt.Errorf("encode item envelope %d: %w", index, err)
		}
		key := string(envelope)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, encodedJobItem{key: key})
	}
	return items, nil
}

func validateJobPayload(kind JobKind, value any) (string, error) {
	switch request := value.(type) {
	case ArticleRequest:
		if kind != JobArticle || request.ArticleID == "" || strings.TrimSpace(request.URL) == "" {
			return "", errors.New("article job item requires article ID and URL")
		}
		return string(request.ArticleID), nil
	case ResourceRequest:
		if kind != JobResource || request.ArticleID == "" || strings.TrimSpace(request.URL) == "" {
			return "", errors.New("resource job item requires article ID and URL")
		}
		return string(request.ArticleID) + ":" + request.URL, nil
	case MetadataRequest:
		if kind != JobMetadata || request.ArticleID == "" || request.AccountID == "" || strings.TrimSpace(request.URL) == "" {
			return "", errors.New("metadata job item requires article ID, account ID, and URL")
		}
		return string(request.ArticleID), nil
	case CommentsRequest:
		if kind != JobComments || request.ArticleID == "" || request.AccountID == "" || strings.TrimSpace(request.CommentID) == "" {
			return "", errors.New("comments job item requires article ID, account ID, and comment ID")
		}
		return string(request.ArticleID), nil
	case PaidContentJobRequest:
		if kind != JobPaid || request.ArticleID == "" || request.AccountID == "" || strings.TrimSpace(request.URL) == "" {
			return "", errors.New("paid content job item requires article ID, account ID, and URL")
		}
		return string(request.ArticleID), nil
	default:
		return "", errors.New("unsupported download job payload")
	}
}

func toAnySlice[T any](values []T) []any {
	result := make([]any, len(values))
	for index := range values {
		result[index] = values[index]
	}
	return result
}

func decodeJobItem(key string) (itemEnvelope, error) {
	var envelope itemEnvelope
	decoder := json.NewDecoder(strings.NewReader(key))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return itemEnvelope{}, fmt.Errorf("decode download job item: %w", err)
	}
	if envelope.Version != 1 || envelope.Kind == "" || len(envelope.Payload) == 0 {
		return itemEnvelope{}, errors.New("download job item envelope is invalid")
	}
	return envelope, nil
}

func (service JobService) execute(ctx context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
	envelope, err := decodeJobItem(item.Key)
	if err != nil {
		return classified(jobs.FailureParsing, false, err)
	}
	switch envelope.Kind {
	case JobArticle:
		var request ArticleRequest
		if err := json.Unmarshal(envelope.Payload, &request); err != nil {
			return classified(jobs.FailureParsing, false, err)
		}
		result, err := service.Articles.Download(ctx, request)
		if err == nil {
			return checkpoint(map[string]any{"committed": true, "cached": result.Cached, "classification": result.Classification.State})
		}
		return classifyDownloadError(err, result.Classification.State)
	case JobResource:
		var request ResourceRequest
		if err := json.Unmarshal(envelope.Payload, &request); err != nil {
			return classified(jobs.FailureParsing, false, err)
		}
		result, err := service.Resources.Download(ctx, request)
		if err == nil {
			return checkpoint(map[string]any{"committed": true, "cached": result.Cached, "missing": result.Missing})
		}
		return classifyDownloadError(err, "")
	case JobMetadata:
		var request MetadataRequest
		if err := json.Unmarshal(envelope.Payload, &request); err != nil {
			return classified(jobs.FailureParsing, false, err)
		}
		result, err := service.Metadata.Download(ctx, request)
		if err == nil {
			return checkpoint(map[string]any{"committed": true, "capturedAt": result.Snapshot.CapturedAt})
		}
		return classifyDownloadError(err, "")
	case JobComments:
		var request CommentsRequest
		if err := json.Unmarshal(envelope.Payload, &request); err != nil {
			return classified(jobs.FailureParsing, false, err)
		}
		result, err := service.Comments.Download(ctx, request)
		checkpointErr := checkpoint(map[string]any{
			"pagesCommitted": result.PagesCommitted, "commentsStored": result.CommentsStored,
			"replyThreadsCompleted": result.ReplyThreadsCompleted, "replyThreadsFailed": result.ReplyThreadsFailed,
			"partial": result.Partial,
		})
		if err == nil {
			return checkpointErr
		}
		classifiedErr := classifyDownloadError(err, "")
		if errors.Is(classifiedErr, context.Canceled) || errors.Is(classifiedErr, context.DeadlineExceeded) {
			if checkpointErr != nil {
				return errors.Join(classifiedErr, checkpointErr)
			}
			return classifiedErr
		}
		if result.Partial {
			class, _ := jobs.Classify(classifiedErr)
			if class == jobs.FailureAuthentication {
				if checkpointErr != nil {
					return errors.Join(classifiedErr, checkpointErr)
				}
				return classifiedErr
			}
			if checkpointErr != nil {
				return errors.Join(classifiedErr, checkpointErr)
			}
			return &jobs.PartialError{Class: class, Err: err}
		}
		if checkpointErr != nil {
			return errors.Join(classifiedErr, checkpointErr)
		}
		return classifiedErr
	case JobPaid:
		var request PaidContentJobRequest
		if err := json.Unmarshal(envelope.Payload, &request); err != nil {
			return classified(jobs.FailureParsing, false, err)
		}
		result, err := service.Paid.Download(ctx, request)
		if err == nil {
			return checkpoint(map[string]any{"committed": true, "classification": result.Classification.State})
		}
		return classifyDownloadError(err, result.Classification.State)
	default:
		return classified(jobs.FailureParsing, false, fmt.Errorf("unsupported download job kind %q", envelope.Kind))
	}
}

func metadataForJobItem(item jobs.Item) jobs.WorkMetadata {
	envelope, err := decodeJobItem(item.Key)
	if err != nil {
		return jobs.WorkMetadata{Operation: "download"}
	}
	host := ""
	var rawURL string
	switch envelope.Kind {
	case JobArticle:
		var request ArticleRequest
		_ = json.Unmarshal(envelope.Payload, &request)
		rawURL = request.URL
	case JobResource:
		var request ResourceRequest
		_ = json.Unmarshal(envelope.Payload, &request)
		rawURL = request.URL
	case JobMetadata:
		var request MetadataRequest
		_ = json.Unmarshal(envelope.Payload, &request)
		rawURL = request.URL
	case JobPaid:
		var request PaidContentJobRequest
		_ = json.Unmarshal(envelope.Payload, &request)
		rawURL = request.URL
	}
	if parsed, err := url.Parse(rawURL); err == nil {
		host = parsed.Hostname()
	}
	return jobs.WorkMetadata{
		Operation: string(envelope.Kind),
		Host:      host,
		Sensitive: envelope.Kind == JobMetadata || envelope.Kind == JobComments || envelope.Kind == JobPaid,
	}
}

func classifyDownloadError(err error, state processor.ClassificationState) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, wechat.ErrContentThrottled) {
		return classified(jobs.FailureThrottling, true, err)
	}
	if errors.Is(err, credentials.ErrCredentialMissing) || errors.Is(err, credentials.ErrCredentialExpired) {
		return classified(jobs.FailureAuthentication, false, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch state {
	case processor.ClassificationDeleted:
		return classified(jobs.FailureDeleted, false, err)
	case processor.ClassificationUnavailable:
		return classified(jobs.FailureUnavailable, false, err)
	case processor.ClassificationRiskControl:
		return classified(jobs.FailureThrottling, true, err)
	case processor.ClassificationParseError:
		return classified(jobs.FailureParsing, false, err)
	}
	var classifiedError *jobs.ClassifiedError
	if errors.As(err, &classifiedError) {
		return err
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "http 429"), strings.Contains(message, "rate limit"), strings.Contains(message, "throttl"):
		return classified(jobs.FailureThrottling, true, err)
	case strings.Contains(message, "download"), strings.Contains(message, "request"), strings.Contains(message, "timeout"),
		strings.Contains(message, "connection"), strings.Contains(message, "temporary upstream"):
		return classified(jobs.FailureNetwork, true, err)
	default:
		var processError *processor.ProcessError
		if errors.As(err, &processError) || errors.Is(err, ErrUnavailable) {
			return classified(jobs.FailureParsing, false, err)
		}
		return classified(jobs.FailureStorage, false, err)
	}
}

func classified(class jobs.FailureClass, retryable bool, err error) error {
	return &jobs.ClassifiedError{Class: class, Retryable: retryable, Err: err}
}
