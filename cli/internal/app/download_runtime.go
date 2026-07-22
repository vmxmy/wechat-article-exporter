package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/download"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type localDownloadRuntime struct {
	profile domain.ProfileID
	library *library.Database
	objects *objects.FileStore
	service download.JobService
}

type downloadRuntimeOptions struct {
	DestinationPolicy network.DestinationPolicy
}

func newLocalDownloadRuntime(runtime *ProfileRuntime, secretStore secrets.Store, httpClient network.Doer, options ...downloadRuntimeOptions) *localDownloadRuntime {
	if runtime == nil || runtime.Library == nil || runtime.Objects == nil || runtime.Jobs == nil || runtime.Network == nil {
		return nil
	}
	client, ok := httpClient.(*http.Client)
	if !ok {
		client = &http.Client{Transport: roundTripperFromDoer{doer: httpClient}}
	}
	policy := network.DestinationPolicy{AllowedHosts: map[string]struct{}{
		"mp.weixin.qq.com": {}, "mmbiz.qpic.cn": {}, "mmbiz.qlogo.cn": {}, "res.wx.qq.com": {},
	}, AllowLoopback: true}
	if len(options) > 0 && options[0].DestinationPolicy.AllowedHosts != nil {
		policy = options[0].DestinationPolicy
	}
	direct := network.NewDirect(client, policy)
	routes, err := runtime.Network.Candidates(context.Background(), direct)
	if err != nil {
		routes = []network.Candidate{{Client: direct, Direct: true, Enabled: true}}
	}
	router := network.Router{Routes: routes, Retryable: func(err error) bool {
		return !errors.Is(err, network.ErrSensitiveRouteRequired)
	}}
	credentialsService := credentials.NewService(credentials.ServiceOptions{
		Profile: string(runtime.Profile.ID), Repository: runtime.Library, Accounts: runtime.Library, Secrets: secretStore,
	})
	contentEndpoint := wechat.ContentEndpoint{Network: router}
	articleDownloader := download.ArticleDownloader{Network: router, Processor: processor.New(), Objects: runtime.Objects,
		Store: runtime.Library, DebugCapture: true}
	resourceDownloader := download.ResourceDownloader{Network: router, Objects: runtime.Objects, Store: runtime.Library}
	metadataDownloader := download.MetadataDownloader{Credentials: credentialsService, Source: contentEndpoint, Store: runtime.Library}
	commentsDownloader := download.CommentsDownloader{Credentials: credentialsService, Source: contentEndpoint, Store: runtime.Library}
	paidDownloader := download.PaidArticleDownloader{Fetcher: download.PaidContentDownloader{Credentials: credentialsService,
		Source: contentEndpoint}, Processor: processor.New(), Objects: runtime.Objects, Store: runtime.Library, DebugCapture: true}
	return &localDownloadRuntime{profile: runtime.Profile.ID, library: runtime.Library, objects: runtime.Objects, service: download.JobService{
		Store: runtime.Jobs, Engine: jobs.EngineOptions{Owner: "local-download-worker"}, Articles: articleDownloader,
		Resources: resourceDownloader, Metadata: metadataDownloader, Comments: commentsDownloader, Paid: paidDownloader,
	}}
}

