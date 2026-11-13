package publicapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/directrender"
)

// projectCorpusList and projectCorpusPage decode one corpus case's envelope.
func projectCorpusList(t *testing.T, tc projectCorpusCase) directrender.ProjectListRenderRequest {
	t.Helper()
	var request directrender.ProjectListRenderRequest
	decoder := json.NewDecoder(bytes.NewReader(tc.Envelope))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatal(err)
	}
	return request
}

func projectCorpusPage(t *testing.T, tc projectCorpusCase) directrender.ProjectRenderRequest {
	t.Helper()
	var request directrender.ProjectRenderRequest
	decoder := json.NewDecoder(bytes.NewReader(tc.Envelope))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatal(err)
	}
	return request
}

// TestProjectHTMLRejection_AcceptsTheCorpus proves both validators accept the
// renderer HTML of the shared corpus.
func TestProjectHTMLRejection_AcceptsTheCorpus(t *testing.T) {
	origin := mustPublicOrigin(t)
	for name, tc := range readProjectCorpus(t) {
		t.Run(name, func(t *testing.T) {
			var rule string
			if strings.HasPrefix(name, "list-") {
				rule = projectListHTMLRejection([]byte(tc.HTML), projectCorpusList(t, tc), origin)
			} else {
				rule = projectHTMLRejection([]byte(tc.HTML), projectCorpusPage(t, tc), origin)
			}
			if rule != "" {
				t.Fatalf("rejection = %q, want the corpus HTML accepted", rule)
			}
		})
	}
}

type projectMutation struct {
	name, old, replacement, rule string
}

func (m projectMutation) apply(source string) string {
	if m.old == "" {
		return m.replacement
	}
	return replaceOnce(source, m.old, m.replacement)
}

