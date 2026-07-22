package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcessSanitizedGolden(t *testing.T) {
	result := processFixture(t, "valid_cgidatanew.html")
	if result.Classification.State != ClassificationValid {
		t.Fatalf("classification = %#v", result.Classification)
	}
	if result.PayloadVariant != PayloadCGIDataNew {
		t.Fatalf("payload variant = %q", result.PayloadVariant)
	}
	if result.Article == nil {
		t.Fatal("article is nil")
	}

	actual, err := json.MarshalIndent(result.Article, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	goldenPath := filepath.Join("testdata", "golden", "valid_cgidatanew.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, actual, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	expected = normalizeGoldenLineEndings(expected)
	if !bytes.Equal(actual, expected) {
		t.Fatalf("golden mismatch\n--- expected\n%s\n--- actual\n%s", expected, actual)
	}
}

func normalizeGoldenLineEndings(value []byte) []byte {
	return bytes.ReplaceAll(value, []byte("\r\n"), []byte("\n"))
}

func TestSupportedPayloadVariants(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		variant     PayloadVariant
		messageType MessageType
	}{
		{name: "cgiData", fixture: "valid_cgidata.html", variant: PayloadCGIData, messageType: MessageTypeTextShare},
		{name: "embedded JSON", fixture: "valid_embedded_json.html", variant: PayloadEmbeddedJSON, messageType: MessageTypeArticleShare},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := processFixture(t, test.fixture)
			if result.PayloadVariant != test.variant {
				t.Fatalf("variant = %q", result.PayloadVariant)
			}
			if result.Article == nil || result.Article.Message.Type != test.messageType {
				t.Fatalf("article = %#v", result.Article)
			}
		})
	}
}

func TestCGIDataNewPrecedesLegacyPayload(t *testing.T) {
	input := `<html><body><div id="js_article"></div><script>
window.cgiData={title:'legacy',user_name:'gh_legacy'};
window.cgiDataNew={title:'current',user_name:'gh_current'};
</script></body></html>`
	result, err := New().Process(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if result.PayloadVariant != PayloadCGIDataNew || result.Article == nil || result.Article.Title != "current" {
		t.Fatalf("result = %#v", result)
	}
}

func TestKnownPageClassification(t *testing.T) {
	tests := []struct {
		fixture string
		state   ClassificationState
		reason  ReasonCode
	}{
		{fixture: "deleted.html", state: ClassificationDeleted, reason: ReasonAuthorDeleted},
		{fixture: "unavailable.html", state: ClassificationUnavailable, reason: ReasonPolicyViolation},
		{fixture: "risk_control.html", state: ClassificationRiskControl, reason: ReasonSecurityVerification},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			result := processFixture(t, test.fixture)
			if result.Classification.State != test.state || result.Classification.Reason != test.reason {
				t.Fatalf("classification = %#v", result.Classification)
			}
			if result.Article != nil {
				t.Fatalf("known terminal page returned article: %#v", result.Article)
			}
		})
	}
}

func TestRiskControlReasonsAndValidBodyIsolation(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		reason ReasonCode
	}{
		{name: "verification", body: "请完成安全验证", reason: ReasonSecurityVerification},
		{name: "rate limit", body: "访问过于频繁，请稍后重试", reason: ReasonRateLimited},
		{name: "environment", body: "当前环境异常", reason: ReasonAbnormalEnvironment},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := `<html><body><div class="weui-msg">` + test.body + `</div></body></html>`
			result, err := New().Process(context.Background(), strings.NewReader(input))
			if err != nil || result.Classification.State != ClassificationRiskControl || result.Classification.Reason != test.reason {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}

	valid := `<html><body><div id="js_article">访问过于频繁</div><script>window.cgiDataNew={title:'quoted warning',user_name:'gh_safe'}</script></body></html>`
	result, err := New().Process(context.Background(), strings.NewReader(valid))
	if err != nil || result.Classification.State != ClassificationValid {
		t.Fatalf("valid article misclassified: result=%#v err=%v", result, err)
	}
}

