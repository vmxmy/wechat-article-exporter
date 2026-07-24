package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/download"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/objects"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/profiles"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/secrets"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type localDownloadRuntime struct {
	profile     domain.ProfileID
	library     *library.Database
	objects     *objects.FileStore
	credentials *credentials.Service
	service     download.JobService
}

type downloadRuntimeOptions struct {
	DestinationPolicy network.DestinationPolicy
	ContentBaseURL    *url.URL
	Proxy             profiles.ProxyPreferences
	ProxyConfigured   bool
	Concurrency       int
	Scheduler         *jobs.Scheduler
}

func newLocalDownloadRuntime(runtime *ProfileRuntime, secretStore secrets.Store, httpClient network.Doer, options ...downloadRuntimeOptions) (*localDownloadRuntime, error) {
	if runtime == nil || runtime.Library == nil || runtime.Objects == nil || runtime.Jobs == nil || runtime.Network == nil {
		return nil, errors.New("download runtime dependencies are incomplete")
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
		return nil, fmt.Errorf("build configured network routes: %w", err)
	}
	if len(options) > 0 {
		routes = configureDownloadRoutes(routes, options[0])
	}
	hasEligibleRoute := false
	for _, route := range routes {
		if route.Enabled && route.Client != nil {
			hasEligibleRoute = true
			break
		}
	}
	if !hasEligibleRoute {
		return nil, errors.New("download routing configuration has no enabled direct or proxy route")
	}
	router := &downloadRouter{routes: routes, retryable: func(err error) bool {
		return !errors.Is(err, network.ErrSensitiveRouteRequired)
	}}
	contentEndpoint := wechat.ContentEndpoint{Network: router}
	if len(options) > 0 {
		contentEndpoint.BaseURL = options[0].ContentBaseURL
	}
	credentialsService := credentials.NewService(credentials.ServiceOptions{
		Profile: string(runtime.Profile.ID), Repository: runtime.Library, Accounts: runtime.Library, Secrets: secretStore,
		Validator: credentials.ValidatorFunc(func(ctx context.Context, record credentials.Record) error {
			_, err := contentEndpoint.ValidateCredential(ctx, wechat.CredentialValidationRequest{
				BusinessID: record.Biz, Credential: record,
			})
			return err
		}),
	})
	articleDownloader := download.ArticleDownloader{Network: router, Processor: processor.New(), Objects: runtime.Objects,
		Store: runtime.Library, DebugCapture: true}
	resourceDownloader := download.ResourceDownloader{Network: router, Objects: runtime.Objects, Store: runtime.Library}
	metadataDownloader := download.MetadataDownloader{Credentials: credentialsService, Source: contentEndpoint, Store: runtime.Library}
	commentsDownloader := download.CommentsDownloader{Credentials: credentialsService, Source: contentEndpoint, Store: runtime.Library}
	paidDownloader := download.PaidArticleDownloader{Fetcher: download.PaidContentDownloader{Credentials: credentialsService,
		Source: contentEndpoint}, Processor: processor.New(), Objects: runtime.Objects, Store: runtime.Library, DebugCapture: true}
	return &localDownloadRuntime{profile: runtime.Profile.ID, library: runtime.Library, objects: runtime.Objects,
		credentials: credentialsService, service: download.JobService{
			Store: runtime.Jobs, Engine: jobs.EngineOptions{Owner: "local-download-worker", Scheduler: downloadScheduler(options)}, Articles: articleDownloader,
			Resources: resourceDownloader, Metadata: metadataDownloader, Comments: commentsDownloader, Paid: paidDownloader,
		}}, nil
}

func filterDirectRoutes(routes []network.Candidate) []network.Candidate {
	result := make([]network.Candidate, 0, 1)
	for _, route := range routes {
		if route.Direct {
			result = append(result, route)
		}
	}
	return result
}

func filterProxyRoutes(routes []network.Candidate) []network.Candidate {
	result := make([]network.Candidate, 0, len(routes))
	for _, route := range routes {
		if !route.Direct {
			result = append(result, route)
		}
	}
	return result
}

func configureDownloadRoutes(routes []network.Candidate, options downloadRuntimeOptions) []network.Candidate {
	configured := append([]network.Candidate(nil), routes...)
	if !options.ProxyConfigured {
		return orderDownloadRoutes(configured, true)
	}
	if !options.Proxy.FallbackEnabled {
		if options.Proxy.DirectFirst {
			return orderDownloadRoutes(filterDirectRoutes(configured), true)
		}
		return orderDownloadRoutes(filterProxyRoutes(configured), false)
	}
	return orderDownloadRoutes(configured, options.Proxy.DirectFirst)
}

func orderDownloadRoutes(routes []network.Candidate, directFirst bool) []network.Candidate {
	sort.SliceStable(routes, func(left, right int) bool {
		if routes[left].Direct != routes[right].Direct {
			return routes[left].Direct == directFirst
		}
		if routes[left].ProbeRequired != routes[right].ProbeRequired {
			return !routes[left].ProbeRequired
		}
		return routes[left].Priority < routes[right].Priority
	})
	return routes
}

type downloadRouter struct {
	routes    []network.Candidate
	now       func() time.Time
	retryable func(error) bool
	mu        sync.Mutex
}

