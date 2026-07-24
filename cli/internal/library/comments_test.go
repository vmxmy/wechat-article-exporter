package library

import (
	"context"
	"testing"
	"time"
)

func TestCommitCommentPageDeduplicatesAndPersistsContinuationAtomically(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	fetchedAt := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	result, err := database.CommitCommentPage(context.Background(), "article-a", CommentPageCommit{
		Comments: []CommentRecord{
			{UpstreamID: "comment-1", AuthorName: "读者甲", Content: "第一条", LikeCount: 2, ReplyTotal: 2,
				ReplyMaxID: 1, EmbeddedReplies: []ReplyRecord{{UpstreamID: "1", AuthorName: "作者", Content: "谢谢"}}},
			{UpstreamID: "comment-1", AuthorName: "读者甲", Content: "第一条", LikeCount: 2},
		},
		Buffer: "buffer-2", Complete: false, FetchedAt: fetchedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Received != 2 || result.Stored != 1 || result.Duplicates != 1 {
		t.Fatalf("result=%#v", result)
	}
	checkpoint, err := database.CommentCheckpointForArticle(context.Background(), "article-a")
	if err != nil || checkpoint.Buffer != "buffer-2" || checkpoint.Complete {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	threads, err := database.PendingReplyThreads(context.Background(), "article-a")
	if err != nil || len(threads) != 1 || threads[0].ContentID != "comment-1" || threads[0].Total != 2 || threads[0].MaxReplyID != 1 {
		t.Fatalf("threads=%#v err=%v", threads, err)
	}
	comments, err := database.CommentsForArticle(context.Background(), "article-a")
	if err != nil || len(comments) != 1 || len(comments[0].EmbeddedReplies) != 1 {
		t.Fatalf("comments=%#v err=%v", comments, err)
	}
}

func TestCommentContinuationResumeKeepsCommittedPages(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	_, err := database.CommitCommentPage(context.Background(), "article-a", CommentPageCommit{
		Comments: []CommentRecord{{UpstreamID: "comment-1", AuthorName: "甲", Content: "one"}},
		Buffer:   "resume-here", Complete: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.CommitCommentPage(context.Background(), "article-a", CommentPageCommit{
		Comments: []CommentRecord{
			{UpstreamID: "comment-1", AuthorName: "甲", Content: "one"},
			{UpstreamID: "comment-2", AuthorName: "乙", Content: "two"},
		},
		Complete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := database.CommentCheckpointForArticle(context.Background(), "article-a")
	if err != nil || !checkpoint.Complete || checkpoint.Buffer != "" {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	comments, err := database.CommentsForArticle(context.Background(), "article-a")
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments=%#v err=%v", comments, err)
	}
}

func TestListCommentsForArticleIsBoundedOrderedAndDoesNotHydrateReplies(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	createdAt := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	_, err := database.CommitCommentPage(context.Background(), "article-a", CommentPageCommit{Comments: []CommentRecord{
		{UpstreamID: "comment-b", Content: "second", CreatedAt: createdAt.Add(time.Minute), EmbeddedReplies: []ReplyRecord{{UpstreamID: "reply-b", Content: "hidden here"}}},
		{UpstreamID: "comment-a", Content: "first", CreatedAt: createdAt},
	}, Complete: true, FetchedAt: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	page, err := database.ListCommentsForArticle(context.Background(), "article-a", 1, 1)
	if err != nil || page.Total != 2 || page.Offset != 1 || page.Limit != 1 || len(page.Items) != 1 || page.Items[0].UpstreamID != "comment-b" || len(page.Items[0].EmbeddedReplies) != 0 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}
