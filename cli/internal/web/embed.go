package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// embeddedAssets is the exact, version-controlled output of webui's Vite
// build. all: is required because Vite writes its manifest below .vite/.
//
//go:embed all:assets
var embeddedAssets embed.FS

const (
	assetRoot         = "assets"
	assetManifestPath = assetRoot + "/.vite/manifest.json"
	assetIndexPath    = assetRoot + "/index.html"
)

var fingerprintedAssetPattern = regexp.MustCompile(`^assets/[^/\\]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

// AssetHandler serves the embedded workspace document and its fingerprinted
// resources. Authentication and common security headers stay with Server;
// callers should mount this handler only after those checks.
//
// Existing files are served literally. A path without an extension is a
// client-side route and receives the application shell. Missing files that
// look like assets are never rewritten to index.html.
func AssetHandler() http.Handler {
	if err := ValidateEmbeddedAssets(); err != nil {
		panic(fmt.Sprintf("invalid embedded browser assets: %v", err))
	}
	return http.HandlerFunc(serveEmbeddedAsset)
}

// ServeAssets writes a validated embedded asset response. The server owns
// listener, session, and security policy; this helper keeps the generated
// asset package independent from those presentation concerns.
func ServeAssets(writer http.ResponseWriter, request *http.Request) {
	AssetHandler().ServeHTTP(writer, request)
}

func serveEmbeddedAsset(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(writer, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/") {
		http.NotFound(writer, request)
		return
	}

	requested, valid := assetRequestPath(request.URL.Path)
	if !valid {
		http.NotFound(writer, request)
		return
	}
	if requested != "" {
		if file, err := embeddedAssets.Open(assetRoot + "/" + requested); err == nil {
			file.Close()
			serveEmbeddedFile(writer, request, requested)
			return
		}
	}
	if requested != "" && (strings.HasPrefix(requested, "assets/") || path.Ext(requested) != "") {
		http.NotFound(writer, request)
		return
	}

	serveEmbeddedFile(writer, request, "index.html")
}

func serveEmbeddedFile(writer http.ResponseWriter, request *http.Request, name string) {
	setAssetCacheControl(writer.Header(), name)
	file, err := embeddedAssetFS().Open(name)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	reader, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(writer, "embedded browser asset is not seekable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(writer, request, name, info.ModTime(), reader)
}

// setAssetCacheControl permits long-lived caching only for assets whose
// content hash is part of their validated filename. The application shell is
// intentionally not cached: it carries the current asset references and is
// used for every SPA fallback route.
func setAssetCacheControl(header http.Header, name string) {
	if fingerprintedAsset(name) {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
		header.Del("Pragma")
	}
}

func assetRequestPath(requestPath string) (string, bool) {
	if requestPath == "" || requestPath == "/" {
		return "", true
	}
	if !strings.HasPrefix(requestPath, "/") {
		return "", false
	}
	requested := strings.TrimPrefix(requestPath, "/")
	if requested == "" || !fs.ValidPath(requested) {
		return "", false
	}
	return requested, true
}

func embeddedAssetFS() fs.FS {
	assets, err := fs.Sub(embeddedAssets, assetRoot)
	if err != nil {
		panic(fmt.Sprintf("embedded browser assets are unavailable: %v", err))
	}
	return assets
}

type viteManifestEntry struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	Assets  []string `json:"assets"`
	IsEntry bool     `json:"isEntry"`
}

// ValidateEmbeddedAssets verifies that the checked-in Vite output is safe to
// embed and complete. It intentionally needs no Node.js, so release builds
// and downstream Go consumers can validate the bundled artifact alone.
func ValidateEmbeddedAssets() error {
	index, err := embeddedAssets.ReadFile(assetIndexPath)
	if err != nil {
		return fmt.Errorf("read embedded application shell: %w", err)
	}
	if len(index) == 0 {
		return errors.New("embedded application shell is empty")
	}
	if hasRemoteAssetReference(string(index)) {
		return errors.New("embedded application shell references an external resource")
	}

	rawManifest, err := embeddedAssets.ReadFile(assetManifestPath)
	if err != nil {
		return fmt.Errorf("read embedded Vite manifest: %w", err)
	}
	manifest := map[string]viteManifestEntry{}
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return fmt.Errorf("parse embedded Vite manifest: %w", err)
	}
	if len(manifest) == 0 {
		return errors.New("embedded Vite manifest has no entries")
	}

	entries := 0
	for name, entry := range manifest {
		if !entry.IsEntry {
			continue
		}
		entries++
		for _, asset := range append(append([]string{entry.File}, entry.CSS...), entry.Assets...) {
			if err := validateManifestAsset(name, asset); err != nil {
				return err
			}
		}
	}
	if entries == 0 {
		return errors.New("embedded Vite manifest has no entrypoint")
	}
	return nil
}

func hasRemoteAssetReference(index string) bool {
	for _, attribute := range []string{"src=\"", "src='", "href=\"", "href='"} {
		for _, reference := range strings.Split(index, attribute)[1:] {
			if strings.HasPrefix(strings.TrimSpace(reference), "http://") || strings.HasPrefix(strings.TrimSpace(reference), "https://") || strings.HasPrefix(strings.TrimSpace(reference), "//") {
				return true
			}
		}
	}
	return false
}

func validateManifestAsset(entryName, asset string) error {
	if !fs.ValidPath(asset) || !strings.HasPrefix(asset, "assets/") || !fingerprintedAsset(asset) {
		return fmt.Errorf("embedded Vite manifest entry %q references invalid asset %q", entryName, asset)
	}
	info, err := fs.Stat(embeddedAssets, assetRoot+"/"+asset)
	if err != nil || info.IsDir() {
		return fmt.Errorf("embedded Vite manifest entry %q references missing asset %q", entryName, asset)
	}
	return nil
}

func fingerprintedAsset(asset string) bool {
	return !strings.Contains(asset, "..") && fingerprintedAssetPattern.MatchString(asset)
}
