package download

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/credentials"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

func TestCommentsDownloaderPagesDeduplicatesAndResumesReplyFailures(t *testing.T) {
	store := newCommentMemoryStore()
	source := &fakeCommentSource{
		commentPages: []wechat.CommentPage{
			{Continue: true, Buffer: "buffer-2", Comments: []wechat.Comment{{ID: "comment-1", Author: "甲", Content: "one", ReplyTotal: 1}}},
			{Continue: false, Comments: []wechat.Comment{{ID: "comment-1", Author: "甲", Content: "one"}, {ID: "comment-2", Author: "乙", Content: "two", ReplyTotal: 1, ReplyMaxID: 8}}},
		},
		replyResults: map[string][]replySourceResult{
			"comment-1": {{page: wechat.ReplyPage{ContentID: "comment-1", MaxReplyID: 1, Replies: []wechat.Reply{{ID: "1", Content: "done"}}}}},
			"comment-2": {{err: errors.New("temporary upstream failure")}},
		},
	}
	downloader := CommentsDownloader{
		Credentials: &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a", AccountID: "account-a"}, record: downloadCredential()},
		Source:      source, Store: store, MaxRetries: 1,
	}
	first, err := downloader.Download(context.Background(), CommentsRequest{
		ArticleID: "article-a", AccountID: "account-a", BusinessID: "fixture-biz",
		AppMessageID: 10001, ItemIndex: 1, CommentID: "comment-stream",
	})
	if err == nil || !first.Partial || first.PagesCommitted != 2 || first.CommentsStored != 2 || first.ReplyThreadsCompleted != 1 || first.ReplyThreadsFailed != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	if store.checkpoint.Buffer != "" || !store.checkpoint.Complete || len(store.comments) != 2 {
		t.Fatalf("checkpoint=%#v comments=%#v", store.checkpoint, store.comments)
	}

	source.commentPages = nil
	source.replyResults["comment-2"] = []replySourceResult{{page: wechat.ReplyPage{
		ContentID: "comment-2", MaxReplyID: 9, Replies: []wechat.Reply{{ID: "9", Content: "resumed"}},
	}}}
	second, err := downloader.Download(context.Background(), CommentsRequest{
		ArticleID: "article-a", AccountID: "account-a", BusinessID: "fixture-biz",
		AppMessageID: 10001, ItemIndex: 1, CommentID: "comment-stream",
	})
	if err != nil || second.Partial || second.PagesCommitted != 0 || second.ReplyThreadsCompleted != 1 || second.ReplyThreadsSkipped != 1 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if len(source.commentRequests) != 2 || len(source.replyRequests) != 3 || source.replyRequests[2].ContentID != "comment-2" || source.replyRequests[2].MaxReplyID != 8 {
		t.Fatalf("comment requests=%#v reply requests=%#v", source.commentRequests, source.replyRequests)
	}
}

func TestCommentsDownloaderKeepsContinuationCheckpointOnPageFailure(t *testing.T) {
	store := newCommentMemoryStore()
	source := &fakeCommentSource{
		commentPages: []wechat.CommentPage{{Continue: true, Buffer: "resume-here", Comments: []wechat.Comment{{ID: "comment-1"}}}},
		commentErrAt: 1,
	}
	result, err := (CommentsDownloader{
		Credentials: &fixedCredentialLoader{metadata: credentials.Metadata{ID: "credential-a"}, record: downloadCredential()},
		Source:      source, Store: store, MaxRetries: 1,
	}).Download(context.Background(), CommentsRequest{ArticleID: "article-a", AccountID: "account-a", BusinessID: "fixture-biz", CommentID: "stream"})
	if err == nil || !result.Partial || result.PagesCommitted != 1 || store.checkpoint.Buffer != "resume-here" || store.checkpoint.Complete {
		t.Fatalf("result=%#v checkpoint=%#v err=%v", result, store.checkpoint, err)
	}
}

type replySourceResult struct {
	page wechat.ReplyPage
	err  error
}

type fakeCommentSource struct {
	commentPages    []wechat.CommentPage
	commentErrAt    int
	commentRequests []wechat.CommentPageRequest
	replyResults    map[string][]replySourceResult
	replyRequests   []wechat.ReplyPageRequest
}

func (source *fakeCommentSource) FetchComments(_ context.Context, request wechat.CommentPageRequest) (wechat.CommentPage, wechat.RequestProvenance, error) {
	source.commentRequests = append(source.commentRequests, request)
	index := len(source.commentRequests) - 1
	if source.commentErrAt > 0 && index >= source.commentErrAt {
		return wechat.CommentPage{}, wechat.RequestProvenance{}, errors.New("comment page failed")
	}
	if index >= len(source.commentPages) {
		return wechat.CommentPage{}, wechat.RequestProvenance{}, errors.New("unexpected comment request")
	}
	return source.commentPages[index], wechat.RequestProvenance{Route: "trusted"}, nil
}

