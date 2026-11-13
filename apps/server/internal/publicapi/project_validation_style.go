package publicapi

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const (
	printFontsStylesheet   = "/_nuxt/assets/print-fonts.css"
	projectPagesStylesheet = "/_nuxt/assets/project-pages.css"
	projectStylesheetCount = 2
)

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

var staticIconTransform = regexp.MustCompile(
	`^\s*` + staticIconTransformFunction + `(?:\s*,?\s*` + staticIconTransformFunction + `)*\s*$`)

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
