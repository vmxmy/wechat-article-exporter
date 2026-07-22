package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/processor"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/safety"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type PaidContentDownloader struct {
	Credentials CredentialLoader
	Source      CredentialArticleSource
}

type PaidContentRequest struct {
	AccountID domain.AccountID
	URL       string
}

type PaidContentResult struct {
	Body         []byte
	MediaType    string
	CredentialID string
	Route        string
	RequestID    string
}

func (downloader PaidContentDownloader) Fetch(ctx context.Context, request PaidContentRequest) (PaidContentResult, error) {
	if request.AccountID == "" {
		return PaidContentResult{}, errors.New("account ID is required")
	}
	if downloader.Credentials == nil || downloader.Source == nil {
		return PaidContentResult{}, errors.New("paid content downloader dependencies are incomplete")
	}
	metadata, credential, err := downloader.Credentials.LoadForAccount(ctx, request.AccountID)
	if err != nil {
		return PaidContentResult{}, err
	}
	response, err := downloader.Source.FetchArticle(ctx, wechat.CredentialArticleRequest{
		URL: request.URL, Credential: credential, Class: network.PaidContent,
	})
	if err != nil {
		return PaidContentResult{}, err
	}
	return PaidContentResult{Body: response.Body, MediaType: response.MediaType, CredentialID: metadata.ID,
		Route: response.Route, RequestID: response.RequestID}, nil
}

type PaidArticleResult struct {
	ArticleResult
	CredentialID string `json:"credentialId"`
}

// PaidArticleDownloader keeps paid-content fetching on the sensitive route,
// then applies the normal article validation and content commit contract.
type PaidArticleDownloader struct {
	Fetcher      PaidContentDownloader
	Processor    Processor
	Objects      ObjectStore
	Store        ArticleStore
	Now          func() time.Time
	DebugCapture bool
	DebugTTL     time.Duration
}

func (downloader PaidArticleDownloader) Download(ctx context.Context, request PaidContentJobRequest) (PaidArticleResult, error) {
	if request.ArticleID == "" || request.AccountID == "" || request.URL == "" {
		return PaidArticleResult{}, errors.New("paid article ID, account ID, and URL are required")
	}
	if !request.Force && downloader.Store != nil {
		cached, err := downloader.Store.CurrentContent(ctx, request.ArticleID, "html")
		if err == nil && cached.Classification == string(processor.ClassificationValid) &&
			(downloader.Objects == nil || downloader.Objects.Validate(ctx, cached.ObjectDigest) == nil) {
			return PaidArticleResult{ArticleResult: ArticleResult{Cached: true,
				Classification: processor.Classification{State: processor.ClassificationValid}, Content: cached}}, nil
		}
	}
	if downloader.Processor == nil || downloader.Objects == nil || downloader.Store == nil {
		return PaidArticleResult{}, errors.New("paid article downloader dependencies are incomplete")
	}
	fetched, err := downloader.Fetcher.Fetch(ctx, PaidContentRequest{AccountID: request.AccountID, URL: request.URL})
	if err != nil {
		return PaidArticleResult{}, err
	}
	parsed, parseErr := downloader.Processor.Process(ctx, bytes.NewReader(fetched.Body))
	result := PaidArticleResult{ArticleResult: ArticleResult{Classification: parsed.Classification,
		Route: fetched.Route, RequestID: fetched.RequestID}, CredentialID: fetched.CredentialID}
	if parseErr != nil || parsed.Classification.State != processor.ClassificationValid || parsed.Article == nil {
		if parsed.Classification.State != "" {
			if markErr := downloader.Store.MarkArticleState(ctx, request.ArticleID, string(parsed.Classification.State),
				parsed.Classification.State == processor.ClassificationDeleted); markErr != nil {
				return result, errors.Join(parseErr, markErr)
			}
		}
		if downloader.DebugCapture {
			if debugErr := downloader.capturePaidDebug(ctx, request, fetched, parsed.Classification); debugErr != nil {
				return result, errors.Join(parseErr, debugErr)
			}
		}
		if parseErr != nil {
			return result, parseErr
		}
		return result, ErrUnavailable
	}
	mediaType := fetched.MediaType
	if mediaType == "" {
		mediaType = "text/html"
	}
	object, err := downloader.Objects.Put(ctx, bytes.NewReader(fetched.Body), mediaType)
	if err != nil {
		return result, fmt.Errorf("persist paid article object: %w", err)
	}
	capturedAt := downloader.now()
	content, err := downloader.Store.CommitContent(ctx, request.ArticleID, object, "html", request.URL,
		string(parsed.Classification.State), parsed.Article.Comments.ID, capturedAt)
	if err != nil {
		return result, err
	}
	result.Content = content
	return result, nil
}

func (downloader PaidArticleDownloader) capturePaidDebug(ctx context.Context, request PaidContentJobRequest,
	fetched PaidContentResult, classification processor.Classification) error {
	mediaType := fetched.MediaType
	if mediaType == "" {
		mediaType = "text/html"
	}
	object, err := downloader.Objects.Put(ctx, bytes.NewReader(fetched.Body), mediaType)
	if err != nil {
		return err
	}
	now := downloader.now()
	expiresAt := time.Time{}
	if downloader.DebugTTL > 0 {
		expiresAt = now.Add(downloader.DebugTTL)
	}
	_, err = downloader.Store.RecordDebugIncident(ctx, library.DebugIncident{
		Operation: "paid_article_download", Classification: string(classification.State), RequestID: fetched.RequestID,
		ObjectDigest: object.Digest, Summary: safety.RedactText(fmt.Sprintf("article=%s url=%s classification=%s reason=%s",
			request.ArticleID, safety.RedactURL(request.URL), classification.State, classification.Reason)),
		CreatedAt: now, ExpiresAt: expiresAt,
	})
	return err
}

func (downloader PaidArticleDownloader) now() time.Time {
	if downloader.Now != nil {
		return downloader.Now()
	}
	return time.Now()
}
