package input

import (
	"strings"
	"testing"
)

func TestLoadAcceptsOneJSONObjectSource(t *testing.T) {
	got, err := Load(Options{Stdin: true}, strings.NewReader(`{"fakeid":"account-1"}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got["fakeid"] != "account-1" {
		t.Fatalf("Load() = %#v", got)
	}
	if _, err := Load(Options{Inline: `{}`, Stdin: true}, strings.NewReader(`{}`)); err == nil {
		t.Fatal("Load() accepted ambiguous input sources")
	}
	if _, err := ParseObject([]byte(`[]`), "test"); err == nil {
		t.Fatal("ParseObject() accepted an array")
	}
	if _, err := ParseObject([]byte(`{} {}`), "test"); err == nil {
		t.Fatal("ParseObject() accepted trailing JSON")
	}
	if _, err := Load(Options{InlineSet: true}, strings.NewReader("")); err == nil {
		t.Fatal("Load() accepted an explicitly empty --input")
	}
}
