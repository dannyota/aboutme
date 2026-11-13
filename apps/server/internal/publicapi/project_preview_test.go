package publicapi

import (
	stdhtml "html"
	"strings"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// TestProjectRequest_PreviewTextIsUncut proves the project preview carries
// the page title and the summary whole, at their longest.
func TestProjectRequest_PreviewTextIsUncut(t *testing.T) {
	title := longestPublicTitle()
	snapshot := projectTestSnapshot("en", " ", nil, schema.MmYyyy, false)
	snapshot.PublicTitle = &title
	input := projectTestInput(t, snapshot, 2, limitProject("longest", 20, nil))
	page, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	summary := input.pages.Projects[0].Document.Text["en"].Summary
	if page.Preview.Description != summary || len(summary) != 1200 {
		t.Errorf("preview description has %d bytes, want the 1200-byte summary uncut", len(page.Preview.Description))
	}
	if page.Preview.Title != page.PageTitle || len(page.Preview.Title) <= 2240 {
		t.Errorf("preview title has %d bytes, want the %d-byte page title uncut", len(page.Preview.Title), len(page.PageTitle))
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Errorf("the longest uncut preview breaks the decoder copy: %q", broken)
	}
}

// TestProjectDescription_OutcomeCutKeepsGraphemes proves the outcome
// fallback is cut at the last grapheme boundary at or below 160 runes, so a
// combining accent is never split from its letter.
func TestProjectDescription_OutcomeCutKeepsGraphemes(t *testing.T) {
	for name, test := range map[string]struct{ outcome, want string }{
		"accent across the cut": {strings.Repeat("a", 159) + "é" + "bbbb", strings.Repeat("a", 159)},
		"cut on a boundary":     {strings.Repeat("a", 160) + "é", strings.Repeat("a", 160)},
		"exactly 160 runes":     {strings.Repeat("a", 158) + "é", strings.Repeat("a", 158) + "é"},
		"precomposed letter":    {strings.Repeat("a", 159) + "é" + "b", strings.Repeat("a", 159) + "é"},
	} {
		if got := projectDescription(project.Text{Outcome: test.outcome}); got != test.want {
			t.Errorf("%s: description = %q (%d runes), want %q", name, got, len([]rune(got)), test.want)
		}
	}
}

// TestProjectValidators_AdmitExactlyThePreviewBounds proves both validators
// accept a preview title of at most 683 code points and 2,727 bytes and a
// description of at most 300 code points and 1,200 bytes, and refuse more.
func TestProjectValidators_AdmitExactlyThePreviewBounds(t *testing.T) {
	origin := mustPublicOrigin(t)
	corpus := readProjectCorpus(t)
	page, list := corpus["project-en-every-field"], corpus["list-vi-two-projects"]
	atTitleBound := strings.Repeat("😀", 680) + "€€a"
	if n, b := len([]rune(atTitleBound)), len(atTitleBound); n != 683 || b != 2727 {
		t.Fatalf("title fixture has %d code points and %d bytes", n, b)
	}
	atDescriptionBound := wideText(300)
	for name, test := range map[string]struct{ title, description, want string }{
		"both at their bounds":         {atTitleBound, atDescriptionBound, ""},
		"title over in code points":    {strings.Repeat("a", 684), "", "preview_bounds"},
		"title over in bytes":          {wideText(682), "", "preview_bounds"},
		"description over code points": {"Title", strings.Repeat("a", 301), "preview_bounds"},
	} {
		request := projectCorpusPage(t, page)
		source := withPreviewText(t, page.HTML, request.Preview.Title, request.Preview.Description, test.title, test.description)
		request.Preview.Title, request.Preview.Description = test.title, test.description
		if got := projectHTMLRejection([]byte(source), request, origin); got != test.want {
			t.Errorf("project %s: rejection = %q, want %q", name, got, test.want)
		}
		if test.description == "" {
			listRequest := projectCorpusList(t, list)
			listSource := withPreviewText(t, list.HTML, listRequest.Preview.Title, "", test.title, "")
			listRequest.Preview.Title = test.title
			if got := projectListHTMLRejection([]byte(listSource), listRequest, origin); got != test.want {
				t.Errorf("list %s: rejection = %q, want %q", name, got, test.want)
			}
		}
	}
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, limitProject("bounds", 1, nil))
	built, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		title, description string
		admitted           bool
	}{
		"both at their bounds": {atTitleBound, atDescriptionBound, true},
		"title over":           {atTitleBound + "a", atDescriptionBound, false},
		"description over":     {atTitleBound, atDescriptionBound + "a", false},
	} {
		built.Preview.Title, built.Preview.Description = test.title, test.description
		if broken := decodeProjectEnvelopeRules(t, built); (len(broken) == 0) != test.admitted {
			t.Errorf("decoder copy, %s: broken = %q, want admitted %t", name, broken, test.admitted)
		}
	}
}

