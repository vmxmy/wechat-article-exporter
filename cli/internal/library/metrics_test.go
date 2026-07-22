package library

import (
	"context"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestMetricSnapshotsPersistCaptureTimeAndCredentialProvenance(t *testing.T) {
	database := openContentDatabase(t)
	seedContentArticle(t, database)
	capturedAt := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	written, err := database.CommitMetricSnapshot(context.Background(), MetricSnapshot{
		ArticleID: "article-a", ReadCount: 1200, OldLikeCount: 31, ShareCount: 17,
		LikeCount: 42, CommentCount: 6, CredentialID: "credential-a", CapturedAt: capturedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if written.ID == "" || written.CredentialID != "credential-a" || !written.CapturedAt.Equal(capturedAt) {
		t.Fatalf("written=%#v", written)
	}
	latest, err := database.LatestMetricSnapshot(context.Background(), "article-a")
	if err != nil || latest != written {
		t.Fatalf("latest=%#v err=%v", latest, err)
	}
	articles, err := database.QueryArticles(context.Background(), domain.ArticleQuery{Keyword: "Article"})
	if err != nil || len(articles.Items) != 1 || articles.Items[0].ReadCount != 1200 || articles.Items[0].CommentCount != 6 {
		t.Fatalf("articles=%#v err=%v", articles, err)
	}
}
