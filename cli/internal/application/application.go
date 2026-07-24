package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/jobs"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/runtime"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

var ErrUnavailable = errors.New("application capability is not available")

// Application is the shared task-oriented seam used by Cobra, Bubble Tea,
// and the local stdio MCP adapter. Presentation adapters must not bypass it.
type Application interface {
	RuntimeStatus(context.Context) (domain.RuntimeStatus, error)
	BeginLogin(context.Context, string) (wechat.LoginFlow, error)
	PollLogin(context.Context) (wechat.PollResult, error)
	CompleteLogin(context.Context) (wechat.Session, error)
	SessionStatus(context.Context) (wechat.Session, error)
	ListSwitchableAccounts(context.Context) ([]wechat.SwitchableAccount, error)
	SwitchAccount(context.Context, string) (wechat.Session, error)
	Logout(context.Context) error
	SearchAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
	ResolveAccountName(context.Context, string) (string, error)
	ResolveAccountFromArticle(context.Context, string) (domain.Account, error)
	AccountDetails(context.Context, string) (wechat.AccountDetails, error)
	AuthorInfo(context.Context, string) (wechat.AuthorInfo, error)
	ListArticles(context.Context, wechat.ArticleListRequest) (wechat.ArticlePage, error)
	SaveAccount(context.Context, domain.Account) (domain.Account, error)
	UpdateAccount(context.Context, domain.Account) (domain.Account, error)
	GetAccount(context.Context, domain.AccountID) (domain.Account, error)
	GetAccountByFakeID(context.Context, string) (domain.Account, error)
	QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error)
	ExportAccounts(context.Context, domain.AccountQuery) (domain.AccountManifest, error)
	ImportAccounts(context.Context, domain.AccountManifest) (domain.AccountImportReport, error)
	DeleteAccounts(context.Context, []domain.AccountID) (domain.AccountDeleteReport, error)
	QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error)
	SaveArticleQuery(context.Context, string, domain.ArticleQuery) (domain.SavedArticleQuery, error)
	ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error)
	DeleteSavedArticleQuery(context.Context, string) (bool, error)
	QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error)
	SynchronizeAccount(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error)
	SynchronizeAlbum(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error)
	StartDownload(context.Context, domain.DownloadRequest) (domain.Job, error)
	StartExport(context.Context, domain.ExportRequest) (domain.Job, error)
	GetJob(context.Context, domain.JobID) (domain.Job, error)
	QueryJobs(context.Context, domain.JobQuery) (domain.Page[domain.Job], error)
	CancelJob(context.Context, domain.JobID) (domain.Job, error)
	StorageStatus(context.Context) (domain.StorageStatus, error)
	DiscoverBrowser(context.Context) (runtimeenv.Browser, error)
	ProcessSignals() <-chan os.Signal
}

type Options struct {
	Version   string
	Runtime   runtimeenv.Dependencies
	Library   library.Queries
	Jobs      jobs.Manager
	Downloads DownloadJobs
	Syncs     SyncJobs
	Exports   ExportJobs
	Starter   JobStarter
	WeChat    wechat.Gateway
	Session   wechat.SessionGateway
}

// DownloadJobs is the production execution seam for persistent download jobs.
// It is deliberately narrower than the download package so application tests
// and alternative adapters can supply deterministic job runtimes.
type DownloadJobs interface {
	Start(context.Context, domain.DownloadRequest) (domain.Job, error)
	Run(context.Context, domain.JobID) (domain.Job, error)
	Recover(context.Context) (int64, error)
}

type SyncJobs interface {
	Start(context.Context, domain.SynchronizeAccountRequest) (domain.Job, error)
	Run(context.Context, domain.JobID) (domain.Job, error)
	Recover(context.Context) (int64, error)
}

// AlbumSyncJobs is an optional execution extension implemented by local sync
// runtimes that can atomically persist album traversal intent with a follow-on
// batch download. The application exposes it through its typed facade.
type AlbumSyncJobs interface {
	StartAlbumByID(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error)
	StartAlbumByIDAndDownload(context.Context, domain.AccountID, domain.AlbumID) (domain.Job, error)
}

type OrderedAlbumSyncJobs interface {
	StartAlbumByIDWithOrder(context.Context, domain.AccountID, domain.AlbumID, wechat.AlbumOrder) (domain.Job, error)
	StartAlbumByIDWithOrderAndDownload(context.Context, domain.AccountID, domain.AlbumID, wechat.AlbumOrder) (domain.Job, error)
}

