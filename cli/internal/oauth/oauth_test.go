package oauth

import "testing"

func TestNormalizeServerRejectsCredentialsWithoutEchoingThem(t *testing.T) {
	_, err := NormalizeServer("https://user:password@example.com/mcp")
	if err == nil {
		t.Fatal("NormalizeServer() accepted embedded credentials")
	}
	if got := err.Error(); got == "" || contains(got, "password@example") {
		t.Fatalf("NormalizeServer() error = %q", got)
	}
	server, err := NormalizeServer("https://example.com/mcp?secret=value#fragment")
	if err != nil {
		t.Fatal(err)
	}
	if server != "https://example.com" {
		t.Fatalf("NormalizeServer() = %q", server)
	}
	server, err = NormalizeServer("https://example.com/mcp/")
	if err != nil || server != "https://example.com" {
		t.Fatalf("NormalizeServer(/mcp/) = %q, %v", server, err)
	}
	if _, err := NormalizeServer("http://example.com"); err == nil {
		t.Fatal("NormalizeServer() accepted remote plaintext HTTP")
	}
}

func contains(value, target string) bool {
	for index := 0; index+len(target) <= len(value); index++ {
		if value[index:index+len(target)] == target {
			return true
		}
	}
	return false
}
