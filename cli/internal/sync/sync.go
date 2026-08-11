package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

const (
	RecommendedMinimumDelay = 3 * time.Second
	defaultPageSize         = 5
	defaultPageDelay        = 5 * time.Second
)

var (
	ErrUnsafePacingConfirmation = errors.New("page delay is below the recommended minimum; explicit confirmation is required for persistent use")
	ErrAccountNotFound          = errors.New("account synchronization target was not found")
)

type Source interface {
	ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error)
}

type Store interface {
	GetAccount(context.Context, domain.AccountID) (domain.Account, error)
	GetAccountSyncState(context.Context, domain.AccountID) (domain.AccountSyncState, error)
	SaveArticlePage(context.Context, library.ArticlePageCommit) error
}

type SleepFunc func(context.Context, time.Duration) error
type JitterFunc func(time.Duration) time.Duration
type CheckpointFunc func(any) error

type Options struct {
	Now    func() time.Time
	Sleep  SleepFunc
	Jitter JitterFunc
}

type Runner struct {
	source Source
	store  Store
	now    func() time.Time
	sleep  SleepFunc
	jitter JitterFunc
}

type Checkpoint struct {
	NextOffset      int       `json:"nextOffset"`
	UpstreamTotal   int       `json:"upstreamTotal"`
	MessagesFetched int       `json:"messagesFetched"`
	ArticlesFetched int       `json:"articlesFetched"`
	PagesCommitted  int       `json:"pagesCommitted"`
	Boundary        time.Time `json:"boundary,omitempty"`
	StopReason      string    `json:"stopReason,omitempty"`
	Completed       bool      `json:"completed"`
}

type Result struct {
	Account         domain.Account `json:"account"`
	UpstreamTotal   int            `json:"upstreamTotal"`
	MessagesFetched int            `json:"messagesFetched"`
	ArticlesFetched int            `json:"articlesFetched"`
	PagesCommitted  int            `json:"pagesCommitted"`
	LastSyncAt      time.Time      `json:"lastSyncAt"`
	Boundary        time.Time      `json:"boundary,omitempty"`
	StopReason      string         `json:"stopReason"`
	NextOffset      int            `json:"nextOffset"`
	Completed       bool           `json:"completed"`
}

type PacePolicy struct {
	Delay        time.Duration `json:"delay"`
	Jitter       time.Duration `json:"jitter"`
	MinimumDelay time.Duration `json:"minimumDelay"`
	Warning      string        `json:"warning,omitempty"`
	Confirmed    bool          `json:"confirmed"`
}

