package main

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRequireExactValues(t *testing.T) {
	expected := map[string]string{"empty": "", "present": "*", "numeric": "#", "fixed": "value"}
	valid := url.Values{"empty": {""}, "present": {"x"}, "numeric": {"123"}, "fixed": {"value"}}
	if err := requireExactValues(valid, expected); err != nil {
		t.Fatalf("valid values rejected: %v", err)
	}
	for name, values := range map[string]url.Values{
		"extra":      {"empty": {""}, "present": {"x"}, "numeric": {"123"}, "fixed": {"value"}, "extra": {"x"}},
		"repeated":   {"empty": {""}, "present": {"x", "y"}, "numeric": {"123"}, "fixed": {"value"}},
		"nonnumeric": {"empty": {""}, "present": {"x"}, "numeric": {"abc"}, "fixed": {"value"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requireExactValues(values, expected); err == nil {
				t.Fatal("invalid values were accepted")
			}
		})
	}
}

func TestRequireExactFormUsesPostBodyOnly(t *testing.T) {
	request := httptest.NewRequest("POST", "/?field=query", strings.NewReader("field=body"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := requireExactForm(request, map[string]string{"field": "body"}); err != nil {
		t.Fatalf("form rejected: %v", err)
	}
}

func TestRequireExactArticleListQueryRejectsDuplicatesAndUnknowns(t *testing.T) {
	valid := url.Values{
		"fakeid": {"fixture-fakeid"}, "token": {"fixture-token"}, "begin": {"0"}, "count": {"10"}, "sub": {"list"},
		"search_field": {"null"}, "query": {""}, "type": {"101_1"}, "free_publish_type": {"1"},
		"sub_action": {"list_ex"}, "lang": {"zh_CN"}, "f": {"json"}, "ajax": {"1"},
	}
	if err := requireExactArticleListQuery(valid); err != nil {
		t.Fatalf("valid article query rejected: %v", err)
	}
	duplicated := cloneValues(valid)
	duplicated["fakeid"] = []string{"fixture-fakeid", "wrong"}
	if err := requireExactArticleListQuery(duplicated); err == nil {
		t.Fatal("duplicate article query value was accepted")
	}
	unknown := cloneValues(valid)
	unknown.Set("extra", "value")
	if err := requireExactArticleListQuery(unknown); err == nil {
		t.Fatal("unknown article query field was accepted")
	}
}

func cloneValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for name, items := range values {
		clone[name] = append([]string(nil), items...)
	}
	return clone
}
