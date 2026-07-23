package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

type CommentSource interface {
	FetchComments(context.Context, wechat.CommentPageRequest) (wechat.CommentPage, wechat.RequestProvenance, error)
	FetchReplies(context.Context, wechat.ReplyPageRequest) (wechat.ReplyPage, wechat.RequestProvenance, error)
}

type CommentStore interface {
	CommentCheckpointForArticle(context.Context, domain.ArticleID) (library.CommentCheckpoint, error)
	CommitCommentPage(context.Context, domain.ArticleID, library.CommentPageCommit) (library.CommentPageResult, error)
	CommentsForArticle(context.Context, domain.ArticleID) ([]library.CommentRecord, error)
	PendingReplyThreads(context.Context, domain.ArticleID) ([]library.ReplyThread, error)
	CommitReplyPage(context.Context, domain.ArticleID, string, library.ReplyPageCommit) (library.ReplyPageResult, error)
	RecordReplyFailure(context.Context, domain.ArticleID, string, string) error
}

type CommentsDownloader struct {
	Credentials CredentialLoader
	Source      CommentSource
	Store       CommentStore
	MaxRetries  int
	Now         func() time.Time
}

type CommentsRequest struct {
	ArticleID    domain.ArticleID
	AccountID    domain.AccountID
	BusinessID   string
	AppMessageID int64
	ItemIndex    int
	CommentID    string
}

type CommentsResult struct {
	PagesCommitted        int
	CommentsStored        int
	CommentsDeduplicated  int
	ReplyThreadsCompleted int
	ReplyThreadsFailed    int
	ReplyThreadsSkipped   int
	Partial               bool
}

func (downloader CommentsDownloader) Download(ctx context.Context, request CommentsRequest) (CommentsResult, error) {
	if request.ArticleID == "" || request.AccountID == "" || request.CommentID == "" {
		return CommentsResult{}, errors.New("article ID, account ID, and comment ID are required")
	}
	if downloader.Credentials == nil || downloader.Source == nil || downloader.Store == nil {
		return CommentsResult{}, errors.New("comments downloader dependencies are incomplete")
	}
	_, credential, err := downloader.Credentials.LoadForAccount(ctx, request.AccountID)
	if err != nil {
		return CommentsResult{}, err
	}
	result := CommentsResult{}
	checkpoint, checkpointErr := downloader.Store.CommentCheckpointForArticle(ctx, request.ArticleID)
	if checkpointErr != nil {
		if !errors.Is(checkpointErr, sql.ErrNoRows) {
			return CommentsResult{}, checkpointErr
		}
		checkpoint = library.CommentCheckpoint{}
	}
	if !checkpoint.Complete {
		buffer := checkpoint.Buffer
		for {
			page, _, fetchErr := downloader.fetchCommentPage(ctx, request, credential, buffer)
			if fetchErr != nil {
				result.Partial = result.PagesCommitted > 0 || checkpoint.Buffer != ""
				return result, fetchErr
			}
			commit := library.CommentPageCommit{Buffer: page.Buffer, Complete: !page.Continue, FetchedAt: downloader.now()}
			for _, comment := range page.Comments {
				commit.Comments = append(commit.Comments, library.CommentRecord{
					UpstreamID: comment.ID, AuthorName: comment.Author, Content: comment.Content,
					LikeCount: comment.LikeCount, CreatedAt: comment.CreatedAt, FetchedAt: commit.FetchedAt,
					ReplyTotal: comment.ReplyTotal, ReplyMaxID: comment.ReplyMaxID,
					EmbeddedReplies: mapReplies(comment.EmbeddedReplies, commit.FetchedAt),
				})
			}
			committed, commitErr := downloader.Store.CommitCommentPage(ctx, request.ArticleID, commit)
			if commitErr != nil {
				return result, commitErr
			}
			result.PagesCommitted++
			result.CommentsStored += committed.Stored
			result.CommentsDeduplicated += committed.Duplicates
			if !page.Continue {
				break
			}
			if page.Buffer == buffer {
				result.Partial = true
				return result, errors.New("comments continuation buffer did not advance")
			}
			buffer = page.Buffer
		}
	}

	threads, err := downloader.Store.PendingReplyThreads(ctx, request.ArticleID)
	if err != nil {
		return result, err
	}
	comments, err := downloader.Store.CommentsForArticle(ctx, request.ArticleID)
	if err != nil {
		return result, err
	}
	pendingIDs := make(map[string]struct{}, len(threads))
	for _, thread := range threads {
		pendingIDs[thread.ContentID] = struct{}{}
	}
	for _, comment := range comments {
		if comment.ReplyTotal > 0 {
			if _, pending := pendingIDs[comment.UpstreamID]; pending {
				continue
			}
			result.ReplyThreadsSkipped++
		}
	}
	sort.Slice(threads, func(left, right int) bool { return threads[left].ContentID < threads[right].ContentID })
	var failures []error
	for _, thread := range threads {
		current := thread
		for {
			page, _, fetchErr := downloader.fetchReplyPage(ctx, request, credential, current)
			if fetchErr != nil {
				_ = downloader.Store.RecordReplyFailure(ctx, request.ArticleID, thread.ContentID, fetchErr.Error())
				result.ReplyThreadsFailed++
				failures = append(failures, fmt.Errorf("reply thread %s: %w", thread.ContentID, fetchErr))
				break
			}
			committed, commitErr := downloader.Store.CommitReplyPage(ctx, request.ArticleID, thread.ContentID, library.ReplyPageCommit{
				Replies: mapReplies(page.Replies, downloader.now()), MaxReplyID: page.MaxReplyID, FetchedAt: downloader.now(),
			})
			if commitErr != nil {
				result.ReplyThreadsFailed++
				failures = append(failures, fmt.Errorf("reply thread %s: %w", thread.ContentID, commitErr))
				break
			}
			if committed.Complete {
				result.ReplyThreadsCompleted++
				break
			}
			if committed.MaxReplyID <= current.MaxReplyID || len(page.Replies) == 0 {
				err := errors.New("reply continuation did not advance")
				_ = downloader.Store.RecordReplyFailure(ctx, request.ArticleID, thread.ContentID, err.Error())
				result.ReplyThreadsFailed++
				failures = append(failures, fmt.Errorf("reply thread %s: %w", thread.ContentID, err))
				break
			}
			current.MaxReplyID = committed.MaxReplyID
			current.Fetched = committed.Fetched
		}
	}
	if len(failures) > 0 {
		result.Partial = true
		return result, errors.Join(failures...)
	}
	return result, nil
}

