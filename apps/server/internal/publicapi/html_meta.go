package publicapi

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// formatDetectionContent stops Safari from auto-linking digit runs, such as
// date ranges, into tel: links.
const formatDetectionContent = "telephone=no, date=no, address=no, email=no"

// publicRenderRequest is the closed render input for one admitted public
// resume and the head its page must carry.
func publicRenderRequest(resume publicresume.PublicResume, discoverable bool, page publicPage, origin publicresume.PublicOrigin) directrender.PublicRenderRequest {
	return directrender.PublicRenderRequest{
		PublicResume:     resume,
		Mode:             directrender.PublicRenderMode,
		CanonicalOrigin:  origin.String(),
		DiscoveryEnabled: discoverable,
		PageTitle:        page.Title,
		FaviconHref:      page.FaviconHref,
		Preview:          page.Preview,
	}
}

// metaKey names one head meta element by its naming attribute ("name" or
// "property") and that attribute's value.
type metaKey struct{ attribute, name string }

// previewMetaKeys is every preview meta element a live resume page may carry,
// in the order of docs/design/link-previews.md, "Page head".
var previewMetaKeys = []metaKey{
	{"name", "description"},
	{"property", "og:type"},
	{"property", "og:site_name"},
	{"property", "og:title"},
	{"property", "og:description"},
	{"property", "og:url"},
	{"property", "og:locale"},
	{"property", "og:image"},
	{"property", "og:image:type"},
	{"property", "og:image:width"},
	{"property", "og:image:height"},
	{"property", "og:image:alt"},
	{"name", "twitter:card"},
	{"name", "twitter:image"},
	{"name", "twitter:image:alt"},
}

// expectedPreviewMeta is the exact content of each preview meta element the
// page must carry once. og:locale is absent when the language maps to none.
func expectedPreviewMeta(resume publicresume.PublicResume, preview previewmeta.Meta, origin publicresume.PublicOrigin) map[metaKey]string {
	imageURL := preview.ImageURL
	if imageURL == "" {
		imageURL = origin.Resolve("/api/v1/public/resumes/" + resume.Slug + "/og.png")
	}
	expected := map[metaKey]string{
		{"name", "description"}:         preview.Description,
		{"property", "og:type"}:         "profile",
		{"property", "og:site_name"}:    previewmeta.SiteName,
		{"property", "og:title"}:        preview.Title,
		{"property", "og:description"}:  preview.Description,
		{"property", "og:url"}:          origin.Resolve("/" + resume.Slug),
		{"property", "og:image"}:        imageURL,
		{"property", "og:image:type"}:   "image/png",
		{"property", "og:image:width"}:  "1200",
		{"property", "og:image:height"}: "630",
		{"property", "og:image:alt"}:    preview.ImageAlt,
		{"name", "twitter:card"}:        "summary_large_image",
		{"name", "twitter:image"}:       imageURL,
		{"name", "twitter:image:alt"}:   preview.ImageAlt,
	}
	if preview.Locale != "" {
		expected[metaKey{"property", "og:locale"}] = preview.Locale
	}
	return expected
}

// headMeta checks every meta element of a page: the charset, the viewport, at
// most one format-detection element, and each expected preview element
// exactly once with the exact value Go computed.
type headMeta struct {
	expected                           map[metaKey]string
	seen                               map[metaKey]int
	charset, viewport, formatDetection int
}

func newHeadMeta(resume publicresume.PublicResume, preview previewmeta.Meta, origin publicresume.PublicOrigin) *headMeta {
	return &headMeta{expected: expectedPreviewMeta(resume, preview, origin), seen: map[metaKey]int{}}
}

// visit returns the closed rule a meta element breaks, or "".
func (m *headMeta) visit(node *html.Node) string {
	if len(node.Attr) == 1 && attributeCount(node, "charset") == 1 && attribute(node, "charset") == "utf-8" {
		m.charset++
		return ""
	}
	if len(node.Attr) != 2 || attributeCount(node, "content") != 1 {
		return "meta_unknown"
	}
	var key metaKey
	switch {
	case attributeCount(node, "name") == 1:
		key = metaKey{"name", attribute(node, "name")}
	case attributeCount(node, "property") == 1:
		key = metaKey{"property", attribute(node, "property")}
	default:
		return "meta_unknown"
	}
	content := attribute(node, "content")
	switch key {
	case metaKey{"name", "viewport"}:
		if content != "width=device-width, initial-scale=1" {
			return "meta_unknown"
		}
		m.viewport++
		return ""
	case metaKey{"name", "format-detection"}:
		// At most one; zero keeps HTML from a renderer without the tag valid.
		if content != formatDetectionContent {
			return "meta_format_detection"
		}
		m.formatDetection++
		return ""
	}
	want, expected := m.expected[key]
	if !expected {
		if knownPreviewMeta(key) {
			return metaRule(key)
		}
		return "meta_unknown"
	}
	if content != want {
		return metaRule(key)
	}
	m.seen[key]++
	if m.seen[key] > 1 {
		return "meta_repeated"
	}
	return ""
}

// finish returns the closed rule the page's meta elements break as a whole,
// or "".
func (m *headMeta) finish() string {
	if m.formatDetection > 1 {
		return "meta_format_detection"
	}
	if m.charset != 1 || m.viewport != 1 {
		return "required_elements"
	}
	for _, key := range previewMetaKeys {
		if _, expected := m.expected[key]; expected && m.seen[key] != 1 {
			return "meta_missing"
		}
	}
	return ""
}

func knownPreviewMeta(key metaKey) bool {
	for _, known := range previewMetaKeys {
		if known == key {
			return true
		}
	}
	return false
}

// metaRule names the rule for one preview meta element. Its name comes from
// the closed previewMetaKeys list, so the rule never carries resume content.
func metaRule(key metaKey) string {
	return "meta_" + strings.NewReplacer(":", "_").Replace(key.name)
}
