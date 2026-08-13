package application

import (
	"context"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/library"
)

type anchorStatsLibrary struct {
	stats []library.AnchorStat
}

func (fake *anchorStatsLibrary) QueryAccounts(context.Context, domain.AccountQuery) (domain.Page[domain.Account], error) {
	return domain.Page[domain.Account]{}, nil
}
func (fake *anchorStatsLibrary) QueryArticles(context.Context, domain.ArticleQuery) (domain.Page[domain.Article], error) {
	return domain.Page[domain.Article]{}, nil
}
func (fake *anchorStatsLibrary) QueryAlbums(context.Context, domain.AlbumQuery) (domain.Page[domain.Album], error) {
	return domain.Page[domain.Album]{}, nil
}
func (fake *anchorStatsLibrary) StorageStatus(context.Context) (domain.StorageStatus, error) {
	return domain.StorageStatus{}, nil
}
func (fake *anchorStatsLibrary) AnchorStats(context.Context) ([]library.AnchorStat, error) {
	return fake.stats, nil
}

func TestAnchorDiagnosticsDriftDetection(t *testing.T) {
	earlier := time.Unix(1_700_000_000, 0)
	later := earlier.Add(time.Hour)

	for _, testCase := range []struct {
		name      string
		stats     []library.AnchorStat
		wantDrift bool
	}{
		{
			name: "primary current means no drift",
			stats: []library.AnchorStat{
				{Surface: "wechat.article_account_name", Anchor: "js_name", HitCount: 10, LastHitAt: later},
				{Surface: "wechat.article_account_name", Anchor: "nickname-var", HitCount: 2, LastHitAt: earlier},
			},
		},
		{
			name: "silent primary with live fallback is drift",
			stats: []library.AnchorStat{
				{Surface: "wechat.article_account_name", Anchor: "nickname-var", HitCount: 5, LastHitAt: later},
			},
			wantDrift: true,
		},
		{
			name: "primary older than fallback is drift",
			stats: []library.AnchorStat{
				{Surface: "wechat.article_account_name", Anchor: "js_name", HitCount: 40, LastHitAt: earlier},
				{Surface: "wechat.article_account_name", Anchor: "wx_follow_nickname", HitCount: 1, LastHitAt: later},
			},
			wantDrift: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service := New(Options{Library: &anchorStatsLibrary{stats: testCase.stats}})
			result, err := service.AnchorDiagnostics(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 1 {
				t.Fatalf("surfaces = %d, want 1", len(result))
			}
			if result[0].DriftSuspected != testCase.wantDrift {
				t.Fatalf("DriftSuspected = %v, want %v (%+v)", result[0].DriftSuspected, testCase.wantDrift, result[0])
			}
		})
	}
}

func TestAnchorDiagnosticsOmitsQuietSurfacesAndUnknownRows(t *testing.T) {
	service := New(Options{Library: &anchorStatsLibrary{stats: []library.AnchorStat{
		{Surface: "wechat.home_nickname", Anchor: "cgidata-nick_name", HitCount: 7, LastHitAt: time.Unix(1_700_000_000, 0)},
		{Surface: "unknown.surface", Anchor: "whatever", HitCount: 1, LastHitAt: time.Unix(1_700_000_000, 0)},
	}}})
	result, err := service.AnchorDiagnostics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Surface != "wechat.home_nickname" {
		t.Fatalf("result = %+v", result)
	}
	if result[0].DriftSuspected {
		t.Fatal("single-anchor surface with hits must not report drift")
	}
	if result[0].Anchors[0].Position != 1 || result[0].Anchors[0].HitCount != 7 {
		t.Fatalf("observation = %+v", result[0].Anchors[0])
	}
}
