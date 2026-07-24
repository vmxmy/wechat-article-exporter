package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	runtimeenv "github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	syncrunner "github.com/wechat-article/wechat-article-exporter/cli/internal/sync"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type localSyncRuntime struct {
	profile   domain.ProfileID
	store     *library.JobStore
	library   *library.Database
	downloads application.DownloadJobs
	starter   application.JobStarter
	runner    *syncrunner.Runner
	album     *syncrunner.AlbumRunner
	scheduler *jobs.Scheduler
}

type syncJobItem struct {
	Version int                              `json:"version"`
	Request domain.SynchronizeAccountRequest `json:"request"`
}

type albumSyncJobItem struct {
	Version  int                         `json:"version"`
	Request  syncrunner.AlbumSyncRequest `json:"request"`
	Download bool                        `json:"download,omitempty"`
}

type multiAlbumSyncTarget struct {
	LocalAlbumID domain.AlbumID              `json:"localAlbumId"`
	Request      syncrunner.AlbumSyncRequest `json:"request"`
}

type multiAlbumSyncJobItem struct {
	Version  int                    `json:"version"`
	Albums   []multiAlbumSyncTarget `json:"albums"`
	Download bool                   `json:"download,omitempty"`
}

type multiAlbumSyncCheckpoint struct {
	Current       int                      `json:"current"`
	Albums        []domain.AlbumCheckpoint `json:"albums"`
	DownloadJobID domain.JobID             `json:"downloadJobId,omitempty"`
}

const maxMultiAlbumDownloadArticles = 500

func newLocalSyncRuntime(runtime *ProfileRuntime, clock runtimeenv.Clock, schedulers ...*jobs.Scheduler) *localSyncRuntime {
	if runtime == nil || runtime.Library == nil || runtime.Jobs == nil || runtime.WeChat == nil {
		return nil
	}
	now := time.Now
	if clock != nil {
		now = clock.Now
	}
	runner, err := syncrunner.NewRunner(runtime.WeChat, runtime.Library, syncrunner.Options{Now: now})
	if err != nil {
		return nil
	}
	albumRunner, err := syncrunner.NewAlbumRunner(runtime.WeChat, runtime.Library, syncrunner.AlbumRunnerOptions{Now: now})
	if err != nil {
		return nil
	}
	var scheduler *jobs.Scheduler
	if len(schedulers) > 0 {
		scheduler = schedulers[0]
	}
	return &localSyncRuntime{profile: runtime.Profile.ID, store: runtime.Jobs, library: runtime.Library,
		downloads: runtime.Downloads, runner: runner, album: albumRunner, scheduler: scheduler}
}

func (runtime *localSyncRuntime) Start(ctx context.Context, request domain.SynchronizeAccountRequest) (domain.Job, error) {
	if runtime == nil || runtime.store == nil || runtime.runner == nil {
		return domain.Job{}, fmt.Errorf("account sync runtime: %w", application.ErrUnavailable)
	}
	if _, err := syncrunner.ValidatePacing(request); err != nil {
		return domain.Job{}, err
	}
	accountIDs := normalizedSyncAccountIDs(request)
	if len(accountIDs) == 0 {
		return domain.Job{}, errors.New("at least one account ID is required")
	}
	items := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		itemRequest := request
		itemRequest.AccountID = accountID
		itemRequest.AccountIDs = nil
		item, err := json.Marshal(syncJobItem{Version: 1, Request: itemRequest})
		if err != nil {
			return domain.Job{}, fmt.Errorf("encode account sync job: %w", err)
		}
		items = append(items, string(item))
	}
	return runtime.store.CreateWithItems(ctx, jobs.Spec{
		Kind: "account_sync", Profile: runtime.profile,
		Payload: map[string]any{"accountIds": accountIDs, "incremental": request.Incremental, "range": request.Range},
	}, items)
}

