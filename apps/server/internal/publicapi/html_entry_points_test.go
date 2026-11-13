package publicapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const (
	firstProjectEntry  = "8c8b959a-0df4-59de-88d9-2ce01b88fb86"
	secondProjectEntry = "2ffd46bf-7cc3-5497-bd46-13fbe94653a4"
	corpusProjectsURL  = "https://aboutme.example/do-hoang-nam/projects"
	corpusFirstPage    = corpusProjectsURL + "/vn-address-kit"
	corpusSecondPage   = corpusProjectsURL + "/nextjs-perf-checklist"
	corpusBarLink      = `<a class="public-projects" href="` + corpusProjectsURL + `">Projects</a>`
)

type entryPointPage struct {
	resume       publicresume.PublicResume
	page         publicPage
	jsonLD       publicformat.JSONLDResult
	discoverable bool
}

func entryPointPageFor(t *testing.T, source resumeLinksSource) entryPointPage {
	t.Helper()
	snapshot := readResumeLinksSnapshot(t, source)
	jsonLD, err := publicformat.JSONLD(snapshot.Public, mustPublicOrigin(t), snapshot.DiscoveryEnabled)
	if err != nil {
		t.Fatal(err)
	}
	return entryPointPage{resume: snapshot.Public, page: resumeLinksPage(t, snapshot), jsonLD: jsonLD, discoverable: snapshot.DiscoveryEnabled}
}

func (p entryPointPage) rejection(t *testing.T, source string) string {
	t.Helper()
	return publicHTMLRejectionForPage([]byte(source), p.resume, p.page, mustPublicOrigin(t), p.jsonLD, p.discoverable)
}

func resumeLinksSourceNamed(t *testing.T, name string) resumeLinksSource {
	t.Helper()
	for _, source := range resumeLinksSources() {
		if source.name == name {
			return source
		}
	}
	t.Fatalf("no resume links source %q", name)
	return resumeLinksSource{}
}