func (runtime *localDownloadRuntime) Start(ctx context.Context, request domain.DownloadRequest) (domain.Job, error) {
	if runtime == nil || runtime.library == nil {
		return domain.Job{}, fmt.Errorf("download runtime: %w", application.ErrUnavailable)
	}
	kind := download.JobKind(strings.TrimSpace(request.Kind))
	if kind == "" || kind == "article" {
		kind = download.JobArticle
	}
	if kind == "resources" {
		kind = download.JobResource
	} else if kind == "metadata" {
		kind = download.JobMetadata
	} else if kind == "comments" {
		kind = download.JobComments
	} else if kind == "paid" {
		kind = download.JobPaid
	}
	jobRequest := download.JobRequest{Kind: kind, Profile: runtime.profile}
	articles, err := runtime.resolveArticles(ctx, request)
	if err != nil {
		return domain.Job{}, err
	}
	for _, article := range articles {
		switch kind {
		case download.JobArticle:
			jobRequest.Articles = append(jobRequest.Articles, download.ArticleRequest{ArticleID: article.ID, URL: article.CanonicalURL, Force: request.Force})
		case download.JobResource:
			if len(request.URLs) > 0 && len(request.ArticleIDs) > 0 {
				for ordinal, rawURL := range request.URLs {
					jobRequest.Resources = append(jobRequest.Resources, download.ResourceRequest{ArticleID: article.ID, URL: rawURL,
						Role: "resource", Ordinal: ordinal, Force: request.Force})
				}
			} else {
				resources, discoverErr := runtime.discoverArticleResources(ctx, article)
				if discoverErr != nil {
					return domain.Job{}, discoverErr
				}
				jobRequest.Resources = append(jobRequest.Resources, resources...)
			}
		case download.JobMetadata:
			jobRequest.Metadata = append(jobRequest.Metadata, download.MetadataRequest{ArticleID: article.ID, AccountID: article.AccountID, URL: article.CanonicalURL})
		case download.JobComments:
			content, contentErr := runtime.library.CurrentContent(ctx, article.ID, "html")
			if contentErr != nil || strings.TrimSpace(content.CommentID) == "" {
				return domain.Job{}, fmt.Errorf("article %s has no downloaded comment identifier", article.ID)
			}
			account, accountErr := runtime.library.GetAccount(ctx, article.AccountID)
			if accountErr != nil {
				return domain.Job{}, accountErr
			}
			jobRequest.Comments = append(jobRequest.Comments, download.CommentsRequest{ArticleID: article.ID,
				AccountID: article.AccountID, BusinessID: account.FakeID, AppMessageID: article.AppMsgID,
				ItemIndex: article.ItemIndex, CommentID: content.CommentID})
		case download.JobPaid:
			jobRequest.Paid = append(jobRequest.Paid, download.PaidContentJobRequest{ArticleID: article.ID,
				AccountID: article.AccountID, URL: article.CanonicalURL, Force: request.Force})
		default:
			return domain.Job{}, fmt.Errorf("unsupported download kind %q", request.Kind)
		}
	}
	return runtime.service.Start(ctx, jobRequest)
}

func (runtime *localDownloadRuntime) discoverArticleResources(
	ctx context.Context,
	article domain.Article,
) ([]download.ResourceRequest, error) {
	content, err := runtime.library.CurrentContent(ctx, article.ID, "html")
	if err != nil {
		return nil, fmt.Errorf("article %s has no downloaded HTML for resource discovery: %w", article.ID, err)
	}
	reader, _, err := runtime.objects.Open(ctx, content.ObjectDigest)
	if err != nil {
		return nil, fmt.Errorf("open article %s HTML for resource discovery: %w", article.ID, err)
	}
	result, processErr := processor.New().Process(ctx, reader)
	closeErr := reader.Close()
	if processErr != nil || closeErr != nil {
		return nil, errors.Join(processErr, closeErr)
	}
	requests := make([]download.ResourceRequest, 0, len(result.Resources))
	for ordinal, resource := range result.Resources {
		requests = append(requests, download.ResourceRequest{
			ArticleID: article.ID, URL: resource.URL, Role: string(resource.Kind), Ordinal: ordinal,
		})
	}
	if len(requests) == 0 {
		return nil, fmt.Errorf("article %s has no discoverable resources", article.ID)
	}
	return requests, nil
}

func (runtime *localDownloadRuntime) resolveArticles(ctx context.Context, request domain.DownloadRequest) ([]domain.Article, error) {
	items := make([]domain.Article, 0, len(request.ArticleIDs)+len(request.URLs))
	seen := map[domain.ArticleID]struct{}{}
	for _, id := range request.ArticleIDs {
		article, err := runtime.library.GetArticle(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("resolve article %s: %w", id, err)
		}
		if _, ok := seen[article.ID]; !ok {
			items, seen[article.ID] = append(items, article), struct{}{}
		}
	}
	if download.JobKind(request.Kind) != download.JobResource || len(request.ArticleIDs) == 0 {
		for _, rawURL := range request.URLs {
			article, err := runtime.library.GetArticleByCanonicalURL(ctx, rawURL)
			if err != nil {
				if kind := download.JobKind(request.Kind); kind == "" || kind == download.JobArticle || kind == "article" {
					article, err = runtime.library.SaveProvisionalArticle(ctx, library.SingleArticleInput{URL: rawURL})
				}
				if err != nil {
					return nil, err
				}
			}
			if _, ok := seen[article.ID]; !ok {
				items, seen[article.ID] = append(items, article), struct{}{}
			}
		}
	}
	return items, nil
}

func (runtime *localDownloadRuntime) Run(ctx context.Context, id domain.JobID) (domain.Job, error) {
	return runtime.service.Run(ctx, id)
}

func (runtime *localDownloadRuntime) Recover(ctx context.Context) (int64, error) {
	return runtime.service.Recover(ctx)
}

var _ application.DownloadJobs = (*localDownloadRuntime)(nil)
