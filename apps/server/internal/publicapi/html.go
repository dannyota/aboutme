package publicapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

const htmlFormatVersion = 1

// HTMLDependencies contains the dependencies for public HTML responses.
type HTMLDependencies struct {
	Reader         *publicresume.Reader
	Cache          *publiccache.Cache
	Renderer       *directrender.Client
	PublicOrigin   publicresume.PublicOrigin
	AppDigest      string
	RendererDigest string
	// Logger receives one closed, content-free line per 503. Nil disables it.
	Logger *slog.Logger
}

// NewHTMLHandler creates the handler for public resume HTML pages.
func NewHTMLHandler(dependencies HTMLDependencies) (http.Handler, error) {
	if dependencies.Reader == nil || dependencies.Cache == nil || dependencies.Renderer == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" || dependencies.RendererDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicHTMLGetOrHead(w, request) {
			return
		}
		slug := strings.TrimPrefix(request.URL.Path, "/")
		snapshot, lease, err := dependencies.Reader.ReadResume(request.Context(), slug, publicstate.RepresentationHTML)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			if errors.Is(err, publicresume.ErrNotFound) {
				serveHTMLError(w, request, http.StatusNotFound)
				return
			}
			logHTMLUnavailable(dependencies.Logger, request, "read_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()

		variant := publiccache.Variant("nondiscoverable")
		if snapshot.DiscoveryEnabled {
			variant = "discoverable"
		}
		key := publiccache.Key{
			RouteClass:     "resume",
			Representation: publicstate.RepresentationHTML,
			Variant:        variant,
			ResumeID:       snapshot.ResumeID,
			Generation:     snapshot.Revision,
			FormatVersion:  htmlFormatVersion,
			AppDigest:      dependencies.AppDigest,
			RendererDigest: dependencies.RendererDigest,
		}
		if cached, ok := dependencies.Cache.Get(key); ok {
			SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}.ServeHTTP(w, request)
			return
		}

		jsonLD, err := publicformat.JSONLD(snapshot.Public, dependencies.PublicOrigin, snapshot.DiscoveryEnabled)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "jsonld_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
		result, err := dependencies.Renderer.Render(lease.Context(), directrender.PublicRenderRequest{
			PublicResume:     snapshot.Public,
			Mode:             directrender.PublicRenderMode,
			CanonicalOrigin:  dependencies.PublicOrigin.String(),
			DiscoveryEnabled: snapshot.DiscoveryEnabled,
		})
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "render_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		if rule := publicHTMLRejection(result.HTML, snapshot.Public, dependencies.PublicOrigin, jsonLD, snapshot.DiscoveryEnabled); rule != "" {
			logHTMLUnavailable(dependencies.Logger, request, "html_rejected", rule)
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		extra := make(http.Header)
		extra.Set("Content-Security-Policy", jsonLD.CSP)
		if !snapshot.DiscoveryEnabled {
			extra.Set("X-Robots-Tag", "noindex, noarchive")
		}
		response, err := NewSelectedResponse(http.StatusOK, "text/html; charset=utf-8", "no-cache, must-revalidate", result.HTML, extra)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "response_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		response.ServeHTTP(w, request)
	}), nil
}

func publicHTMLGetOrHead(w http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	serveHTMLError(w, request, http.StatusMethodNotAllowed)
	return false
}

// logHTMLUnavailable records why a public page returned 503 with closed values
// only: a reason and, for rejected HTML, the rule name. It never logs the slug
// or any resume content.
func logHTMLUnavailable(logger *slog.Logger, request *http.Request, reason, rule string) {
	if logger == nil {
		return
	}
	attrs := []any{"reason", reason}
	if rule != "" {
		attrs = append(attrs, "rule", rule)
	}
	logger.WarnContext(request.Context(), "publicapi: html unavailable", attrs...)
}

func serveHTMLError(w http.ResponseWriter, request *http.Request, status int) {
	label := map[int]string{
		http.StatusBadRequest:         "Bad request",
		http.StatusNotFound:           "Not found",
		http.StatusMethodNotAllowed:   "Method not allowed",
		http.StatusServiceUnavailable: "Temporarily unavailable",
	}[status]
	body := []byte(fmt.Sprintf("<!doctype html>\n<html lang=\"en\">\n  <head>\n    <meta charset=\"utf-8\" />\n    <title>%s</title>\n  </head>\n  <body>\n    <a href=\"#main\">Skip to content</a>\n    <main id=\"main\"><h1>%s</h1></main>\n  </body>\n</html>\n", label, label))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	if request.Method != http.MethodHead {
		if _, err := w.Write(body); err != nil {
			return
		}
	}
}