func (downloader CommentsDownloader) fetchCommentPage(ctx context.Context, request CommentsRequest,
	credential credentials.Record, buffer string) (wechat.CommentPage, wechat.RequestProvenance, error) {
	attempts := downloader.retries()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		page, provenance, err := downloader.Source.FetchComments(ctx, wechat.CommentPageRequest{
			BusinessID: request.BusinessID, AppMessageID: request.AppMessageID, ItemIndex: request.ItemIndex,
			CommentID: request.CommentID, Buffer: buffer, Credential: credential,
		})
		if err == nil {
			return page, provenance, nil
		}
		lastErr = err
		if errors.Is(err, credentials.ErrCredentialExpired) {
			break
		}
	}
	return wechat.CommentPage{}, wechat.RequestProvenance{}, lastErr
}

func (downloader CommentsDownloader) fetchReplyPage(ctx context.Context, request CommentsRequest,
	credential credentials.Record, thread library.ReplyThread) (wechat.ReplyPage, wechat.RequestProvenance, error) {
	attempts := downloader.retries()
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		page, provenance, err := downloader.Source.FetchReplies(ctx, wechat.ReplyPageRequest{
			BusinessID: request.BusinessID, AppMessageID: request.AppMessageID, ItemIndex: request.ItemIndex,
			CommentID: request.CommentID, ContentID: thread.ContentID, MaxReplyID: thread.MaxReplyID, Credential: credential,
		})
		if err == nil {
			return page, provenance, nil
		}
		lastErr = err
		if errors.Is(err, credentials.ErrCredentialExpired) {
			break
		}
	}
	return wechat.ReplyPage{}, wechat.RequestProvenance{}, lastErr
}

func (downloader CommentsDownloader) retries() int {
	if downloader.MaxRetries > 0 {
		return downloader.MaxRetries
	}
	return 3
}

func (downloader CommentsDownloader) now() time.Time {
	if downloader.Now != nil {
		return downloader.Now()
	}
	return time.Now()
}

func mapReplies(replies []wechat.Reply, fetchedAt time.Time) []library.ReplyRecord {
	result := make([]library.ReplyRecord, 0, len(replies))
	for _, reply := range replies {
		result = append(result, library.ReplyRecord{UpstreamID: reply.ID, AuthorName: reply.Author,
			Content: reply.Content, LikeCount: reply.LikeCount, CreatedAt: reply.CreatedAt, FetchedAt: fetchedAt})
	}
	return result
}