// MultiAlbumSyncJobs persists a bounded ordered set as one album_sync job.
// Its runtime resolves local IDs before any upstream work begins.
type MultiAlbumSyncJobs interface {
	StartAlbumsByIDWithOrder(context.Context, []domain.AlbumID, wechat.AlbumOrder, bool) (domain.Job, error)
}

type ExportJobs interface {
	Start(context.Context, domain.ExportRequest) (domain.Job, error)
	Run(context.Context, domain.JobID) (domain.Job, error)
	Recover(context.Context) (int64, error)
}

type JobStarter interface {
	Start(context.Context, domain.Job) error
}

type Service struct {
	version   string
	runtime   runtimeenv.Dependencies
	library   library.Queries
	accounts  library.Accounts
	jobs      jobs.Manager
	downloads DownloadJobs
	syncs     SyncJobs
	exports   ExportJobs
	starter   JobStarter
	wechat    wechat.Gateway
	discovery wechat.DiscoveryGateway
	session   wechat.SessionGateway
}

type savedArticleQueries interface {
	SaveArticleQuery(context.Context, string, domain.ArticleQuery) (domain.SavedArticleQuery, error)
	ListSavedArticleQueries(context.Context) ([]domain.SavedArticleQuery, error)
	DeleteSavedArticleQuery(context.Context, string) (bool, error)
}

func New(options Options) *Service {
	service := &Service{
		version:   options.Version,
		runtime:   runtimeenv.Normalize(options.Runtime),
		library:   options.Library,
		jobs:      options.Jobs,
		downloads: options.Downloads,
		syncs:     options.Syncs,
		exports:   options.Exports,
		starter:   options.Starter,
		wechat:    options.WeChat,
		session:   options.Session,
	}
	if discovery, ok := options.WeChat.(wechat.DiscoveryGateway); ok {
		service.discovery = discovery
	}
	if accounts, ok := options.Library.(library.Accounts); ok {
		service.accounts = accounts
	}
	return service
}

func (service *Service) BeginLogin(ctx context.Context, sessionID string) (wechat.LoginFlow, error) {
	if service.session == nil {
		return wechat.LoginFlow{}, fmt.Errorf("begin login: %w", ErrUnavailable)
	}
	return service.session.BeginLogin(ctx, sessionID)
}

func (service *Service) PollLogin(ctx context.Context) (wechat.PollResult, error) {
	if service.session == nil {
		return wechat.PollResult{}, fmt.Errorf("poll login: %w", ErrUnavailable)
	}
	return service.session.PollLogin(ctx)
}

func (service *Service) CompleteLogin(ctx context.Context) (wechat.Session, error) {
	if service.session == nil {
		return wechat.Session{}, fmt.Errorf("complete login: %w", ErrUnavailable)
	}
	return service.session.CompleteLogin(ctx)
}

func (service *Service) RuntimeStatus(ctx context.Context) (domain.RuntimeStatus, error) {
	if err := ctx.Err(); err != nil {
		return domain.RuntimeStatus{}, err
	}
	storage := domain.StorageStatus{}
	if service.library != nil {
		var err error
		storage, err = service.library.StorageStatus(ctx)
		if err != nil {
			return domain.RuntimeStatus{}, fmt.Errorf("read storage status: %w", err)
		}
	}
	return domain.RuntimeStatus{
		Version:       service.version,
		Profile:       service.runtime.Profile,
		Paths:         service.runtime.Paths,
		Portable:      service.runtime.Portable,
		OfflineReady:  storage.DatabaseAvailable && storage.ObjectStoreReady,
		SecretBackend: service.runtime.Secrets.Backend(),
		Storage:       storage,
		CheckedAt:     service.runtime.Clock.Now(),
	}, nil
}

func (service *Service) DiscoverBrowser(ctx context.Context) (runtimeenv.Browser, error) {
	if service.runtime.Browser == nil {
		return runtimeenv.Browser{}, fmt.Errorf("discover Chromium browser: %w", ErrUnavailable)
	}
	return service.runtime.Browser.FindChromium(ctx)
}

func (service *Service) ProcessSignals() <-chan os.Signal {
	if service.runtime.Signals == nil {
		return nil
	}
	return service.runtime.Signals.Signals()
}

func (service *Service) SessionStatus(ctx context.Context) (wechat.Session, error) {
	if service.session != nil {
		return service.session.SessionStatus(ctx)
	}
	if service.wechat == nil {
		return wechat.Session{State: wechat.SessionMissing}, nil
	}
	return service.wechat.SessionStatus(ctx)
}

