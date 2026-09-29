package publicapi

// The sign-in-to-view gate: the pass check every gated route shares, and the
// gate page itself, rendered by Nuxt from a closed envelope and validated by
// Go's own closed rule set, exactly as the resume page is
// (docs/design/viewer-analytics/sign-in-to-view.md "Gate", "Gated routes";
// AC-VIEW-002, AC-VIEW-003, AC-VIEW-007).

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/html"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/viewpass"
)

// hasValidPass reports whether request carries a __Host-view-pass cookie
// with a pass for resumeID at exactly passEpoch, not expired as of clock()
// (design "Pass cookie", "Gated routes"; AC-VIEW-003, AC-VIEW-007). A nil
// clock uses time.Now.
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

// gateMessage reads only the two closed ?signin= values the design names
// (canceled and failed); every other query, including any other signin
// value, becomes "none". The gate is never cached, so this is safe to read
// per request (design "Gate").
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

// gateLng is "vi" when the resume language is Vietnamese, else "en" (docs/design/viewer-analytics/sign-in-to-view.md
// "Gate").
func gateLng(resumeLng string) string {
	language, _, _ := strings.Cut(resumeLng, "-")
	if strings.EqualFold(language, "vi") {
		return "vi"
	}
	return "en"
}

// serveGate renders and serves the sign-in-to-view gate for a resume with no
// valid pass: status 200, Cache-Control: no-store, X-Robots-Tag: noindex,
// noarchive, and the resume page's CSP, never cached (design "Gate";
// AC-VIEW-002).
func serveGate(ctx context.Context, w http.ResponseWriter, request *http.Request, dependencies HTMLDependencies, snapshot publicresume.Snapshot) {
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, noarchive")
	w.Header().Set("Content-Security-Policy", publicformat.BaseCSP)
	w.Header().Set("Content-Length", strconv.Itoa(len(result.HTML)))
	w.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		if _, writeErr := w.Write(result.HTML); writeErr != nil {
			return
		}
	}
}

// gateExpectedAnchors is every href the gate page may link, from the
// envelope's own providers in order plus the home page (design "Gate").
func gateExpectedAnchors(envelope directrender.GateRenderRequest, origin publicresume.PublicOrigin) map[string]bool {
	anchors := make(map[string]bool, len(envelope.Providers)+1)
	for _, provider := range envelope.Providers {
		anchors["/api/v1/auth/"+provider+"/start?purpose=view&slug="+envelope.Slug] = true
	}
	anchors[origin.Resolve("/")] = true
	return anchors
}

// gateImageMetaKeys is every image meta element of the preview head. A gate
// with no card image URL carries none of them: the fallback share image is
// the resume itself, which the gate must not advertise (design "Gate";
// AC-VIEW-002).
var gateImageMetaKeys = []metaKey{
	{"property", "og:image"},
	{"property", "og:image:type"},
	{"property", "og:image:width"},
	{"property", "og:image:height"},
	{"property", "og:image:alt"},
	{"name", "twitter:image"},
	{"name", "twitter:image:alt"},
}

// newGateHeadMeta is newHeadMeta for the gate head. With no card image URL,
// every image element is unexpected, so a present one is rejected, and
// twitter:card may be absent.
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

// gateHTMLRejection returns "" for gate HTML that passes every closed rule,
// or the first rule's name it breaks. It never carries resume content, so
// the name is safe to log (design "Gate"; AC-VIEW-002).
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
				if attributeCount(node, "href") != 1 || len(node.Attr) != 1 {
					reject("anchor_href")
					return
				}
				if href == "#public-gate" {
					if skipLinks != 0 || textNode(node) != "Skip to content" {
						reject("skip_link")
						return
					}
					skipLinks++
					break
				}
				if !wantAnchors[href] {
					reject("anchor_unexpected")
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
