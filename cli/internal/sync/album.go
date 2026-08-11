package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type AlbumSource interface {
	ListAlbumArticles(context.Context, wechat.AlbumListRequest) (wechat.AlbumPage, error)
}

type AlbumStore interface {
	SaveAlbumPage(context.Context, library.AlbumPageCommit) (library.AlbumCommitResult, error)
}

type AlbumRunner struct {
	source AlbumSource
	store  AlbumStore
	now    func() time.Time
	sleep  SleepFunc
}

type AlbumRunnerOptions struct {
	Now   func() time.Time
	Sleep SleepFunc
}

type AlbumSyncRequest struct {
	FakeID    string
	AlbumID   string
	Order     wechat.AlbumOrder
	PageSize  int
	PageDelay time.Duration
}

type AlbumSyncResult struct {
	Album          domain.Album           `json:"album"`
	PagesCommitted int                    `json:"pagesCommitted"`
	ItemsCommitted int                    `json:"itemsCommitted"`
	Checkpoint     domain.AlbumCheckpoint `json:"checkpoint"`
	Completed      bool                   `json:"completed"`
}

func NewAlbumRunner(source AlbumSource, store AlbumStore, options AlbumRunnerOptions) (*AlbumRunner, error) {
	if source == nil {
		return nil, errors.New("album sync source is required")
	}
	if store == nil {
		return nil, errors.New("album sync store is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	return &AlbumRunner{source: source, store: store, now: options.Now, sleep: options.Sleep}, nil
}

func (runner *AlbumRunner) Run(
	ctx context.Context,
	request AlbumSyncRequest,
	checkpointData json.RawMessage,
	checkpointFunc CheckpointFunc,
) (AlbumSyncResult, error) {
	if request.FakeID == "" || request.AlbumID == "" {
		return AlbumSyncResult{}, errors.New("album sync fakeid and album ID are required")
	}
	checkpoint := domain.AlbumCheckpoint{}
	if len(checkpointData) > 0 && string(checkpointData) != "{}" && string(checkpointData) != "null" {
		if err := json.Unmarshal(checkpointData, &checkpoint); err != nil {
			return AlbumSyncResult{}, fmt.Errorf("decode album sync checkpoint: %w", err)
		}
	}
	result := AlbumSyncResult{Checkpoint: checkpoint, PagesCommitted: checkpoint.PagesCommitted,
		ItemsCommitted: checkpoint.ItemsCommitted}
	for {
		page, err := runner.source.ListAlbumArticles(ctx, wechat.AlbumListRequest{
			FakeID: request.FakeID, AlbumID: request.AlbumID, Order: request.Order,
			BeginMessageID: checkpoint.BeginMessageID, BeginItemIndex: checkpoint.BeginItemIndex, Limit: request.PageSize,
		})
		if err != nil {
			if classified := classifyDiscoveryError(err); classified != nil {
				return result, classified
			}
			return result, err
		}
		if result.Album.ID == "" {
			result.Album = page.Album
		}
		commits := make([]library.AlbumArticleCommit, 0, len(page.Items))
		seen := make(map[string]struct{}, len(checkpoint.SeenKeys)+len(page.Items))
		for _, key := range checkpoint.SeenKeys {
			seen[key] = struct{}{}
		}
		nextOrdinal := checkpoint.ItemsCommitted
		for _, item := range page.Items {
			commits = append(commits, library.AlbumArticleCommit{Article: item.Article, Ordinal: nextOrdinal, Key: item.Key})
			if _, duplicate := seen[item.Key]; duplicate {
				continue
			}
			seen[item.Key] = struct{}{}
			nextOrdinal++
		}
		commitResult, err := runner.store.SaveAlbumPage(ctx, library.AlbumPageCommit{
			Album: page.Album, Articles: commits, Checkpoint: checkpoint, Completed: page.Completed,
			FetchedAt: runner.now(),
		})
		if err != nil {
			return result, err
		}
		checkpoint = commitResult.Checkpoint
		checkpoint.BeginMessageID = page.Next.BeginMessageID
		checkpoint.BeginItemIndex = page.Next.BeginItemIndex
		result.Checkpoint = checkpoint
		result.PagesCommitted = checkpoint.PagesCommitted
		result.ItemsCommitted = checkpoint.ItemsCommitted
		result.Completed = page.Completed
		if checkpointFunc != nil {
			if err := checkpointFunc(checkpoint); err != nil {
				return result, err
			}
		}
		if page.Completed {
			return result, nil
		}
		if checkpoint.BeginMessageID == "" {
			return result, fmt.Errorf("album pagination did not provide a resumable continuation")
		}
		if err := runner.sleep(ctx, request.PageDelay); err != nil {
			return result, err
		}
	}
}
