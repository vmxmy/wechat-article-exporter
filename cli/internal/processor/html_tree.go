package processor

import (
	"html"
	"sort"
	"strings"
)

type htmlNodeType uint8

const (
	htmlDocumentNode htmlNodeType = iota
	htmlElementNode
	htmlTextNode
)

type htmlNode struct {
	typeID   htmlNodeType
	tag      string
	attrs    map[string]string
	children []*htmlNode
	parent   *htmlNode
	text     string
}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true,
}

var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "div": true, "dl": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true, "form": true, "h1": true,
	"h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "header": true, "hr": true,
	"main": true, "nav": true, "ol": true, "p": true, "pre": true, "section": true, "table": true, "ul": true,
}

func parseHTMLFragment(value string, limits Limits) (*htmlNode, error) {
	limits = limits.withDefaults()
	root := &htmlNode{typeID: htmlDocumentNode}
	current := root
	nodes := 1
	position := 0
	lower := strings.ToLower(value)

	appendNode := func(parent, child *htmlNode) error {
		nodes++
		if nodes > limits.MaxHTMLNodes {
			return processError(ErrorLimit, ReasonHTMLLimit, position, "HTML nodes exceed %d", limits.MaxHTMLNodes)
		}
		child.parent = parent
		parent.children = append(parent.children, child)
		return nil
	}

	for position < len(value) {
		if value[position] != '<' {
			next := strings.IndexByte(value[position:], '<')
			if next < 0 {
				next = len(value) - position
			}
			text := html.UnescapeString(value[position : position+next])
			if text != "" {
				if err := appendNode(current, &htmlNode{typeID: htmlTextNode, text: text}); err != nil {
					return nil, err
				}
			}
			position += next
			continue
		}

		if strings.HasPrefix(value[position:], "<!--") {
			end := strings.Index(value[position+4:], "-->")
			if end < 0 {
				break
			}
			position += end + 7
			continue
		}
		if strings.HasPrefix(value[position:], "<!") || strings.HasPrefix(value[position:], "<?") {
			end, err := findTagEnd([]byte(value), position+2)
			if err != nil {
				break
			}
			position = end + 1
			continue
		}

		end, err := findTagEnd([]byte(value), position+1)
		if err != nil {
			// Treat a trailing malformed tag opener as text instead of executing
			// or recursively reparsing it.
			if appendErr := appendNode(current, &htmlNode{typeID: htmlTextNode, text: html.UnescapeString(value[position:])}); appendErr != nil {
				return nil, appendErr
			}
			break
		}
		raw := strings.TrimSpace(value[position+1 : end])
		if raw == "" {
			position = end + 1
			continue
		}
		if raw[0] == '/' {
			tag := htmlTagName(strings.TrimSpace(raw[1:]))
			for node := current; node != nil && node != root; node = node.parent {
				if node.tag == tag {
					current = node.parent
					break
				}
			}
			position = end + 1
			continue
		}

		selfClosing := strings.HasSuffix(raw, "/")
		if selfClosing {
			raw = strings.TrimSpace(strings.TrimSuffix(raw, "/"))
		}
		tag, attrs := parseHTMLTag(raw)
		if tag == "" {
			position = end + 1
			continue
		}
		if current.tag == "p" && blockElements[tag] {
			current = current.parent
		}
		if tag == "li" && current.tag == "li" {
			current = current.parent
		}
		node := &htmlNode{typeID: htmlElementNode, tag: tag, attrs: attrs}
		if err := appendNode(current, node); err != nil {
			return nil, err
		}
		position = end + 1

		if tag == "script" || tag == "style" {
			closing := "</" + tag
			relative := strings.Index(lower[position:], closing)
			if relative < 0 {
				if position < len(value) {
					if err := appendNode(node, &htmlNode{typeID: htmlTextNode, text: value[position:]}); err != nil {
						return nil, err
					}
				}
				break
			}
			closeStart := position + relative
			if closeStart > position {
				if err := appendNode(node, &htmlNode{typeID: htmlTextNode, text: value[position:closeStart]}); err != nil {
					return nil, err
				}
			}
			closeEnd, closeErr := findTagEnd([]byte(value), closeStart+len(closing))
			if closeErr != nil {
				break
			}
			position = closeEnd + 1
			continue
		}

		if !selfClosing && !voidElements[tag] {
			depth := 0
			for ancestor := node; ancestor != nil && ancestor != root; ancestor = ancestor.parent {
				depth++
			}
			if depth > limits.MaxHTMLDepth {
				return nil, processError(ErrorLimit, ReasonHTMLLimit, position, "HTML nesting exceeds %d", limits.MaxHTMLDepth)
			}
			current = node
		}
	}
	return root, nil
}

