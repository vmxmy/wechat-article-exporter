package processor

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
)

type Comment struct {
	ID        string     `json:"id,omitempty"`
	Author    string     `json:"author"`
	AvatarURL string     `json:"avatarUrl,omitempty"`
	Location  string     `json:"location,omitempty"`
	OpenID    string     `json:"openId,omitempty"`
	Content   string     `json:"content"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	Likes     int64      `json:"likes,omitempty"`
	Replies   []Reply    `json:"replies,omitempty"`
}

type Reply struct {
	ID        string     `json:"id,omitempty"`
	Author    string     `json:"author"`
	AvatarURL string     `json:"avatarUrl,omitempty"`
	Location  string     `json:"location,omitempty"`
	OpenID    string     `json:"openId,omitempty"`
	Content   string     `json:"content"`
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	Likes     int64      `json:"likes,omitempty"`
}

type CommentPrivacy struct {
	AnonymizeAuthors bool `json:"anonymizeAuthors,omitempty"`
	HideAvatars      bool `json:"hideAvatars,omitempty"`
	HideLocations    bool `json:"hideLocations,omitempty"`
	HideIdentifiers  bool `json:"hideIdentifiers,omitempty"`
}

type RenderOptions struct {
	Limits          Limits
	ResourceMap     map[string]string
	ResourcePolicy  ResourceRewritePolicy
	IncludeComments bool
	Comments        []Comment
	Privacy         CommentPrivacy
}

type RenderedArticle struct {
	SchemaVersion    string     `json:"schemaVersion"`
	Article          Article    `json:"article"`
	HTML             string     `json:"html"`
	Text             string     `json:"text"`
	Markdown         string     `json:"markdown"`
	Resources        []Resource `json:"resources,omitempty"`
	MissingResources []Resource `json:"missingResources,omitempty"`
	Comments         []Comment  `json:"comments,omitempty"`
}

func Render(article Article, options RenderOptions) (RenderedArticle, error) {
	limits := options.Limits.withDefaults()
	resources, err := DiscoverResources(article.Content, article.Media, limits)
	if err != nil {
		return RenderedArticle{}, err
	}
	root, err := parseHTMLFragment(article.Content, limits)
	if err != nil {
		return RenderedArticle{}, err
	}

	rewriter := newResourceRewriter(options.ResourceMap, options.ResourcePolicy)
	sanitized := sanitizeHTMLTree(root, rewriter)
	appendMissingMedia(sanitized, article.Media, rewriter)
	comments := []Comment(nil)
	if options.IncludeComments {
		comments = applyCommentPrivacy(options.Comments, options.Privacy)
	}
	if options.ResourcePolicy == ResourceRewriteStrict && len(rewriter.missing) > 0 {
		return RenderedArticle{}, &ResourceRewriteError{Missing: append([]Resource(nil), rewriter.missing...)}
	}

	htmlOutput := renderHTMLDocument(article, sanitized, comments)
	textOutput := renderTextDocument(article, sanitized, comments)
	markdownOutput := renderMarkdownDocument(article, sanitized, comments)
	for _, output := range []string{htmlOutput, textOutput, markdownOutput} {
		if len(output) > limits.MaxOutputBytes {
			return RenderedArticle{}, processError(ErrorLimit, ReasonOutputLimit, len(output), "rendered output exceeds %d bytes", limits.MaxOutputBytes)
		}
	}

	return RenderedArticle{
		SchemaVersion:    NormalizedArticleSchemaVersion,
		Article:          article,
		HTML:             htmlOutput,
		Text:             textOutput,
		Markdown:         markdownOutput,
		Resources:        resources,
		MissingResources: append([]Resource(nil), rewriter.missing...),
		Comments:         comments,
	}, nil
}

type resourceRewriter struct {
	mapping map[string]string
	policy  ResourceRewritePolicy
	missing []Resource
	seen    map[string]struct{}
}

func newResourceRewriter(mapping map[string]string, policy ResourceRewritePolicy) *resourceRewriter {
	normalized := make(map[string]string, len(mapping))
	for source, destination := range mapping {
		key := normalizeResourceURL(source)
		if key == "" {
			key = strings.TrimSpace(source)
		}
		normalized[key] = strings.TrimSpace(destination)
	}
	if policy == "" {
		policy = ResourceRewriteBestEffort
	}
	return &resourceRewriter{mapping: normalized, policy: policy, seen: make(map[string]struct{})}
}

func (rewriter *resourceRewriter) rewrite(kind ResourceKind, value string) string {
	normalized := normalizeResourceURL(value)
	if normalized == "" || isIgnoredResourceURL(normalized) {
		return ""
	}
	if destination := rewriter.mapping[normalized]; safeLocalResourceReference(destination) {
		return destination
	}
	key := string(kind) + "\x00" + normalized
	if _, exists := rewriter.seen[key]; !exists {
		rewriter.seen[key] = struct{}{}
		rewriter.missing = append(rewriter.missing, Resource{Kind: kind, URL: normalized})
	}
	return normalized
}

func safeLocalResourceReference(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return !strings.HasPrefix(lower, "javascript:") && !strings.HasPrefix(lower, "vbscript:") && !strings.HasPrefix(lower, "data:text/html")
}

func sanitizeHTMLTree(root *htmlNode, rewriter *resourceRewriter) *htmlNode {
	sanitizedRoot := &htmlNode{typeID: htmlDocumentNode}
	var sanitize func(*htmlNode, *htmlNode)
	sanitize = func(source, destination *htmlNode) {
		for _, child := range source.children {
			switch child.typeID {
			case htmlTextNode:
				appendHTMLChild(destination, &htmlNode{typeID: htmlTextNode, text: child.text})
			case htmlElementNode:
				if forbiddenHTMLNode(child) {
					continue
				}
				tag := child.tag
				if !allowedHTMLTag(tag) {
					sanitize(child, destination)
					continue
				}
				node := &htmlNode{typeID: htmlElementNode, tag: tag, attrs: sanitizeHTMLAttributes(child, rewriter)}
				appendHTMLChild(destination, node)
				if tag == "img" {
					if node.attrs["src"] == "" {
						destination.children = destination.children[:len(destination.children)-1]
					}
					continue
				}
				if tag == "style" {
					style := rewriteCSS(htmlNodeText(child), rewriter)
					if strings.TrimSpace(style) != "" {
						appendHTMLChild(node, &htmlNode{typeID: htmlTextNode, text: style})
					}
					continue
				}
				sanitize(child, node)
			}
		}
	}
	sanitize(root, sanitizedRoot)
	return sanitizedRoot
}

func forbiddenHTMLNode(node *htmlNode) bool {
	switch node.tag {
	case "script", "noscript", "template", "iframe", "frame", "frameset", "object", "embed", "form", "input", "button", "textarea", "select", "option", "canvas":
		return true
	}
	identity := strings.ToLower(htmlAttribute(node, "id") + " " + htmlAttribute(node, "class"))
	for _, marker := range []string{
		"js_top_ad_area", "js_bottom_ad_area", "advertisement", "advertise", "qr_code", "qrcode",
		"js_pc_qr", "appmsg_action_area", "reward_area", "js_reward", "tracking", "js_toobar", "js_toolbar",
		"weui-dialog", "rich_media_tool", "related_article", "recommend", "js_share_appmsg",
	} {
		if strings.Contains(identity, marker) {
			return true
		}
	}
	return node.tag == "img" && isTrackingImage(preferredImageURL(node))
}

func allowedHTMLTag(tag string) bool {
	switch tag {
	case "a", "abbr", "article", "aside", "audio", "b", "blockquote", "br", "caption", "cite", "code", "col", "colgroup", "dd", "del", "details", "div", "dl", "dt", "em", "figcaption", "figure", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "i", "img", "ins", "kbd", "li", "link", "main", "mark", "nav", "ol", "p", "pre", "q", "s", "samp", "section", "small", "source", "span", "strong", "style", "sub", "summary", "sup", "table", "tbody", "td", "tfoot", "th", "thead", "time", "tr", "u", "ul", "var", "video", "wbr":
		return true
	default:
		return false
	}
}

func sanitizeHTMLAttributes(node *htmlNode, rewriter *resourceRewriter) map[string]string {
	attrs := make(map[string]string)
	for name, value := range node.attrs {
		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, "on") || lowerName == "nonce" || lowerName == "integrity" || lowerName == "crossorigin" || lowerName == "referrerpolicy" || lowerName == "srcdoc" || lowerName == "hidden" {
			continue
		}
		switch lowerName {
		case "id", "class", "title", "alt", "width", "height", "colspan", "rowspan", "scope", "open", "controls", "loop", "muted", "autoplay", "preload", "datetime", "lang", "dir", "role", "aria-label":
			attrs[lowerName] = value
		case "style":
			if style := sanitizeInlineStyle(value, rewriter); style != "" {
				attrs["style"] = style
			}
		}
	}
	switch node.tag {
	case "a":
		if href := safeHyperlink(htmlAttribute(node, "href")); href != "" {
			attrs["href"] = href
		}
	case "link":
		if relationIncludes(htmlAttribute(node, "rel"), "stylesheet") {
			attrs["rel"] = "stylesheet"
			if href := rewriter.rewrite(ResourceStylesheet, htmlAttribute(node, "href")); href != "" {
				attrs["href"] = href
			}
		}
	case "img":
		if source := rewriter.rewrite(ResourceImage, preferredImageURL(node)); source != "" {
			attrs["src"] = source
		}
		delete(attrs, "data-src")
	case "audio":
		if source := rewriter.rewrite(ResourceAudio, htmlAttribute(node, "src")); source != "" {
			attrs["src"] = source
		}
		attrs["controls"] = ""
	case "video":
		if source := rewriter.rewrite(ResourceVideo, htmlAttribute(node, "src")); source != "" {
			attrs["src"] = source
		}
		if poster := rewriter.rewrite(ResourceImage, htmlAttribute(node, "poster")); poster != "" {
			attrs["poster"] = poster
		}
		attrs["controls"] = ""
	case "source":
		kind := ResourceVideo
		if hasAncestorTag(node, "audio") {
			kind = ResourceAudio
		}
		if source := rewriter.rewrite(kind, htmlAttribute(node, "src")); source != "" {
			attrs["src"] = source
		}
		if mediaType := htmlAttribute(node, "type"); mediaType != "" {
			attrs["type"] = mediaType
		}
	}
	return attrs
}

func safeHyperlink(value string) string {
	value = strings.TrimSpace(html.UnescapeString(value))
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "vbscript:") || strings.HasPrefix(lower, "data:") {
		return ""
	}
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	return value
}

func sanitizeInlineStyle(value string, rewriter *resourceRewriter) string {
	declarations := strings.Split(value, ";")
	kept := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		parts := strings.SplitN(declaration, ":", 2)
		if len(parts) != 2 {
			continue
		}
		property := strings.ToLower(strings.TrimSpace(parts[0]))
		propertyValue := strings.TrimSpace(parts[1])
		lowerValue := strings.ToLower(propertyValue)
		if property == "display" && lowerValue == "none" || property == "visibility" && lowerValue == "hidden" || property == "opacity" && strings.TrimSpace(lowerValue) == "0" || strings.Contains(lowerValue, "expression(") || strings.Contains(lowerValue, "javascript:") {
			continue
		}
		if strings.Contains(lowerValue, "url(") {
			propertyValue = rewriteCSS(propertyValue, rewriter)
		}
		if property != "" && propertyValue != "" {
			kept = append(kept, property+":"+propertyValue)
		}
	}
	return strings.Join(kept, ";")
}

func rewriteCSS(value string, rewriter *resourceRewriter) string {
	var builder strings.Builder
	lower := strings.ToLower(value)
	position := 0
	for position < len(value) {
		relative := strings.Index(lower[position:], "url(")
		if relative < 0 {
			builder.WriteString(value[position:])
			break
		}
		start := position + relative
		builder.WriteString(value[position:start])
		urlStart := start + len("url(")
		urlEnd := urlStart
		var quote byte
		for urlEnd < len(value) {
			char := value[urlEnd]
			if quote != 0 {
				if char == quote {
					quote = 0
				}
				urlEnd++
				continue
			}
			if char == '\'' || char == '"' {
				quote = char
				urlEnd++
				continue
			}
			if char == ')' {
				break
			}
			urlEnd++
		}
		if urlEnd >= len(value) {
			builder.WriteString(value[start:])
			break
		}
		candidate := strings.Trim(strings.TrimSpace(value[urlStart:urlEnd]), "\"'")
		rewritten := rewriter.rewrite(ResourceBackground, candidate)
		if rewritten != "" {
			builder.WriteString("url('")
			builder.WriteString(strings.ReplaceAll(rewritten, "'", "%27"))
			builder.WriteString("')")
		}
		position = urlEnd + 1
	}
	return builder.String()
}

func appendMissingMedia(root *htmlNode, media Media, rewriter *resourceRewriter) {
	seen := make(map[string]struct{})
	var walk func(*htmlNode)
	walk = func(node *htmlNode) {
		if node.typeID == htmlElementNode {
			for _, attribute := range []string{"src", "poster"} {
				if value := htmlAttribute(node, attribute); value != "" {
					seen[value] = struct{}{}
				}
			}
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(root)
	appendImage := func(image Image) {
		source := rewriter.rewrite(ResourceImage, image.URL)
		if source == "" {
			return
		}
		if _, exists := seen[source]; exists {
			return
		}
		seen[source] = struct{}{}
		attrs := map[string]string{"src": source}
		if image.Caption != "" {
			attrs["alt"] = image.Caption
		}
		if image.Width > 0 {
			attrs["width"] = strconv.FormatInt(image.Width, 10)
		}
		if image.Height > 0 {
			attrs["height"] = strconv.FormatInt(image.Height, 10)
		}
		paragraph := &htmlNode{typeID: htmlElementNode, tag: "p", attrs: map[string]string{"class": "article-media article-media-image"}}
		appendHTMLChild(paragraph, &htmlNode{typeID: htmlElementNode, tag: "img", attrs: attrs})
		appendHTMLChild(root, paragraph)
	}
	for _, image := range media.Images {
		appendImage(image)
	}
	for _, audio := range media.Audio {
		source := rewriter.rewrite(ResourceAudio, audio.URL)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		figure := &htmlNode{typeID: htmlElementNode, tag: "figure", attrs: map[string]string{"class": "article-media article-media-audio"}}
		appendHTMLChild(figure, &htmlNode{typeID: htmlElementNode, tag: "audio", attrs: map[string]string{"controls": "", "src": source}})
		if audio.Title != "" {
			caption := &htmlNode{typeID: htmlElementNode, tag: "figcaption"}
			appendHTMLChild(caption, &htmlNode{typeID: htmlTextNode, text: audio.Title})
			appendHTMLChild(figure, caption)
		}
		appendHTMLChild(root, figure)
	}
	for _, video := range media.Videos {
		source := rewriter.rewrite(ResourceVideo, video.URL)
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		attrs := map[string]string{"controls": "", "src": source}
		if poster := rewriter.rewrite(ResourceImage, video.CoverURL); poster != "" {
			attrs["poster"] = poster
		}
		figure := &htmlNode{typeID: htmlElementNode, tag: "figure", attrs: map[string]string{"class": "article-media article-media-video"}}
		appendHTMLChild(figure, &htmlNode{typeID: htmlElementNode, tag: "video", attrs: attrs})
		if video.Title != "" {
			caption := &htmlNode{typeID: htmlElementNode, tag: "figcaption"}
			appendHTMLChild(caption, &htmlNode{typeID: htmlTextNode, text: video.Title})
			appendHTMLChild(figure, caption)
		}
		appendHTMLChild(root, figure)
	}
}

func renderHTMLDocument(article Article, content *htmlNode, comments []Comment) string {
	var builder strings.Builder
	builder.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	builder.WriteString("<title>")
	builder.WriteString(html.EscapeString(article.Title))
	builder.WriteString("</title><style>")
	builder.WriteString(defaultArticleCSS)
	builder.WriteString("</style></head><body><article id=\"js_article\" class=\"article\"><header class=\"article-header\"><h1 class=\"article-title\">")
	builder.WriteString(html.EscapeString(article.Title))
	builder.WriteString("</h1>")
	metadata := articleMetadata(article)
	if metadata != "" {
		builder.WriteString("<p class=\"article-metadata\">")
		builder.WriteString(html.EscapeString(metadata))
		builder.WriteString("</p>")
	}
	if article.Description != "" {
		builder.WriteString("<p class=\"article-description\">")
		builder.WriteString(html.EscapeString(article.Description))
		builder.WriteString("</p>")
	}
	builder.WriteString("</header><main id=\"js_content\" class=\"article-content\">")
	builder.WriteString(serializeHTMLChildren(content))
	builder.WriteString("</main>")
	if len(comments) > 0 {
		builder.WriteString(renderCommentsHTML(comments))
	}
	builder.WriteString("</article></body></html>")
	return builder.String()
}

const defaultArticleCSS = `:root{color-scheme:light dark}body{margin:0;background:#fff;color:#222;font:16px/1.75 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.article{box-sizing:border-box;max-width:760px;margin:0 auto;padding:40px 24px}.article-title{font-size:2rem;line-height:1.3}.article-metadata,.article-description{color:#6b7280}.article-content img,.article-content video{max-width:100%;height:auto}.article-content audio{width:100%}.article-content table{border-collapse:collapse;max-width:100%}.article-content th,.article-content td{border:1px solid #d1d5db;padding:.4rem .6rem}.article-content pre{overflow:auto;padding:1rem;background:#f3f4f6}.article-comments{margin-top:3rem;border-top:1px solid #d1d5db}.article-comment,.article-reply{margin-top:1.25rem}.article-replies{margin-left:1.5rem}@media(prefers-color-scheme:dark){body{background:#111827;color:#f3f4f6}.article-content pre{background:#1f2937}}`

func articleMetadata(article Article) string {
	parts := make([]string, 0, 3)
	account := firstNonEmpty(article.Account.Nickname, article.Account.Alias, article.Account.Username)
	if account != "" {
		parts = append(parts, account)
	}
	if article.Author != "" && article.Author != account {
		parts = append(parts, article.Author)
	}
	if article.Timestamps.PublishedAt != nil {
		parts = append(parts, article.Timestamps.PublishedAt.Format("2006-01-02"))
	}
	return strings.Join(parts, " · ")
}

func renderCommentsHTML(comments []Comment) string {
	var builder strings.Builder
	builder.WriteString(`<section class="article-comments"><h2>Comments</h2>`)
	for _, comment := range comments {
		builder.WriteString(`<article class="article-comment"><header><strong>`)
		builder.WriteString(html.EscapeString(comment.Author))
		builder.WriteString(`</strong>`)
		if metadata := commentMetadata(comment.CreatedAt, comment.Location, comment.Likes); metadata != "" {
			builder.WriteString(`<span class="article-comment-metadata"> · `)
			builder.WriteString(html.EscapeString(metadata))
			builder.WriteString(`</span>`)
		}
		builder.WriteString(`</header><p>`)
		builder.WriteString(html.EscapeString(comment.Content))
		builder.WriteString(`</p>`)
		if len(comment.Replies) > 0 {
			builder.WriteString(`<div class="article-replies">`)
			for _, reply := range comment.Replies {
				builder.WriteString(`<article class="article-reply"><header><strong>`)
				builder.WriteString(html.EscapeString(reply.Author))
				builder.WriteString(`</strong>`)
				if metadata := commentMetadata(reply.CreatedAt, reply.Location, reply.Likes); metadata != "" {
					builder.WriteString(`<span class="article-comment-metadata"> · `)
					builder.WriteString(html.EscapeString(metadata))
					builder.WriteString(`</span>`)
				}
				builder.WriteString(`</header><p>`)
				builder.WriteString(html.EscapeString(reply.Content))
				builder.WriteString(`</p></article>`)
			}
			builder.WriteString(`</div>`)
		}
		builder.WriteString(`</article>`)
	}
	builder.WriteString(`</section>`)
	return builder.String()
}

func commentMetadata(createdAt *time.Time, location string, likes int64) string {
	parts := make([]string, 0, 3)
	if createdAt != nil {
		parts = append(parts, createdAt.Format("2006-01-02"))
	}
	if location != "" {
		parts = append(parts, location)
	}
	if likes > 0 {
		parts = append(parts, fmt.Sprintf("%d likes", likes))
	}
	return strings.Join(parts, " · ")
}

func applyCommentPrivacy(comments []Comment, privacy CommentPrivacy) []Comment {
	result := make([]Comment, len(comments))
	pseudonyms := make(map[string]string)
	nextPseudonym := 1
	anonymize := func(author, openID string) string {
		if !privacy.AnonymizeAuthors {
			return author
		}
		key := openID
		if key == "" {
			key = author
		}
		if key == "" {
			key = fmt.Sprintf("anonymous-%d", nextPseudonym)
		}
		if pseudonym := pseudonyms[key]; pseudonym != "" {
			return pseudonym
		}
		pseudonym := fmt.Sprintf("Reader %d", nextPseudonym)
		nextPseudonym++
		pseudonyms[key] = pseudonym
		return pseudonym
	}
	for index, comment := range comments {
		copy := comment
		copy.Author = anonymize(comment.Author, comment.OpenID)
		if privacy.HideAvatars {
			copy.AvatarURL = ""
		}
		if privacy.HideLocations {
			copy.Location = ""
		}
		if privacy.HideIdentifiers {
			copy.ID = ""
			copy.OpenID = ""
		}
		copy.Replies = make([]Reply, len(comment.Replies))
		for replyIndex, reply := range comment.Replies {
			replyCopy := reply
			replyCopy.Author = anonymize(reply.Author, reply.OpenID)
			if privacy.HideAvatars {
				replyCopy.AvatarURL = ""
			}
			if privacy.HideLocations {
				replyCopy.Location = ""
			}
			if privacy.HideIdentifiers {
				replyCopy.ID = ""
				replyCopy.OpenID = ""
			}
			copy.Replies[replyIndex] = replyCopy
		}
		result[index] = copy
	}
	return result
}

func renderTextDocument(article Article, content *htmlNode, comments []Comment) string {
	lines := []string{article.Title}
	if metadata := articleMetadata(article); metadata != "" {
		lines = append(lines, metadata)
	}
	body := strings.TrimSpace(renderTextNodes(content.children, 0))
	if body != "" {
		lines = append(lines, "", body)
	}
	if len(comments) > 0 {
		lines = append(lines, "", "Comments")
		for _, comment := range comments {
			header := comment.Author
			if metadata := commentMetadata(comment.CreatedAt, comment.Location, comment.Likes); metadata != "" {
				header += " · " + metadata
			}
			lines = append(lines, header, comment.Content)
			for _, reply := range comment.Replies {
				replyLine := "  " + reply.Author
				if metadata := commentMetadata(reply.CreatedAt, reply.Location, reply.Likes); metadata != "" {
					replyLine += " · " + metadata
				}
				replyLine += ": " + reply.Content
				lines = append(lines, replyLine)
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

func renderTextNodes(nodes []*htmlNode, listDepth int) string {
	var blocks []string
	for _, node := range nodes {
		block := renderTextNode(node, listDepth)
		if strings.TrimSpace(block) != "" {
			blocks = append(blocks, strings.TrimSpace(block))
		}
	}
	return strings.Join(blocks, "\n")
}

func renderTextNode(node *htmlNode, listDepth int) string {
	if node.typeID == htmlTextNode {
		return compactInlineText(node.text)
	}
	switch node.tag {
	case "br":
		return "\n"
	case "img":
		alt := strings.TrimSpace(htmlAttribute(node, "alt"))
		if alt == "" {
			alt = "image"
		}
		return "[Image: " + alt + "]"
	case "audio":
		return "[Audio] " + htmlAttribute(node, "src")
	case "video":
		return "[Video] " + firstNonEmpty(htmlAttribute(node, "src"), sourceChildURL(node))
	case "source", "style", "link":
		return ""
	case "ul", "ol":
		return renderTextList(node, listDepth)
	case "table":
		return renderTextTable(node)
	case "pre":
		return strings.TrimSpace(htmlNodeText(node))
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "figcaption", "caption", "th", "td":
		return compactInlineText(renderTextInlineChildren(node))
	case "a", "abbr", "b", "cite", "code", "del", "em", "i", "ins", "kbd", "mark", "q", "s", "samp", "small", "span", "strong", "sub", "sup", "time", "u", "var":
		return compactInlineText(renderTextInlineChildren(node))
	default:
		return renderTextNodes(node.children, listDepth)
	}
}

func renderTextInlineChildren(node *htmlNode) string {
	var builder strings.Builder
	for _, child := range node.children {
		switch {
		case child.typeID == htmlTextNode:
			builder.WriteString(child.text)
		case child.tag == "img":
			alt := strings.TrimSpace(htmlAttribute(child, "alt"))
			if alt == "" {
				alt = "image"
			}
			builder.WriteString("[Image: " + alt + "]")
		case child.tag == "br":
			builder.WriteByte('\n')
		default:
			builder.WriteString(renderTextInlineChildren(child))
		}
	}
	return builder.String()
}

func renderTextList(node *htmlNode, depth int) string {
	lines := make([]string, 0)
	index := 0
	for _, child := range node.children {
		if child.typeID != htmlElementNode || child.tag != "li" {
			continue
		}
		index++
		prefix := "- "
		if node.tag == "ol" {
			prefix = strconv.Itoa(index) + ". "
		}
		inline, nested := listItemParts(child)
		lines = append(lines, strings.Repeat("  ", depth)+prefix+compactInlineText(inline))
		for _, nestedList := range nested {
			if nestedText := renderTextList(nestedList, depth+1); nestedText != "" {
				lines = append(lines, nestedText)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func renderTextTable(node *htmlNode) string {
	rows := tableRows(node)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, strings.Join(row, " | "))
	}
	return strings.Join(lines, "\n")
}

func renderMarkdownDocument(article Article, content *htmlNode, comments []Comment) string {
	blocks := []string{"# " + escapeMarkdownInline(article.Title)}
	if metadata := articleMetadata(article); metadata != "" {
		blocks = append(blocks, escapeMarkdownInline(metadata))
	}
	body := renderMarkdownNodes(content.children, 0)
	if body != "" {
		blocks = append(blocks, body)
	}
	if len(comments) > 0 {
		commentBlocks := []string{"## Comments"}
		for _, comment := range comments {
			commentBlocks = append(commentBlocks, "### "+escapeMarkdownInline(comment.Author), escapeMarkdownText(comment.Content))
			for _, reply := range comment.Replies {
				commentBlocks = append(commentBlocks, "- **"+escapeMarkdownInline(reply.Author)+":** "+escapeMarkdownText(reply.Content))
			}
		}
		blocks = append(blocks, strings.Join(commentBlocks, "\n\n"))
	}
	return strings.TrimSpace(strings.Join(blocks, "\n\n")) + "\n"
}

func renderMarkdownNodes(nodes []*htmlNode, listDepth int) string {
	blocks := make([]string, 0)
	var inline strings.Builder
	flushInline := func() {
		value := strings.TrimSpace(compactMarkdownSpaces(inline.String()))
		if value != "" {
			blocks = append(blocks, value)
		}
		inline.Reset()
	}
	for _, node := range nodes {
		if node.typeID == htmlTextNode || isInlineHTMLNode(node) {
			inline.WriteString(renderMarkdownInline(node))
			continue
		}
		flushInline()
		value := renderMarkdownBlock(node, listDepth)
		if strings.TrimSpace(value) != "" {
			blocks = append(blocks, strings.TrimSpace(value))
		}
	}
	flushInline()
	return strings.Join(blocks, "\n\n")
}

func renderMarkdownBlock(node *htmlNode, listDepth int) string {
	switch node.tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level, _ := strconv.Atoi(strings.TrimPrefix(node.tag, "h"))
		return strings.Repeat("#", level) + " " + strings.TrimSpace(renderMarkdownInlineChildren(node))
	case "p", "div", "section", "article", "main", "header", "footer", "aside", "figure", "figcaption", "details", "summary":
		return renderMarkdownNodes(node.children, listDepth)
	case "blockquote":
		value := renderMarkdownNodes(node.children, listDepth)
		lines := strings.Split(value, "\n")
		for index := range lines {
			lines[index] = "> " + lines[index]
		}
		return strings.Join(lines, "\n")
	case "pre":
		language := ""
		if len(node.children) == 1 && node.children[0].tag == "code" {
			class := htmlAttribute(node.children[0], "class")
			for _, part := range strings.Fields(class) {
				if strings.HasPrefix(part, "language-") {
					language = strings.TrimPrefix(part, "language-")
				}
			}
		}
		return "```" + language + "\n" + strings.TrimSpace(htmlNodeText(node)) + "\n```"
	case "ul", "ol":
		return renderMarkdownList(node, listDepth)
	case "table":
		return renderMarkdownTable(node)
	case "audio":
		return "[Audio](" + escapeMarkdownURL(htmlAttribute(node, "src")) + ")"
	case "video":
		return "[Video](" + escapeMarkdownURL(firstNonEmpty(htmlAttribute(node, "src"), sourceChildURL(node))) + ")"
	case "style", "link", "source":
		return ""
	default:
		return renderMarkdownNodes(node.children, listDepth)
	}
}

func renderMarkdownInline(node *htmlNode) string {
	if node.typeID == htmlTextNode {
		return escapeMarkdownText(node.text)
	}
	content := renderMarkdownInlineChildren(node)
	switch node.tag {
	case "a":
		href := htmlAttribute(node, "href")
		if href == "" {
			return content
		}
		return "[" + content + "](" + escapeMarkdownURL(href) + ")"
	case "img":
		alt := strings.TrimSpace(htmlAttribute(node, "alt"))
		return "![" + escapeMarkdownInline(alt) + "](" + escapeMarkdownURL(htmlAttribute(node, "src")) + ")"
	case "strong", "b":
		return "**" + content + "**"
	case "em", "i":
		return "*" + content + "*"
	case "code", "kbd", "samp":
		return "`" + strings.ReplaceAll(content, "`", "\\`") + "`"
	case "del", "s":
		return "~~" + content + "~~"
	case "br":
		return "  \n"
	default:
		return content
	}
}

func renderMarkdownInlineChildren(node *htmlNode) string {
	var builder strings.Builder
	for _, child := range node.children {
		if child.typeID == htmlElementNode && !isInlineHTMLNode(child) {
			builder.WriteString(renderMarkdownBlock(child, 0))
		} else {
			builder.WriteString(renderMarkdownInline(child))
		}
	}
	return compactMarkdownSpaces(builder.String())
}

func isInlineHTMLNode(node *htmlNode) bool {
	if node == nil || node.typeID == htmlTextNode {
		return true
	}
	switch node.tag {
	case "a", "abbr", "b", "br", "cite", "code", "del", "em", "i", "img", "ins", "kbd", "mark", "q", "s", "samp", "small", "span", "strong", "sub", "sup", "time", "u", "var", "wbr":
		return true
	default:
		return false
	}
}

func renderMarkdownList(node *htmlNode, depth int) string {
	lines := make([]string, 0)
	index := 0
	for _, child := range node.children {
		if child.typeID != htmlElementNode || child.tag != "li" {
			continue
		}
		index++
		prefix := "- "
		if node.tag == "ol" {
			prefix = strconv.Itoa(index) + ". "
		}
		inline, nested := listItemParts(child)
		lines = append(lines, strings.Repeat("   ", depth)+prefix+strings.TrimSpace(renderMarkdownFragment(inline)))
		for _, nestedList := range nested {
			if nestedMarkdown := renderMarkdownList(nestedList, depth+1); nestedMarkdown != "" {
				lines = append(lines, nestedMarkdown)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func listItemParts(node *htmlNode) (string, []*htmlNode) {
	var inline strings.Builder
	nested := make([]*htmlNode, 0)
	for _, child := range node.children {
		if child.typeID == htmlElementNode && (child.tag == "ul" || child.tag == "ol") {
			nested = append(nested, child)
			continue
		}
		if child.typeID == htmlTextNode {
			inline.WriteString(child.text)
		} else {
			inline.WriteString(serializeHTMLChildren(&htmlNode{typeID: htmlDocumentNode, children: []*htmlNode{child}}))
		}
	}
	return inline.String(), nested
}

func renderMarkdownFragment(value string) string {
	root, err := parseHTMLFragment(value, DefaultLimits())
	if err != nil {
		return escapeMarkdownText(value)
	}
	return renderMarkdownNodes(root.children, 0)
}

func renderMarkdownTable(node *htmlNode) string {
	rows := tableRows(node)
	if len(rows) == 0 {
		return ""
	}
	columns := 0
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
	}
	if columns == 0 {
		return ""
	}
	for index := range rows {
		for len(rows[index]) < columns {
			rows[index] = append(rows[index], "")
		}
	}
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, markdownTableRow(rows[0]))
	separator := make([]string, columns)
	for index := range separator {
		separator[index] = "---"
	}
	lines = append(lines, markdownTableRow(separator))
	for _, row := range rows[1:] {
		lines = append(lines, markdownTableRow(row))
	}
	return strings.Join(lines, "\n")
}

func tableRows(node *htmlNode) [][]string {
	rows := make([][]string, 0)
	var walk func(*htmlNode)
	walk = func(current *htmlNode) {
		if current.typeID == htmlElementNode && current.tag == "tr" {
			row := make([]string, 0)
			for _, cell := range current.children {
				if cell.typeID == htmlElementNode && (cell.tag == "th" || cell.tag == "td") {
					row = append(row, compactInlineText(htmlNodeText(cell)))
				}
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for _, child := range current.children {
			walk(child)
		}
	}
	walk(node)
	return rows
}

func markdownTableRow(row []string) string {
	escaped := make([]string, len(row))
	for index, cell := range row {
		escaped[index] = strings.ReplaceAll(escapeMarkdownText(cell), "|", "\\|")
	}
	return "| " + strings.Join(escaped, " | ") + " |"
}

func sourceChildURL(node *htmlNode) string {
	for _, child := range node.children {
		if child.typeID == htmlElementNode && child.tag == "source" {
			if source := htmlAttribute(child, "src"); source != "" {
				return source
			}
		}
	}
	return ""
}

func compactInlineText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func compactMarkdownSpaces(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lines[index] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

func escapeMarkdownText(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}

func escapeMarkdownInline(value string) string {
	value = escapeMarkdownText(value)
	replacer := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`")
	return replacer.Replace(value)
}

func escapeMarkdownURL(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), ")", "%29")
}
