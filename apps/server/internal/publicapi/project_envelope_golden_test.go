package publicapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// The golden envelopes are what the builder makes for a fixed set of
// projects. The web suite runs the renderer's decoder on each
// (apps/web/test/public-render/project-envelope-parity.test.ts), so a
// builder change the renderer would refuse fails there.
const projectEnvelopesPath = "testdata/project-envelopes.json"

// projectEnvelopeCase is one named envelope the builder makes.
type projectEnvelopeCase struct {
	Name     string `json:"name"`
	Envelope any    `json:"envelope"`
}

// smallProject is a project with short English text and the given
// technologies count.
func smallProject(slug string, technologies int, dates *schema.DateRange) publicresume.PublicProject {
	names := []string{}
	for i := range technologies {
		names = append(names, fmt.Sprintf("Tool %d", i))
	}
	return publicresume.PublicProject{Slug: slug, Document: project.Document{
		Dates: dates, Technologies: names, DefaultLanguage: "en",
		Text: map[string]project.Text{"en": {Name: "Project " + slug, Role: "Lead", Summary: "Summary of " + slug + "."}},
	}}
}

// withText replaces a project's English text.
func withText(available publicresume.PublicProject, text project.Text) publicresume.PublicProject {
	available.Document.Text = map[string]project.Text{"en": text}
	return available
}

