package publicformat

import (
	"bytes"
	"errors"
	"sort"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

const (
	// SitemapFormatVersion identifies the sitemap byte format.
	SitemapFormatVersion = 1
	// RobotsFormatVersion identifies the robots.txt byte format.
	RobotsFormatVersion = 1
	// LLMSFormatVersion identifies the llms.txt byte format.
	LLMSFormatVersion = 1
)

// Sitemap encodes the sorted discoverable slugs as XML.
func Sitemap(origin publicresume.PublicOrigin, slugs []string) ([]byte, error) {
	if origin.String() == "" {
		return nil, errors.New("public origin is required")
	}
	sorted := sortedSlugs(slugs)
	var out bytes.Buffer
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, slug := range sorted {
		out.WriteString("  <url><loc>")
		if err := xmlEscape(&out, origin.Resolve("/"+slug)); err != nil {
			return nil, err
		}
		out.WriteString("</loc></url>\n")
	}
	out.WriteString("</urlset>\n")
	return out.Bytes(), nil
}

// Robots encodes the canonical robots.txt response.
func Robots(origin publicresume.PublicOrigin) []byte {
	return []byte("User-agent: *\nAllow: /\nSitemap: " + origin.Resolve("/sitemap.xml") + "\n")
}

// LLMS encodes the sorted discoverable slugs as llms.txt.
func LLMS(origin publicresume.PublicOrigin, slugs []string) ([]byte, error) {
	if origin.String() == "" {
		return nil, errors.New("public origin is required")
	}
	var out bytes.Buffer
	out.WriteString("# aboutme resumes\n")
	for _, slug := range sortedSlugs(slugs) {
		out.WriteString("- ")
		out.WriteString(origin.Resolve("/" + slug))
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func sortedSlugs(slugs []string) []string {
	sorted := append([]string{}, slugs...)
	sort.Strings(sorted)
	return sorted
}

func xmlEscape(out *bytes.Buffer, value string) error {
	for _, r := range value {
		switch r {
		case '&':
			out.WriteString("&amp;")
		case '<':
			out.WriteString("&lt;")
		case '>':
			out.WriteString("&gt;")
		case '\'':
			out.WriteString("&apos;")
		case '"':
			out.WriteString("&quot;")
		default:
			out.WriteRune(r)
		}
	}
	return nil
}