func (service *Service) ListSwitchableAccounts(ctx context.Context) ([]wechat.SwitchableAccount, error) {
	if service.session == nil {
		return nil, fmt.Errorf("list switchable accounts: %w", ErrUnavailable)
	}
	return service.session.ListSwitchableAccounts(ctx)
}

func (service *Service) SwitchAccount(ctx context.Context, accountID string) (wechat.Session, error) {
	if service.session == nil {
		return wechat.Session{}, fmt.Errorf("switch account: %w", ErrUnavailable)
	}
	return service.session.SwitchAccount(ctx, accountID)
}

func (service *Service) Logout(ctx context.Context) error {
	if service.session == nil {
		return fmt.Errorf("logout: %w", ErrUnavailable)
	}
	return service.session.Logout(ctx)
}

func (service *Service) SearchAccounts(ctx context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	if service.discovery == nil {
		return domain.Page[domain.Account]{}, fmt.Errorf("search accounts: %w", ErrUnavailable)
	}
	return service.discovery.SearchAccounts(ctx, query)
}

func (service *Service) ResolveAccountName(ctx context.Context, articleURL string) (string, error) {
	if service.discovery == nil {
		return "", fmt.Errorf("resolve account name: %w", ErrUnavailable)
	}
	return service.discovery.ResolveAccountName(ctx, articleURL)
}

func (service *Service) ResolveAccountFromArticle(ctx context.Context, articleURL string) (domain.Account, error) {
	if service.discovery == nil {
		return domain.Account{}, fmt.Errorf("resolve account from article: %w", ErrUnavailable)
	}
	return service.discovery.ResolveAccountFromArticle(ctx, articleURL)
}

func (service *Service) AccountDetails(ctx context.Context, fakeID string) (wechat.AccountDetails, error) {
	if service.discovery == nil {
		return wechat.AccountDetails{}, fmt.Errorf("account details: %w", ErrUnavailable)
	}
	return service.discovery.AccountDetails(ctx, fakeID)
}

func (service *Service) AuthorInfo(ctx context.Context, fakeID string) (wechat.AuthorInfo, error) {
	if service.discovery == nil {
		return wechat.AuthorInfo{}, fmt.Errorf("author info: %w", ErrUnavailable)
	}
	return service.discovery.AuthorInfo(ctx, fakeID)
}

func (service *Service) ListArticles(ctx context.Context, request wechat.ArticleListRequest) (wechat.ArticlePage, error) {
	if service.discovery == nil {
		return wechat.ArticlePage{}, fmt.Errorf("list articles: %w", ErrUnavailable)
	}
	return service.discovery.ListArticles(ctx, request)
}

func (service *Service) SaveAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	if service.accounts == nil {
		return domain.Account{}, fmt.Errorf("save account: %w", ErrUnavailable)
	}
	return service.accounts.SaveAccount(ctx, account)
}

func (service *Service) UpdateAccount(ctx context.Context, account domain.Account) (domain.Account, error) {
	if service.accounts == nil {
		return domain.Account{}, fmt.Errorf("update account: %w", ErrUnavailable)
	}
	return service.accounts.UpdateAccount(ctx, account)
}

func (service *Service) GetAccount(ctx context.Context, id domain.AccountID) (domain.Account, error) {
	if service.accounts == nil {
		return domain.Account{}, fmt.Errorf("get account: %w", ErrUnavailable)
	}
	return service.accounts.GetAccount(ctx, id)
}

func (service *Service) GetAccountByFakeID(ctx context.Context, fakeID string) (domain.Account, error) {
	if service.accounts == nil {
		return domain.Account{}, fmt.Errorf("get account by fakeid: %w", ErrUnavailable)
	}
	return service.accounts.GetAccountByFakeID(ctx, fakeID)
}

func (service *Service) GetArticle(ctx context.Context, id domain.ArticleID) (domain.Article, error) {
	articles, ok := service.library.(interface {
		GetArticle(context.Context, domain.ArticleID) (domain.Article, error)
	})
	if !ok {
		return domain.Article{}, fmt.Errorf("get article: %w", ErrUnavailable)
	}
	return articles.GetArticle(ctx, id)
}

