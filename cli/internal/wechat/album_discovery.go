package wechat

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/htmlx"
)

// AlbumRef is one album an article page advertises. It carries only what the
// page states about the album itself; enumerating its articles is a separate
// zero-credential call to ListAlbumArticles.
type AlbumRef struct {
	AlbumID      string `json:"albumId"`
	Title        string `json:"title,omitempty"`
	ArticleCount int    `json:"articleCount,omitempty"`
}

// ArticleAlbums is the album membership a single public article page exposes.
// It is the entry point of the zero-credential discovery path: one article URL
// yields the albums it belongs to, each of which enumerates further articles.
type ArticleAlbums struct {
	FakeID string     `json:"fakeid"`
	Albums []AlbumRef `json:"albums"`
}

// albumAnchor mirrors htmlx.Anchor for a multi-valued extraction: article pages
// state album membership as a list, and taking only the first album would
// silently drop memberships the page plainly lists. The name is the
// observability identity, exactly as in htmlx.Chain.
type albumAnchor struct {
	name    string
	extract func(raw []byte) ([]AlbumRef, bool)
}

var (
	// Album membership is stated in script bootstrap objects, never in the
	// tree, so these read the raw document the way htmlx script anchors do.
	// Tried newest layout first: the tags array is the richest and the only
	// shape that lists several albums; appmsgalbuminfo covers the single
	// primary album; the bare identifier is the last resort when the
	// surrounding object is restructured and only the field name survives.
	albumTagsPattern = regexp.MustCompile(
		`tag_name:\s*'((?:[^'\\]|\\.)*)'[\s\S]{0,400}?tag_content_num:\s*'(\d+)'[\s\S]{0,200}?album_id:\s*'(\d{6,})'`)
	albumInfoPattern = regexp.MustCompile(
		`appmsgalbuminfo:\s*\{[\s\S]{0,200}?album_id:\s*'(\d{6,})'[\s\S]{0,200}?title:\s*'((?:[^'\\]|\\.)*)'[\s\S]{0,300}?content_size:\s*'(\d+)'`)
	albumBarePattern = regexp.MustCompile(`album_id(?:_str)?:\s*'(\d{6,})'`)

	articleAlbumChain = []albumAnchor{
		{name: "album-tags", extract: func(raw []byte) ([]AlbumRef, bool) {
			return albumRefsFrom(albumTagsPattern.FindAllSubmatch(raw, -1), 3, 1, 2)
		}},
		{name: "appmsgalbuminfo", extract: func(raw []byte) ([]AlbumRef, bool) {
			return albumRefsFrom(albumInfoPattern.FindAllSubmatch(raw, -1), 1, 2, 3)
		}},
		{name: "album-id-bare", extract: func(raw []byte) ([]AlbumRef, bool) {
			return albumRefsFrom(albumBarePattern.FindAllSubmatch(raw, -1), 1, 0, 0)
		}},
	}

	// A page states its account as a bootstrap variable; the album permalink
	// carries the same value and survives if that variable is renamed.
	articleAccountIDChain = htmlx.Chain{
		htmlx.ByScriptVarRaw("biz-var", `var\s+biz\s*=\s*"([A-Za-z0-9+/=_-]+)"`),
		htmlx.ByScriptVarRaw("appmsgalbum-link-biz", `appmsgalbum\?__biz=([A-Za-z0-9+/=_-]+)`),
	}
)

// albumRefsFrom maps submatches to refs. Group indexes differ per anchor
// because each layout orders the fields differently; 0 means the layout does
// not state that field.
func albumRefsFrom(matches [][][]byte, idIndex, titleIndex, countIndex int) ([]AlbumRef, bool) {
	if len(matches) == 0 {
		return nil, false
	}
	refs := make([]AlbumRef, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if idIndex >= len(match) {
			continue
		}
		id := strings.TrimSpace(string(match[idIndex]))
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ref := AlbumRef{AlbumID: id}
		if titleIndex > 0 && titleIndex < len(match) {
			ref.Title = strings.TrimSpace(unescapeScriptString(string(match[titleIndex])))
		}
		if countIndex > 0 && countIndex < len(match) {
			ref.ArticleCount, _ = strconv.Atoi(strings.TrimSpace(string(match[countIndex])))
		}
		refs = append(refs, ref)
	}
	// matched stays true even with no usable refs: the layout was recognised,
	// which is what separates "this article is in no album" from drift.
	return refs, true
}

func unescapeScriptString(value string) string {
	return strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`).Replace(value)
}

// ResolveArticleAlbums reads album membership off one public article page. It
// needs no management session and no personal credential: the page and the
// album enumeration it points at are both public surfaces.
func (client *Client) ResolveArticleAlbums(ctx context.Context, rawURL string) (ArticleAlbums, error) {
	document, err := client.fetchArticleDocument(ctx, rawURL)
	if err != nil {
		return ArticleAlbums{}, err
	}
	fakeID, anchorName, matched := articleAccountIDChain.Resolve(document)
	if fakeID == "" {
		if matched {
			return ArticleAlbums{}, fmt.Errorf("%w: article account id was empty", ErrDiscoveryProtocol)
		}
		return ArticleAlbums{}, fmt.Errorf("%w: article did not expose an account id", ErrDiscoveryProtocol)
	}
	client.observeAnchor(anchorSurfaceArticleAccountID, anchorName)
	for _, anchor := range articleAlbumChain {
		refs, anchorMatched := anchor.extract(document.Raw)
		if !anchorMatched {
			continue
		}
		client.observeAnchor(anchorSurfaceArticleAlbums, anchor.name)
		return ArticleAlbums{FakeID: fakeID, Albums: refs}, nil
	}
	// No anchor matched. Unlike a value chain this cannot separate "the
	// article belongs to no album" from "the album markup moved": both look
	// like an absent field. Drift shows up instead as the album-tags anchor
	// going quiet while a fallback keeps hitting, which is why every anchor
	// reports its own name.
	return ArticleAlbums{FakeID: fakeID}, nil
}
