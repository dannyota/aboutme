package publicapi

// Closed validators for the two project page modes
// (docs/design/projects-contract.md "Render envelope"). They check named
// hooks and parsed trees, never whitespace or class order, so the
// renderer's exact output passes as the reference HTML does. Every rule
// name is a closed constant, safe to log.

import (
	"strings"

	"golang.org/x/net/html"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// projectExternalRel is the exact rel of every repository, demo and entry
// link.
const projectExternalRel = "noopener noreferrer nofollow"

// projectExpectation is what one project page's HTML must carry.
type projectExpectation struct {
	resume      directrender.ProjectPageResume
	title       string
	faviconHref string
	pageURL     string
	preview     previewmeta.Meta
	// internal holds every internal link target a page may carry; listed
	// holds the ones the list must link exactly once.
	internal map[string]bool
	listed   []string
	// external counts, for each repository, demo and entry link of the
	// request, the fields that supply it: the page may carry that link at
	// most that many times, so equal repository and demo addresses keep
	// both buttons.
	external map[string]int
	// description is the project's sanitized rich text, or "" for none.
	description string
}

// projectListHTMLRejection returns "" for list HTML that passes every rule,
// or the name of the first rule it breaks.
func projectListHTMLRejection(source []byte, request directrender.ProjectListRenderRequest, origin publicresume.PublicOrigin) string {
	want := projectExpectation{
		resume: request.Resume, title: request.PageTitle, faviconHref: request.FaviconHref, pageURL: request.PageURL,
		preview: request.Preview, internal: map[string]bool{request.Resume.URL: true, request.PageURL: true},
	}
	for _, row := range request.Projects {
		want.internal[row.URL] = true
		want.listed = append(want.listed, row.URL)
	}
	return projectPageRejection(source, want, origin)
}

// projectHTMLRejection returns "" for project page HTML that passes every
// rule, or the name of the first rule it breaks.
func projectHTMLRejection(source []byte, request directrender.ProjectRenderRequest, origin publicresume.PublicOrigin) string {
	want := projectExpectation{
		resume: request.Resume, title: request.PageTitle, faviconHref: request.FaviconHref, pageURL: request.PageURL,
		preview: request.Preview, description: request.Project.Description,
		internal: map[string]bool{request.Resume.URL: true, request.ListURL: true, request.Project.URL: true},
		external: map[string]int{},
	}
	for _, link := range []string{request.Project.RepositoryURL, request.Project.DemoURL, request.EntryLink} {
		if link != "" {
			want.external[link]++
		}
	}
	return projectPageRejection(source, want, origin)
}

// projectValidator walks one parsed page and records the first broken rule.
type projectValidator struct {
	want   projectExpectation
	origin publicresume.PublicOrigin
	meta   *headMeta
	rule   string

	htmlRoot, title, canonical, main               *html.Node
	mains, headings, styles, stylesheets, favicons int
	skipLinks, credits, backs, descriptions        int
	internalSeen, externalSeen                     map[string]int
}

func projectPageRejection(source []byte, want projectExpectation, origin publicresume.PublicOrigin) string {
	if len(source) == 0 || len(source) > 2_097_152 {
		return "size"
	}
	document, err := html.Parse(strings.NewReader(string(source)))
	if err != nil || !hasHTMLDoctype(document) {
		return "doctype"
	}
	v := &projectValidator{
		want: want, origin: origin, meta: newProjectHeadMeta(want, origin),
		internalSeen: map[string]int{}, externalSeen: map[string]int{},
	}
	v.walk(document)
	if v.rule != "" {
		return v.rule
	}
	return v.finish()
}

func (v *projectValidator) reject(name string) {
	if v.rule == "" {
		v.rule = name
	}
}

func (v *projectValidator) walk(node *html.Node) {
	if v.rule != "" {
		return
	}
	if node.Type == html.ElementNode {
		v.visitElement(node)
		if v.rule == "" && relHasToken(attribute(node, "class"), "project-description") {
			// The description region is compared as a tree; its own links
			// are the description's, so its children are not walked as page
			// elements.
			v.visitDescription(node)
			return
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		v.walk(child)
	}
}

func (v *projectValidator) visitElement(node *html.Node) {
	switch {
	case node.Data == "script":
		v.reject("script_element")
		return
	case forbiddenResourceElement(node.Data), node.Data == "img", node.Data == "picture", node.Data == "noscript":
		v.reject("resource_element")
		return
	}
	if node.Namespace == "svg" {
		if name := staticIconRule(node); name != "" {
			v.reject(name)
			return
		}
	}
	if name := unexpectedResourceAttribute(node); name != "" {
		v.reject(name)
		return
	}
	if attributeCount(node, "style") != 0 && declaresStyleContent(attribute(node, "style")) {
		v.reject("style_content")
		return
	}
	switch node.Data {
	case "html":
		v.visitHTML(node)
	case "meta":
		if name := v.meta.visit(node); name != "" {
			v.reject(name)
		}
	case "title":
		if node.Namespace != "" {
			// An icon's own SVG title names the icon, not the page.
			break
		}
		if v.title != nil || len(node.Attr) != 0 || textNode(node) != v.want.title {
			v.reject("title")
		}
		v.title = node
	case "link":
		v.visitLink(node)
	case "style":
		v.styles++
		if v.styles > 1 || unsafeInlineStyle(textNode(node)) {
			v.reject("style_element")
		} else if declaresStyleContent(textNode(node)) {
			v.reject("style_content")
		}
	case "main":
		v.mains++
		if v.mains > 1 || attribute(node, "id") != "public-project" || !onlyAttributes(node, "id", "class") {
			v.reject("main")
		}
		v.main = node
	case "h1":
		v.headings++
		if v.headings > 1 {
			v.reject("h1_count")
		}
	case "a":
		v.visitAnchor(node)
	}
}

// visitHTML checks the root: lang equal to the resume language, at most a
// data-scheme equal to its color scheme, and nothing else.
func (v *projectValidator) visitHTML(node *html.Node) {
	if v.htmlRoot != nil {
		return
	}
	v.htmlRoot = node
	switch {
	case attributeCount(node, "lang") != 1 || attribute(node, "lang") != v.want.resume.Lng:
		v.reject("lang")
	case attributeCount(node, "data-scheme") > 1 ||
		attributeCount(node, "data-scheme") == 1 && attribute(node, "data-scheme") != string(v.want.resume.ColorScheme):
		v.reject("color_scheme")
	case !onlyAttributes(node, "lang", "data-scheme"):
		v.reject("html_attributes")
	}
}

// visitLink allows the favicon only when the request names one, the one
// canonical link to the page itself, and at most the versioned font
// stylesheet.
func (v *projectValidator) visitLink(node *html.Node) {
	rel := attribute(node, "rel")
	switch {
	case relHasToken(rel, "icon"):
		v.favicons++
		if v.want.faviconHref == "" || v.favicons > 1 || rel != "icon" || !onlyAttributes(node, "rel", "href") || attribute(node, "href") != v.want.faviconHref {
			v.reject("favicon")
		}
	case rel == "stylesheet":
		v.stylesheets++
		if v.stylesheets > 1 || !onlyAttributes(node, "rel", "href") || !versionedAsset(attribute(node, "href"), "/_nuxt/assets/print-fonts.css") {
			v.reject("stylesheet")
		}
	case relHasToken(rel, "canonical"):
		if v.canonical != nil || rel != "canonical" || !onlyAttributes(node, "rel", "href") || attribute(node, "href") != v.want.pageURL {
			v.reject("canonical")
		}
		v.canonical = node
	default:
		v.reject("link_element")
	}
}

// finish checks the counts the walk cannot decide alone.
func (v *projectValidator) finish() string {
	if name := v.meta.finish(); name != "" {
		return name
	}
	switch {
	case v.htmlRoot == nil:
		return "lang"
	case v.title == nil:
		return "title"
	case v.canonical == nil:
		return "canonical"
	case v.want.faviconHref != "" && v.favicons != 1:
		return "favicon"
	case v.styles != 1:
		return "style_element"
	case v.main == nil:
		return "main"
	case v.headings != 1:
		return "h1_count"
	case v.skipLinks != 1:
		return "skip_link"
	case v.credits != 1:
		return "credit_link"
	case v.backs != 1:
		return "back_link"
	case v.descriptions != boolToInt(v.want.description != ""):
		return "description_region"
	}
	for _, url := range v.want.listed {
		if v.internalSeen[url] != 1 {
			return "project_links"
		}
	}
	return ""
}

// newProjectHeadMeta expects the resume page's preview elements with
// og:type website, og:url the page itself, and the description pair only
// when the page has a description.
func newProjectHeadMeta(want projectExpectation, origin publicresume.PublicOrigin) *headMeta {
	meta := newHeadMeta(publicresume.PublicResume{Slug: want.resume.Slug}, want.preview, origin)
	meta.expected[metaKey{"property", "og:type"}] = "website"
	meta.expected[metaKey{"property", "og:url"}] = want.pageURL
	if want.preview.Description == "" {
		delete(meta.expected, metaKey{"name", "description"})
		delete(meta.expected, metaKey{"property", "og:description"})
	}
	return meta
}

// onlyAttributes reports whether every attribute of node is one of keys,
// in no namespace, and appears at most once.
func onlyAttributes(node *html.Node, keys ...string) bool {
	for _, attr := range node.Attr {
		known := attr.Namespace == ""
		if known {
			known = false
			for _, key := range keys {
				known = known || attr.Key == key
			}
		}
		if !known || attributeCount(node, attr.Key) != 1 {
			return false
		}
	}
	return true
}
