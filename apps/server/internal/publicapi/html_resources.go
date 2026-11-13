package publicapi

import (
	"slices"
	"strings"

	"golang.org/x/net/html"

	"danny.vn/aboutme/apps/server/internal/contactlink"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func resumeStylesheet(href string) (string, bool) {
	for _, path := range []string{"/_nuxt/assets/print-fonts.css", "/_nuxt/assets/print.css"} {
		if versionedAsset(href, path) {
			return path, true
		}
	}
	return "", false
}

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

func insideResumeHeader(node *html.Node) bool {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == html.ElementNode && ancestor.Data == "header" &&
			slices.Contains(strings.Fields(attribute(ancestor, "class")), "resume-header") {
			return true
		}
	}
	return false
}

const creditClass = "public-credit"

func creditText(lng string) string {
	language, _, _ := strings.Cut(lng, "-")
	if strings.EqualFold(language, "vi") {
		return "Tạo bằng aboutme.vn"
	}
	return "Built with aboutme.vn"
}

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
	withoutComments, ok := cssWithoutComments(lower)
	if !ok {
		return true
	}
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

func cssWithoutComments(value string) (string, bool) {
	var output strings.Builder
	for i := 0; i < len(value); {
		switch {
		case value[i] == '"' || value[i] == '\'':
			end, ok := cssStringEnd(value, i)
			if !ok {
				return "", false
			}
			output.WriteString(value[i:end])
			i = end
		case strings.HasPrefix(value[i:], "/*"):
			end := strings.Index(value[i+2:], "*/")
			if end < 0 {
				return "", false
			}
			output.WriteByte(' ')
			i += 2 + end + 2
		default:
			output.WriteByte(value[i])
			i++
		}
	}
	return output.String(), true
}

func cssStringEnd(value string, start int) (int, bool) {
	quote := value[start]
	for i := start + 1; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '\n', '\r', '\f':
			return 0, false
		case quote:
			return i + 1, true
		}
	}
	return 0, false
}
