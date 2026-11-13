package publicapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/html"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/viewpass"
)

func hasValidPass(request *http.Request, key []byte, resumeID uuid.UUID, passEpoch int32, clock func() time.Time) bool {
	cookie, err := request.Cookie(viewpass.CookieName)
	if err != nil {
		return false
	}
	now := time.Now
	if clock != nil {
		now = clock
	}
	return viewpass.Valid(cookie.Value, key, resumeID, passEpoch, now())
}

func gateMessage(request *http.Request) string {
	switch request.URL.Query().Get("signin") {
	case "cancelled": //nolint:misspell // Exact wire value uses double-L "cancelled".
		return "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".
	case "failed":
		return "failed"
	default:
		return "none"
	}
}

func gateLng(resumeLng string) string {
	language, _, _ := strings.Cut(resumeLng, "-")
	if strings.EqualFold(language, "vi") {
		return "vi"
	}
	return "en"
}

func serveGate(ctx context.Context, w http.ResponseWriter, request *http.Request, dependencies HTMLDependencies, snapshot publicresume.Snapshot, projectPage string, lease cancelHooks) {
	page := expectedPublicPage(snapshot.Public, snapshot.PublicTitle, snapshot.FaviconEmoji)
	if dependencies.Cards != nil {
		version, err := dependencies.Cards.Version(snapshot)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "card_version_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		page.Preview.ImageURL = dependencies.PublicOrigin.Resolve(previewcard.Path(snapshot.Public.Slug, version))
	}
	envelope := directrender.GateRenderRequest{
		Mode:            directrender.GateRenderMode,
		CanonicalOrigin: dependencies.PublicOrigin.String(),
		Slug:            snapshot.Public.Slug,
		Lng:             gateLng(snapshot.Public.Lng),
		PageTitle:       page.Title,
		FaviconHref:     page.FaviconHref,
		Preview:         page.Preview,
		Providers:       dependencies.GateProviders,
		Message:         gateMessage(request),
		Page:            projectPage,
	}
	result, err := dependencies.Renderer.RenderGate(ctx, envelope)
	if err != nil {
		logHTMLUnavailable(dependencies.Logger, request, "gate_render_failed", "")
		serveHTMLError(w, request, http.StatusServiceUnavailable)
		return
	}
	if rule := gateHTMLRejection(result.HTML, envelope, dependencies.PublicOrigin); rule != "" {
		logHTMLUnavailable(dependencies.Logger, request, "gate_html_rejected", rule)
		serveHTMLError(w, request, http.StatusServiceUnavailable)
		return
	}
	if err := writeWhileLeased(w, lease, func() { writeGate(w, request, result.HTML) }); err != nil {
		logHTMLUnavailable(dependencies.Logger, request, "lease_hook_failed", "")
		serveHTMLError(w, request, http.StatusServiceUnavailable)
	}
}

func writeGate(w http.ResponseWriter, request *http.Request, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, noarchive")
	w.Header().Set("Content-Security-Policy", publicformat.BaseCSP)
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		if _, writeErr := w.Write(page); writeErr != nil {
			return
		}
	}
}

func gateExpectedAnchors(envelope directrender.GateRenderRequest, origin publicresume.PublicOrigin) map[string]string {
	anchors := make(map[string]string, len(envelope.Providers)+1)
	suffix := ""
	if envelope.Page != "" {
		suffix = "&page=" + envelope.Page
	}
	for _, provider := range envelope.Providers {
		anchors["/api/v1/auth/"+provider+"/start?purpose=view&slug="+envelope.Slug+suffix] = "gate-provider"
	}
	anchors[origin.Resolve("/")] = "gate-home"
	return anchors
}

var gateImageMetaKeys = []metaKey{
	{"property", "og:image"},
	{"property", "og:image:type"},
	{"property", "og:image:width"},
	{"property", "og:image:height"},
	{"property", "og:image:alt"},
	{"name", "twitter:image"},
	{"name", "twitter:image:alt"},
}