func validPublicHTML(source []byte, resume publicresume.PublicResume, origin publicresume.PublicOrigin, jsonLD publicformat.JSONLDResult, discoverable bool) bool {
	return publicHTMLRejection(source, resume, origin, jsonLD, discoverable) == ""
}

// publicHTMLRejection returns "" for renderer output that passes every public
// HTML rule, or the closed name of the first rule it breaks. The name never
// carries resume content, so it is safe to log.
func publicHTMLRejection(source []byte, resume publicresume.PublicResume, origin publicresume.PublicOrigin, jsonLD publicformat.JSONLDResult, discoverable bool) string {
	if len(source) == 0 || len(source) > 2_097_152 {
		return "size"
	}
	document, err := html.Parse(strings.NewReader(string(source)))
	if err != nil || !hasHTMLDoctype(document) {
		return "doctype"
	}
	var title, canonical, main *html.Node
	var scriptCount, externalScripts, dataScripts, mainCount, images, skipLinks, charsetMeta, viewportMeta int
	var ogImageMeta, ogImageWidthMeta, ogImageHeightMeta, twitterCardMeta, twitterImageMeta int
	imageURL := origin.Resolve("/api/v1/public/resumes/" + resume.Slug + "/og.png")
	stylesheets := map[string]bool{}
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
				if len(node.Attr) == 1 && attributeCount(node, "charset") == 1 && attribute(node, "charset") == "utf-8" {
					charsetMeta++
					break
				}
				if len(node.Attr) == 2 && attributeCount(node, "name") == 1 && attribute(node, "name") == "viewport" && attributeCount(node, "content") == 1 && attribute(node, "content") == "width=device-width, initial-scale=1" {
					viewportMeta++
					break
				}
				if len(node.Attr) == 2 && attributeCount(node, "property") == 1 && attributeCount(node, "content") == 1 {
					switch attribute(node, "property") {
					case "og:image":
						if attribute(node, "content") != imageURL {
							reject("meta_og_image")
							return
						}
						ogImageMeta++
					case "og:image:width":
						if attribute(node, "content") != "1200" {
							reject("meta_og_image_size")
							return
						}
						ogImageWidthMeta++
					case "og:image:height":
						if attribute(node, "content") != "630" {
							reject("meta_og_image_size")
							return
						}
						ogImageHeightMeta++
					default:
						reject("meta_unknown")
						return
					}
					break
				}
				if len(node.Attr) == 2 && attributeCount(node, "name") == 1 && attributeCount(node, "content") == 1 {
					switch attribute(node, "name") {
					case "twitter:card":
						if attribute(node, "content") != "summary_large_image" {
							reject("meta_twitter")
							return
						}
						twitterCardMeta++
					case "twitter:image":
						if attribute(node, "content") != imageURL {
							reject("meta_twitter")
							return
						}
						twitterImageMeta++
					default:
						reject("meta_unknown")
						return
					}
					break
				}
				reject("meta_unknown")
				return
			case "style":
				reject("style_element")
				return
			case "a":
				href := attribute(node, "href")
				if attributeCount(node, "href") != 1 {
					reject("anchor_href")
					return
				}
				if href == "#public-resume" {
					if skipLinks != 0 || textNode(node) != "Skip to content" {
						reject("skip_link")
						return
					}
					skipLinks++
				} else if !allowedPublicAnchor(href) {
					reject("anchor_scheme")
					return
				}
			case "title":
				if title != nil || len(node.Attr) != 0 || textNode(node) != resume.Document.PersonalDetails.FullName+" — Resume" {
					reject("title")
					return
				}
				title = node
			case "link":
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
					if main != nil || len(node.Attr) != 2 || attributeCount(node, "id") != 1 || attributeCount(node, "data-revision") != 1 || attribute(node, "data-revision") != resume.Revision {
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
	if title == nil || canonical == nil || main == nil || mainCount != 1 || skipLinks != 1 || charsetMeta != 1 || viewportMeta != 1 || externalScripts != 1 || ogImageMeta != 1 || ogImageWidthMeta != 1 || ogImageHeightMeta != 1 || twitterCardMeta != 1 || twitterImageMeta != 1 {
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

func hasHTMLDoctype(node *html.Node) bool {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.DoctypeNode && strings.EqualFold(child.Data, "html") {
			return true
		}
	}
	return false
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
