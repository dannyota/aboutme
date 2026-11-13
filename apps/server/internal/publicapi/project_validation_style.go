package publicapi

// Static content rules the project validators add to the shared resource
// checks: SVG only as static icons, and no CSS-generated text.

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// cssContentDeclaration finds each content declaration and its value in
// CSS with comments removed. A name such as justify-content does not match.
var cssContentDeclaration = regexp.MustCompile(`(?i)(?:^|[{;\s])content\s*:([^;}]*)`)

// unsafeStyleContent reports whether css sets content to anything but an
// empty string or none, which would put text on the page outside the
// validated tree.
func unsafeStyleContent(css string) bool {
	for _, match := range cssContentDeclaration.FindAllStringSubmatch(stripCSSComments(css), -1) {
		value := strings.TrimSpace(match[1])
		if len(value) >= len("!important") && strings.EqualFold(value[len(value)-len("!important"):], "!important") {
			value = strings.TrimSpace(value[:len(value)-len("!important")])
		}
		if value != `""` && value != `''` && !strings.EqualFold(value, "none") {
			return true
		}
	}
	return false
}

// staticIconRule returns "" for an SVG element of a static icon, or the rule
// it breaks. Only drawing elements and presentation attributes are allowed,
// none of which can embed, animate, or reference a resource.
func staticIconRule(node *html.Node) string {
	if !staticIconElement(node.Data) {
		return "svg_element"
	}
	for _, attr := range node.Attr {
		if attr.Namespace != "" || !staticIconAttribute(attr.Key) || unsafeInlineStyle(attr.Val) {
			return "svg_attribute"
		}
	}
	return ""
}

func staticIconElement(name string) bool {
	switch name {
	case "svg", "g", "path", "circle", "rect", "line", "polyline", "polygon", "title":
		return true
	default:
		return false
	}
}

func staticIconAttribute(key string) bool {
	switch key {
	case "class", "viewBox", "width", "height", "fill", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin",
		"d", "cx", "cy", "r", "x", "y", "x1", "y1", "x2", "y2", "rx", "ry", "points", "aria-hidden", "focusable", "xmlns":
		return true
	default:
		return false
	}
}
