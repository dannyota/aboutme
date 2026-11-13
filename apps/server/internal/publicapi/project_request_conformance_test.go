package publicapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

// Library-limit text: each rune is four bytes, so byte bounds meet their
// worst case.
func wideText(runes int) string { return strings.Repeat("😀", runes) }

// longestLink is an https link of exactly 2048 code points.
func longestLink(path string) string {
	prefix := "https://ada.example/" + path + "/"
	return prefix + strings.Repeat("a", 2048-len(prefix))
}

// longestPublicTitle is a public title at both of its limits: 56 graphemes
// of ten code points, 560 code points and 1,960 bytes.
func longestPublicTitle() string {
	return strings.Repeat("👩🏽‍❤️‍💋‍👨🏻", 56)
}

func limitProject(slug string, technologies int, dates *schema.DateRange) publicresume.PublicProject {
	names := []string{}
	for i := range technologies {
		names = append(names, fmt.Sprintf("%02d", i)+wideText(38))
	}
	return publicresume.PublicProject{Slug: slug, Document: project.Document{
		Dates: dates, Technologies: names, DefaultLanguage: "en",
		RepositoryURL: longestLink("repository"), DemoURL: longestLink("demo"),
		Text: map[string]project.Text{
			"en": {Name: wideText(120), Role: wideText(120), Summary: wideText(300), Outcome: wideText(600), Description: "<p>" + strings.Repeat("a", 16000) + "</p>"},
			"vi": {Name: " ", Summary: "Tóm tắt"},
		},
	}}
}

// TestProjectRequests_MeetTheRendererDecoder builds both envelopes from
// fixtures at the library's limits and edge cases, and requires each to pass
// every rule of the web renderer's strict decoder.
func TestProjectRequests_MeetTheRendererDecoder(t *testing.T) {
	completed := &schema.DateRange{Start: schema.YearMonth{Y: 1900, M: monthOf(1)}, End: &schema.YearMonth{Y: 2100, M: monthOf(12)}}
	present := &schema.DateRange{Start: schema.YearMonth{Y: 2020}, Present: true}

	thirty := []publicresume.PublicProject{}
	for i := range 30 {
		dates := []*schema.DateRange{nil, completed, present}[i%3]
		thirty = append(thirty, limitProject(fmt.Sprintf("project-%02d", i), []int{0, 5, 6, 7, 20}[i%5], dates))
	}
	named := func(fullName string, publicTitle *string, lng string) publicresume.Snapshot {
		snapshot := projectTestSnapshot(lng, fullName, schemeOf(schema.System), schema.MonYYYY, false)
		snapshot.PublicTitle = publicTitle
		return snapshot
	}
	title := longestPublicTitle()
	differs := limitProject("differs", 20, completed)
	differs.EntryLink = longestLink("entry")
	hostCase := limitProject("host-case", 3, present)
	hostCase.EntryLink = strings.Replace(hostCase.Document.DemoURL, "ada.example", "ADA.example", 1)
	sameBytes := limitProject("same-bytes", 1, nil)
	sameBytes.EntryLink = sameBytes.Document.RepositoryURL
	equalButtons := limitProject("equal-buttons", 2, nil)
	equalButtons.Document.DemoURL = equalButtons.Document.RepositoryURL
	sparse := publicresume.PublicProject{Slug: "ab", Document: project.Document{DefaultLanguage: "ja", Text: map[string]project.Text{"ja": {Name: "名"}}}}

	cardsOff := func(input projectPageInput) projectPageInput {
		input.imageURL = ""
		return input
	}
	for name, input := range map[string]projectPageInput{
		"thirty rows, vi resume":          projectTestInput(t, named("Ada Lovelace", nil, "vi-VN"), 30, thirty...),
		"blank-name fallback":             projectTestInput(t, named("Ada", nil, "vi"), 2, differs),
		"und resume":                      projectTestInput(t, named("Ada", nil, "und"), 2, differs),
		"cards off":                       cardsOff(projectTestInput(t, named("Ada", nil, "en"), 1, sparse)),
		"entry link differs":              projectTestInput(t, named("Ada", nil, "en-GB"), 2, differs),
		"entry link equal but host case":  projectTestInput(t, named("Ada", nil, "en"), 2, hostCase),
		"entry link equal in bytes":       projectTestInput(t, named("Ada", nil, "en"), 2, sameBytes),
		"equal repository and demo":       projectTestInput(t, named("Ada", nil, "en"), 2, equalButtons),
		"longest owner from public title": projectTestInput(t, named(" ", &title, "zh-Hant-TW"), 2, differs),
		"owner with no name and no title": projectTestInput(t, named("", nil, "x-private"), 1, sparse),
		"longest full name":               projectTestInput(t, named(wideText(160), nil, "en"), 2, differs),
	} {
		t.Run(name, func(t *testing.T) {
			list, err := buildProjectListRequest(input)
			if err != nil {
				t.Fatal(err)
			}
			if broken := decodeProjectEnvelopeRules(t, list); len(broken) != 0 {
				t.Errorf("list envelope breaks the renderer's decoder: %q", broken)
			}
			for _, available := range input.pages.Projects[:min(3, len(input.pages.Projects))] {
				single := input
				single.pages.Projects = []publicresume.PublicProject{available}
				page, err := buildProjectRequest(single)
				if err != nil {
					t.Fatal(err)
				}
				if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
					t.Errorf("page %s breaks the renderer's decoder: %q", available.Slug, broken)
				}
			}
		})
	}
}

// TestProjectDecoderRules_CatchEachViolation proves the Go mirror of the
// decoder catches the violations it names, so its passing result is
// evidence.
func TestProjectDecoderRules_CatchEachViolation(t *testing.T) {
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 2, limitProject("differs", 6, nil))
	page, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Fatalf("baseline breaks the decoder: %q", broken)
	}
	page.Resume.UILng = "vi"
	page.EntryLink = page.Project.DemoURL
	page.Project.URL += "/"
	page.Preview.Title = wideText(561)
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	decoderObject(envelope["project"])["role"] = ""
	broken := strings.Join(decodeProjectEnvelopeRules(t, envelope), "\n")
	for _, rule := range []string{
		"uiLng follows lng's primary subtag", "entryLink differs from both buttons", "project url is listUrl/slug",
		"preview title is 1 to 2240 bytes", "role is omitted or 1 to its limit",
	} {
		if !strings.Contains(broken, rule) {
			t.Errorf("decoder mirror missed %q; broken = %q", rule, broken)
		}
	}
}