// withPreviewText replaces the preview title and description in a corpus
// page's head: og:title, and the description pair, added when the page had
// none.
func withPreviewText(t *testing.T, source, oldTitle, oldDescription, title, description string) string {
	t.Helper()
	source = replaceOnce(source, `<meta property="og:title" content="`+stdhtml.EscapeString(oldTitle)+`">`,
		`<meta property="og:title" content="`+stdhtml.EscapeString(title)+`">`)
	if oldDescription != "" {
		source = replaceOnce(source, `<meta name="description" content="`+stdhtml.EscapeString(oldDescription)+`">`, "")
		source = replaceOnce(source, `<meta property="og:description" content="`+stdhtml.EscapeString(oldDescription)+`">`, "")
	}
	if description != "" {
		escaped := stdhtml.EscapeString(description)
		source = replaceOnce(source, "</head>", `<meta name="description" content="`+escaped+`"><meta property="og:description" content="`+escaped+`"></head>`)
	}
	return source
}

// TestProjectRequest_OmitsLinksTheRendererCannotParse proves a stored link
// the renderer's URL parser refuses is left out of the request, so its page
// still renders.
func TestProjectRequest_OmitsLinksTheRendererCannotParse(t *testing.T) {
	stored := publicresume.PublicProject{Slug: "ports", EntryLink: "https://example.123/notes", Document: project.Document{
		DefaultLanguage: "en", RepositoryURL: "https://example.org:70000/code", DemoURL: "https://1.2.3.256/demo",
		Text: map[string]project.Text{"en": {Name: "Ports"}},
	}}
	for _, link := range []string{stored.EntryLink, stored.Document.RepositoryURL, stored.Document.DemoURL} {
		if normalized, ok := project.NormalizeLink(link); !ok || normalized != link {
			t.Fatalf("fixture link %q is not one the library stores", link)
		}
	}
	page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, stored))
	if err != nil {
		t.Fatal(err)
	}
	if page.Project.RepositoryURL != "" || page.Project.DemoURL != "" || page.EntryLink != "" {
		t.Fatalf("links = %q %q %q, want each left out", page.Project.RepositoryURL, page.Project.DemoURL, page.EntryLink)
	}
	kept := stored
	kept.Document.RepositoryURL, kept.Document.DemoURL, kept.EntryLink = "https://example.org:65535/code", "https://0x7f.1/demo", "https://bücher.example/notes"
	page, err = buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, kept))
	if err != nil {
		t.Fatal(err)
	}
	if page.Project.RepositoryURL != kept.Document.RepositoryURL || page.Project.DemoURL != kept.Document.DemoURL || page.EntryLink != kept.EntryLink {
		t.Fatalf("links = %q %q %q, want each kept", page.Project.RepositoryURL, page.Project.DemoURL, page.EntryLink)
	}
}

// TestProjectRequests_BlankIsWhatTheRendererTrims proves the builder reads a
// name as blank exactly when the renderer's trim does: U+FEFF alone is
// blank there, U+0085 alone is not.
func TestProjectRequests_BlankIsWhatTheRendererTrims(t *testing.T) {
	bom, nel := string(rune(0xfeff)), string(rune(0x85))
	document := project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Engine"}, "vi": {Name: bom}}}
	text, lang, err := chooseProjectText(document, "vi")
	if err != nil || text.Name != "Engine" || lang != "en" {
		t.Errorf("chooseProjectText() = %q, %q, %v; want the default text for a byte order mark name", text.Name, lang, err)
	}
	document.Text["vi"] = project.Text{Name: nel}
	if kept, _, keptErr := chooseProjectText(document, "vi"); keptErr != nil || kept.Name != nel {
		t.Errorf("chooseProjectText() = %q, %v; want the U+0085 name the renderer keeps", kept.Name, keptErr)
	}
	title := bom
	snapshot := projectTestSnapshot("en", bom, nil, schema.MmYyyy, false)
	snapshot.PublicTitle = &title
	input := projectTestInput(t, snapshot, 1, publicresume.PublicProject{Slug: "engine", Document: project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Engine"}}}})
	page, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if project.Blank(page.Resume.OwnerName) || page.Resume.OwnerName != bom+" — Resume" {
		t.Errorf("ownerName = %q, want the default title the renderer reads as non-blank", page.Resume.OwnerName)
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Errorf("decoder copy refuses the fallback owner: %q", broken)
	}
}
