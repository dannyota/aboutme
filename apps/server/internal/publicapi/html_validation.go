package publicapi

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func validPublicHTML(source []byte, resume publicresume.PublicResume, origin publicresume.PublicOrigin, jsonLD publicformat.JSONLDResult, discoverable bool) bool {
	return publicHTMLRejection(source, resume, origin, jsonLD, discoverable) == ""
}

// publicHTMLRejection returns "" for renderer output that passes every public
// HTML rule, or the closed name of the first rule it breaks. The name never
// carries resume content, so it is safe to log.
// publicHTMLRejection validates a page with no owner page settings.
func publicHTMLRejection(source []byte, resume publicresume.PublicResume, origin publicresume.PublicOrigin, jsonLD publicformat.JSONLDResult, discoverable bool) string {
	return publicHTMLRejectionForPage(source, resume, expectedPublicPage(resume, nil, nil), origin, jsonLD, discoverable)
}

func publicHTMLRejectionForPage(source []byte, resume publicresume.PublicResume, page publicPage, origin publicresume.PublicOrigin, jsonLD publicformat.JSONLDResult, discoverable bool) string {
	if len(source) == 0 || len(source) > 2_097_152 {
		return "size"
	}
	document, err := html.Parse(strings.NewReader(string(source)))
	if err != nil || !hasHTMLDoctype(document) {
		return "doctype"
	}
	var title, canonical, main *html.Node
	var scriptCount, externalScripts, dataScripts, mainCount, images, skipLinks, downloadLinks, creditLinks, favicons int
	meta := newHeadMeta(resume, page.Preview, origin)
	stylesheets := map[string]bool{}
	contactLinks := derivedContactLinks(resume)
	rule := ""
	reject := func(name string) {
		if rule == "" {
			rule = name
		}
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if rule != "" {
			return
		}
		if node.Type == html.ElementNode {
			if forbiddenResourceElement(node.Data) {
				reject("resource_element")
				return
			}
			if name := unexpectedResourceAttribute(node); name != "" {
				reject(name)
				return
			}
			switch node.Data {
			case "meta":
				if name := meta.visit(node); name != "" {
					reject(name)
					return
				}
			case "style":
				reject("style_element")
				return
			case "a":
				href := attribute(node, "href")
				if attributeCount(node, "href") != 1 {
					reject("anchor_href")
					return
				}
				if relHasToken(attribute(node, "class"), creditClass) {
					// The page credit, once, exactly as the renderer writes it.
					if creditLinks != 0 || !isPublicCredit(node, origin.Resolve("/"), creditText(resume.Lng)) {
						reject("credit_link")
						return
					}
					creditLinks++
				} else if href == "#public-resume" {
					if skipLinks != 0 || textNode(node) != "Skip to content" {
						reject("skip_link")
						return
					}
					skipLinks++
				} else if (strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:")) && insideResumeHeader(node) {
					// Header contact links are exactly the hrefs derived from
					// visible email and phone details, each once.
					if contactLinks[href] == 0 {
						reject("contact_link")
						return
					}
					contactLinks[href]--
				} else if href == publicPDFPath(resume.Slug) {
					// The page's own PDF, once, only while download is enabled.
					if !resume.DownloadEnabled || downloadLinks != 0 {
						reject("download_link")
						return
					}
					downloadLinks++
				} else if !allowedPublicAnchor(href) {
					reject("anchor_scheme")
					return
				}
			case "title":
				if title != nil || len(node.Attr) != 0 || textNode(node) != page.Title {
					reject("title")
					return
				}
				title = node
			case "link":
				if relHasToken(attribute(node, "rel"), "icon") {
					// Only the owner's emoji favicon: exactly rel and href, once,
					// with the href derived from the stored emoji.
					if page.FaviconHref == "" || favicons != 0 || len(node.Attr) != 2 || attributeCount(node, "rel") != 1 ||
						attribute(node, "rel") != "icon" || attributeCount(node, "href") != 1 || attribute(node, "href") != page.FaviconHref {
						reject("favicon")
						return
					}
					favicons++
					break
				}
				if attribute(node, "rel") == "stylesheet" {
					// Only the two self-hosted resume stylesheets, once each, with
					// exactly rel and href; style-src 'self' then loads them.
					path, ok := resumeStylesheet(attribute(node, "href"))
					if len(node.Attr) != 2 || attributeCount(node, "rel") != 1 || attributeCount(node, "href") != 1 || !ok || stylesheets[path] {
						reject("stylesheet")
						return
					}
					stylesheets[path] = true
					break
				}
				if !relHasToken(attribute(node, "rel"), "canonical") || canonical != nil || len(node.Attr) != 2 || attributeCount(node, "rel") != 1 || attribute(node, "rel") != "canonical" || attributeCount(node, "href") != 1 || attribute(node, "href") != origin.Resolve("/"+resume.Slug) {
					if !relHasToken(attribute(node, "rel"), "canonical") {
						reject("stylesheet")
					} else {
						reject("canonical")
					}
					return
				}
				canonical = node
			case "img":
				images++
				photo := resume.Document.PersonalDetails.Photo
				if photo == nil || attributeCount(node, "src") != 1 || attribute(node, "src") != photo.URL || attributeCount(node, "alt") != 1 || attribute(node, "alt") != "" {
					reject("image")
					return
				}
			case "main":
				mainCount++
				if attribute(node, "id") == "public-resume" {
					wantAttrs := 2
					if page.JoinInvite != "" {
						wantAttrs = 3
					}
					if main != nil || len(node.Attr) != wantAttrs || attributeCount(node, "id") != 1 ||
						attributeCount(node, "data-revision") != 1 || attribute(node, "data-revision") != resume.Revision {
						reject("main")
						return
					}
					// data-join-invite carries exactly page.JoinInvite for a
					// sign-in-to-view resume, and is absent for every other
					// public resume (docs/design/viewer-analytics/
					// sign-in-to-view.md "Join invite").
					if attributeCount(node, "data-join-invite") != boolToInt(page.JoinInvite != "") ||
						(page.JoinInvite != "" && attribute(node, "data-join-invite") != page.JoinInvite) {
						reject("main")
						return
					}
					main = node
				}
			case "script":
				scriptCount++
				if attributeCount(node, "src") == 1 {
					if len(node.Attr) != 2 || !versionedAsset(attribute(node, "src"), "/_nuxt/assets/public-resume.mjs") || attributeCount(node, "type") != 1 || attribute(node, "type") != "module" || textNode(node) != "" {
						reject("script")
						return
					}
					externalScripts++
					return
				}
				if len(node.Attr) != 1 || attributeCount(node, "src") != 0 || !discoverable || attributeCount(node, "type") != 1 || attribute(node, "type") != "application/ld+json" || textNode(node) != string(jsonLD.JSON) {
					reject("json_ld")
					return
				}
				dataScripts++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	if rule != "" {
		return rule
	}
	if name := meta.finish(); name != "" {
		return name
	}
	if page.FaviconHref != "" && favicons != 1 {
		return "favicon"
	}
	if creditLinks != 1 {
		return "credit_link"
	}
	if title == nil || canonical == nil || main == nil || mainCount != 1 || skipLinks != 1 || externalScripts != 1 {
		return "required_elements"
	}
	if (resume.Document.PersonalDetails.Photo == nil && images != 0) || (resume.Document.PersonalDetails.Photo != nil && images != 1) {
		return "image_count"
	}
	if discoverable {
		if scriptCount != 2 || dataScripts != 1 {
			return "script_count"
		}
		return ""
	}
	if scriptCount != 1 || dataScripts != 0 || jsonLD.JSON != nil || jsonLD.Script != nil || jsonLD.CSP != publicformat.BaseCSP {
		return "script_count"
	}
	return ""
}

func hasHTMLDoctype(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.DoctypeNode && strings.EqualFold(child.Data, "html") {
			return true
		}
	}
	return false
}

// boolToInt is the exact-count idiom the validator uses for an attribute
// that is present exactly once when a condition holds, and absent otherwise.
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func attribute(node *html.Node, key string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

func attributeCount(node *html.Node, key string) int {
	count := 0
	for _, attribute := range node.Attr {
		if attribute.Key == key {
			count++
		}
	}
	return count
}

func textNode(node *html.Node) string {
	var output strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			output.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return output.String()
}