func TestMalformedUnknownFailsWithoutPartialArticle(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		reason ReasonCode
	}{
		{name: "unsupported function", input: readFixture(t, "malformed_payload.html"), reason: ReasonUnsupportedPayload},
		{name: "missing payload", input: `<html><body><div id="js_article">unknown</div></body></html>`, reason: ReasonMissingPayload},
		{name: "missing root", input: `<html><body><script>window.cgiDataNew={title:'x',user_name:'gh_x'}</script></body></html>`, reason: ReasonMissingContentRoot},
		{name: "invalid article", input: `<html><body><div id="js_article"></div><script>window.cgiDataNew={title:'',user_name:'gh_x'}</script></body></html>`, reason: ReasonInvalidArticle},
		{name: "duplicate key", input: `<html><body><div id="js_article"></div><script>window.cgiDataNew={title:'x',title:'y',user_name:'gh_x'}</script></body></html>`, reason: ReasonMalformedPayload},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := New().Process(context.Background(), strings.NewReader(test.input))
			if err == nil {
				t.Fatal("expected error")
			}
			if result.Classification.State != ClassificationParseError || result.Classification.Reason != test.reason {
				t.Fatalf("classification = %#v, error = %v", result.Classification, err)
			}
			if result.Article != nil {
				t.Fatalf("partial article leaked: %#v", result.Article)
			}
		})
	}
}

