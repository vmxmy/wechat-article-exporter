package htmlx

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// FindByID returns the first element carrying the id, in document order.
func FindByID(root *html.Node, id string) *html.Node {
	return find(root, func(node *html.Node) bool { return Attr(node, "id") == id })
}

// FindByClass returns the first element whose class attribute contains the
// given class token.
func FindByClass(root *html.Node, class string) *html.Node {
	return find(root, func(node *html.Node) bool {
		for _, token := range strings.Fields(Attr(node, "class")) {
			if token == class {
				return true
			}
		}
		return false
	})
}

// FindByTag returns the first element with the given tag, in document order.
func FindByTag(root *html.Node, tag atom.Atom) *html.Node {
	return find(root, func(node *html.Node) bool { return node.DataAtom == tag })
}

func find(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if root.Type == html.ElementNode && match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := find(child, match); found != nil {
			return found
		}
	}
	return nil
}

// Attr returns the value of the named attribute, or "" when absent.
func Attr(node *html.Node, name string) string {
	if node == nil {
		return ""
	}
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

// Text concatenates the node's descendant text, whitespace-trimmed. Entity
// decoding already happened at tokenization, so the result is plain text.
func Text(node *html.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.TrimSpace(builder.String())
}
