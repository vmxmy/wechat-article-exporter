package library

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAnchorStatsUpsertAndList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anchor-stats.sqlite3")
	database, err := Open(context.Background(), OpenOptions{Path: path, ProfileID: "profile-a", ProfileName: "Profile A"})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx := context.Background()

	for range 3 {
		if err := database.RecordAnchorHit(ctx, "wechat.article_account_name", "js_name"); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.RecordAnchorHit(ctx, "wechat.article_account_name", "nickname-var"); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordAnchorHit(ctx, "wechat.home_nickname", "cgidata-nick_name"); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordAnchorHit(ctx, "", "ignored"); err != nil {
		t.Fatal("empty surface must be a silent no-op")
	}

	stats, err := database.AnchorStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("stats = %d rows, want 3", len(stats))
	}
	if stats[0].Surface != "wechat.article_account_name" || stats[0].Anchor != "js_name" || stats[0].HitCount != 3 {
		t.Fatalf("stats[0] = %+v", stats[0])
	}
	if stats[1].Anchor != "nickname-var" || stats[1].HitCount != 1 {
		t.Fatalf("stats[1] = %+v", stats[1])
	}
	if stats[0].LastHitAt.IsZero() {
		t.Fatal("LastHitAt must be recorded")
	}
}