func (runtime *localSyncRuntime) StartAlbum(ctx context.Context, request syncrunner.AlbumSyncRequest, download bool) (domain.Job, error) {
	if runtime == nil || runtime.store == nil || runtime.album == nil {
		return domain.Job{}, fmt.Errorf("album sync runtime: %w", application.ErrUnavailable)
	}
	request.FakeID = strings.TrimSpace(request.FakeID)
	request.AlbumID = strings.TrimSpace(request.AlbumID)
	if request.FakeID == "" || request.AlbumID == "" {
		return domain.Job{}, errors.New("album fakeid and album ID are required")
	}
	if request.Order == "" {
		request.Order = "forward"
	}
	if request.Order != "forward" && request.Order != "reverse" {
		return domain.Job{}, errors.New("album order must be forward or reverse")
	}
	if request.PageSize <= 0 {
		request.PageSize = 20
	}
	if request.PageSize > 50 {
		return domain.Job{}, errors.New("album page size must be between 1 and 50")
	}
	if request.PageDelay < 0 {
		return domain.Job{}, errors.New("album page delay must be non-negative")
	}
	encoded, err := json.Marshal(albumSyncJobItem{Version: 1, Request: request, Download: download})
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode album sync job: %w", err)
	}
	return runtime.store.CreateWithItems(ctx, jobs.Spec{
		Kind: "album_sync", Profile: runtime.profile,
		Payload: map[string]any{"fakeid": request.FakeID, "albumId": request.AlbumID, "order": request.Order, "download": download},
	}, []string{string(encoded)})
}

func (runtime *localSyncRuntime) StartAlbumByID(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	return runtime.StartAlbumByIDWithOrder(ctx, accountID, albumID, wechat.AlbumForward)
}

func (runtime *localSyncRuntime) StartAlbumByIDAndDownload(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	return runtime.StartAlbumByIDWithOrderAndDownload(ctx, accountID, albumID, wechat.AlbumForward)
}

func (runtime *localSyncRuntime) StartAlbumByIDWithOrder(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	return runtime.startAlbumByID(ctx, accountID, albumID, order, false)
}

func (runtime *localSyncRuntime) StartAlbumByIDWithOrderAndDownload(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	return runtime.startAlbumByID(ctx, accountID, albumID, order, true)
}

func (runtime *localSyncRuntime) StartAlbumsByIDWithOrder(ctx context.Context, albumIDs []domain.AlbumID, order wechat.AlbumOrder, download bool) (domain.Job, error) {
	if runtime == nil || runtime.library == nil || runtime.store == nil || runtime.album == nil {
		return domain.Job{}, fmt.Errorf("album sync runtime: %w", application.ErrUnavailable)
	}
	if len(albumIDs) < 1 || len(albumIDs) > 50 {
		return domain.Job{}, errors.New("album traversal requires between 1 and 50 album IDs")
	}
	if order != wechat.AlbumForward && order != wechat.AlbumReverse {
		return domain.Job{}, errors.New("album order must be forward or reverse")
	}
	targets := make([]multiAlbumSyncTarget, 0, len(albumIDs))
	seen := make(map[domain.AlbumID]struct{}, len(albumIDs))
	for _, albumID := range albumIDs {
		albumID = domain.AlbumID(strings.TrimSpace(string(albumID)))
		if albumID == "" {
			return domain.Job{}, errors.New("album traversal IDs must not be empty")
		}
		if _, duplicate := seen[albumID]; duplicate {
			return domain.Job{}, errors.New("album traversal IDs must be unique")
		}
		seen[albumID] = struct{}{}
		album, err := runtime.library.GetAlbum(ctx, albumID)
		if err != nil {
			return domain.Job{}, err
		}
		account, err := runtime.library.GetAccount(ctx, album.AccountID)
		if err != nil {
			return domain.Job{}, err
		}
		targets = append(targets, multiAlbumSyncTarget{LocalAlbumID: albumID, Request: syncrunner.AlbumSyncRequest{
			FakeID: account.FakeID, AlbumID: album.UpstreamID, Order: order, PageSize: 20, PageDelay: 5 * time.Second,
		}})
	}
	encoded, err := json.Marshal(multiAlbumSyncJobItem{Version: 2, Albums: targets, Download: download})
	if err != nil {
		return domain.Job{}, fmt.Errorf("encode multi-album sync job: %w", err)
	}
	return runtime.store.CreateWithItems(ctx, jobs.Spec{Kind: "album_sync", Profile: runtime.profile,
		Payload: map[string]any{"albumIds": albumIDs, "order": order, "download": download},
	}, []string{string(encoded)})
}

