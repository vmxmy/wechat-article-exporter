package processor

import (
	"context"
	"strings"
	"testing"
)

func FuzzProcessNeverExecutesOrPanics(f *testing.F) {
	for _, seed := range []string{
		`<html><body><div id="js_article"></div><script>window.cgiDataNew={title:'safe',user_name:'gh_safe',content_noencode:'<p>body</p>'}</script></body></html>`,
		`<html><script>alert(1)</script></html>`,
		`<div id="js_article"><img src="javascript:alert(1)"></div>`,
		strings.Repeat("<div>", 64) + strings.Repeat("</div>", 64),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		processor := New(Options{Limits: Limits{MaxInputBytes: 1 << 20, MaxPayloadBytes: 1 << 18, MaxDecodedPayloadBytes: 1 << 18}})
		_, _ = processor.Process(context.Background(), strings.NewReader(input))
	})
}

func FuzzRenderNeverEmitsExecutableContent(f *testing.F) {
	for _, seed := range []string{
		`<p>Hello</p>`,
		`<script>alert(1)</script><img src="javascript:alert(2)" onerror="alert(3)">`,
		`<blockquote><pre><code>safe</code></pre></blockquote>`,
		`<table><tr><td>x`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		article := Article{SchemaVersion: NormalizedArticleSchemaVersion, Title: "Fuzz", Account: Account{Nickname: "Fixture"}, Content: content}
		rendered, err := Render(article, RenderOptions{ResourcePolicy: ResourceRewriteBestEffort, Limits: Limits{MaxOutputBytes: 2 << 20}})
		if err != nil {
			return
		}
		lower := strings.ToLower(rendered.HTML)
		for _, forbidden := range []string{"<script", `href="javascript:`, `src="javascript:`, ` onerror="`, ` onclick="`} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("rendered HTML contains %q: %s", forbidden, rendered.HTML)
			}
		}
	})
}