// TestProjectHTMLRejection_ProjectPageRules applies one mutation per rule to
// the every-field page and expects that rule's name.
func TestProjectHTMLRejection_ProjectPageRules(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-en-every-field"]
	request := projectCorpusPage(t, tc)
	external := `<a class="project-external" href="https://ada.example/demo" rel="noopener noreferrer nofollow">`
	for _, m := range []projectMutation{
		{"empty", "", "", "size"},
		{"oversized", "", strings.Repeat("x", 2_097_153), "size"},
		{"no doctype", "<!doctype html>", "", "doctype"},
		{"wrong lang", `<html lang="en" data-scheme="dark">`, `<html lang="vi" data-scheme="dark">`, "lang"},
		{"wrong scheme", `data-scheme="dark"`, `data-scheme="light"`, "color_scheme"},
		{"extra html attribute", `<html lang="en"`, `<html dir="ltr" lang="en"`, "html_attributes"},
		{"wrong title", "<title>Analytical Engine notes · Ada Lovelace</title>", "<title>Ada</title>", "title"},
		{"wrong canonical", `<link rel="canonical" href="https://aboutme.example/ada/projects/analytical-engine">`, `<link rel="canonical" href="https://aboutme.example/ada">`, "canonical"},
		{"missing favicon", `<link rel="icon" href="data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%2F%3E">`, "", "favicon"},
		{"profile type", `<meta property="og:type" content="website">`, `<meta property="og:type" content="profile">`, "meta_og_type"},
		{"resume og url", `<meta property="og:url" content="https://aboutme.example/ada/projects/analytical-engine">`, `<meta property="og:url" content="https://aboutme.example/ada">`, "meta_og_url"},
		{"missing description", `<meta name="description" content="Notes on the first mechanical general-purpose computer.">`, "", "meta_missing"},
		{"refresh", "</head>", `<meta http-equiv="refresh" content="0"></head>`, "meta_unknown"},
		{"second stylesheet", "</head>", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef"></head>`, "stylesheet"},
		{"preload", "</head>", `<link rel="preload" href="https://evil.example/x"></head>`, "link_element"},
		{"second style", "</head>", "<style></style></head>", "style_element"},
		{"unsafe style", "<style>/* project page style */</style>", "<style>body{background:url(https://evil.example/x)}</style>", "style_element"},
		{"missing style", "<style>/* project page style */</style>", "", "style_element"},
		{"script", "</head>", "<script>alert(1)</script></head>", "script_element"},
		{"iframe", "</main>", `<iframe></iframe></main>`, "resource_element"},
		{"image", "</main>", `<img src="https://evil.example/x.png" alt=""></main>`, "resource_element"},
		{"form", "</main>", "<form></form></main>", "resource_element"},
		{"event attribute", "<h1>", `<h1 onclick="x()">`, "event_attribute"},
		{"svg resource", `<svg class="public-mark" aria-hidden="true" focusable="false"></svg>`, `<svg class="public-mark"><image href="https://evil.example/x.png"></image></svg>`, "svg_element"},
		{"second main", "</main>", "</main><main></main>", "main"},
		{"main attribute", `<main id="public-project" class="public-measure project-page">`, `<main id="public-project" class="public-measure project-page" data-x="1">`, "main"},
		{"second h1", "</main>", "<h1>Again</h1></main>", "h1_count"},
		{"second title", "</main>", "<title>Again</title></main>", "title"},
		{"missing skip link", `<a href="#public-project">Skip to content</a>`, "", "skip_link"},
		{"second skip link", "</main>", `<a href="#public-project">Skip</a></main>`, "skip_link"},
		{"link to another resume", "</main>", `<a href="https://aboutme.example/grace">Grace</a></main>`, "anchor_unexpected"},
		{"link to another project", "</main>", `<a href="https://aboutme.example/ada/projects/note-g">Note G</a></main>`, "anchor_unexpected"},
		{"internal link attribute", `<p class="project-more"><a href="https://aboutme.example/ada/projects">`, `<p class="project-more"><a href="https://aboutme.example/ada/projects" target="_blank">`, "anchor_attributes"},
		{"credit text", "Built with aboutme.vn", "Tạo bằng aboutme.vn", "credit_link"},
		{"back label", `aria-label="Back to Ada Lovelace&#39;s resume"`, `aria-label="Back"`, "back_link"},
		{"missing back link", `<a class="public-back" href="https://aboutme.example/ada" aria-label="Back to Ada Lovelace&#39;s resume">← Ada Lovelace</a>`, "", "back_link"},
		{"external not in the request", external, `<a class="project-external" href="https://evil.example/demo" rel="noopener noreferrer nofollow">`, "external_link"},
		{"external repeated", "</main>", external + "Demo</a></main>", "external_link"},
		{"missing rel", external, `<a class="project-external" href="https://ada.example/demo">`, "external_rel"},
		{"weaker rel", external, `<a class="project-external" href="https://ada.example/demo" rel="noopener">`, "external_rel"},
		{"description differs", "<li>Note G</li>", "<li>Note H</li>", "description_content"},
		{"description script text", "<li>Note G</li></ul></div>", "<li>Note G</li></ul>&lt;script&gt;</div>", "description_content"},
		{"second description region", "</main>", `<div class="project-description"></div></main>`, "description_region"},
		{"missing description region", `<div class="project-description"><p>Translated and annotated <a href="https://en.wikipedia.org/wiki/Analytical_engine" rel="noopener noreferrer">the memoir</a>.</p><ul><li>Note G</li></ul></div>`, "", "description_region"},
		{"description links outside the region", `<div class="project-description">`, "<div>", "anchor_unexpected"},
	} {
		t.Run(m.name, func(t *testing.T) {
			if got := projectHTMLRejection([]byte(m.apply(tc.HTML)), request, origin); got != m.rule {
				t.Fatalf("rejection = %q, want %q", got, m.rule)
			}
		})
	}
}

// TestProjectHTMLRejection_PageWithoutDescription proves a description region
// or description meta on a project with no description is rejected.
func TestProjectHTMLRejection_PageWithoutDescription(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-en-name-only"]
	request := projectCorpusPage(t, tc)
	for _, m := range []projectMutation{
		{"description region", "<h1>Note G</h1>", `<h1>Note G</h1><div class="project-description"></div>`, "description_region"},
		{"description meta", "</head>", `<meta name="description" content="x"></head>`, "meta_description"},
		{"favicon without one", "</head>", `<link rel="icon" href="data:image/svg+xml,x"></head>`, "favicon"},
		{"entry link not in the request", "<h1>Note G</h1>", `<h1>Note G</h1><a class="project-external" href="https://ada.example/notes" rel="noopener noreferrer nofollow">Link</a>`, "external_link"},
		{"other projects link to another project", "<h1>Note G</h1>", `<h1>Note G</h1><a href="https://aboutme.example/ada/projects/analytical-engine">x</a>`, "anchor_unexpected"},
	} {
		t.Run(m.name, func(t *testing.T) {
			if got := projectHTMLRejection([]byte(m.apply(tc.HTML)), request, origin); got != m.rule {
				t.Fatalf("rejection = %q, want %q", got, m.rule)
			}
		})
	}
}

// TestProjectHTMLRejection_DescriptionTreeIgnoresComments proves the
// description is compared as a parsed tree with comments ignored, not as
// bytes.
func TestProjectHTMLRejection_DescriptionTreeIgnoresComments(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-en-every-field"]
	request := projectCorpusPage(t, tc)
	for name, source := range map[string]string{
		"comments":         replaceOnce(tc.HTML, "<li>Note G</li>", "<!--[--><li>Note<!----> G</li><!--]-->"),
		"attribute order":  replaceOnce(tc.HTML, `<a href="https://en.wikipedia.org/wiki/Analytical_engine" rel="noopener noreferrer">`, `<a rel="noopener noreferrer" href="https://en.wikipedia.org/wiki/Analytical_engine">`),
		"entity spelling":  replaceOnce(tc.HTML, "Translated and annotated", "Translated &amp; annotated"),
		"class order hook": replaceOnce(tc.HTML, `<div class="project-description">`, `<div class="prose project-description">`),
	} {
		t.Run(name, func(t *testing.T) {
			got := projectHTMLRejection([]byte(source), request, origin)
			want := ""
			if name == "entity spelling" {
				want = "description_content"
			}
			if got != want {
				t.Fatalf("rejection = %q, want %q", got, want)
			}
		})
	}
}

// TestProjectListHTMLRejection_ListRules applies list-only mutations.
func TestProjectListHTMLRejection_ListRules(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["list-vi-two-projects"]
	request := projectCorpusList(t, tc)
	row := `<a href="https://aboutme.example/ada/projects/difference-engine">Difference Engine study</a>`
	for _, m := range []projectMutation{
		{"project linked twice", "</main>", row + "</main>", "project_links"},
		{"project not linked", row, "Difference Engine study", "project_links"},
		{"description region", "</main>", `<div class="project-description"></div></main>`, "description_region"},
		{"description meta", "</head>", `<meta property="og:description" content="x"></head>`, "meta_og_description"},
		{"english credit", "Tạo bằng aboutme.vn", "Built with aboutme.vn", "credit_link"},
		{"english back label", `aria-label="Về CV của Ada Lovelace"`, `aria-label="Back to Ada Lovelace&#39;s resume"`, "back_link"},
		{"external link", "</main>", `<a class="project-external" href="https://github.com/ada" rel="noopener noreferrer nofollow">x</a></main>`, "external_link"},
		{"page title", "<title>Dự án · Ada Lovelace</title>", "<title>Projects · Ada Lovelace</title>", "title"},
	} {
		t.Run(m.name, func(t *testing.T) {
			if got := projectListHTMLRejection([]byte(m.apply(tc.HTML)), request, origin); got != m.rule {
				t.Fatalf("rejection = %q, want %q", got, m.rule)
			}
		})
	}
}

// TestProjectHTMLRejection_ImageFallsBackToTheResumeShareImage proves a page
// with stored cards off names the resume's og.png, as the resume page does.
func TestProjectHTMLRejection_ImageFallsBackToTheResumeShareImage(t *testing.T) {
	origin := mustPublicOrigin(t)
	tc := readProjectCorpus(t)["project-en-name-only"]
	request := projectCorpusPage(t, tc)
	request.Preview.ImageURL = ""
	fallback := strings.ReplaceAll(tc.HTML, projectTestCardURL, origin.Resolve("/api/v1/public/resumes/ada/og.png"))
	if rule := projectHTMLRejection([]byte(fallback), request, origin); rule != "" {
		t.Fatalf("rejection = %q, want the og.png fallback accepted", rule)
	}
	if rule := projectHTMLRejection([]byte(tc.HTML), request, origin); rule != "meta_og_image" {
		t.Fatalf("rejection = %q, want meta_og_image for a card the request does not name", rule)
	}
}
