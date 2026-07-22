package download

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type CredentialLoader interface {
	LoadForAccount(context.Context, domain.AccountID) (credentials.Metadata, credentials.Record, error)
}

type CredentialArticleSource interface {
	FetchArticle(context.Context, wechat.CredentialArticleRequest) (wechat.ContentResponse, error)
}

type MetadataStore interface {
	CommitMetricSnapshot(context.Context, library.MetricSnapshot) (library.MetricSnapshot, error)
	UpdateCredentialStatus(context.Context, string, credentials.Status, time.Time) (credentials.Metadata, error)
}

type MetadataDownloader struct {
	Credentials CredentialLoader
	Source      CredentialArticleSource
	Store       MetadataStore
	Now         func() time.Time
}

type MetadataRequest struct {
	ArticleID domain.ArticleID
	AccountID domain.AccountID
	URL       string
}

type MetadataResult struct {
	Snapshot  library.MetricSnapshot
	Route     string
	RequestID string
}

func (downloader MetadataDownloader) Download(ctx context.Context, request MetadataRequest) (MetadataResult, error) {
	if request.ArticleID == "" || request.AccountID == "" {
		return MetadataResult{}, errors.New("article ID and account ID are required")
	}
	if downloader.Credentials == nil || downloader.Source == nil || downloader.Store == nil {
		return MetadataResult{}, errors.New("metadata downloader dependencies are incomplete")
	}
	metadata, credential, err := downloader.Credentials.LoadForAccount(ctx, request.AccountID)
	if err != nil {
		return MetadataResult{}, err
	}
	response, err := downloader.Source.FetchArticle(ctx, wechat.CredentialArticleRequest{
		URL: request.URL, Credential: credential, Class: network.EngagementMetrics,
	})
	if err != nil {
		if errors.Is(err, credentials.ErrCredentialExpired) {
			_, _ = downloader.Store.UpdateCredentialStatus(ctx, metadata.ID, credentials.StatusInvalid, downloader.now())
		}
		return MetadataResult{}, fmt.Errorf("download engagement metadata: %w", err)
	}
	engagement, err := wechat.DecodeEngagement(response.Body)
	if err != nil {
		return MetadataResult{}, err
	}
	snapshot, err := downloader.Store.CommitMetricSnapshot(ctx, library.MetricSnapshot{
		ArticleID: request.ArticleID, ReadCount: engagement.ReadCount, OldLikeCount: engagement.OldLikeCount,
		ShareCount: engagement.ShareCount, LikeCount: engagement.LikeCount, CommentCount: engagement.CommentCount,
		CredentialID: metadata.ID, CapturedAt: downloader.now(),
	})
	if err != nil {
		return MetadataResult{}, err
	}
	return MetadataResult{Snapshot: snapshot, Route: response.Route, RequestID: response.RequestID}, nil
}

func (downloader MetadataDownloader) now() time.Time {
	if downloader.Now != nil {
		return downloader.Now()
	}
	return time.Now()
}