// ArticleResourceAvailability exposes only aggregate resource state. The
// concrete library retains all resource URLs and object metadata.
func (service *Service) ArticleResourceAvailability(ctx context.Context, id domain.ArticleID) (library.ArticleResourceAvailability, error) {
	resources, ok := service.library.(interface {
		ArticleResourceAvailability(context.Context, domain.ArticleID) (library.ArticleResourceAvailability, error)
	})
	if !ok {
		return library.ArticleResourceAvailability{}, fmt.Errorf("article resource availability: %w", ErrUnavailable)
	}
	return resources.ArticleResourceAvailability(ctx, id)
}

// LatestArticleMetrics exposes the safe metric projection while the concrete
// library retains snapshot identifiers and credential references.
func (service *Service) LatestArticleMetrics(ctx context.Context, id domain.ArticleID) (library.ArticleMetrics, error) {
	metrics, ok := service.library.(interface {
		LatestArticleMetrics(context.Context, domain.ArticleID) (library.ArticleMetrics, error)
	})
	if !ok {
		return library.ArticleMetrics{}, fmt.Errorf("article metrics: %w", ErrUnavailable)
	}
	return metrics.LatestArticleMetrics(ctx, id)
}

// ListArticleResourceDetails exposes bounded safe resource state while the
// concrete library retains resource identifiers, URLs, digests, and media.
func (service *Service) ListArticleResourceDetails(ctx context.Context, id domain.ArticleID, offset, limit int) (domain.Page[library.ArticleResourceDetail], error) {
	resources, ok := service.library.(interface {
		ListArticleResourceDetails(context.Context, domain.ArticleID, int, int) (domain.Page[library.ArticleResourceDetail], error)
	})
	if !ok {
		return domain.Page[library.ArticleResourceDetail]{}, fmt.Errorf("article resource details: %w", ErrUnavailable)
	}
	return resources.ListArticleResourceDetails(ctx, id, offset, limit)
}

func (service *Service) QueryAccounts(ctx context.Context, query domain.AccountQuery) (domain.Page[domain.Account], error) {
	if service.library == nil {
		return domain.Page[domain.Account]{Items: []domain.Account{}, Offset: query.Offset, Limit: query.Limit}, nil
	}
	return service.library.QueryAccounts(ctx, query)
}

func (service *Service) ExportAccounts(ctx context.Context, query domain.AccountQuery) (domain.AccountManifest, error) {
	if service.accounts == nil {
		return domain.AccountManifest{}, fmt.Errorf("export accounts: %w", ErrUnavailable)
	}
	return service.accounts.ExportAccounts(ctx, query)
}

func (service *Service) ImportAccounts(ctx context.Context, manifest domain.AccountManifest) (domain.AccountImportReport, error) {
	if service.accounts == nil {
		return domain.AccountImportReport{}, fmt.Errorf("import accounts: %w", ErrUnavailable)
	}
	return service.accounts.ImportAccounts(ctx, manifest)
}

func (service *Service) DeleteAccounts(ctx context.Context, ids []domain.AccountID) (domain.AccountDeleteReport, error) {
	if service.accounts == nil {
		return domain.AccountDeleteReport{}, fmt.Errorf("delete accounts: %w", ErrUnavailable)
	}
	return service.accounts.DeleteAccounts(ctx, ids)
}

func (service *Service) QueryArticles(ctx context.Context, query domain.ArticleQuery) (domain.Page[domain.Article], error) {
	if service.library == nil {
		return domain.Page[domain.Article]{Items: []domain.Article{}, Offset: query.Offset, Limit: query.Limit}, nil
	}
	return service.library.QueryArticles(ctx, query)
}

func (service *Service) SaveArticleQuery(ctx context.Context, name string, query domain.ArticleQuery) (domain.SavedArticleQuery, error) {
	queries, ok := service.library.(savedArticleQueries)
	if !ok {
		return domain.SavedArticleQuery{}, fmt.Errorf("save article query: %w", ErrUnavailable)
	}
	return queries.SaveArticleQuery(ctx, name, query)
}

func (service *Service) ListSavedArticleQueries(ctx context.Context) ([]domain.SavedArticleQuery, error) {
	queries, ok := service.library.(savedArticleQueries)
	if !ok {
		return nil, fmt.Errorf("list saved article queries: %w", ErrUnavailable)
	}
	return queries.ListSavedArticleQueries(ctx)
}

func (service *Service) DeleteSavedArticleQuery(ctx context.Context, name string) (bool, error) {
	queries, ok := service.library.(savedArticleQueries)
	if !ok {
		return false, fmt.Errorf("delete saved article query: %w", ErrUnavailable)
	}
	return queries.DeleteSavedArticleQuery(ctx, name)
}

