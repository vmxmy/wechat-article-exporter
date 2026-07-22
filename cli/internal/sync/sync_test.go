package sync

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestRunnerCommitsPagesPacesAndStopsAtDateBoundary(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	source := &fakeSource{pages: map[int]wechat.ArticlePage{
		0: {
			Items: []domain.Article{
				{ID: "new", Aid: "new", Title: "new", CanonicalURL: "https://mp.weixin.qq.com/s/new", PublishedAt: boundary.Add(time.Hour)},
			},
			Total: 4, Next: 1,
		},
		1: {
			Items: []domain.Article{
				{ID: "old", Aid: "old", Title: "old", CanonicalURL: "https://mp.weixin.qq.com/s/old", PublishedAt: boundary.Add(-time.Hour)},
			},
			Total: 4, Next: 2,
		},
	}}
	store := &fakeStore{state: domain.AccountSyncState{Account: domain.Account{ID: "account-a", FakeID: "fake-a", Name: "Fixture"}}}
	var sleeps []time.Duration
	runner, err := NewRunner(source, store, Options{
		Now: func() time.Time { return now },
		Sleep: func(_ context.Context, duration time.Duration) error {
			sleeps = append(sleeps, duration)
			return nil
		},
		Jitter: func(time.Duration) time.Duration { return 250 * time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	var checkpoints []Checkpoint
	result, err := runner.Run(context.Background(), domain.SynchronizeAccountRequest{
		AccountID: "account-a", Range: domain.SyncRangePoint, NotBefore: boundary,
		PageDelay: 3 * time.Second, Jitter: time.Second,
	}, nil, func(value any) error {
		checkpoints = append(checkpoints, value.(Checkpoint))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "date_boundary" || !result.Completed || result.PagesCommitted != 2 ||
		result.ArticlesFetched != 1 || result.NextOffset != 2 {
		t.Fatalf("result = %#v", result)
	}
	if len(store.commits) != 2 || len(store.commits[0].Articles) != 1 || len(store.commits[1].Articles) != 0 ||
		!store.commits[1].Completed {
		t.Fatalf("commits = %#v", store.commits)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{3250 * time.Millisecond}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
	if len(checkpoints) != 2 || checkpoints[1].StopReason != "date_boundary" {
		t.Fatalf("checkpoints = %#v", checkpoints)
	}
}

func TestRunnerResumesFromCheckpointAndBlocksOnAuthentication(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{errs: map[int]error{2: wechat.ErrDiscoveryAuthentication}}
	store := &fakeStore{state: domain.AccountSyncState{Account: domain.Account{ID: "account-a", FakeID: "fake-a", Name: "Fixture"}}}
	runner, err := NewRunner(source, store, Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := Checkpoint{NextOffset: 2, UpstreamTotal: 8, MessagesFetched: 2, ArticlesFetched: 3, PagesCommitted: 1}
	result, err := runner.Run(context.Background(), domain.SynchronizeAccountRequest{AccountID: "account-a"},
		EncodeCheckpoint(checkpoint), nil)
	var classified *jobs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != jobs.FailureAuthentication {
		t.Fatalf("error = %T %v", err, err)
	}
	if !reflect.DeepEqual(source.offsets, []int{2}) || result.NextOffset != 2 || result.PagesCommitted != 1 || len(store.commits) != 0 {
		t.Fatalf("offsets=%v result=%#v commits=%#v", source.offsets, result, store.commits)
	}
}

func TestResolveBoundarySupportsCurrentRangesAndIncrementalState(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.FixedZone("fixture", 8*3600))
	state := domain.AccountSyncState{LatestArticle: time.Date(2026, 7, 20, 3, 0, 0, 0, now.Location())}
	boundary, err := ResolveBoundary(domain.SynchronizeAccountRequest{Incremental: true}, now, state)
	if err != nil || !boundary.Equal(state.LatestArticle) {
		t.Fatalf("incremental boundary=%v error=%v", boundary, err)
	}
	tests := []struct {
		name       string
		rangeValue domain.SyncRange
		want       time.Time
	}{
		{name: "24 hours", rangeValue: domain.SyncRange24Hours, want: now.Add(-24 * time.Hour)},
		{name: "1 day", rangeValue: domain.SyncRange1Day, want: time.Date(2026, 7, 22, 0, 0, 0, 0, now.Location())},
		{name: "3 days", rangeValue: domain.SyncRange3Days, want: time.Date(2026, 7, 20, 0, 0, 0, 0, now.Location())},
		{name: "7 days", rangeValue: domain.SyncRange7Days, want: time.Date(2026, 7, 16, 0, 0, 0, 0, now.Location())},
		{name: "1 month", rangeValue: domain.SyncRange1Month, want: time.Date(2026, 6, 23, 0, 0, 0, 0, now.Location())},
		{name: "3 months", rangeValue: domain.SyncRange3Months, want: time.Date(2026, 4, 23, 0, 0, 0, 0, now.Location())},
		{name: "6 months", rangeValue: domain.SyncRange6Months, want: time.Date(2026, 1, 23, 0, 0, 0, 0, now.Location())},
		{name: "1 year", rangeValue: domain.SyncRange1Year, want: time.Date(2025, 7, 23, 0, 0, 0, 0, now.Location())},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, rangeErr := ResolveBoundary(domain.SynchronizeAccountRequest{Range: test.rangeValue}, now, domain.AccountSyncState{})
			if rangeErr != nil || !got.Equal(test.want) {
				t.Fatalf("range %s boundary=%v want=%v error=%v", test.rangeValue, got, test.want, rangeErr)
			}
		})
	}
	boundary, err = ResolveBoundary(domain.SynchronizeAccountRequest{Range: domain.SyncRangeAll}, now, domain.AccountSyncState{})
	if err != nil || !boundary.IsZero() {
		t.Fatalf("all boundary=%v error=%v", boundary, err)
	}
	point := time.Date(2024, 5, 6, 7, 8, 9, 0, now.Location())
	boundary, err = ResolveBoundary(domain.SynchronizeAccountRequest{Range: domain.SyncRangePoint, NotBefore: point}, now, domain.AccountSyncState{})
	if err != nil || !boundary.Equal(point) {
		t.Fatalf("point boundary=%v want=%v error=%v", boundary, point, err)
	}
}

func TestValidatePacingWarnsAndRequiresConfirmationForPersistentUnsafeValue(t *testing.T) {
	policy, err := ValidatePacing(domain.SynchronizeAccountRequest{PageDelay: time.Second})
	if err != nil || policy.Warning == "" || policy.Confirmed {
		t.Fatalf("policy=%#v error=%v", policy, err)
	}
	_, err = ValidatePacing(domain.SynchronizeAccountRequest{PageDelay: time.Second, PersistentPacing: true})
	if !errors.Is(err, ErrUnsafePacingConfirmation) {
		t.Fatalf("error = %v", err)
	}
	policy, err = ValidatePacing(domain.SynchronizeAccountRequest{
		PageDelay: time.Second, PersistentPacing: true, ConfirmUnsafePacing: true,
	})
	if err != nil || !policy.Confirmed {
		t.Fatalf("confirmed policy=%#v error=%v", policy, err)
	}
}

func TestRunnerRejectsMalformedCheckpointBeforeNetwork(t *testing.T) {
	source := &fakeSource{}
	store := &fakeStore{state: domain.AccountSyncState{Account: domain.Account{ID: "account-a", FakeID: "fake-a", Name: "Fixture"}}}
	runner, err := NewRunner(source, store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(context.Background(), domain.SynchronizeAccountRequest{AccountID: "account-a"}, json.RawMessage(`{bad`), nil)
	if err == nil || len(source.offsets) != 0 {
		t.Fatalf("error=%v offsets=%v", err, source.offsets)
	}
}

type fakeSource struct {
	pages   map[int]wechat.ArticlePage
	errs    map[int]error
	offsets []int
}

func (source *fakeSource) ListArticles(_ context.Context, request wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	source.offsets = append(source.offsets, request.Offset)
	if err := source.errs[request.Offset]; err != nil {
		return wechat.ArticlePage{}, err
	}
	return source.pages[request.Offset], nil
}

type fakeStore struct {
	state   domain.AccountSyncState
	commits []library.ArticlePageCommit
}

func (store *fakeStore) GetAccount(context.Context, domain.AccountID) (domain.Account, error) {
	return store.state.Account, nil
}

func (store *fakeStore) GetAccountSyncState(context.Context, domain.AccountID) (domain.AccountSyncState, error) {
	return store.state, nil
}

func (store *fakeStore) SaveArticlePage(_ context.Context, commit library.ArticlePageCommit) error {
	store.commits = append(store.commits, commit)
	return nil
}
