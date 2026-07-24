package library

import (
	"context"
	"testing"
	"time"
)

func TestReplyThreadPartialFailurePersistsAndResumeTargetsOnlyIncomplete(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	_, err := database.CommitCommentPage(context.Background(), "article-a", CommentPageCommit{
		Comments: []CommentRecord{
			{UpstreamID: "comment-1", ReplyTotal: 1},
			{UpstreamID: "comment-2", ReplyTotal: 1, ReplyMaxID: 8},
		},
		Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CommitReplyPage(context.Background(), "article-a", "comment-1", ReplyPageCommit{
		Replies: []ReplyRecord{{UpstreamID: "1", AuthorName: "作者", Content: "完成"}}, MaxReplyID: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordReplyFailure(context.Background(), "article-a", "comment-2", "temporary upstream failure"); err != nil {
		t.Fatal(err)
	}
	pending, err := database.PendingReplyThreads(context.Background(), "article-a")
	if err != nil || len(pending) != 1 || pending[0].ContentID != "comment-2" || pending[0].Attempts != 1 || pending[0].MaxReplyID != 8 {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	if _, err := database.CommitReplyPage(context.Background(), "article-a", "comment-2", ReplyPageCommit{
		Replies: []ReplyRecord{{UpstreamID: "9", AuthorName: "乙", Content: "恢复成功"}}, MaxReplyID: 9,
	}); err != nil {
		t.Fatal(err)
	}
	pending, err = database.PendingReplyThreads(context.Background(), "article-a")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after resume=%#v err=%v", pending, err)
	}
	replies, err := database.RepliesForComment(context.Background(), "article-a", "comment-2")
	if err != nil || len(replies) != 1 || replies[0].UpstreamID != "9" {
		t.Fatalf("replies=%#v err=%v", replies, err)
	}
}

func TestCommitReplyPageReturnsPersistedMonotonicMaxReplyID(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	ctx := context.Background()
	if _, err := database.CommitCommentPage(ctx, "article-a", CommentPageCommit{
		Comments: []CommentRecord{{UpstreamID: "comment-1", ReplyTotal: 3}}, Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	first, err := database.CommitReplyPage(ctx, "article-a", "comment-1", ReplyPageCommit{
		Replies: []ReplyRecord{{UpstreamID: "10", Content: "newer"}}, MaxReplyID: 10,
	})
	if err != nil || first.MaxReplyID != 10 {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	outOfOrder, err := database.CommitReplyPage(ctx, "article-a", "comment-1", ReplyPageCommit{
		Replies: []ReplyRecord{{UpstreamID: "9", Content: "older"}}, MaxReplyID: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outOfOrder.MaxReplyID != 10 {
		t.Fatalf("out-of-order page returned maxReplyId=%d, want persisted 10", outOfOrder.MaxReplyID)
	}
}

func TestListRepliesForCommentIsBoundedAndOrdered(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	createdAt := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	_, err := database.CommitCommentPage(context.Background(), "article-a", CommentPageCommit{Comments: []CommentRecord{{UpstreamID: "comment-1", ReplyTotal: 2}}, Complete: true, FetchedAt: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.CommitReplyPage(context.Background(), "article-a", "comment-1", ReplyPageCommit{Replies: []ReplyRecord{
		{UpstreamID: "reply-b", Content: "second", CreatedAt: createdAt.Add(time.Minute)},
		{UpstreamID: "reply-a", Content: "first", CreatedAt: createdAt},
	}, FetchedAt: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.ListRepliesForComment(context.Background(), "article-a", "comment-1", 1, 1)
	if err != nil || page.Total != 2 || page.Offset != 1 || page.Limit != 1 || len(page.Items) != 1 || page.Items[0].UpstreamID != "reply-b" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}
