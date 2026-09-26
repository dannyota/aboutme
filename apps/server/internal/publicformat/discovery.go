package publicformat

import (
	"bytes"
	"errors"
	"sort"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

const (
	// SitemapFormatVersion identifies the sitemap byte format.
	SitemapFormatVersion = 3
	// RobotsFormatVersion identifies the robots.txt byte format.
	RobotsFormatVersion = 2
	// LLMSFormatVersion identifies the llms.txt byte format.
	LLMSFormatVersion = 3
)

// sitePages are the static site pages every sitemap and llms.txt lists.
var sitePages = []struct{ path, title string }{
	{"/", "Home"},
	{"/privacy", "Privacy policy"},
	{"/terms", "Terms of service"},
	{"/templates", "Resume templates"},
}

// templateIDs are the template presets, each with a gallery page at
// /templates/{id}. A test keeps the list equal to packages/schema/templates.
var templateIDs = []string{
	"academic-dense", "ats-plain", "classic-serif", "consulting-formal", "creative-accent",
	"designer-tag", "editorial-wide", "elegant-serif-two", "engineer-compact", "executive-band",
	"government-formal", "graduate-friendly", "high-contrast", "international-lang", "minimal-air",
	"modern-sidebar", "mono-print", "nordic-muted", "one-page-tight", "startup-bold",
}

// repositoryURL is the project's public source code.
const repositoryURL = "https://github.com/dannyota/aboutme"

// crawlerHiddenPages are the sign-in and account pages crawlers skip. Each is
// matched exactly, with or without a query, so a resume slug that merely
// starts with the same letters stays crawlable.
var crawlerHiddenPages = []string{"authorize", "forgot-password", "login", "register", "reset-password", "verify-email"}

// Sitemap encodes the static site pages, which include the template gallery,
// then each template page, then the sorted discoverable slugs, as XML. Resumes
// with discovery off never appear.
func Sitemap(origin publicresume.PublicOrigin, slugs []string) ([]byte, error) {
	if origin.String() == "" {
		return nil, errors.New("public origin is required")
	}
	paths := make([]string, 0, len(sitePages)+len(templateIDs)+len(slugs))
	for _, page := range sitePages {
		paths = append(paths, page.path)
	}
	for _, id := range templateIDs {
		paths = append(paths, "/templates/"+id)
	}
	for _, slug := range sortedSlugs(slugs) {
		paths = append(paths, "/"+slug)
	}
	var out bytes.Buffer
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, path := range paths {
		out.WriteString("  <url><loc>")
		if err := xmlEscape(&out, origin.Resolve(path)); err != nil {
			return nil, err
		}
		out.WriteString("</loc></url>\n")
	}
	out.WriteString("</urlset>\n")
	return out.Bytes(), nil
}

// Robots encodes the canonical robots.txt response. Crawlers may read every
// page except the API, the signed-in app, and the sign-in pages. Under /api
// only the share image and the preview cards stay open, because social cards
// fetch them; PDFs and JSON of resumes with discovery off must not be
// crawled.
func Robots(origin publicresume.PublicOrigin) []byte {
	var out bytes.Buffer
	out.WriteString("User-agent: *\nAllow: /\nAllow: /api/v1/public/resumes/*/og.png$\nAllow: /api/v1/public/resumes/*/og/\nDisallow: /api/\nDisallow: /app/\n")
	for _, page := range crawlerHiddenPages {
		out.WriteString("Disallow: /" + page + "$\nDisallow: /" + page + "?\n")
	}
	out.WriteString("Sitemap: " + origin.Resolve("/sitemap.xml") + "\n")
	return out.Bytes()
}

// LLMS encodes llms.txt (https://llmstxt.org): a title, a summary, the site
// pages and source code, then each discoverable resume's markdown.
func LLMS(origin publicresume.PublicOrigin, slugs []string) ([]byte, error) {
	if origin.String() == "" {
		return nil, errors.New("public origin is required")
	}
	var out bytes.Buffer
	out.WriteString("# aboutme\n\n")
	out.WriteString("> aboutme is a free, open-source resume builder. A resume is private until\n")
	out.WriteString("> you publish it; this file lists only the resumes their owners made discoverable.\n\n")
	out.WriteString("## Site\n\n")
	for _, page := range sitePages {
		out.WriteString("- [" + page.title + "](" + origin.Resolve(page.path) + ")\n")
	}
	out.WriteString("- [Source code](" + repositoryURL + ")\n")
	sorted := sortedSlugs(slugs)
	if len(sorted) > 0 {
		out.WriteString("\n## Resumes\n\n")
		for _, slug := range sorted {
			out.WriteString("- [" + slug + "](" + origin.Resolve("/"+slug+".md") + ")\n")
		}
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
