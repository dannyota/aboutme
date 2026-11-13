package publicapi

import (
	"strings"

	"golang.org/x/net/html"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// format-detection stops Safari auto-linking digit runs, such as date ranges, into tel: links.
const formatDetectionContent = "telephone=no, date=no, address=no, email=no"

func publicRenderRequest(resume publicresume.PublicResume, discoverable bool, page publicPage, origin publicresume.PublicOrigin) directrender.PublicRenderRequest {
	return directrender.PublicRenderRequest{
		PublicResume:     resume,
		Mode:             directrender.PublicRenderMode,
		CanonicalOrigin:  origin.String(),
		DiscoveryEnabled: discoverable,
		PageTitle:        page.Title,
		FaviconHref:      page.FaviconHref,
		Preview:          page.Preview,
		JoinInvite:       page.JoinInvite,
		ProjectLinks:     page.ProjectLinks,
		ProjectsURL:      page.ProjectsURL,
	}
}

type metaKey struct{ attribute, name string }

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

type headMeta struct {
	expected                           map[metaKey]string
	optional                           map[metaKey]bool
	seen                               map[metaKey]int
	charset, viewport, formatDetection int
}

func newHeadMeta(resume publicresume.PublicResume, preview previewmeta.Meta, origin publicresume.PublicOrigin) *headMeta {
	return &headMeta{expected: expectedPreviewMeta(resume, preview, origin), seen: map[metaKey]int{}}
}

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

func (m *headMeta) finish() string {
	if m.formatDetection > 1 {
		return "meta_format_detection"
	}
	if m.charset != 1 || m.viewport != 1 {
		return "required_elements"
	}
	for _, key := range previewMetaKeys {
		if _, expected := m.expected[key]; !expected || m.seen[key] == 1 {
			continue
		}
		if m.optional[key] && m.seen[key] == 0 {
			continue
		}
		return "meta_missing"
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

func metaRule(key metaKey) string {
	return "meta_" + strings.NewReplacer(":", "_").Replace(key.name)
}