func newGateHeadMeta(resume publicresume.PublicResume, preview previewmeta.Meta, origin publicresume.PublicOrigin) *headMeta {
	meta := newHeadMeta(resume, preview, origin)
	if preview.ImageURL != "" {
		return meta
	}
	for _, key := range gateImageMetaKeys {
		delete(meta.expected, key)
	}
	meta.optional = map[metaKey]bool{{"name", "twitter:card"}: true}
	return meta
}

func gateHTMLRejection(source []byte, envelope directrender.GateRenderRequest, origin publicresume.PublicOrigin) string {
	if len(source) == 0 || len(source) > 2_097_152 {
		return "size"
	}
	document, err := html.Parse(strings.NewReader(string(source)))
	if err != nil || !hasHTMLDoctype(document) {
		return "doctype"
	}

	previewResume := publicresume.PublicResume{Slug: envelope.Slug}
	meta := newGateHeadMeta(previewResume, envelope.Preview, origin)
	wantAnchors := gateExpectedAnchors(envelope, origin)

	var htmlRoot, title, canonical, main *html.Node
	var scriptCount, mainCount, styleCount, stylesheets, favicons, skipLinks, anchorCount int
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
			if node.Data == "html" && htmlRoot == nil {
				htmlRoot = node
			}
			if forbiddenResourceElement(node.Data) || node.Data == "img" || node.Data == "form" {
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
				styleCount++
				if styleCount > 1 || unsafeInlineStyle(textNode(node)) {
					reject("style_element")
					return
				}
			case "a":
				href := attribute(node, "href")
				if attributeCount(node, "href") != 1 || attributeCount(node, "class") > 1 || len(node.Attr) != 1+attributeCount(node, "class") {
					reject("anchor_href")
					return
				}
				if href == "#public-gate" {
					if len(node.Attr) != 1 || skipLinks != 0 || textNode(node) != "Skip to content" {
						reject("skip_link")
						return
					}
					skipLinks++
					break
				}
				wantClass, expected := wantAnchors[href]
				if !expected {
					reject("anchor_unexpected")
					return
				}
				if attributeCount(node, "class") != 1 || attribute(node, "class") != wantClass {
					reject("anchor_class")
					return
				}
				anchorCount++
			case "title":
				if title != nil || len(node.Attr) != 0 || textNode(node) != envelope.PageTitle {
					reject("title")
					return
				}
				title = node
			case "link":
				if relHasToken(attribute(node, "rel"), "icon") {
					if envelope.FaviconHref == "" || favicons != 0 || len(node.Attr) != 2 || attributeCount(node, "rel") != 1 ||
						attribute(node, "rel") != "icon" || attributeCount(node, "href") != 1 || attribute(node, "href") != envelope.FaviconHref {
						reject("favicon")
						return
					}
					favicons++
					break
				}
				if attribute(node, "rel") == "stylesheet" {
					if !versionedAsset(attribute(node, "href"), "/_nuxt/assets/print-fonts.css") ||
						len(node.Attr) != 2 || attributeCount(node, "rel") != 1 || attributeCount(node, "href") != 1 {
						reject("stylesheet")
						return
					}
					stylesheets++
					break
				}
				if !relHasToken(attribute(node, "rel"), "canonical") || canonical != nil || len(node.Attr) != 2 ||
					attributeCount(node, "rel") != 1 || attribute(node, "rel") != "canonical" ||
					attributeCount(node, "href") != 1 || attribute(node, "href") != origin.Resolve("/"+envelope.Slug) {
					reject("canonical")
					return
				}
				canonical = node
			case "main":
				mainCount++
				if attribute(node, "id") == "public-gate" {
					if main != nil || len(node.Attr) != 1 || attributeCount(node, "id") != 1 {
						reject("main")
						return
					}
					main = node
				}
			case "script":
				scriptCount++
				reject("script_element")
				return
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
	if htmlRoot == nil || attribute(htmlRoot, "lang") != envelope.Lng {
		return "lang"
	}
	if envelope.FaviconHref != "" && favicons != 1 {
		return "favicon"
	}
	if title == nil || canonical == nil || main == nil || mainCount != 1 || stylesheets != 1 || scriptCount != 0 {
		return "required_elements"
	}
	if anchorCount != len(wantAnchors) {
		return "anchor_count"
	}
	return ""
}
