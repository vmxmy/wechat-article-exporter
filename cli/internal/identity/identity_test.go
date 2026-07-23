package identity

import "testing"

func TestStableUpstreamIDs(t *testing.T) {
	if got, want := AccountID("fixture-fakeid"), "account:89fa182cc63b5f1b0cf12975928046a9"; got != want {
		t.Fatalf("AccountID = %q, want %q", got, want)
	}
	if got, want := ArticleID("fixture-fakeid", "fixture-aid-1"), "article:efc3a405910aa8d4dd98bb2e095017b6"; got != want {
		t.Fatalf("ArticleID = %q, want %q", got, want)
	}
}

func TestArticleIDPreservesInputBytes(t *testing.T) {
	if ArticleID(" one ", "two") == ArticleID("one", "two") {
		t.Fatal("ArticleID unexpectedly normalized fakeID input")
	}
	if ArticleID("one", " two ") == ArticleID("one", "two") {
		t.Fatal("ArticleID unexpectedly normalized aid input")
	}
}
