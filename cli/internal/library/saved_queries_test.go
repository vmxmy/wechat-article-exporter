package library

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestSavedArticleQueriesRoundTripUpdateDeleteAndProfileIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	ctx := context.Background()
	databaseA, err := Open(ctx, OpenOptions{Path: path, ProfileID: "profile-a"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = databaseA.Close() })
	databaseB, err := Open(ctx, OpenOptions{Path: path, ProfileID: "profile-b"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = databaseB.Close() })
	original := true
	saved, err := databaseA.SaveArticleQuery(ctx, " original ", domain.ArticleQuery{
		Author: "Alice", Original: &original,
		Sorts: []domain.ArticleSort{{Field: "published", Direction: domain.SortDescending}}, Limit: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Name != "original" || saved.Query.Author != "Alice" || saved.CreatedAt.IsZero() {
		t.Fatalf("saved = %#v", saved)
	}
	updated, err := databaseA.SaveArticleQuery(ctx, "original", domain.ArticleQuery{Keyword: "agent", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Query.Keyword != "agent" || !updated.CreatedAt.Equal(saved.CreatedAt) {
		t.Fatalf("updated = %#v", updated)
	}
	items, err := databaseA.ListSavedArticleQueries(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v error=%v", items, err)
	}
	if _, err := databaseB.GetSavedArticleQuery(ctx, "original"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("profile B error = %v", err)
	}
	deleted, err := databaseA.DeleteSavedArticleQuery(ctx, "original")
	if err != nil || !deleted {
		t.Fatalf("deleted=%v error=%v", deleted, err)
	}
	deleted, err = databaseA.DeleteSavedArticleQuery(ctx, "original")
	if err != nil || deleted {
		t.Fatalf("second deleted=%v error=%v", deleted, err)
	}
}
