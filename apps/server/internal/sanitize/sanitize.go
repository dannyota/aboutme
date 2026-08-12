// Package sanitize applies the versioned rich-text policy shared by write and
// server-rendering boundaries.
package sanitize

import (
	"bytes"
	"strings"

	schemagen "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// AllowlistVersion is the generated sanitizer policy version.
const AllowlistVersion = schemagen.SanitizerAllowlistVersion

var richTextPolicy = newRichTextPolicy()

func newRichTextPolicy() *bluemonday.Policy {
	policy := bluemonday.NewPolicy()
	policy.AllowElements(schemagen.AllowedTags...)
	for tag, attributes := range schemagen.AllowedAttributes {
		policy.AllowAttrs(attributes...).OnElements(tag)
	}
	policy.RequireParseableURLs(true)
	policy.AllowRelativeURLs(false)
	policy.AllowURLSchemes(schemagen.AllowedURLSchemes...)
	return policy
}

// RichText sanitizes one rich-text HTML fragment with the generated policy.
func RichText(fragment string) string {
	return normalizeAnchors(richTextPolicy.Sanitize(fragment))
}

func normalizeAnchors(fragment string) string {
	context := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return ""
	}

	for _, root := range nodes {
		normalizeAnchorTree(root)
	}

	var output bytes.Buffer
	for _, root := range nodes {
		if err := html.Render(&output, root); err != nil {
			return ""
		}
	}
	return output.String()
}

func normalizeAnchorTree(node *html.Node) {
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, "a") {
		attributes := node.Attr[:0]
		for _, attribute := range node.Attr {
			name := strings.ToLower(attribute.Key)
			switch name {
			case "rel":
				continue
			case "target":
				if attribute.Val != "_blank" {
					continue
				}
			}
			attributes = append(attributes, attribute)
		}
		node.Attr = append(attributes, html.Attribute{Key: "rel", Val: schemagen.ExternalRel})
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		normalizeAnchorTree(child)
	}
}
