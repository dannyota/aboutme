package publicapi

import (
	"errors"
	"fmt"
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
		if err != nil || !validPublicHTML(result.HTML, snapshot.Public, dependencies.PublicOrigin, jsonLD, snapshot.DiscoveryEnabled) {
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
	if len(source) == 0 || len(source) > 2_097_152 {
		return false
	}
	document, err := html.Parse(strings.NewReader(string(source)))
	if err != nil || !hasHTMLDoctype(document) {
		return false
	}
	var title, canonical, main *html.Node
	var scriptCount, externalScripts, dataScripts, mainCount, images, skipLinks, charsetMeta, viewportMeta int
	var ogImageMeta, ogImageWidthMeta, ogImageHeightMeta, twitterCardMeta, twitterImageMeta int
	imageURL := origin.Resolve("/api/v1/public/resumes/" + resume.Slug + "/og.png")
	valid := true
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if !valid {
			return
		}
		if node.Type == html.ElementNode {
			if forbiddenResourceElement(node.Data) || hasUnexpectedResourceAttribute(node) {
				valid = false
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
							valid = false
							return
						}
						ogImageMeta++
					case "og:image:width":
						if attribute(node, "content") != "1200" {
							valid = false
							return
						}
						ogImageWidthMeta++
					case "og:image:height":
						if attribute(node, "content") != "630" {
							valid = false
							return
						}
						ogImageHeightMeta++
					default:
						valid = false
						return
					}
					break
				}
				if len(node.Attr) == 2 && attributeCount(node, "name") == 1 && attributeCount(node, "content") == 1 {
					switch attribute(node, "name") {
					case "twitter:card":
						if attribute(node, "content") != "summary_large_image" {
							valid = false
							return
						}
						twitterCardMeta++
					case "twitter:image":
						if attribute(node, "content") != imageURL {
							valid = false
							return
						}
						twitterImageMeta++
					default:
						valid = false
						return
					}
					break
				}
				valid = false
				return
			case "style":
				valid = false
				return
			case "a":
				href := attribute(node, "href")
				if attributeCount(node, "href") != 1 {
					valid = false
					return
				}
				if href == "#public-resume" {
					if skipLinks != 0 || textNode(node) != "Skip to content" {
						valid = false
						return
					}
					skipLinks++
				} else if !allowedPublicAnchor(href) {
					valid = false
					return
				}
			case "title":
				if title != nil || len(node.Attr) != 0 || textNode(node) != resume.Document.PersonalDetails.FullName+" — Resume" {
					valid = false
					return
				}
				title = node
			case "link":
				if !relHasToken(attribute(node, "rel"), "canonical") || canonical != nil || len(node.Attr) != 2 || attributeCount(node, "rel") != 1 || attribute(node, "rel") != "canonical" || attributeCount(node, "href") != 1 || attribute(node, "href") != origin.Resolve("/"+resume.Slug) {
					valid = false
					return
				}
				canonical = node
			case "img":
				images++
				photo := resume.Document.PersonalDetails.Photo
				if photo == nil || attributeCount(node, "src") != 1 || attribute(node, "src") != photo.URL || attributeCount(node, "alt") != 1 || attribute(node, "alt") != "" {
					valid = false
					return
				}
			case "main":
				mainCount++
				if attribute(node, "id") == "public-resume" {
					if main != nil || len(node.Attr) != 2 || attributeCount(node, "id") != 1 || attributeCount(node, "data-revision") != 1 || attribute(node, "data-revision") != resume.Revision {
						valid = false
						return
					}
					main = node
				}
			case "script":
				scriptCount++
				if attributeCount(node, "src") == 1 {
					if len(node.Attr) != 2 || attribute(node, "src") != "/_nuxt/assets/public-resume.mjs" || attributeCount(node, "type") != 1 || attribute(node, "type") != "module" || textNode(node) != "" {
						valid = false
						return
					}
					externalScripts++
					return
				}
				if len(node.Attr) != 1 || attributeCount(node, "src") != 0 || !discoverable || attributeCount(node, "type") != 1 || attribute(node, "type") != "application/ld+json" || textNode(node) != string(jsonLD.JSON) {
					valid = false
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
	if !valid || title == nil || canonical == nil || main == nil || mainCount != 1 || skipLinks != 1 || charsetMeta != 1 || viewportMeta != 1 || externalScripts != 1 || ogImageMeta != 1 || ogImageWidthMeta != 1 || ogImageHeightMeta != 1 || twitterCardMeta != 1 || twitterImageMeta != 1 {
		return false
	}
	if resume.Document.PersonalDetails.Photo == nil && images != 0 {
		return false
	}
	if resume.Document.PersonalDetails.Photo != nil && images != 1 {
		return false
	}
	if discoverable {
		return scriptCount == 2 && dataScripts == 1
	}
	return scriptCount == 1 && dataScripts == 0 && jsonLD.JSON == nil && jsonLD.Script == nil && jsonLD.CSP == publicformat.BaseCSP
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

func hasUnexpectedResourceAttribute(node *html.Node) bool {
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if strings.HasPrefix(key, "on") {
			return true
		}
		switch key {
		case "action", "background", "data", "formaction", "ping", "poster", "srcset":
			return true
		case "href":
			if node.Data != "a" && node.Data != "link" {
				return true
			}
		case "src":
			if node.Data != "img" && node.Data != "script" {
				return true
			}
		case "style":
			if unsafeInlineStyle(attribute.Val) {
				return true
			}
		}
	}
	return false
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
