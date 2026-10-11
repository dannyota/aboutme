package publicapi

import (
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/dannyota/aboutme/apps/server/internal/contactlink"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// resumeStylesheet reports whether href is one of the self-hosted stylesheets
// that carry the resume template's CSS and fonts, and returns its path.
func resumeStylesheet(href string) (string, bool) {
	for _, path := range []string{"/_nuxt/assets/print-fonts.css", "/_nuxt/assets/print.css"} {
		if versionedAsset(href, path) {
			return path, true
		}
	}
	return "", false
}

// versionedAsset reports whether url is path plus the renderer's content
// version as its only query, ?v=<16 lowercase hex>. These assets keep fixed
// names and are cached as immutable, so an unversioned URL could serve a
// year-old copy after a release.
func versionedAsset(url, path string) bool {
	version, found := strings.CutPrefix(url, path+"?v=")
	if !found || len(version) != 16 {
		return false
	}
	for _, character := range version {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// derivedContactLinks counts the mailto: and tel: hrefs the renderer may emit
// for the resume's visible contact details (docs/adr/0013-resume-header-and-contacts.md).
func derivedContactLinks(resume publicresume.PublicResume) map[string]int {
	links := map[string]int{}
	if !resume.Document.PersonalDetails.Details.Present() {
		return links
	}
	for _, detail := range resume.Document.PersonalDetails.Details.Value() {
		if href, ok := contactlink.Href(detail.Type, detail.Value); ok {
			links[href]++
		}
	}
	return links
}

// insideResumeHeader reports whether node sits in the resume's header block.
func insideResumeHeader(node *html.Node) bool {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.Data == "header" &&
			slices.Contains(strings.Fields(attribute(ancestor, "class")), "resume-header") {
			return true
		}
	}
	return false
}

// creditClass marks the page credit, which links the service's home page from
// the page chrome (docs/design/web.md).
const creditClass = "public-credit"

// creditText is the credit's text in the resume language: Vietnamese for vi,
// English for every other language.
func creditText(lng string) string {
	language, _, _ := strings.Cut(lng, "-")
	if strings.EqualFold(language, "vi") {
		return "Tạo bằng aboutme.vn"
	}
	return "Built with aboutme.vn"
}

// isPublicCredit requires exactly class and href, the canonical home URL, one
// text child with the expected text, and a place outside the resume article.
func isPublicCredit(node *html.Node, href, text string) bool {
	if len(node.Attr) != 2 || attributeCount(node, "class") != 1 || attribute(node, "class") != creditClass ||
		attributeCount(node, "href") != 1 || attribute(node, "href") != href {
		return false
	}
	child := node.FirstChild
	if child == nil || child.NextSibling != nil || child.Type != html.TextNode || child.Data != text {
		return false
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.Data == "article" {
			return false
		}
	}
	return true
}

func publicPDFPath(slug string) string {
	return "/api/v1/public/resumes/" + slug + "/pdf"
}

func allowedPublicAnchor(href string) bool {
	return strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:")
}

func relHasToken(value, token string) bool {
	for _, candidate := range strings.Fields(value) {
		if candidate == token {
			return true
		}
	}
	return false
}

func forbiddenResourceElement(name string) bool {
	switch name {
	case "base", "embed", "form", "frame", "iframe", "object", "source", "track", "audio", "video":
		return true
	default:
		return false
	}
}

// unexpectedResourceAttribute returns the closed rule an attribute breaks, or
// "" when every attribute is allowed.
func unexpectedResourceAttribute(node *html.Node) string {
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if strings.HasPrefix(key, "on") {
			return "event_attribute"
		}
		switch key {
		case "action", "background", "data", "formaction", "ping", "poster", "srcset":
			return "resource_attribute"
		case "href":
			if node.Data != "a" && node.Data != "link" {
				return "href_element"
			}
		case "src":
			if node.Data != "img" && node.Data != "script" {
				return "src_element"
			}
		case "style":
			if unsafeInlineStyle(attribute.Val) {
				return "inline_style"
			}
		}
	}
	return ""
}

func unsafeInlineStyle(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "\\") {
		return true
	}
	withoutComments := stripCSSComments(lower)
	compact := strings.Map(func(character rune) rune {
		switch character {
		case ' ', '\t', '\n', '\r', '\f':
			return -1
		default:
			return character
		}
	}, withoutComments)
	return strings.Contains(compact, "url") || strings.Contains(compact, "image-set") || strings.Contains(compact, "@import")
}

func stripCSSComments(value string) string {
	var output strings.Builder
	for {
		start := strings.Index(value, "/*")
		if start < 0 {
			output.WriteString(value)
			return output.String()
		}
		output.WriteString(value[:start])
		end := strings.Index(value[start+2:], "*/")
		if end < 0 {
			return output.String()
		}
		value = value[start+2+end+2:]
	}
}