// goldenProjectEnvelopes builds the golden set: thirty rows, an und resume,
// the blank-name fallback, cards off, entry-link equality, the preview
// bounds, an outcome cut across an accent, and blank optional text.
func goldenProjectEnvelopes(t *testing.T) []projectEnvelopeCase {
	t.Helper()
	cases := []projectEnvelopeCase{}
	add := func(name string, request any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cases = append(cases, projectEnvelopeCase{Name: name, Envelope: request})
	}
	list := func(name string, input projectPageInput) {
		t.Helper()
		request, err := buildProjectListRequest(input)
		add("list: "+name, request, err)
	}
	page := func(name string, input projectPageInput) {
		t.Helper()
		request, err := buildProjectRequest(input)
		add("page: "+name, request, err)
	}
	resume := func(lng, fullName string, publicTitle *string) publicresume.Snapshot {
		snapshot := projectTestSnapshot(lng, fullName, nil, schema.MmYyyy, false)
		snapshot.PublicTitle = publicTitle
		return snapshot
	}
	en := resume("en", "Ada Lovelace", nil)
	completed := &schema.DateRange{Start: schema.YearMonth{Y: 1900, M: monthOf(1)}, End: &schema.YearMonth{Y: 2100, M: monthOf(12)}}
	sameMonth := &schema.DateRange{Start: schema.YearMonth{Y: 2020, M: monthOf(3)}, End: &schema.YearMonth{Y: 2020, M: monthOf(3)}}
	present := &schema.DateRange{Start: schema.YearMonth{Y: 2020}, Present: true}

	thirty := []publicresume.PublicProject{limitProject("project-00", 20, completed)}
	for i := 1; i < 30; i++ {
		thirty = append(thirty, smallProject(fmt.Sprintf("project-%02d", i), i%9, []*schema.DateRange{nil, completed, present, sameMonth}[i%4]))
	}
	list("thirty rows, vi resume", projectTestInput(t, resume("vi-VN", "Ada Lovelace", nil), 30, thirty...))
	list("und resume", projectTestInput(t, resume("und", "Ada", nil), 2, smallProject("first", 1, nil), smallProject("second", 0, present)))
	fallback := smallProject("fallback", 3, nil)
	fallback.Document.Text["vi"] = project.Text{Name: " ", Summary: "Tóm tắt"}
	list("blank-name fallback", projectTestInput(t, resume("vi", "Ada", nil), 1, fallback))
	page("blank-name fallback", projectTestInput(t, resume("vi", "Ada", nil), 1, fallback))
	cardsOff := projectTestInput(t, en, 1, smallProject("cards-off", 2, sameMonth))
	cardsOff.imageURL = ""
	list("cards off", cardsOff)
	page("cards off, the only project", cardsOff)
	page("und resume", projectTestInput(t, resume("und", "Ada", nil), 2, smallProject("und", 1, present)))

	title := longestPublicTitle()
	bounds := limitProject("bounds", 20, completed)
	bounds.EntryLink = longestLink("entry")
	list("longest owner from the public title", projectTestInput(t, resume("en", " ", &title), 2, bounds))
	page("every field at its bound", projectTestInput(t, resume("en", " ", &title), 2, bounds))
	wide := withText(smallProject("wide-outcome", 0, nil), project.Text{Name: wideText(120), Outcome: wideText(600)})
	page("outcome cut at 160 wide code points", projectTestInput(t, en, 2, wide))

	accent := strings.Repeat("a", 159) + "e" + combiningAccent + "bbbb"
	page("outcome cut across an accent", projectTestInput(t, en, 2, withText(smallProject("accent", 0, nil), project.Text{Name: "Accent", Outcome: accent})))
	page("whitespace summary", projectTestInput(t, en, 2, withText(smallProject("whitespace", 0, nil), project.Text{Name: "Whitespace", Summary: "   ", Outcome: "Outcome."})))
	blank := " \u00a0" + string(rune(0xfeff))
	blankText := project.Text{Name: "Blank", Role: blank, Summary: blank, Outcome: blank, Description: blank}
	list("blank optional text", projectTestInput(t, en, 1, withText(smallProject("blank", 1, nil), blankText)))
	page("blank optional text", projectTestInput(t, en, 1, withText(smallProject("blank", 1, nil), blankText)))

	repository, demo := "https://github.com/ada/engine", "https://ada.example/demo"
	for _, entry := range [][2]string{
		{"entry link differs", "https://ada.example/notes"},
		{"entry link equal to the repository", repository},
		{"entry link equal to the demo but for host case", "https://ADA.example/demo"},
		{"entry link equal to the demo but for the port", "https://ada.example:/demo"},
	} {
		linked := smallProject("linked", 1, nil)
		linked.Document.RepositoryURL, linked.Document.DemoURL, linked.EntryLink = repository, demo, entry[1]
		page(entry[0], projectTestInput(t, en, 2, linked))
	}
	sigma := smallProject("sigma", 1, nil)
	sigma.Document.DemoURL, sigma.EntryLink = "https://ada.\u03b1\u03c2/", "https://ada.\u0391\u03a3/"
	page("entry link equal to the demo but for a final sigma", projectTestInput(t, en, 2, sigma))
	dotted := smallProject("dotted", 1, nil)
	dotted.Document.DemoURL, dotted.EntryLink = "https://i\u0307.example/", "https://\u0130.example/"
	page("entry link equal to the demo but for a dotted capital I", projectTestInput(t, en, 2, dotted))

	favicon := projectTestSnapshot("en-GB", "Ada", schemeOf(schema.Dark), schema.Yyyy, true)
	emoji := "🚀"
	favicon.FaviconEmoji = &emoji
	page("favicon, dark, discoverable", projectTestInput(t, favicon, 2, smallProject("favicon", 1, present)))
	return cases
}

// TestProjectEnvelopes_MatchTheGoldenFile proves the builder still makes
// the golden envelopes, each of which passes the Go copy of the renderer's
// decoder. Run with -update to rewrite the file, then review its diff.
func TestProjectEnvelopes_MatchTheGoldenFile(t *testing.T) {
	cases := goldenProjectEnvelopes(t)
	for _, entry := range cases {
		if broken := decodeProjectEnvelopeRules(t, entry.Envelope); len(broken) != 0 {
			t.Errorf("%s breaks the decoder copy: %q", entry.Name, broken)
		}
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"cases": cases}); err != nil {
		t.Fatal(err)
	}
	if *updateGolden {
		if err := os.WriteFile(projectEnvelopesPath, encoded.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	golden, err := os.ReadFile(projectEnvelopesPath)
	if err != nil {
		t.Fatalf("read %s: %v; run this test with -update", projectEnvelopesPath, err)
	}
	if !bytes.Equal(golden, encoded.Bytes()) {
		t.Fatalf("the builder's envelopes differ from %s; run this test with -update and review the diff", projectEnvelopesPath)
	}
}
