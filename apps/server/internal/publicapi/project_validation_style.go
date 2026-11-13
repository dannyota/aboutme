package publicapi

// Static content rules the project validators add to the shared resource
// checks: SVG only as static icons, and no CSS-generated text.

import (
	"strings"

	"golang.org/x/net/html"
)

// declaresStyleContent reports whether css may declare the content
// property, whatever its value, or cannot be scanned. The project pages'
// stylesheet declares no content, and any declaration could put text on the
// page outside the validated tree. With comments outside strings removed and
// strings kept, it matches the name content wherever the character before it
// cannot continue a name or selector (not a letter, digit, "_", "-", "." or
// "#") and the next character that is not whitespace is ":". That holds after
// any brace, nested rule or semicolon, and it errs toward rejection, such as
// for an element selector named content with a pseudo-class.
func declaresStyleContent(css string) bool {
	scanned, ok := cssWithoutComments(css)
	if !ok {
		return true
	}
	lower := strings.ToLower(scanned)
	const name = "content"
	for offset := 0; ; {
		found := strings.Index(lower[offset:], name)
		if found < 0 {
			return false
		}
		at := offset + found
		if (at == 0 || !cssNameOrSelectorByte(lower[at-1])) && nextNonSpaceByte(lower[at+len(name):]) == ':' {
			return true
		}
		offset = at + 1
	}
}

// cssNameOrSelectorByte reports a byte that continues a CSS name, class or
// id before it, so the word after it is not a property of its own.
func cssNameOrSelectorByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.' || b == '#'
}

// nextNonSpaceByte returns the first byte of value that is not CSS
// whitespace, or 0 when there is none.
func nextNonSpaceByte(value string) byte {
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case ' ', '\t', '\n', '\r', '\f':
			continue
		default:
			return value[i]
		}
	}
	return 0
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