func (runtime *localSyncRuntime) startAlbumByID(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder, download bool) (domain.Job, error) {
	if runtime == nil || runtime.library == nil {
		return domain.Job{}, fmt.Errorf("album sync runtime: %w", application.ErrUnavailable)
	}
	account, err := runtime.library.GetAccount(ctx, accountID)
	if err != nil {
		return domain.Job{}, err
	}
	album, err := runtime.library.GetAlbumForAccount(ctx, accountID, albumID)
	if err != nil {
		return domain.Job{}, fmt.Errorf("album %s does not belong to account %s: %w", albumID, accountID, err)
	}
	return runtime.StartAlbum(ctx, syncrunner.AlbumSyncRequest{
		FakeID: account.FakeID, AlbumID: album.UpstreamID, Order: order, PageSize: 20, PageDelay: 5 * time.Second,
	}, download)
}

func normalizedSyncAccountIDs(request domain.SynchronizeAccountRequest) []domain.AccountID {
	values := make([]domain.AccountID, 0, len(request.AccountIDs)+1)
	if request.AccountID != "" {
		values = append(values, request.AccountID)
	}
	values = append(values, request.AccountIDs...)
	result := make([]domain.AccountID, 0, len(values))
	seen := make(map[domain.AccountID]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (runtime *localSyncRuntime) Run(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if runtime == nil || runtime.store == nil {
		return domain.Job{}, fmt.Errorf("sync runtime: %w", application.ErrUnavailable)
	}
	job, err := runtime.store.Get(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.Kind == "album_sync" && runtime.album == nil {
		return domain.Job{}, fmt.Errorf("album sync runtime: %w", application.ErrUnavailable)
	}
	if job.Kind != "album_sync" && runtime.runner == nil {
		return domain.Job{}, fmt.Errorf("account sync runtime: %w", application.ErrUnavailable)
	}
	operation := "account_sync"
	if job.Kind == "album_sync" {
		operation = "album_sync"
	}
	engine, err := jobs.NewEngine(runtime.store, jobs.EngineOptions{
		Owner: "local-sync-worker", Scheduler: runtime.scheduler,
		Metadata: func(jobs.Item) jobs.WorkMetadata {
			return jobs.WorkMetadata{Operation: operation, Host: "mp.weixin.qq.com", Sensitive: true}
		},
	})
	if err != nil {
		return domain.Job{}, err
	}
	if job.Kind == "album_sync" {
		return engine.Run(ctx, id, runtime.executeAlbum)
	}
	return engine.Run(ctx, id, runtime.execute)
}

func (runtime *localSyncRuntime) execute(ctx context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
	var envelope syncJobItem
	decoder := json.NewDecoder(strings.NewReader(item.Key))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode account sync job item: %w", err)}
	}
	if envelope.Version != 1 {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode account sync job item: unsupported version %d", envelope.Version)}
	}
	_, err := runtime.runner.Run(ctx, envelope.Request, item.Checkpoint, func(value any) error {
		return checkpoint(value)
	})
	return err
}

func (runtime *localSyncRuntime) executeAlbum(ctx context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal([]byte(item.Key), &version); err != nil {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode album sync job item: %w", err)}
	}
	if version.Version == 2 {
		return runtime.executeMultiAlbum(ctx, item, checkpoint)
	}
	var envelope albumSyncJobItem
	decoder := json.NewDecoder(strings.NewReader(item.Key))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode album sync job item: %w", err)}
	}
	if envelope.Version != 1 {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: fmt.Errorf("decode album sync job item: unsupported version %d", envelope.Version)}
	}
	result, err := runtime.album.Run(ctx, envelope.Request, item.Checkpoint, func(value any) error {
		return checkpoint(value)
	})
	if err != nil {
		return err
	}
	if envelope.Download {
		if runtime.library == nil || runtime.downloads == nil {
			return fmt.Errorf("queue album article downloads: %w", application.ErrUnavailable)
		}
		articleIDs, queryErr := runtime.library.QueryArticleIDs(ctx, domain.ArticleQuery{AlbumID: result.Album.ID})
		if queryErr != nil {
			return queryErr
		}
		downloadJob, startErr := runtime.downloads.Start(ctx, domain.DownloadRequest{ArticleIDs: articleIDs})
		if startErr != nil {
			return startErr
		}
		if runtime.starter != nil {
			if startErr := runtime.starter.Start(ctx, downloadJob); startErr != nil {
				return fmt.Errorf("launch album download job %s: %w", downloadJob.ID, startErr)
			}
		}
		return checkpoint(map[string]any{
			"album": result.Album, "pagesCommitted": result.PagesCommitted, "itemsCommitted": result.ItemsCommitted,
			"completed": result.Completed, "downloadRequested": true, "downloadJobId": downloadJob.ID,
		})
	}
	return nil
}

