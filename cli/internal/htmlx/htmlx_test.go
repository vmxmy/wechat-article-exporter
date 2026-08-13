package htmlx

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.org/x/net/html/atom"
)

const samplePage = `<!doctype html>
<html><head><title>t</title></head><body>
<span class="rich_media_meta rich_media_meta_nickname">
  <a id="js_name" class="wx_tap_link">  示例公众号  </a>
</span>
<script>
var nickname = htmlDecode("示例&amp;公众号");
var s = "</scr" + "ipt> not a real close";
var wx = {};
wx.cgiData = { nick_name: "示例", head_img: "https://example.invalid/a.png" };
</script>
</body></html>`

func mustParse(t *testing.T, input string) *Document {
	t.Helper()
	document, err := Parse(strings.NewReader(input), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func TestParseBoundsInput(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxInputBytes = 16
	if _, err := Parse(strings.NewReader(samplePage), limits); !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("err = %v, want ErrInputTooLarge", err)
	}
}

func TestParseBoundsNodesAndDepth(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxHTMLNodes = 3
	if _, err := Parse(strings.NewReader(samplePage), limits); !errors.Is(err, ErrTooManyNodes) {
		t.Fatalf("err = %v, want ErrTooManyNodes", err)
	}
	limits = DefaultLimits()
	limits.MaxHTMLDepth = 2
	if _, err := Parse(strings.NewReader(samplePage), limits); !errors.Is(err, ErrTooDeep) {
		t.Fatalf("err = %v, want ErrTooDeep", err)
	}
}

func TestQueries(t *testing.T) {
	document := mustParse(t, samplePage)
	if got := Text(FindByID(document.Root, "js_name")); got != "示例公众号" {
		t.Fatalf("FindByID text = %q", got)
	}
	if got := Text(FindByClass(document.Root, "rich_media_meta_nickname")); got != "示例公众号" {
		t.Fatalf("FindByClass text = %q", got)
	}
	if node := FindByTag(document.Root, atom.Title); node == nil || Text(node) != "t" {
		t.Fatal("FindByTag(title) failed")
	}
	if got := Attr(FindByID(document.Root, "js_name"), "class"); got != "wx_tap_link" {
		t.Fatalf("Attr class = %q", got)
	}
	if FindByID(document.Root, "absent") != nil || FindByClass(document.Root, "absent") != nil {
		t.Fatal("absent lookups must return nil")
	}
}

// The escaped close tag inside a JS string must not terminate the script
// block: the tokenizer follows the spec's script states, which the retired
// byte scanner did not.
func TestScriptsAreByteFaithful(t *testing.T) {
	document := mustParse(t, samplePage)
	scripts, err := document.Scripts()
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 {
		t.Fatalf("scripts = %d, want 1", len(scripts))
	}
	block := scripts[0]
	if !bytes.Contains(block.Body, []byte(`not a real close`)) {
		t.Fatal("script body terminated early at escaped close tag")
	}
	if !bytes.Equal(document.Raw[block.Offset:block.Offset+len(block.Body)], block.Body) {
		t.Fatal("offset does not reconstruct the block from Raw")
	}
	if !bytes.Contains(block.Body, []byte("示例&amp;公众号")) {
		t.Fatal("entities inside script must stay raw")
	}
}

func TestScriptsRespectLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxScriptBytes = 8
	document, err := Parse(strings.NewReader(samplePage), limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.Scripts(); !errors.Is(err, ErrScriptTooLarge) {
		t.Fatalf("err = %v, want ErrScriptTooLarge", err)
	}
}

func TestFindBalancedObject(t *testing.T) {
	script := []byte(`wx.cgiData = { a: "x}", /* } */ b: { c: '}' } }; rest`)
	start := bytes.IndexByte(script, '{')
	end, err := FindBalancedObject(script, start, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(script[start:end]); got != `{ a: "x}", /* } */ b: { c: '}' } }` {
		t.Fatalf("object = %q", got)
	}
	if _, err := FindBalancedObject([]byte(`{ never closed`), 0, 0); err == nil {
		t.Fatal("unterminated object must error")
	}
	if _, err := FindBalancedObject(script, start, 4); !errors.Is(err, ErrScriptTooLarge) {
		t.Fatalf("err = %v, want ErrScriptTooLarge", err)
	}
}

func TestChainResolveSemantics(t *testing.T) {
	document := mustParse(t, samplePage)
	chain := Chain{
		ByID("primary", "absent"),
		ByID("secondary", "js_name"),
		ByScriptVar("script-var", `var\s+nickname\s*=\s*htmlDecode\("((?:[^"\\]|\\.)*)"\)`),
	}
	value, anchorName, matched := chain.Resolve(document)
	if value != "示例公众号" || anchorName != "secondary" || !matched {
		t.Fatalf("Resolve = %q, %q, %v", value, anchorName, matched)
	}

	scriptOnly := Chain{ByScriptVar("script-var", `var\s+nickname\s*=\s*htmlDecode\("((?:[^"\\]|\\.)*)"\)`)}
	value, anchorName, matched = scriptOnly.Resolve(document)
	if value != "示例&公众号" || anchorName != "script-var" || !matched {
		t.Fatalf("script anchor = %q, %q, %v (entity must decode)", value, anchorName, matched)
	}

	empty := mustParse(t, `<html><body><a id="js_name">   </a></body></html>`)
	value, anchorName, matched = Chain{ByID("primary", "js_name")}.Resolve(empty)
	if value != "" || anchorName != "" || !matched {
		t.Fatalf("empty anchor = %q, %q, %v — want matched=true with empty value", value, anchorName, matched)
	}

	none := mustParse(t, `<html><body>nothing</body></html>`)
	if _, _, matched := (Chain{ByID("primary", "js_name")}).Resolve(none); matched {
		t.Fatal("no anchor present must report matched=false")
	}
}