func (router *downloadRouter) Do(ctx context.Context, request network.Request) (network.Result, error) {
	now := router.now
	if now == nil {
		now = time.Now
	}
	var failures []error
	for index := range router.routes {
		if err := ctx.Err(); err != nil {
			return network.Result{}, err
		}
		router.mu.Lock()
		candidate := router.routes[index]
		router.mu.Unlock()
		if candidate.Client == nil || !candidate.Enabled || candidate.CooldownUntil.After(now()) {
			continue
		}
		if len(candidate.Classes) > 0 {
			if _, ok := candidate.Classes[request.Class]; !ok {
				continue
			}
		}
		if err := network.ValidateRoute(request.Class, candidate.Direct, candidate.Trust); err != nil {
			failures = append(failures, err)
			continue
		}
		if candidate.ProbeRequired {
			router.mu.Lock()
			candidate = router.routes[index]
			if !candidate.ProbeRequired {
				router.mu.Unlock()
			} else if candidate.CooldownUntil.After(now()) {
				router.mu.Unlock()
				continue
			} else if candidate.Probe == nil {
				router.mu.Unlock()
				failures = append(failures, fmt.Errorf("route %s requires a recovery probe", candidate.Client.Name()))
				continue
			} else {
				probe := candidate.Probe
				router.mu.Unlock()
				probeErr := probe(ctx)
				router.mu.Lock()
				current := router.routes[index]
				if probeErr != nil {
					router.routes[index].ProbeRequired = true
					router.routes[index].CooldownUntil = now().Add(time.Minute)
				} else if current.ProbeRequired && current.Probe != nil {
					router.routes[index].ProbeRequired = false
					router.routes[index].CooldownUntil = time.Time{}
				}
				router.mu.Unlock()
				if probeErr == nil {
					candidate = current
					candidate.ProbeRequired = false
				} else {
					if cancellationErr := cancellationError(ctx, probeErr); cancellationErr != nil {
						return network.Result{}, cancellationErr
					}
					failures = append(failures, fmt.Errorf("route %s recovery probe: %w", candidate.Client.Name(), probeErr))
					continue
				}
			}
		}
		result, err := candidate.Client.Do(ctx, request)
		if err == nil {
			return result, nil
		}
		if cancellationErr := cancellationError(ctx, err); cancellationErr != nil {
			return network.Result{}, cancellationErr
		}
		failures = append(failures, fmt.Errorf("route %s: %w", candidate.Client.Name(), err))
		if router.retryable == nil || !router.retryable(err) {
			return network.Result{}, failures[len(failures)-1]
		}
		if request.Body != nil {
			seeker, ok := request.Body.(io.Seeker)
			if !ok {
				return network.Result{}, errors.New("request body is not replayable for route fallback")
			}
			if _, err := seeker.Seek(0, io.SeekStart); err != nil {
				return network.Result{}, fmt.Errorf("rewind request body for route fallback: %w", err)
			}
		}
	}
	if len(failures) == 0 {
		return network.Result{}, errors.New("no eligible network route")
	}
	return network.Result{}, errors.Join(failures...)
}

func cancellationError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func downloadScheduler(options []downloadRuntimeOptions) *jobs.Scheduler {
	if len(options) > 0 && options[0].Scheduler != nil {
		return options[0].Scheduler
	}
	limit := 4
	if len(options) > 0 && options[0].Concurrency > 0 {
		limit = options[0].Concurrency
	}
	return jobs.NewScheduler(downloadSchedulerLimits(limit))
}

func downloadSchedulerLimits(limit int) jobs.Limits {
	if limit <= 0 {
		limit = 4
	}
	return jobs.Limits{Global: limit, PerOperation: map[string]int{
		string(download.JobArticle): limit, string(download.JobResource): limit, string(download.JobMetadata): 1,
		string(download.JobComments): 1, string(download.JobPaid): 1,
	}, PerHost: limit, Sensitive: 1}
}

func (runtime *localDownloadRuntime) Start(ctx context.Context, request domain.DownloadRequest) (domain.Job, error) {
	return runtime.start(ctx, request, "")
}

func (runtime *localDownloadRuntime) StartWithIdempotency(ctx context.Context, request domain.DownloadRequest, key string) (domain.Job, error) {
	return runtime.start(ctx, request, key)
}

func (runtime *localDownloadRuntime) GetByIdempotency(ctx context.Context, key string) (domain.Job, bool, error) {
	if runtime == nil {
		return domain.Job{}, false, fmt.Errorf("download runtime: %w", application.ErrUnavailable)
	}
	return runtime.service.GetByIdempotency(ctx, runtime.profile, download.JobArticle, strings.TrimSpace(key))
}

func (runtime *localDownloadRuntime) start(ctx context.Context, request domain.DownloadRequest, idempotencyKey string) (domain.Job, error) {
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
	jobRequest := download.JobRequest{Kind: kind, Profile: runtime.profile, IdempotencyKey: strings.TrimSpace(idempotencyKey)}
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
				resources, discoverErr := runtime.discoverArticleResources(ctx, article, request.Force)
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
	force bool,
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
			ArticleID: article.ID, URL: resource.URL, Role: string(resource.Kind), Ordinal: ordinal, Force: force,
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
var _ application.IdempotentDownloadJobs = (*localDownloadRuntime)(nil)