func resumeLinksHTML(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(resumeLinksCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	var corpus resumeLinksCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, entry := range corpus.Cases {
		if entry.Name == name {
			if entry.HTML == "" {
				t.Fatalf("corpus case %s has no renderer HTML", name)
			}
			return entry.HTML
		}
	}
	t.Fatalf("corpus has no case %s", name)
	return ""
}

func titleAnchor(entryID, href, title string) string {
	return `<strong><a data-project-entry="` + entryID + `" href="` + href + `" style="color:var(--color-link);text-decoration:underline;">` + title + `</a></strong>`
}

func TestResumeLinksCorpusHTMLPassesTheResumePageValidator(t *testing.T) {
	for _, source := range resumeLinksSources() {
		t.Run(source.name, func(t *testing.T) {
			if rule := entryPointPageFor(t, source).rejection(t, resumeLinksHTML(t, source.name)); rule != "" {
				t.Fatalf("renderer HTML rejected by %q", rule)
			}
		})
	}
}

func TestResumePageValidatorRejectsBrokenEntryPoints(t *testing.T) {
	linked := entryPointPageFor(t, resumeLinksSourceNamed(t, "en-two-linked-entries"))
	html := resumeLinksHTML(t, "en-two-linked-entries")
	first := titleAnchor(firstProjectEntry, corpusFirstPage, "vn-address-kit")
	second := titleAnchor(secondProjectEntry, corpusSecondPage, "Next.js performance checklist")
	swapped := replaceOnce(replaceOnce(html, first, titleAnchor(firstProjectEntry, corpusSecondPage, "vn-address-kit")),
		second, titleAnchor(secondProjectEntry, corpusFirstPage, "Next.js performance checklist"))
	for _, test := range []struct{ name, html, want string }{
		{"swapped title destinations", swapped, "project_entry_link"},
		{"duplicated title anchor", replaceOnce(html, first, first+first), "project_entry_link"},
		{"missing title anchor", replaceOnce(html, first, "<strong>vn-address-kit</strong>"), "project_entry_link"},
		{"anchor for an entry outside the map", replaceOnce(html, `data-project-entry="`+firstProjectEntry+`"`, `data-project-entry="00000000-0000-4000-8000-000000000000"`), "project_entry_link"},
		{"title anchor with a class", replaceOnce(html, `<a data-project-entry="`+firstProjectEntry+`"`, `<a class="x" data-project-entry="`+firstProjectEntry+`"`), "project_entry_link"},
		{"title anchor with rel", replaceOnce(html, `<a data-project-entry="`+firstProjectEntry+`"`, `<a rel="noopener noreferrer" data-project-entry="`+firstProjectEntry+`"`), "project_entry_link"},
		{"title anchor outside strong", replaceOnce(html, first, strings.TrimSuffix(strings.TrimPrefix(first, "<strong>"), "</strong>")), "project_entry_link"},
		{"missing root marker", replaceOnce(html, ` data-project-links=""`, ""), "project_links_marker"},
		{"root marker with a value", replaceOnce(html, `data-project-links=""`, `data-project-links="2"`), "project_links_marker"},
		{"missing bar link with links", replaceOnce(html, corpusBarLink, ""), "projects_link"},
		{"bar link twice", replaceOnce(html, corpusBarLink, corpusBarLink+corpusBarLink), "projects_link"},
		{"bar link to a project page", replaceOnce(html, corpusBarLink, strings.Replace(corpusBarLink, corpusProjectsURL, corpusFirstPage, 1)), "projects_link"},
		{"bar link in the wrong language", replaceOnce(html, corpusBarLink, strings.Replace(corpusBarLink, ">Projects<", ">Dự án<", 1)), "projects_link"},
		{"bar link outside the actions", replaceOnce(replaceOnce(html, corpusBarLink, ""), `<span class="public-brand">`, `<span class="public-brand">`+corpusBarLink), "projects_link"},
		{"bar link with another attribute", replaceOnce(html, corpusBarLink, strings.Replace(corpusBarLink, `<a class`, `<a rel="nofollow" class`, 1)), "projects_link"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := linked.rejection(t, test.html); got != test.want {
				t.Fatalf("rule = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResumePageValidatorKeepsAuthoredLinksToProjectPages(t *testing.T) {
	linked := entryPointPageFor(t, resumeLinksSourceNamed(t, "en-two-linked-entries"))
	html := resumeLinksHTML(t, "en-two-linked-entries")
	paragraph := "<p>Vietnamese address library in TypeScript"
	for name, link := range map[string]string{
		"project page": `<a href="` + corpusSecondPage + `" rel="noopener noreferrer nofollow">checklist</a> `,
		"project list": `<a href="` + corpusProjectsURL + `">all projects</a> `,
	} {
		t.Run(name, func(t *testing.T) {
			candidate := replaceOnce(html, paragraph, "<p>"+link+strings.TrimPrefix(paragraph, "<p>"))
			if rule := linked.rejection(t, candidate); rule != "" {
				t.Fatalf("authored link rejected by %q", rule)
			}
		})
	}
	insecure := replaceOnce(html, paragraph, `<p><a href="http://aboutme.example/do-hoang-nam/projects/vn-address-kit">x</a>`+strings.TrimPrefix(paragraph, "<p>"))
	if rule := linked.rejection(t, insecure); rule != "anchor_scheme" {
		t.Fatalf("authored http link: rule = %q, want anchor_scheme", rule)
	}
}

func TestResumePageValidatorWithoutLinks(t *testing.T) {
	none := entryPointPageFor(t, resumeLinksSourceNamed(t, "en-no-available-project"))
	html := resumeLinksHTML(t, "en-no-available-project")
	download := `<a class="public-download"`
	for _, test := range []struct{ name, html, want string }{
		{"bar link with no links", replaceOnce(html, download, `<span class="public-actions">`+corpusBarLink+`</span>`+download), "projects_link"},
		{"title anchor with no links", replaceOnce(html, "</main>", titleAnchor(firstProjectEntry, corpusFirstPage, "x")+"</main>"), "project_entry_link"},
		{"missing root marker", replaceOnce(html, ` data-project-links=""`, ""), "project_links_marker"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := none.rejection(t, test.html); got != test.want {
				t.Fatalf("rule = %q, want %q", got, test.want)
			}
		})
	}
	withoutEntryPoints := none
	withoutEntryPoints.page.ProjectsURL = ""
	if got := withoutEntryPoints.rejection(t, html); got != "project_links_marker" {
		t.Fatalf("marker without projectsUrl: rule = %q, want project_links_marker", got)
	}
	if got := withoutEntryPoints.rejection(t, replaceOnce(html, ` data-project-links=""`, "")); got != "" {
		t.Fatalf("page without entry points rejected by %q", got)
	}
}

func untitledSecondEntry(title *string) resumeLinksSource {
	source := resumeLinksSources()[0]
	prepare := source.prepare
	source.prepare = func(entries []schema.ProjectEntry) {
		prepare(entries)
		entries[1].Title = title
	}
	return source
}

func TestResumePageValidatorUntitledLinkedEntry(t *testing.T) {
	html := resumeLinksHTML(t, "en-two-linked-entries")
	second := `<div class="entry-title">` + titleAnchor(secondProjectEntry, corpusSecondPage, "Next.js performance checklist") + `</div>`
	untitledHTML := replaceOnce(html, second, "<!---->")
	empty := ""
	for name, title := range map[string]*string{"absent title": nil, "empty title": &empty} {
		t.Run(name, func(t *testing.T) {
			untitled := entryPointPageFor(t, untitledSecondEntry(title))
			if len(untitled.page.ProjectLinks) != 2 || untitled.page.ProjectLinks[1].EntryID != secondProjectEntry {
				t.Fatalf("links = %+v, want the untitled entry kept in the map", untitled.page.ProjectLinks)
			}
			if rule := untitled.rejection(t, untitledHTML); rule != "" {
				t.Fatalf("linked untitled entry rejected by %q", rule)
			}
			if rule := untitled.rejection(t, html); rule != "project_entry_link" {
				t.Fatalf("anchor for an untitled entry: rule = %q, want project_entry_link", rule)
			}
		})
	}
}

func TestResumePageValidatorWhitespaceTitleIsTitled(t *testing.T) {
	html := resumeLinksHTML(t, "en-two-linked-entries")
	second := `<div class="entry-title">` + titleAnchor(secondProjectEntry, corpusSecondPage, "Next.js performance checklist") + `</div>`
	blank := "   "
	page := entryPointPageFor(t, untitledSecondEntry(&blank))
	if rule := page.rejection(t, replaceOnce(html, second, `<div class="entry-title">`+titleAnchor(secondProjectEntry, corpusSecondPage, blank)+`</div>`)); rule != "" {
		t.Fatalf("anchor for a whitespace-only title rejected by %q", rule)
	}
	if rule := page.rejection(t, replaceOnce(html, second, "<!---->")); rule != "project_entry_link" {
		t.Fatalf("whitespace-only title without an anchor: rule = %q, want project_entry_link", rule)
	}
}

func TestBlankNamedProjectLeavesTheJSONTheRenderRequestAndThePrintMap(t *testing.T) {
	source := resumeLinksSourceNamed(t, "en-two-linked-entries")
	source.blankName = "nextjs-perf-checklist"
	snapshot := readResumeLinksSnapshot(t, source)
	request := resumeLinksRequest(t, snapshot)
	if len(request.ProjectLinks) != 1 || request.ProjectLinks[0].EntryID != firstProjectEntry || request.ProjectsURL != corpusProjectsURL {
		t.Fatalf("render request links = %+v, want only the named project", request.ProjectLinks)
	}
	response, err := NewPublicJSON(snapshot.Public, snapshot.ProjectLinks)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Data struct {
			ProjectLinks []publicresume.ProjectLink `json:"projectLinks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.ProjectLinks) != 1 || body.Data.ProjectLinks[0].URL != corpusFirstPage {
		t.Fatalf("public JSON links = %+v, want only the named project", body.Data.ProjectLinks)
	}
	var envelope struct {
		ProjectLinks map[string]string `json:"projectLinks"`
	}
	if err := json.Unmarshal(printEnvelope(t, snapshot), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.ProjectLinks) != 1 || envelope.ProjectLinks[firstProjectEntry] != corpusFirstPage {
		t.Fatalf("print links = %v, want only the named project", envelope.ProjectLinks)
	}
}