func parseHTMLTag(raw string) (string, map[string]string) {
	position := 0
	for position < len(raw) && isSpace(raw[position]) {
		position++
	}
	start := position
	for position < len(raw) && isHTMLNameChar(raw[position]) {
		position++
	}
	if start == position {
		return "", nil
	}
	tag := strings.ToLower(raw[start:position])
	attrs := make(map[string]string)
	for position < len(raw) {
		for position < len(raw) && isSpace(raw[position]) {
			position++
		}
		if position >= len(raw) {
			break
		}
		start = position
		for position < len(raw) && !isSpace(raw[position]) && raw[position] != '=' && raw[position] != '>' {
			position++
		}
		name := strings.ToLower(strings.TrimSpace(raw[start:position]))
		if name == "" {
			position++
			continue
		}
		for position < len(raw) && isSpace(raw[position]) {
			position++
		}
		value := ""
		if position < len(raw) && raw[position] == '=' {
			position++
			for position < len(raw) && isSpace(raw[position]) {
				position++
			}
			if position < len(raw) && (raw[position] == '\'' || raw[position] == '"') {
				quote := raw[position]
				position++
				start = position
				for position < len(raw) && raw[position] != quote {
					position++
				}
				value = raw[start:position]
				if position < len(raw) {
					position++
				}
			} else {
				start = position
				for position < len(raw) && !isSpace(raw[position]) && raw[position] != '>' {
					position++
				}
				value = raw[start:position]
			}
		}
		if _, exists := attrs[name]; !exists {
			attrs[name] = html.UnescapeString(value)
		}
	}
	return tag, attrs
}

func htmlTagName(value string) string {
	for index, char := range value {
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' || char == '>' {
			return strings.ToLower(value[:index])
		}
	}
	return strings.ToLower(value)
}

func appendHTMLChild(parent, child *htmlNode) {
	child.parent = parent
	parent.children = append(parent.children, child)
}

func htmlAttribute(node *htmlNode, name string) string {
	if node == nil || node.attrs == nil {
		return ""
	}
	return node.attrs[strings.ToLower(name)]
}

func htmlNodeText(node *htmlNode) string {
	if node == nil {
		return ""
	}
	if node.typeID == htmlTextNode {
		return node.text
	}
	var builder strings.Builder
	for _, child := range node.children {
		builder.WriteString(htmlNodeText(child))
	}
	return builder.String()
}

func serializeHTMLChildren(node *htmlNode) string {
	var builder strings.Builder
	for _, child := range node.children {
		serializeHTMLNode(&builder, child)
	}
	return builder.String()
}

func serializeHTMLNode(builder *strings.Builder, node *htmlNode) {
	switch node.typeID {
	case htmlTextNode:
		builder.WriteString(html.EscapeString(node.text))
	case htmlElementNode:
		builder.WriteByte('<')
		builder.WriteString(node.tag)
		keys := make([]string, 0, len(node.attrs))
		for key := range node.attrs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteByte(' ')
			builder.WriteString(key)
			if node.attrs[key] != "" {
				builder.WriteString(`="`)
				builder.WriteString(html.EscapeString(node.attrs[key]))
				builder.WriteByte('"')
			}
		}
		builder.WriteByte('>')
		if voidElements[node.tag] {
			return
		}
		for _, child := range node.children {
			serializeHTMLNode(builder, child)
		}
		builder.WriteString("</")
		builder.WriteString(node.tag)
		builder.WriteByte('>')
	case htmlDocumentNode:
		for _, child := range node.children {
			serializeHTMLNode(builder, child)
		}
	}
}