func (service *Service) QueryAlbums(ctx context.Context, query domain.AlbumQuery) (domain.Page[domain.Album], error) {
	if service.library == nil {
		return domain.Page[domain.Album]{Items: []domain.Album{}, Offset: query.Offset, Limit: query.Limit}, nil
	}
	return service.library.QueryAlbums(ctx, query)
}

func (service *Service) SynchronizeAccount(ctx context.Context, request domain.SynchronizeAccountRequest) (domain.Job, error) {
	if service.syncs != nil {
		job, err := service.syncs.Start(ctx, request)
		return service.startJob(ctx, job, err)
	}
	return service.createJob(ctx, "account_sync", request)
}

func (service *Service) SynchronizeAlbum(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	return service.SynchronizeAlbumWithOrder(ctx, accountID, albumID, wechat.AlbumForward)
}

func (service *Service) SynchronizeAlbumWithOrder(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	if accountID == "" || albumID == "" {
		return domain.Job{}, errors.New("album synchronization requires account and album IDs")
	}
	if order != wechat.AlbumForward && order != wechat.AlbumReverse {
		return domain.Job{}, errors.New("album traversal order must be forward or reverse")
	}
	albumRuntime, ok := service.syncs.(OrderedAlbumSyncJobs)
	if !ok {
		return domain.Job{}, fmt.Errorf("album synchronization: %w", ErrUnavailable)
	}
	if service.starter == nil {
		return domain.Job{}, fmt.Errorf("start album_sync worker: %w", ErrUnavailable)
	}
	job, err := albumRuntime.StartAlbumByIDWithOrder(ctx, accountID, albumID, order)
	return service.startJob(ctx, job, err)
}

// SynchronizeAlbumAndDownload traverses a saved album through the same
// resumable album_sync worker, then has that worker enqueue one persistent
// batch article-download job after traversal commits. It is intentionally an
// additive capability so older adapter fakes do not gain a bypass around the
// shared Application interface.
func (service *Service) SynchronizeAlbumAndDownload(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID) (domain.Job, error) {
	return service.SynchronizeAlbumWithOrderAndDownload(ctx, accountID, albumID, wechat.AlbumForward)
}

func (service *Service) SynchronizeAlbumWithOrderAndDownload(ctx context.Context, accountID domain.AccountID, albumID domain.AlbumID, order wechat.AlbumOrder) (domain.Job, error) {
	if accountID == "" || albumID == "" {
		return domain.Job{}, errors.New("album synchronization requires account and album IDs")
	}
	if order != wechat.AlbumForward && order != wechat.AlbumReverse {
		return domain.Job{}, errors.New("album traversal order must be forward or reverse")
	}
	albumRuntime, ok := service.syncs.(OrderedAlbumSyncJobs)
	if !ok {
		return domain.Job{}, fmt.Errorf("album batch download: %w", ErrUnavailable)
	}
	if service.starter == nil {
		return domain.Job{}, fmt.Errorf("start album_sync worker: %w", ErrUnavailable)
	}
	job, err := albumRuntime.StartAlbumByIDWithOrderAndDownload(ctx, accountID, albumID, order)
	return service.startJob(ctx, job, err)
}

// SynchronizeAlbumsWithOrder keeps multi-album traversal inside one durable
// operation. The old single-album methods are intentionally unchanged.
func (service *Service) SynchronizeAlbumsWithOrder(ctx context.Context, albumIDs []domain.AlbumID, order wechat.AlbumOrder, download bool) (domain.Job, error) {
	albumIDs, err := normalizeAlbumIDs(albumIDs)
	if err != nil {
		return domain.Job{}, err
	}
	if order != wechat.AlbumForward && order != wechat.AlbumReverse {
		return domain.Job{}, errors.New("album traversal order must be forward or reverse")
	}
	runtime, ok := service.syncs.(MultiAlbumSyncJobs)
	if !ok {
		return domain.Job{}, fmt.Errorf("multi-album synchronization: %w", ErrUnavailable)
	}
	if service.starter == nil {
		return domain.Job{}, fmt.Errorf("start album_sync worker: %w", ErrUnavailable)
	}
	job, err := runtime.StartAlbumsByIDWithOrder(ctx, albumIDs, order, download)
	return service.startJob(ctx, job, err)
}