func (source *fakeCommentSource) FetchReplies(_ context.Context, request wechat.ReplyPageRequest) (wechat.ReplyPage, wechat.RequestProvenance, error) {
	source.replyRequests = append(source.replyRequests, request)
	results := source.replyResults[request.ContentID]
	if len(results) == 0 {
		return wechat.ReplyPage{}, wechat.RequestProvenance{}, errors.New("unexpected reply request")
	}
	result := results[0]
	source.replyResults[request.ContentID] = results[1:]
	return result.page, wechat.RequestProvenance{Route: "trusted"}, result.err
}

type commentMemoryStore struct {
	checkpoint library.CommentCheckpoint
	comments   map[string]library.CommentRecord
	threads    map[string]library.ReplyThread
	replies    map[string]map[string]library.ReplyRecord
}

func newCommentMemoryStore() *commentMemoryStore {
	return &commentMemoryStore{comments: map[string]library.CommentRecord{}, threads: map[string]library.ReplyThread{}, replies: map[string]map[string]library.ReplyRecord{}}
}

func (store *commentMemoryStore) CommentCheckpointForArticle(context.Context, domain.ArticleID) (library.CommentCheckpoint, error) {
	if store.checkpoint.ArticleID == "" {
		return library.CommentCheckpoint{}, sql.ErrNoRows
	}
	return store.checkpoint, nil
}

func (store *commentMemoryStore) CommitCommentPage(_ context.Context, articleID domain.ArticleID, page library.CommentPageCommit) (library.CommentPageResult, error) {
	result := library.CommentPageResult{Received: len(page.Comments)}
	for _, comment := range page.Comments {
		if _, exists := store.comments[comment.UpstreamID]; exists {
			result.Duplicates++
		} else {
			result.Stored++
		}
		store.comments[comment.UpstreamID] = comment
		if comment.ReplyTotal > 0 {
			if _, exists := store.threads[comment.UpstreamID]; !exists {
				store.threads[comment.UpstreamID] = library.ReplyThread{ArticleID: articleID, ContentID: comment.UpstreamID, Total: comment.ReplyTotal, MaxReplyID: comment.ReplyMaxID}
			}
		}
	}
	buffer := page.Buffer
	if page.Complete {
		buffer = ""
	}
	store.checkpoint = library.CommentCheckpoint{ArticleID: articleID, Buffer: buffer, Complete: page.Complete}
	return result, nil
}

func (store *commentMemoryStore) PendingReplyThreads(context.Context, domain.ArticleID) ([]library.ReplyThread, error) {
	items := make([]library.ReplyThread, 0)
	for _, thread := range store.threads {
		if !thread.Complete {
			items = append(items, thread)
		}
	}
	return items, nil
}

func (store *commentMemoryStore) CommentsForArticle(context.Context, domain.ArticleID) ([]library.CommentRecord, error) {
	items := make([]library.CommentRecord, 0, len(store.comments))
	for _, comment := range store.comments {
		items = append(items, comment)
	}
	return items, nil
}

func (store *commentMemoryStore) CommitReplyPage(_ context.Context, _ domain.ArticleID, contentID string, page library.ReplyPageCommit) (library.ReplyPageResult, error) {
	if store.replies[contentID] == nil {
		store.replies[contentID] = map[string]library.ReplyRecord{}
	}
	result := library.ReplyPageResult{Received: len(page.Replies)}
	for _, reply := range page.Replies {
		if _, exists := store.replies[contentID][reply.UpstreamID]; exists {
			result.Duplicates++
		} else {
			result.Stored++
		}
		store.replies[contentID][reply.UpstreamID] = reply
	}
	thread := store.threads[contentID]
	thread.MaxReplyID = page.MaxReplyID
	thread.Fetched = len(store.replies[contentID])
	thread.Complete = thread.Fetched >= thread.Total
	thread.LastError = ""
	store.threads[contentID] = thread
	comment := store.comments[contentID]
	comment.ReplyTotal = thread.Total
	comment.ReplyMaxID = thread.MaxReplyID
	store.comments[contentID] = comment
	return result, nil
}

func (store *commentMemoryStore) RecordReplyFailure(_ context.Context, _ domain.ArticleID, contentID, message string) error {
	thread := store.threads[contentID]
	thread.Attempts++
	thread.LastError = message
	store.threads[contentID] = thread
	return nil
}
