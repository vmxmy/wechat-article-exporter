package exporter

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

type fakeSelectionSource struct {
	urls         map[string]domain.ArticleID
	savedQueries map[string]domain.ArticleQuery
	queries      map[string][]domain.ArticleID
	seenQueries  []domain.ArticleQuery
}

func (source *fakeSelectionSource) ResolveArticleURL(_ context.Context, rawURL string) (domain.ArticleID, error) {
	return source.urls[rawURL], nil
}

func (source *fakeSelectionSource) LoadSavedArticleQuery(_ context.Context, id string) (domain.ArticleQuery, error) {
	return source.savedQueries[id], nil
}

func (source *fakeSelectionSource) QueryArticleIDs(_ context.Context, query domain.ArticleQuery) ([]domain.ArticleID, error) {
	source.seenQueries = append(source.seenQueries, query)
	return append([]domain.ArticleID(nil), source.queries[queryKey(query)]...), nil
}

func queryKey(query domain.ArticleQuery) string {
	return string(query.AccountID) + "|" + string(query.AlbumID) + "|" + query.Keyword + "|" + query.State + "|" + query.Sort
}

func TestBuildSelectionManifestFreezesExactSelectionAndOptions(t *testing.T) {
	createdAt := time.Date(2026, 7, 22, 8, 9, 10, 123000000, time.FixedZone("CST", 8*60*60))
	formatOptions := map[string]any{"comments": true, "metadata": map[string]any{"header": "yaml"}}
	request := domain.ExportRequest{
		Format: "markdown",
		Selection: domain.ExportSelection{
			Kind:       domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-b", "article-a"},
		},
		Options: domain.ExportOptions{
			NamingTemplate:   "${YYYY}-${title}",
			MaximumNameBytes: 120,
			CollisionPolicy:  "fail",
			FormatOptions:    formatOptions,
		},
	}

	manifest, err := BuildSelectionManifest(context.Background(), nil, request, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != SelectionManifestVersion || manifest.Kind != domain.ExportSelectionExplicitIDs {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if !reflect.DeepEqual(manifest.ArticleIDs, []domain.ArticleID{"article-b", "article-a"}) {
		t.Fatalf("article order = %#v", manifest.ArticleIDs)
	}
	if manifest.CreatedAt != createdAt.UTC() {
		t.Fatalf("createdAt = %s, want %s", manifest.CreatedAt, createdAt.UTC())
	}
	if manifest.FilterSummary != `{"articleIds":["article-b","article-a"],"kind":"explicit_ids"}` {
		t.Fatalf("filter summary = %s", manifest.FilterSummary)
	}
	if manifest.ID == "" || len(manifest.DigestSHA256) != 64 {
		t.Fatalf("stable identity missing: %#v", manifest)
	}

	request.Selection.ArticleIDs[0] = "mutated"
	formatOptions["comments"] = false
	if manifest.ArticleIDs[0] != "article-b" || manifest.Options.FormatOptions["comments"] != true {
		t.Fatalf("manifest retained caller-owned data: %#v", manifest)
	}

	again, err := BuildSelectionManifest(context.Background(), nil, domain.ExportRequest{
		Format: "markdown",
		Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-b", "article-a"}},
		Options: domain.ExportOptions{NamingTemplate: "${YYYY}-${title}", MaximumNameBytes: 120,
			CollisionPolicy: "fail", FormatOptions: map[string]any{"metadata": map[string]any{"header": "yaml"}, "comments": true}},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != manifest.ID || again.DigestSHA256 != manifest.DigestSHA256 {
		t.Fatalf("same selection produced unstable identity: %#v != %#v", again, manifest)
	}
}

func TestBuildSelectionManifestAcceptsEverySelectionSource(t *testing.T) {
	source := &fakeSelectionSource{
		urls: map[string]domain.ArticleID{"https://mp.weixin.qq.com/s/a": "url-a", "https://mp.weixin.qq.com/s/b": "url-b"},
		savedQueries: map[string]domain.ArticleQuery{
			"unread": {Keyword: "release", State: "ready", Sort: "title", Limit: 20, Offset: 20},
		},
		queries: map[string][]domain.ArticleID{
			"account-a||||published_desc":    {"account-2", "account-1"},
			"|album-a|||published_desc":      {"album-2", "album-1"},
			"||release|ready|title":          {"saved-2", "saved-1"},
			"||release|ready|published_desc": {"matching-2", "matching-1"},
		},
	}
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		selection domain.ExportSelection
		want      []domain.ArticleID
	}{
		{name: "URLs", selection: domain.ExportSelection{Kind: domain.ExportSelectionURLs,
			URLs: []string{"https://mp.weixin.qq.com/s/b", "https://mp.weixin.qq.com/s/a"}}, want: []domain.ArticleID{"url-b", "url-a"}},
		{name: "account", selection: domain.ExportSelection{Kind: domain.ExportSelectionAccount, AccountID: "account-a"},
			want: []domain.ArticleID{"account-2", "account-1"}},
		{name: "album", selection: domain.ExportSelection{Kind: domain.ExportSelectionAlbum, AlbumID: "album-a"},
			want: []domain.ArticleID{"album-2", "album-1"}},
		{name: "saved query", selection: domain.ExportSelection{Kind: domain.ExportSelectionSavedQuery, SavedQueryID: "unread"},
			want: []domain.ArticleID{"saved-2", "saved-1"}},
		{name: "all matching", selection: domain.ExportSelection{Kind: domain.ExportSelectionAllMatching,
			Query: domain.ArticleQuery{Keyword: "release", State: "ready"}}, want: []domain.ArticleID{"matching-2", "matching-1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := BuildSelectionManifest(context.Background(), source,
				domain.ExportRequest{Format: "json", Selection: test.selection}, now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(manifest.ArticleIDs, test.want) {
				t.Fatalf("article IDs = %#v, want %#v", manifest.ArticleIDs, test.want)
			}
			if manifest.FilterSummary == "" {
				t.Fatal("filter summary was empty")
			}
		})
	}
}

func TestBuildSelectionManifestRejectsAmbiguousOrDuplicateSelection(t *testing.T) {
	tests := []domain.ExportRequest{
		{Format: "text"},
		{Format: "text", Selection: domain.ExportSelection{Kind: domain.ExportSelectionURLs}},
		{Format: "text", Selection: domain.ExportSelection{Kind: domain.ExportSelectionExplicitIDs,
			ArticleIDs: []domain.ArticleID{"article-a", "article-a"}}},
		{Format: "text", Selection: domain.ExportSelection{Kind: domain.ExportSelectionAccount,
			AccountID: "account-a", AlbumID: "album-a"}},
	}
	for index, request := range tests {
		if _, err := BuildSelectionManifest(context.Background(), &fakeSelectionSource{}, request, time.Now()); err == nil {
			t.Fatalf("case %d unexpectedly succeeded", index)
		}
	}
}

func TestBuildSelectionManifestSupportsLegacyExplicitArticleIDs(t *testing.T) {
	manifest, err := BuildSelectionManifest(context.Background(), nil, domain.ExportRequest{
		ArticleIDs: []domain.ArticleID{"legacy-b", "legacy-a"}, Format: "html",
	}, time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != domain.ExportSelectionExplicitIDs || !reflect.DeepEqual(manifest.ArticleIDs, []domain.ArticleID{"legacy-b", "legacy-a"}) {
		t.Fatalf("legacy manifest = %#v", manifest)
	}
}