func normalizeAlbumIDs(values []domain.AlbumID) ([]domain.AlbumID, error) {
	if len(values) < 1 || len(values) > 50 {
		return nil, errors.New("album traversal requires between 1 and 50 album IDs")
	}
	result := make([]domain.AlbumID, 0, len(values))
	seen := make(map[domain.AlbumID]struct{}, len(values))
	for _, value := range values {
		value = domain.AlbumID(strings.TrimSpace(string(value)))
		if value == "" {
			return nil, errors.New("album traversal IDs must not be empty")
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, errors.New("album traversal IDs must be unique")
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func (service *Service) StartDownload(ctx context.Context, request domain.DownloadRequest) (domain.Job, error) {
	if service.downloads != nil {
		job, err := service.downloads.Start(ctx, request)
		return service.startJob(ctx, job, err)
	}
	kind := "article_download"
	switch request.Kind {
	case "", "article":
	case "resources":
		kind = "resource_download"
	case "metadata":
		kind = "metadata_download"
	case "comments":
		kind = "comments_download"
	case "paid":
		kind = "paid_content_download"
	default:
		return domain.Job{}, fmt.Errorf("unsupported download kind %q", request.Kind)
	}
	return service.createJob(ctx, kind, request)
}

func (service *Service) StartExport(ctx context.Context, request domain.ExportRequest) (domain.Job, error) {
	if service.exports != nil {
		job, err := service.exports.Start(ctx, request)
		return service.startJob(ctx, job, err)
	}
	return service.createJob(ctx, "export", request)
}

func (service *Service) startJob(ctx context.Context, job domain.Job, err error) (domain.Job, error) {
	if err != nil {
		return domain.Job{}, err
	}
	if service.starter == nil {
		if job.ID != "" && service.jobs == nil {
			return job, errors.Join(fmt.Errorf("start %s worker: %w", job.Kind, ErrUnavailable),
				errors.New("cancel unstarted job: job manager is unavailable"))
		}
		if service.jobs != nil && job.ID != "" {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, cleanupErr := service.jobs.Cancel(cleanupCtx, job.ID)
			cancel()
			if cleanupErr != nil {
				return domain.Job{}, errors.Join(fmt.Errorf("start %s worker: %w", job.Kind, ErrUnavailable),
					fmt.Errorf("cancel unstarted job: %w", cleanupErr))
			}
		}
		return domain.Job{}, fmt.Errorf("start %s worker: %w", job.Kind, ErrUnavailable)
	}
	if err := service.starter.Start(ctx, job); err != nil {
		if job.ID != "" && service.jobs == nil {
			return job, errors.Join(err, errors.New("cancel unstarted job: job manager is unavailable"))
		}
		if service.jobs != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_, cleanupErr := service.jobs.Cancel(cleanupCtx, job.ID)
			cancel()
			if cleanupErr != nil {
				return domain.Job{}, errors.Join(err, fmt.Errorf("cancel unstarted job: %w", cleanupErr))
			}
		}
		return domain.Job{}, err
	}
	return job, nil
}

func (service *Service) createJob(ctx context.Context, kind string, payload any) (domain.Job, error) {
	if service.jobs == nil {
		return domain.Job{}, fmt.Errorf("%s: %w", kind, ErrUnavailable)
	}
	return service.jobs.Create(ctx, jobs.Spec{Kind: kind, Profile: service.runtime.Profile, Payload: payload})
}

func (service *Service) GetJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if service.jobs == nil {
		return domain.Job{}, fmt.Errorf("get job: %w", ErrUnavailable)
	}
	return service.jobs.Get(ctx, id)
}

func (service *Service) QueryJobs(ctx context.Context, query domain.JobQuery) (domain.Page[domain.Job], error) {
	if service.jobs == nil {
		return domain.Page[domain.Job]{Items: []domain.Job{}, Offset: query.Offset, Limit: query.Limit}, nil
	}
	return service.jobs.Query(ctx, query)
}

func (service *Service) CancelJob(ctx context.Context, id domain.JobID) (domain.Job, error) {
	if service.jobs == nil {
		return domain.Job{}, fmt.Errorf("cancel job: %w", ErrUnavailable)
	}
	return service.jobs.Cancel(ctx, id)
}

func (service *Service) StorageStatus(ctx context.Context) (domain.StorageStatus, error) {
	if service.library == nil {
		return domain.StorageStatus{}, nil
	}
	return service.library.StorageStatus(ctx)
}

var _ Application = (*Service)(nil)
