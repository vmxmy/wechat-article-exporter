package processor

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoverResourcesCoversSupportedKindsAndNormalizesURLs(t *testing.T) {
	content := `<link rel="stylesheet" href="//cdn.example.test/article.css">
<style>.hero { background-image: url('//cdn.example.test/hero.jpg') }</style>
<img src="data:image/gif;base64,placeholder" data-src="//cdn.example.test/photo.jpg" alt="Photo">
<div style="background:url(https://cdn.example.test/pattern.png)"></div>
<audio src="//media.example.test/audio.mp3"></audio>
<video poster="//cdn.example.test/poster.jpg"><source src="//media.example.test/video.mp4"></video>`
	media := Media{
		CoverURL: "//cdn.example.test/cover.jpg",
		Images:   []Image{{URL: "//cdn.example.test/photo.jpg"}},
		Audio:    []Audio{{URL: "//media.example.test/audio.mp3"}},
		Videos:   []Video{{URL: "//media.example.test/other.mp4", CoverURL: "//cdn.example.test/poster.jpg"}},
	}

	resources, err := DiscoverResources(content, media, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := []Resource{
		{Kind: ResourceStylesheet, URL: "https://cdn.example.test/article.css"},
		{Kind: ResourceBackground, URL: "https://cdn.example.test/hero.jpg"},
		{Kind: ResourceImage, URL: "https://cdn.example.test/photo.jpg"},
		{Kind: ResourceBackground, URL: "https://cdn.example.test/pattern.png"},
		{Kind: ResourceAudio, URL: "https://media.example.test/audio.mp3"},
		{Kind: ResourceImage, URL: "https://cdn.example.test/poster.jpg"},
		{Kind: ResourceVideo, URL: "https://media.example.test/video.mp4"},
		{Kind: ResourceImage, URL: "https://cdn.example.test/cover.jpg"},
		{Kind: ResourceVideo, URL: "https://media.example.test/other.mp4"},
	}
	assertJSONEqual(t, resources, want)
}

func TestRenderNormalizesSafeHTMLAndPreservesSemantics(t *testing.T) {
	article := richArticle(t)
	comments := []Comment{{
		ID: "comment-1", Author: "Reader", AvatarURL: "https://cdn.example.test/reader.png", Location: "Shanghai",
		Content: "Useful <script>alert(1)</script>", CreatedAt: timePointer(t, "2026-07-22T01:02:03Z"), Likes: 2,
		Replies: []Reply{{ID: "reply-1", Author: "Author", Content: "Thanks", CreatedAt: timePointer(t, "2026-07-22T01:03:00Z")}},
	}}
	resourceMap := map[string]string{
		"https://cdn.example.test/article.css": "./assets/article.css",
		"https://cdn.example.test/hero.jpg":    "./assets/hero.jpg",
		"https://cdn.example.test/photo.jpg":   "./assets/photo.jpg",
		"https://cdn.example.test/audio.mp3":   "./assets/audio.mp3",
		"https://cdn.example.test/poster.jpg":  "./assets/poster.jpg",
		"https://cdn.example.test/video.mp4":   "./assets/video.mp4",
	}

	rendered, err := Render(article, RenderOptions{
		ResourceMap: resourceMap, ResourcePolicy: ResourceRewriteStrict,
		IncludeComments: true, Comments: comments,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, forbidden := range []string{"<script", "javascript:", "onclick=", "js_top_ad_area", "display:none", "tracking.example.test"} {
		if strings.Contains(strings.ToLower(rendered.HTML), forbidden) {
			t.Fatalf("normalized HTML contains %q:\n%s", forbidden, rendered.HTML)
		}
	}
	for _, required := range []string{
		`id="js_article"`, `id="js_content"`, `class="article-metadata"`, `./assets/article.css`,
		`./assets/hero.jpg`, `./assets/photo.jpg`, `./assets/audio.mp3`, `./assets/video.mp4`,
		"Reader", "Useful &lt;script&gt;alert(1)&lt;/script&gt;", "Author", "Thanks",
	} {
		if !strings.Contains(rendered.HTML, required) {
			t.Fatalf("normalized HTML missing %q:\n%s", required, rendered.HTML)
		}
	}
	if len(rendered.MissingResources) != 0 {
		t.Fatalf("missing resources = %#v", rendered.MissingResources)
	}

	markdownGolden := readGolden(t, "rich_article.md")
	if rendered.Markdown != markdownGolden {
		t.Fatalf("Markdown golden mismatch\n--- expected\n%s\n--- actual\n%s", markdownGolden, rendered.Markdown)
	}
	textGolden := readGolden(t, "rich_article.txt")
	if rendered.Text != textGolden {
		t.Fatalf("text golden mismatch\n--- expected\n%s\n--- actual\n%s", textGolden, rendered.Text)
	}
}

func TestRenderResourcePoliciesAreExplicit(t *testing.T) {
	article := Article{SchemaVersion: NormalizedArticleSchemaVersion, Title: "Missing", Account: Account{Nickname: "Fixture"}, Content: `<p><img src="//cdn.example.test/missing.jpg"></p>`}

	_, err := Render(article, RenderOptions{ResourcePolicy: ResourceRewriteStrict})
	var rewriteErr *ResourceRewriteError
	if err == nil || !errors.As(err, &rewriteErr) || len(rewriteErr.Missing) != 1 {
		t.Fatalf("strict error = %#v", err)
	}

	rendered, err := Render(article, RenderOptions{ResourcePolicy: ResourceRewriteBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	if len(rendered.MissingResources) != 1 || rendered.MissingResources[0].URL != "https://cdn.example.test/missing.jpg" {
		t.Fatalf("missing resources = %#v", rendered.MissingResources)
	}
	if !strings.Contains(rendered.HTML, `src="https://cdn.example.test/missing.jpg"`) {
		t.Fatalf("best-effort HTML = %s", rendered.HTML)
	}
}

func TestCommentPrivacyAppliesToHTMLTextMarkdownAndJSONFacingModel(t *testing.T) {
	article := Article{SchemaVersion: NormalizedArticleSchemaVersion, Title: "Private", Account: Account{Nickname: "Fixture"}, Content: "<p>Body</p>"}
	rendered, err := Render(article, RenderOptions{
		IncludeComments: true,
		Comments: []Comment{{
			ID: "private-comment", Author: "Alice", AvatarURL: "https://cdn.example.test/alice.png", Location: "Shanghai", OpenID: "openid-alice", Content: "Hello",
			Replies: []Reply{{ID: "private-reply", Author: "Bob", AvatarURL: "https://cdn.example.test/bob.png", Location: "Beijing", OpenID: "openid-bob", Content: "Hi"}},
		}},
		Privacy: CommentPrivacy{AnonymizeAuthors: true, HideAvatars: true, HideLocations: true, HideIdentifiers: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	combined := rendered.HTML + rendered.Text + rendered.Markdown
	for _, secret := range []string{"Alice", "Bob", "Shanghai", "Beijing", "openid-alice", "openid-bob", "private-comment", "private-reply", "alice.png", "bob.png"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("rendered representations leaked %q: %s", secret, combined)
		}
	}
	for _, pseudonym := range []string{"Reader 1", "Reader 2"} {
		if !strings.Contains(combined, pseudonym) {
			t.Fatalf("rendered representations missing %q: %s", pseudonym, combined)
		}
	}
	if len(rendered.Comments) != 1 || rendered.Comments[0].ID != "" || rendered.Comments[0].Author != "Reader 1" || rendered.Comments[0].AvatarURL != "" || rendered.Comments[0].Location != "" || rendered.Comments[0].OpenID != "" {
		t.Fatalf("JSON-facing comments = %#v", rendered.Comments)
	}
	if len(rendered.Comments[0].Replies) != 1 || rendered.Comments[0].Replies[0].Author != "Reader 2" || rendered.Comments[0].Replies[0].ID != "" {
		t.Fatalf("JSON-facing replies = %#v", rendered.Comments[0].Replies)
	}
}

func TestRenderLimitsResourcesAndOutput(t *testing.T) {
	article := Article{SchemaVersion: NormalizedArticleSchemaVersion, Title: "Limits", Account: Account{Nickname: "Fixture"}, Content: `<img src="https://example.test/1"><img src="https://example.test/2"><img src="https://example.test/3">`}
	_, err := Render(article, RenderOptions{Limits: Limits{MaxResources: 2}, ResourcePolicy: ResourceRewriteBestEffort})
	assertLimitError(t, err, ReasonResourceLimit)

	article.Content = `<p>` + strings.Repeat("output", 40) + `</p>`
	_, err = Render(article, RenderOptions{Limits: Limits{MaxOutputBytes: 64}})
	assertLimitError(t, err, ReasonOutputLimit)
}

func TestRenderRejectsDeepHTMLAndSanitizesMalformedUnsafeContent(t *testing.T) {
	deep := Article{
		SchemaVersion: NormalizedArticleSchemaVersion,
		Title:         "Deep",
		Account:       Account{Nickname: "Fixture"},
		Content:       strings.Repeat("<div>", 32) + "body" + strings.Repeat("</div>", 32),
	}
	_, err := Render(deep, RenderOptions{Limits: Limits{MaxHTMLDepth: 8}})
	assertLimitError(t, err, ReasonHTMLLimit)

	malformed := Article{
		SchemaVersion: NormalizedArticleSchemaVersion,
		Title:         "Malformed",
		Account:       Account{Nickname: "Fixture"},
		Content:       `<table><tr><td>safe<script>fetch('https://must-not-run.invalid')</script><a href="javascript:alert(1)" onclick="alert(2)">link`,
	}
	rendered, err := Render(malformed, RenderOptions{ResourcePolicy: ResourceRewriteBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(rendered.HTML)
	for _, forbidden := range []string{"<script", "must-not-run.invalid", "javascript:", "onclick="} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("malformed HTML retained %q: %s", forbidden, rendered.HTML)
		}
	}
	if !strings.Contains(rendered.Text, "safe") || !strings.Contains(rendered.Markdown, "link") {
		t.Fatalf("safe malformed content was lost: %#v", rendered)
	}
}

func TestPureDiscoveryAndRenderingNeverFetchResources(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	article := Article{
		SchemaVersion: NormalizedArticleSchemaVersion,
		Title:         "Offline",
		Account:       Account{Nickname: "Fixture"},
		Content:       `<link rel="stylesheet" href="` + server.URL + `/article.css"><img src="` + server.URL + `/image.jpg"><audio src="` + server.URL + `/audio.mp3"></audio><video src="` + server.URL + `/video.mp4"></video>`,
	}
	if _, err := DiscoverResources(article.Content, article.Media, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(article, RenderOptions{ResourcePolicy: ResourceRewriteBestEffort}); err != nil {
		t.Fatal(err)
	}
	if actual := requests.Load(); actual != 0 {
		t.Fatalf("pure processing made %d network requests", actual)
	}
}

func TestMessageTypeFixturesRender(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		messageType MessageType
		assert      func(*testing.T, *Article)
	}{
		{name: "standard", fixture: "types/standard.html", messageType: MessageTypeGraphic},
		{name: "text", fixture: "types/text.html", messageType: MessageTypeTextShare},
		{name: "image", fixture: "types/image.html", messageType: MessageTypeImageShare, assert: func(t *testing.T, article *Article) {
			if len(article.Media.Images) != 2 {
				t.Fatalf("images = %#v", article.Media.Images)
			}
		}},
		{name: "audio", fixture: "types/audio.html", messageType: MessageTypeAudio, assert: func(t *testing.T, article *Article) {
			if len(article.Media.Audio) != 1 || article.Media.Audio[0].DurationMS != 42000 {
				t.Fatalf("audio = %#v", article.Media.Audio)
			}
		}},
		{name: "video", fixture: "types/video.html", messageType: MessageTypeVideo, assert: func(t *testing.T, article *Article) {
			if len(article.Media.Videos) != 1 || article.Media.Videos[0].URL == "" {
				t.Fatalf("videos = %#v", article.Media.Videos)
			}
		}},
		{name: "paid", fixture: "types/paid.html", messageType: MessageTypeGraphic, assert: func(t *testing.T, article *Article) {
			if !article.Payment.Required || article.Payment.PreviewPercent != 25 {
				t.Fatalf("payment = %#v", article.Payment)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := processFixture(t, test.fixture)
			if result.Article == nil || result.Article.Message.Type != test.messageType {
				t.Fatalf("result = %#v", result)
			}
			if test.assert != nil {
				test.assert(t, result.Article)
			}
			rendered, err := Render(*result.Article, RenderOptions{ResourcePolicy: ResourceRewriteBestEffort})
			if err != nil || rendered.HTML == "" || rendered.Text == "" || rendered.Markdown == "" {
				t.Fatalf("rendered = %#v, err = %v", rendered, err)
			}
		})
	}

	unavailable := processFixture(t, "types/unavailable.html")
	if unavailable.Classification.State != ClassificationUnavailable || unavailable.Article != nil {
		t.Fatalf("unavailable result = %#v", unavailable)
	}
}

func TestSampleSemanticAndStructuralGoldenSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("sample corpus tests are skipped in short mode")
	}
	var cases []struct {
		Path        []string    `json:"path"`
		Title       string      `json:"title"`
		Account     string      `json:"account"`
		MessageType MessageType `json:"messageType"`
		Contains    string      `json:"contains"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "golden", "sample_semantics.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(strings.Join(test.Path, "/"), func(t *testing.T) {
			result := processSample(t, test.Path...)
			if result.Article == nil {
				t.Fatalf("article is nil: %#v", result.Classification)
			}
			if result.Article.Title != test.Title || result.Article.Account.Nickname != test.Account || result.Article.Message.Type != test.MessageType {
				t.Fatalf("semantic mismatch: %#v", result.Article)
			}
			rendered, err := Render(*result.Article, RenderOptions{ResourcePolicy: ResourceRewriteBestEffort})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(rendered.Text, test.Contains) {
				t.Fatalf("text missing approved semantic %q:\n%s", test.Contains, rendered.Text)
			}
			for _, forbidden := range []string{"<script", "javascript:", "onclick="} {
				if strings.Contains(strings.ToLower(rendered.HTML), forbidden) {
					t.Fatalf("HTML contains %q", forbidden)
				}
			}
			if !strings.Contains(rendered.HTML, `id="js_article"`) || !strings.Contains(rendered.HTML, `id="js_content"`) {
				t.Fatalf("HTML structure missing article roots")
			}
		})
	}
}

func richArticle(t *testing.T) Article {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sanitized", "rich-content.html"))
	if err != nil {
		t.Fatal(err)
	}
	return Article{
		SchemaVersion: NormalizedArticleSchemaVersion,
		Title:         "Offline fixture",
		Author:        "Fixture Author",
		Account:       Account{Nickname: "Fixture Account"},
		CanonicalURL:  "https://mp.weixin.qq.com/s/fixture",
		Content:       string(data),
		Timestamps:    Timestamps{PublishedAt: timePointer(t, "2026-01-01T00:00:00Z")},
		Media: Media{
			Audio:  []Audio{{Title: "Fixture audio", URL: "https://cdn.example.test/audio.mp3", DurationMS: 42000}},
			Videos: []Video{{Title: "Fixture video", URL: "https://cdn.example.test/video.mp4", CoverURL: "https://cdn.example.test/poster.jpg"}},
		},
	}
}

func timePointer(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return &parsed
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func assertJSONEqual(t *testing.T, actual, expected any) {
	t.Helper()
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf("actual = %s\nexpected = %s", actualJSON, expectedJSON)
	}
}

func assertLimitError(t *testing.T, err error, reason ReasonCode) {
	t.Helper()
	var typed *ProcessError
	if err == nil || !errors.As(err, &typed) || typed.Kind != ErrorLimit || typed.Reason != reason {
		t.Fatalf("error = %#v, want limit reason %q", err, reason)
	}
}