func NewRunner(source Source, store Store, options Options) (*Runner, error) {
	if source == nil {
		return nil, errors.New("account sync source is required")
	}
	if store == nil {
		return nil, errors.New("account sync store is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.Jitter == nil {
		options.Jitter = func(maximum time.Duration) time.Duration {
			if maximum <= 0 {
				return 0
			}
			return time.Duration(rand.Int64N(int64(maximum) + 1))
		}
	}
	return &Runner{source: source, store: store, now: options.Now, sleep: options.Sleep, jitter: options.Jitter}, nil
}

func ValidatePacing(request domain.SynchronizeAccountRequest) (PacePolicy, error) {
	delay := request.PageDelay
	if delay <= 0 {
		delay = defaultPageDelay
	}
	policy := PacePolicy{Delay: delay, Jitter: request.Jitter, MinimumDelay: RecommendedMinimumDelay}
	if policy.Jitter < 0 {
		return PacePolicy{}, errors.New("sync jitter cannot be negative")
	}
	if delay >= RecommendedMinimumDelay {
		policy.Confirmed = true
		return policy, nil
	}
	policy.Warning = fmt.Sprintf(
		"page delay %s is below the recommended minimum %s and can increase WeChat account risk",
		delay, RecommendedMinimumDelay,
	)
	if request.PersistentPacing && !request.ConfirmUnsafePacing {
		return policy, fmt.Errorf("%w: %s", ErrUnsafePacingConfirmation, policy.Warning)
	}
	policy.Confirmed = request.ConfirmUnsafePacing
	return policy, nil
}

func ResolveBoundary(request domain.SynchronizeAccountRequest, now time.Time, state domain.AccountSyncState) (time.Time, error) {
	rangeChoice := request.Range
	if rangeChoice == "" {
		if !request.NotBefore.IsZero() {
			rangeChoice = domain.SyncRangePoint
		} else if request.Incremental {
			if !state.LatestArticle.IsZero() {
				return state.LatestArticle, nil
			}
			if !state.Account.LastSyncAt.IsZero() {
				return state.Account.LastSyncAt, nil
			}
			rangeChoice = domain.SyncRangeAll
		} else {
			rangeChoice = domain.SyncRangeAll
		}
	}
	switch rangeChoice {
	case domain.SyncRangeAll:
		return time.Time{}, nil
	case domain.SyncRangePoint:
		if request.NotBefore.IsZero() {
			return time.Time{}, errors.New("point synchronization range requires a date or timestamp boundary")
		}
		return request.NotBefore, nil
	case domain.SyncRange24Hours:
		return now.Add(-24 * time.Hour), nil
	case domain.SyncRange1Day:
		return startOfTomorrow(now).AddDate(0, 0, -1), nil
	case domain.SyncRange3Days:
		return startOfTomorrow(now).AddDate(0, 0, -3), nil
	case domain.SyncRange7Days:
		return startOfTomorrow(now).AddDate(0, 0, -7), nil
	case domain.SyncRange1Month:
		return startOfTomorrow(now).AddDate(0, -1, 0), nil
	case domain.SyncRange3Months:
		return startOfTomorrow(now).AddDate(0, -3, 0), nil
	case domain.SyncRange6Months:
		return startOfTomorrow(now).AddDate(0, -6, 0), nil
	case domain.SyncRange1Year:
		return startOfTomorrow(now).AddDate(-1, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported synchronization range %q", rangeChoice)
	}
}

func (runner *Runner) Run(
	ctx context.Context,
	request domain.SynchronizeAccountRequest,
	checkpointData json.RawMessage,
	checkpoint CheckpointFunc,
) (Result, error) {
	if request.AccountID == "" {
		return Result{}, errors.New("account ID is required")
	}
	pace, err := ValidatePacing(request)
	if err != nil {
		return Result{}, err
	}
	state, err := runner.store.GetAccountSyncState(ctx, request.AccountID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrAccountNotFound, err)
	}
	if strings.TrimSpace(state.Account.FakeID) == "" {
		return Result{}, fmt.Errorf("%w: account lacks a fakeid", ErrAccountNotFound)
	}
	boundary, err := ResolveBoundary(request, runner.now(), state)
	if err != nil {
		return Result{}, err
	}
	pageSize := request.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > 50 {
		pageSize = 50
	}
	current := Checkpoint{Boundary: boundary}
	if len(checkpointData) > 0 && string(checkpointData) != "{}" && string(checkpointData) != "null" {
		if err := json.Unmarshal(checkpointData, &current); err != nil {
			return Result{}, fmt.Errorf("decode account sync checkpoint: %w", err)
		}
		if current.Boundary.IsZero() {
			current.Boundary = boundary
		}
	}
	offset := current.NextOffset
	for {
		page, err := runner.source.ListArticles(ctx, wechat.ArticleListRequest{
			FakeID: state.Account.FakeID, Offset: offset, Limit: pageSize,
		})
		if err != nil {
			if classified := classifyDiscoveryError(err); classified != nil {
				return resultFromCheckpoint(state.Account, current, runner.now()), classified
			}
			return resultFromCheckpoint(state.Account, current, runner.now()), err
		}
		accepted, boundaryReached := filterPageByBoundary(page.Items, boundary)
		completed := page.Completed || boundaryReached
		stopReason := ""
		if boundaryReached {
			stopReason = "date_boundary"
		} else if page.Completed {
			stopReason = "upstream_complete"
		}
		fetchedAt := runner.now()
		if err := runner.store.SaveArticlePage(ctx, library.ArticlePageCommit{
			AccountFakeID: state.Account.FakeID,
			Articles:      accepted,
			UpstreamTotal: page.Total,
			NextOffset:    page.Next,
			MessageCount:  page.Next,
			Completed:     completed,
			FetchedAt:     fetchedAt,
		}); err != nil {
			return resultFromCheckpoint(state.Account, current, fetchedAt), err
		}
		current.NextOffset = page.Next
		current.UpstreamTotal = page.Total
		current.MessagesFetched = page.Next
		current.ArticlesFetched += len(accepted)
		current.PagesCommitted++
		current.Completed = completed
		current.StopReason = stopReason
		if checkpoint != nil {
			if err := checkpoint(current); err != nil {
				return resultFromCheckpoint(state.Account, current, fetchedAt), err
			}
		}
		if completed {
			return resultFromCheckpoint(state.Account, current, fetchedAt), nil
		}
		if page.Next <= offset {
			return resultFromCheckpoint(state.Account, current, fetchedAt), fmt.Errorf(
				"WeChat article pagination did not advance beyond offset %d", offset,
			)
		}
		offset = page.Next
		delay := pace.Delay + runner.jitter(pace.Jitter)
		if err := runner.sleep(ctx, delay); err != nil {
			return resultFromCheckpoint(state.Account, current, fetchedAt), err
		}
	}
}

func EncodeCheckpoint(checkpoint Checkpoint) json.RawMessage {
	encoded, _ := json.Marshal(checkpoint)
	return encoded
}

func DecodeOffset(cursor string) int {
	offset, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func filterPageByBoundary(items []domain.Article, boundary time.Time) ([]domain.Article, bool) {
	if boundary.IsZero() || len(items) == 0 {
		return append([]domain.Article(nil), items...), false
	}
	accepted := make([]domain.Article, 0, len(items))
	hasTimestamp := false
	allOlder := true
	for _, article := range items {
		if article.PublishedAt.IsZero() {
			allOlder = false
			accepted = append(accepted, article)
			continue
		}
		hasTimestamp = true
		if article.PublishedAt.Before(boundary) {
			continue
		}
		allOlder = false
		accepted = append(accepted, article)
	}
	return accepted, hasTimestamp && allOlder
}

func resultFromCheckpoint(account domain.Account, checkpoint Checkpoint, lastSyncAt time.Time) Result {
	return Result{
		Account: account, UpstreamTotal: checkpoint.UpstreamTotal, MessagesFetched: checkpoint.MessagesFetched,
		ArticlesFetched: checkpoint.ArticlesFetched, PagesCommitted: checkpoint.PagesCommitted,
		LastSyncAt: lastSyncAt, Boundary: checkpoint.Boundary, StopReason: checkpoint.StopReason,
		NextOffset: checkpoint.NextOffset, Completed: checkpoint.Completed,
	}
}

// classifyDiscoveryError separates a rejected session from a throttled one.
// Only the first blocks the job on authentication; throttling stays retryable so
// a transient rate limit does not ask the user to sign in again.
func classifyDiscoveryError(err error) error {
	switch {
	case errors.Is(err, wechat.ErrDiscoveryAuthentication):
		return &jobs.ClassifiedError{Class: jobs.FailureAuthentication, Err: err}
	case errors.Is(err, wechat.ErrDiscoveryThrottled):
		return &jobs.ClassifiedError{Class: jobs.FailureThrottling, Retryable: true, Err: err}
	default:
		return nil
	}
}

func startOfTomorrow(now time.Time) time.Time {
	year, month, day := now.Date()
	return time.Date(year, month, day+1, 0, 0, 0, 0, now.Location())
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