func (runtime *localSyncRuntime) executeMultiAlbum(ctx context.Context, item jobs.Item, checkpoint jobs.CheckpointFunc) error {
	var envelope multiAlbumSyncJobItem
	decoder := json.NewDecoder(strings.NewReader(item.Key))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || envelope.Version != 2 || len(envelope.Albums) < 1 || len(envelope.Albums) > 50 {
		return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: errors.New("decode multi-album sync job item")}
	}
	state := multiAlbumSyncCheckpoint{Albums: make([]domain.AlbumCheckpoint, len(envelope.Albums))}
	if len(item.Checkpoint) > 0 && string(item.Checkpoint) != "{}" && string(item.Checkpoint) != "null" {
		if err := json.Unmarshal(item.Checkpoint, &state); err != nil || state.Current < 0 || state.Current > len(envelope.Albums) || len(state.Albums) != len(envelope.Albums) || (state.DownloadJobID != "" && state.Current != len(envelope.Albums)) {
			return &jobs.ClassifiedError{Class: jobs.FailureParsing, Err: errors.New("decode multi-album sync checkpoint")}
		}
	}
	for state.Current < len(envelope.Albums) {
		index := state.Current
		checkpointData, _ := json.Marshal(state.Albums[index])
		result, err := runtime.album.Run(ctx, envelope.Albums[index].Request, checkpointData, func(value any) error {
			current, ok := value.(domain.AlbumCheckpoint)
			if !ok {
				return errors.New("album checkpoint has an unexpected type")
			}
			state.Albums[index] = current
			return checkpoint(state)
		})
		if err != nil {
			return err
		}
		state.Albums[index] = result.Checkpoint
		state.Current++
		if err := checkpoint(state); err != nil {
			return err
		}
	}
	if !envelope.Download {
		return nil
	}
	if runtime.library == nil || runtime.downloads == nil {
		return fmt.Errorf("queue album article downloads: %w", application.ErrUnavailable)
	}
	downloadJob := domain.Job{ID: state.DownloadJobID}
	if downloadJob.ID == "" {
		articleIDs, err := runtime.multiAlbumDownloadArticleIDs(ctx, envelope)
		if err != nil {
			return err
		}
		idempotent, ok := runtime.downloads.(application.IdempotentDownloadJobs)
		if !ok {
			return fmt.Errorf("queue album article downloads: %w", application.ErrUnavailable)
		}
		downloadJob, err = idempotent.StartWithIdempotency(ctx, domain.DownloadRequest{ArticleIDs: articleIDs}, multiAlbumDownloadKey(item))
		if err != nil {
			return err
		}
		state.DownloadJobID = downloadJob.ID
		if err := checkpoint(state); err != nil {
			return err
		}
	}
	if runtime.starter != nil {
		if err := runtime.starter.Start(ctx, downloadJob); err != nil {
			return fmt.Errorf("launch album download job %s: %w", downloadJob.ID, err)
		}
	}
	return nil
}

func (runtime *localSyncRuntime) multiAlbumDownloadArticleIDs(ctx context.Context, envelope multiAlbumSyncJobItem) ([]domain.ArticleID, error) {
	articleIDs := make([]domain.ArticleID, 0)
	seen := make(map[domain.ArticleID]struct{})
	for _, target := range envelope.Albums {
		ids, err := runtime.library.QueryArticleIDs(ctx, domain.ArticleQuery{AlbumID: target.LocalAlbumID})
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			articleIDs = append(articleIDs, id)
			if len(articleIDs) > maxMultiAlbumDownloadArticles {
				return nil, fmt.Errorf("multi-album traversal download exceeds %d unique articles", maxMultiAlbumDownloadArticles)
			}
		}
	}
	return articleIDs, nil
}

func multiAlbumDownloadKey(item jobs.Item) string {
	return "album-sync-download:" + string(item.JobID) + ":" + item.ID
}

func (runtime *localSyncRuntime) Recover(ctx context.Context) (int64, error) {
	if runtime == nil || runtime.store == nil {
		return 0, fmt.Errorf("account sync runtime: %w", application.ErrUnavailable)
	}
	return runtime.store.RecoverStale(ctx)
}

var _ application.SyncJobs = (*localSyncRuntime)(nil)
var _ application.MultiAlbumSyncJobs = (*localSyncRuntime)(nil)
