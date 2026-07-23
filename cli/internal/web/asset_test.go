package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedAssetsAreCompleteAndLocal(t *testing.T) {
	if err := ValidateEmbeddedAssets(); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintedAsset(t *testing.T) {
	for _, test := range []struct {
		name  string
		asset string
		want  bool
	}{
		{name: "URL-safe Vite hash", asset: "assets/index-Dlmg_X-D.js", want: true},
		{name: "eight-character hash", asset: "assets/index-abcd_123.js", want: true},
		{name: "short hash", asset: "assets/index-abc_123.js", want: false},
		{name: "unsafe hash character", asset: "assets/index-abcd.123.js", want: false},
		{name: "nested asset", asset: "assets/chunks/index-Dlmg_X-D.js", want: false},
		{name: "parent-like filename", asset: "assets/..index-Dlmg_X-D.js", want: false},
		{name: "backslash path", asset: "assets\\index-Dlmg_X-D.js", want: false},
		{name: "unsafe extension", asset: "assets/index-Dlmg_X-D.j-s", want: false},
		{name: "outside asset directory", asset: "index-Dlmg_X-D.js", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := fingerprintedAsset(test.asset); got != test.want {
				t.Errorf("fingerprintedAsset(%q) = %t; want %t", test.asset, got, test.want)
			}
		})
	}
}

func TestAssetHandlerServesAssetsAndSPAFallback(t *testing.T) {
	handler := AssetHandler()
	manifest := mustEmbeddedManifest(t)
	for target, want := range map[string]struct {
		contentType  string
		cacheControl string
	}{
		"/": {
			contentType:  "text/html",
			cacheControl: "",
		},
		"/articles": {
			contentType:  "text/html",
			cacheControl: "",
		},
		"/" + manifest["index.html"].File: {
			contentType:  "text/javascript",
			cacheControl: "public, max-age=31536000, immutable",
		},
		"/" + manifest["index.html"].CSS[0]: {
			contentType:  "text/css",
			cacheControl: "public, max-age=31536000, immutable",
		},
	} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d; want 200", target, response.Code)
			}
			if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, want.contentType) {
				t.Fatalf("GET %s Content-Type = %q; want prefix %q", target, got, want.contentType)
			}
			if got := response.Header().Get("Cache-Control"); got != want.cacheControl {
				t.Fatalf("GET %s Cache-Control = %q; want %q", target, got, want.cacheControl)
			}
		})
	}
}

func TestAssetHandlerServesTheActualEmbeddedEntrypoint(t *testing.T) {
	manifest := mustEmbeddedManifest(t)
	entrypoint := manifest["index.html"]

	shell := httptest.NewRecorder()
	AssetHandler().ServeHTTP(shell, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := shell.Body.String(); !strings.Contains(got, "/"+entrypoint.File) || !strings.Contains(got, "/"+entrypoint.CSS[0]) {
		t.Fatalf("embedded shell does not reference manifest entrypoint %q and CSS %q", entrypoint.File, entrypoint.CSS[0])
	}

	asset := httptest.NewRecorder()
	AssetHandler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/"+entrypoint.File, nil))
	if asset.Code != http.StatusOK || asset.Body.Len() == 0 {
		t.Fatalf("embedded entrypoint status=%d bytes=%d; want a non-empty 200", asset.Code, asset.Body.Len())
	}
}

func mustEmbeddedManifest(t *testing.T) map[string]viteManifestEntry {
	t.Helper()
	raw, err := embeddedAssets.ReadFile(assetManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]viteManifestEntry{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	entry, ok := manifest["index.html"]
	if !ok || entry.File == "" || len(entry.CSS) == 0 {
		t.Fatalf("entrypoint manifest = %#v", entry)
	}
	return manifest
}

func TestAssetHandlerDoesNotRewriteMissingAssetsOrAPIRequests(t *testing.T) {
	handler := AssetHandler()
	for _, target := range []string{"/assets/missing.js", "/missing.css", "/api/v1/missing", "/../index.html"} {
		t.Run(target, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d; want 404", target, response.Code)
			}
		})
	}
}

func TestAssetHandlerAcceptsHEADOnly(t *testing.T) {
	response := httptest.NewRecorder()
	AssetHandler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ignored")))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status = %d; want 405", response.Code)
	}
	if got := response.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q", got)
	}

	response = httptest.NewRecorder()
	AssetHandler().ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("HEAD / status = %d; want 200", response.Code)
	}
	if body, err := io.ReadAll(response.Result().Body); err != nil || len(body) != 0 {
		t.Fatalf("HEAD / body = %q, error = %v; want empty", body, err)
	}
}
