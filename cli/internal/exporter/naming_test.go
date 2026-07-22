package exporter

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestRenderFilenameUsesDeterministicTemplatesAcrossPlatforms(t *testing.T) {
	article := NamingData{
		ArticleID: "article-123", Account: "Example", AccountID: "account-9", Title: "Release notes", AID: "42",
		Author: "Alice", PublishedAt: time.Date(2026, 7, 2, 3, 4, 0, 0, time.FixedZone("CST", 8*60*60)), Index: 7,
	}
	for _, platform := range []FilenamePlatform{PlatformWindows, PlatformMacOS, PlatformLinux, PlatformPortable} {
		name, err := RenderFilename(NamingOptions{
			Template: "${YYYY}-${MM}-${DD}_${account}_${title}_${aid}_${author}_${index}", Extension: ".md",
			MaximumBytes: 200, Platform: platform,
		}, article)
		if err != nil {
			t.Fatalf("%s: %v", platform, err)
		}
		if name != "2026-07-01_Example_Release notes_42_Alice_7.md" {
			t.Fatalf("%s filename = %q", platform, name)
		}
	}
}

func TestRenderFilenameFiltersPlatformInvalidAndReservedNames(t *testing.T) {
	tests := []struct {
		name     string
		platform FilenamePlatform
		title    string
		want     string
	}{
		{name: "windows invalid and trailing", platform: PlatformWindows, title: `report<>:"/\\|?* .`, want: "report_"},
		{name: "windows reserved", platform: PlatformWindows, title: "con.txt", want: "_con.txt"},
		{name: "portable reserved", platform: PlatformPortable, title: "LPT9", want: "_LPT9"},
		{name: "mac separators and normalization", platform: PlatformMacOS, title: "Cafe\u0301:part/one", want: "Café_part_one"},
		{name: "linux slash and control", platform: PlatformLinux, title: "part/one\x01two", want: "part_one_two"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := RenderFilename(NamingOptions{Template: "${title}", MaximumBytes: 200, Platform: test.platform},
				NamingData{ArticleID: "article-a", Title: test.title})
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("filename = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRenderFilenameBoundsUTF8AndPreventsTraversal(t *testing.T) {
	name, err := RenderFilename(NamingOptions{Template: "../../${title}/../escape", Extension: ".html", MaximumBytes: 48,
		Platform: PlatformPortable}, NamingData{ArticleID: "article-a", Title: strings.Repeat("界", 40)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(name, `/\\`) || strings.Contains(name, "..") {
		t.Fatalf("unsafe filename = %q", name)
	}
	if len(name) > 48 || !utf8.ValidString(name) || !strings.HasSuffix(name, ".html") {
		t.Fatalf("bounded filename = %q (%d bytes)", name, len(name))
	}
}

func TestPlanFilenamesResolvesCollisionsDeterministically(t *testing.T) {
	items := []NamingData{
		{ArticleID: "article-z", Title: "Same"},
		{ArticleID: "article-a", Title: "same"},
		{ArticleID: "article-m", Title: "Same"},
	}
	options := NamingOptions{Template: "${title}", Extension: ".txt", MaximumBytes: 64, Platform: PlatformWindows}
	first, err := PlanFilenames(options, items)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := PlanFilenames(options, []NamingData{items[2], items[1], items[0]})
	if err != nil {
		t.Fatal(err)
	}
	byID := func(plans []PlannedName) map[domain.ArticleID]string {
		result := make(map[domain.ArticleID]string, len(plans))
		for _, plan := range plans {
			result[plan.ArticleID] = plan.Path
		}
		return result
	}
	left, right := byID(first), byID(reversed)
	for id, path := range left {
		if right[id] != path {
			t.Fatalf("article %s changed from %q to %q after reorder", id, path, right[id])
		}
	}
	if left["article-a"] != "same.txt" {
		t.Fatalf("stable collision winner = %q", left["article-a"])
	}
	seen := map[string]bool{}
	for _, path := range left {
		key := strings.ToLower(path)
		if seen[key] {
			t.Fatalf("duplicate planned path %q", path)
		}
		seen[key] = true
	}
}

func TestRenderFilenameUsesStableFallback(t *testing.T) {
	first, err := RenderFilename(NamingOptions{Template: "${title}", Platform: PlatformPortable},
		NamingData{ArticleID: "article-fallback", Title: "..."})
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderFilename(NamingOptions{Template: "${title}", Platform: PlatformPortable},
		NamingData{ArticleID: "article-fallback", Title: "..."})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "article-") {
		t.Fatalf("fallback names = %q and %q", first, second)
	}
}
