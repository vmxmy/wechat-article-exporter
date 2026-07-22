package library

import (
	"context"
	"testing"
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