func TestLimits(t *testing.T) {
	base := func(payload string) string {
		return `<html><body><div id="js_article"></div><script>window.cgiDataNew=` + payload + `;</script></body></html>`
	}
	tests := []struct {
		name   string
		input  string
		limits Limits
	}{
		{name: "input bytes", input: strings.Repeat("x", 256), limits: Limits{MaxInputBytes: 64}},
		{name: "script bytes", input: base(`{title:'x',user_name:'gh_x',padding:'` + strings.Repeat("x", 128) + `'}`), limits: Limits{MaxScriptBytes: 64}},
		{name: "payload bytes", input: base(`{title:'` + strings.Repeat("x", 128) + `',user_name:'gh_x'}`), limits: Limits{MaxPayloadBytes: 64}},
		{name: "decoded bytes", input: base(`{title:'` + strings.Repeat("x", 64) + `',user_name:'gh_x'}`), limits: Limits{MaxDecodedPayloadBytes: 32}},
		{name: "string bytes", input: base(`{title:'` + strings.Repeat("x", 64) + `',user_name:'gh_x'}`), limits: Limits{MaxStringBytes: 32}},
		{name: "nesting", input: base(`{title:'x',user_name:'gh_x',nested:{a:{b:{c:{d:1}}}}}`), limits: Limits{MaxDepth: 3}},
		{name: "members", input: base(`{title:'x',user_name:'gh_x',a:1,b:2}`), limits: Limits{MaxObjectMembers: 3}},
		{name: "items", input: base(`{title:'x',user_name:'gh_x',a:[1,2,3]}`), limits: Limits{MaxArrayItems: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := New(Options{Limits: test.limits}).Process(context.Background(), strings.NewReader(test.input))
			if err == nil {
				t.Fatal("expected limit error")
			}
			var typed *ProcessError
			if !errors.As(err, &typed) || typed.Kind != ErrorLimit {
				t.Fatalf("error = %#v", err)
			}
			if result.Classification.State != ClassificationParseError || result.Article != nil {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestEmbeddedJSONLimits(t *testing.T) {
	base := func(payload string) string {
		return `<html><body><div id="js_article"></div><script type="application/json" id="wechat-cgi-data">` + payload + `</script></body></html>`
	}
	tests := []struct {
		name   string
		input  string
		limits Limits
	}{
		{name: "decoded bytes", input: base(`{"title":"` + strings.Repeat("x", 64) + `","user_name":"gh_x"}`), limits: Limits{MaxDecodedPayloadBytes: 32}},
		{name: "string bytes", input: base(`{"title":"` + strings.Repeat("x", 64) + `","user_name":"gh_x"}`), limits: Limits{MaxStringBytes: 32}},
		{name: "nesting", input: base(`{"title":"x","user_name":"gh_x","a":{"b":{"c":{"d":1}}}}`), limits: Limits{MaxDepth: 3}},
		{name: "members", input: base(`{"title":"x","user_name":"gh_x","a":1,"b":2}`), limits: Limits{MaxObjectMembers: 3}},
		{name: "items", input: base(`{"title":"x","user_name":"gh_x","a":[1,2,3]}`), limits: Limits{MaxArrayItems: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := New(Options{Limits: test.limits}).Process(context.Background(), strings.NewReader(test.input))
			var typed *ProcessError
			if err == nil || !errors.As(err, &typed) || typed.Kind != ErrorLimit {
				t.Fatalf("error = %#v", err)
			}
			if result.Classification.State != ClassificationParseError || result.Article != nil {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestEmbeddedJSONRejectsTrailingMalformedData(t *testing.T) {
	input := `<html><body><div id="js_article"></div><script type="application/json" id="wechat-cgi-data">{"title":"safe","user_name":"gh_safe"} attack()</script></body></html>`
	result, err := New().Process(context.Background(), strings.NewReader(input))
	if err == nil || result.Classification.State != ClassificationParseError || result.Article != nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestParserLiteralSubset(t *testing.T) {
	payload := `{unicode:'\u4e2d\u6587 \uD83D\uDE00',hex:0x10,decimal:-1.5e2,quoted_key:'ok','dash-key':true,line:'a\
b',empty:'' * 1,not_number:'LINK_TYPE_MP_APPMSG' * 1}`
	value, err := parseObjectLiteral([]byte(payload), 0, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	object := objectValue(value)
	if object["unicode"] != "中文 😀" || stringValue(object["hex"]) != "16" || stringValue(object["decimal"]) != "-1.5e2" {
		t.Fatalf("object = %#v", object)
	}
	if stringValue(object["line"]) != "ab" || int64Value(object["empty"]) != 0 || stringValue(object["not_number"]) != "" {
		t.Fatalf("coercion object = %#v", object)
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := New().Process(ctx, strings.NewReader(readFixture(t, "valid_cgidatanew.html")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(result, Result{}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestMessageTypeMapping(t *testing.T) {
	expected := map[int64]MessageType{
		0: MessageTypeGraphic, 5: MessageTypeVideo, 6: MessageTypeMusic, 7: MessageTypeAudio,
		8: MessageTypeImageShare, 10: MessageTypeTextShare, 11: MessageTypeArticleShare, 17: MessageTypeShortPost,
		999: MessageTypeUnknown,
	}
	for code, messageType := range expected {
		if actual := normalizeMessageType(code); actual != messageType {
			t.Errorf("code %d: got %q, want %q", code, actual, messageType)
		}
	}
}

func TestRepresentativeSamples(t *testing.T) {
	if testing.Short() {
		t.Skip("sample corpus tests are skipped in short mode")
	}
	tests := []struct {
		path        []string
		title       string
		messageType MessageType
		account     string
		minImages   int
	}{
		{path: []string{"普通图文", "01.html"}, title: "AI编程害死我了，给我的抓包软件埋了一个大雷！", messageType: MessageTypeGraphic, account: "轩辕的编程宇宙", minImages: 1},
		{path: []string{"文本分享", "01.html"}, title: "AI编程带给程序员的危与机", messageType: MessageTypeTextShare, account: "轩辕的编程宇宙"},
		{path: []string{"图片分享", "01.html"}, title: "新能源车均价创6年新低，价格战「寒气」侵蚀供应链", messageType: MessageTypeImageShare, account: "36氪", minImages: 1},
		{path: []string{"文章分享", "01.html"}, title: "比小学生说烂梗更可怕的，是大人也说", messageType: MessageTypeGraphic, account: "36氪", minImages: 1},
	}
	for _, test := range tests {
		t.Run(test.path[0], func(t *testing.T) {
			result := processSample(t, test.path...)
			if result.Article == nil {
				t.Fatalf("article is nil: %#v", result.Classification)
			}
			article := result.Article
			if article.Title != test.title || article.Message.Type != test.messageType || article.Account.Nickname != test.account {
				t.Fatalf("unexpected semantic result: title=%q type=%q account=%q", article.Title, article.Message.Type, article.Account.Nickname)
			}
			if article.SchemaVersion != NormalizedArticleSchemaVersion || article.Timestamps.PublishedAt == nil || article.Comments.ID == "" {
				t.Fatalf("missing normalized metadata: %#v", article)
			}
			if len(article.Media.Images) < test.minImages {
				t.Fatalf("images = %d, want >= %d", len(article.Media.Images), test.minImages)
			}
		})
	}
}

func TestSampleEngagementAndAudio(t *testing.T) {
	if testing.Short() {
		t.Skip("sample corpus tests are skipped in short mode")
	}
	engagement := processSample(t, "普通图文", "c02.html")
	if engagement.Article == nil {
		t.Fatal("engagement sample article is nil")
	}
	assertInt64Pointer(t, "likes", engagement.Article.Engagement.Likes, 112)
	assertInt64Pointer(t, "old likes", engagement.Article.Engagement.OldLikes, 201)
	assertInt64Pointer(t, "shares", engagement.Article.Engagement.Shares, 335)
	assertInt64Pointer(t, "collections", engagement.Article.Engagement.Collections, 369)
	assertInt64Pointer(t, "comments", engagement.Article.Engagement.Comments, 89)

	audio := processSample(t, "普通图文", "c01.html")
	if audio.Article == nil || len(audio.Article.Media.Audio) != 1 {
		t.Fatalf("audio = %#v", audio.Article)
	}
	if audio.Article.Media.Audio[0].ID != "MzIzNDcyMTI5MV8yMjQ3NjY0NTQ1" || audio.Article.Media.Audio[0].ListenID != "222483580371378689" {
		t.Fatalf("audio metadata = %#v", audio.Article.Media.Audio[0])
	}
}

func TestRepresentativeUnavailableSamples(t *testing.T) {
	if testing.Short() {
		t.Skip("sample corpus tests are skipped in short mode")
	}
	tests := []struct {
		path   []string
		state  ClassificationState
		reason ReasonCode
	}{
		{path: []string{"作者已删除", "01.html"}, state: ClassificationDeleted, reason: ReasonAuthorDeleted},
		{path: []string{"内容违规", "01.html"}, state: ClassificationUnavailable, reason: ReasonPolicyViolation},
		{path: []string{"该内容暂时无法查看", "01.html"}, state: ClassificationUnavailable, reason: ReasonTemporarilyUnavailable},
	}
	for _, test := range tests {
		t.Run(test.path[0], func(t *testing.T) {
			result := processSample(t, test.path...)
			if result.Classification.State != test.state || result.Classification.Reason != test.reason || result.Article != nil {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func processFixture(t *testing.T, name string) Result {
	t.Helper()
	result, err := New().Process(context.Background(), strings.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatalf("process fixture %s: %v", name, err)
	}
	return result
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func processSample(t *testing.T, parts ...string) Result {
	t.Helper()
	path := samplePath(t, parts...)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := New().Process(context.Background(), file)
	if err != nil {
		t.Fatalf("process sample %s: %v", path, err)
	}
	return result
}

func samplePath(t *testing.T, parts ...string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate processor tests")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	path := append([]string{root, "samples"}, parts...)
	return filepath.Join(path...)
}

func TestNoExecutionSentinel(t *testing.T) {
	input := fmt.Sprintf(`<html><body><div id="js_article"></div><script>globalThis.mustNotRun=%d; window.cgiDataNew={title:'safe',user_name:'gh_safe'}; throw new Error('must not run')</script></body></html>`, time.Now().UnixNano())
	result, err := New().Process(context.Background(), strings.NewReader(input))
	if err != nil || result.Article == nil || result.Article.Title != "safe" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func assertInt64Pointer(t *testing.T, name string, actual *int64, expected int64) {
	t.Helper()
	if actual == nil || *actual != expected {
		t.Fatalf("%s = %v, want %d", name, actual, expected)
	}
}
