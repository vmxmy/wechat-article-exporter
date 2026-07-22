package processor

import (
	"fmt"
	"strings"
)

type ResourceKind string

const (
	ResourceImage      ResourceKind = "image"
	ResourceStylesheet ResourceKind = "stylesheet"
	ResourceBackground ResourceKind = "background"
	ResourceAudio      ResourceKind = "audio"
	ResourceVideo      ResourceKind = "video"
)

type Resource struct {
	Kind ResourceKind `json:"kind"`
	URL  string       `json:"url"`
}

type ResourceRewritePolicy string

const (
	ResourceRewriteBestEffort ResourceRewritePolicy = "best_effort"
	ResourceRewriteStrict     ResourceRewritePolicy = "strict"
)

type ResourceRewriteError struct {
	Missing []Resource
}

func (err *ResourceRewriteError) Error() string {
	return fmt.Sprintf("processor resource rewrite: %d resources are missing an explicit local mapping", len(err.Missing))
}

func DiscoverResources(content string, media Media, limits Limits) ([]Resource, error) {
	limits = limits.withDefaults()
	root, err := parseHTMLFragment(content, limits)
	if err != nil {
		return nil, err
	}
	resources := make([]Resource, 0, 32)
	seen := make(map[string]struct{})
	add := func(kind ResourceKind, rawURL string) error {
		normalized := normalizeResourceURL(rawURL)
		if normalized == "" || isIgnoredResourceURL(normalized) {
			return nil
		}
		key := string(kind) + "\x00" + normalized
		if _, exists := seen[key]; exists {
			return nil
		}
		if len(resources) >= limits.MaxResources {
			return processError(ErrorLimit, ReasonResourceLimit, 0, "resources exceed %d", limits.MaxResources)
		}
		seen[key] = struct{}{}
		resources = append(resources, Resource{Kind: kind, URL: normalized})
		return nil
	}

	var walk func(*htmlNode) error
	walk = func(node *htmlNode) error {
		if node.typeID == htmlElementNode {
			switch node.tag {
			case "link":
				if relationIncludes(htmlAttribute(node, "rel"), "stylesheet") {
					if err := add(ResourceStylesheet, htmlAttribute(node, "href")); err != nil {
						return err
					}
				}
			case "style":
				for _, value := range cssURLs(htmlNodeText(node)) {
					if err := add(ResourceBackground, value); err != nil {
						return err
					}
				}
			case "img":
				if err := add(ResourceImage, preferredImageURL(node)); err != nil {
					return err
				}
				for _, value := range srcsetURLs(htmlAttribute(node, "srcset")) {
					if err := add(ResourceImage, value); err != nil {
						return err
					}
				}
			case "audio":
				if err := add(ResourceAudio, htmlAttribute(node, "src")); err != nil {
					return err
				}
			case "video":
				if err := add(ResourceImage, htmlAttribute(node, "poster")); err != nil {
					return err
				}
				if err := add(ResourceVideo, htmlAttribute(node, "src")); err != nil {
					return err
				}
			case "source":
				kind := ResourceVideo
				if hasAncestorTag(node, "audio") {
					kind = ResourceAudio
				}
				if err := add(kind, htmlAttribute(node, "src")); err != nil {
					return err
				}
			}
			for _, value := range cssURLs(htmlAttribute(node, "style")) {
				if err := add(ResourceBackground, value); err != nil {
					return err
				}
			}
		}
		for _, child := range node.children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}

	if err := add(ResourceImage, media.CoverURL); err != nil {
		return nil, err
	}
	for _, image := range media.Images {
		if err := add(ResourceImage, image.URL); err != nil {
			return nil, err
		}
	}
	for _, audio := range media.Audio {
		if err := add(ResourceAudio, audio.URL); err != nil {
			return nil, err
		}
	}
	for _, video := range media.Videos {
		if err := add(ResourceImage, video.CoverURL); err != nil {
			return nil, err
		}
		if err := add(ResourceVideo, video.URL); err != nil {
			return nil, err
		}
	}
	return resources, nil
}

func normalizeResourceURL(value string) string {
	return normalizeURL(strings.TrimSpace(value))
}

func isIgnoredResourceURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return lower == "" || strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "blob:") || strings.HasPrefix(lower, "about:") || strings.HasPrefix(lower, "#")
}

func relationIncludes(value, expected string) bool {
	for _, item := range strings.Fields(strings.ToLower(value)) {
		if item == expected {
			return true
		}
	}
	return false
}

func preferredImageURL(node *htmlNode) string {
	for _, attribute := range []string{"data-src", "data-original", "data-backsrc", "src"} {
		value := htmlAttribute(node, attribute)
		if normalizeResourceURL(value) != "" && !isTrackingImage(value) {
			return value
		}
	}
	return ""
}

func isTrackingImage(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "pixel") || strings.Contains(lower, "tracking") || strings.Contains(lower, "spacer.gif") || strings.Contains(lower, "pic_blank.gif")
}

func srcsetURLs(value string) []string {
	parts := strings.Split(value, ",")
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) > 0 {
			urls = append(urls, fields[0])
		}
	}
	return urls
}

func cssURLs(value string) []string {
	urls := make([]string, 0, 2)
	lower := strings.ToLower(value)
	position := 0
	for position < len(value) {
		relative := strings.Index(lower[position:], "url(")
		if relative < 0 {
			break
		}
		start := position + relative + len("url(")
		end := start
		var quote byte
		for end < len(value) {
			char := value[end]
			if quote != 0 {
				if char == quote {
					quote = 0
				}
				end++
				continue
			}
			if char == '\'' || char == '"' {
				quote = char
				end++
				continue
			}
			if char == ')' {
				break
			}
			end++
		}
		if end >= len(value) {
			break
		}
		candidate := strings.TrimSpace(value[start:end])
		candidate = strings.Trim(candidate, "\"'")
		urls = append(urls, candidate)
		position = end + 1
	}
	return urls
}

func hasAncestorTag(node *htmlNode, tag string) bool {
	for ancestor := node.parent; ancestor != nil; ancestor = ancestor.parent {
		if ancestor.tag == tag {
			return true
		}
	}
	return false
}
