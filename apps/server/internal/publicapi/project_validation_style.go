package publicapi

// Rules the project validators add to the shared resource checks: only the
// two versioned stylesheets, and SVG only as static icons.

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// The two stylesheets the project pages load, each once, with one shared
// content version. The pages carry no inline CSS.
const (
	printFontsStylesheet   = "/_nuxt/assets/print-fonts.css"
	projectPagesStylesheet = "/_nuxt/assets/project-pages.css"
	projectStylesheetCount = 2
)

// projectStylesheet records one stylesheet link and reports whether it is
// one of the two the pages load, not seen before, with the same version as
// the other.
func (v *projectValidator) projectStylesheet(href string) bool {
	for _, path := range []string{printFontsStylesheet, projectPagesStylesheet} {
		if !versionedAsset(href, path) {
			continue
		}
		if _, seen := v.stylesheetVersions[path]; seen {
			return false
		}
		version := strings.TrimPrefix(href, path+"?v=")
		for _, other := range v.stylesheetVersions {
			if other != version {
				return false
			}
		}
		v.stylesheetVersions[path] = version
		return true
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
		if attr.Namespace != "" || unsafeInlineStyle(attr.Val) {
			return "svg_attribute"
		}
		if attr.Key == "transform" {
			if !staticIconTransform.MatchString(attr.Val) {
				return "svg_attribute"
			}
			continue
		}
		if !staticIconAttribute(attr.Key) {
			return "svg_attribute"
		}
	}
	return ""
}

// staticIconTransform admits a transform list of rotate(a), rotate(a x y),
// translate(x), translate(x y), scale(x) and scale(x y) with plain numbers
// (an optional sign and decimals) separated by whitespace or a comma, so a
// transform can only position a shape: no url(), var(), other function or
// unit.
var staticIconTransform = regexp.MustCompile(
	`^\s*` + staticIconTransformFunction + `(?:\s*,?\s*` + staticIconTransformFunction + `)*\s*$`)

// The number and argument patterns staticIconTransform is built from.
const (
	staticIconNumber            = `[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)`
	staticIconSeparator         = `(?:\s*,\s*|\s+)`
	staticIconTransformFunction = `(?:rotate\(\s*` + staticIconNumber + `(?:` + staticIconSeparator + staticIconNumber + staticIconSeparator + staticIconNumber + `)?\s*\)` +
		`|(?:translate|scale)\(\s*` + staticIconNumber + `(?:` + staticIconSeparator + staticIconNumber + `)?\s*\))`
)

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
