package publicapi

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const projectTestCardURL = "https://aboutme.example/api/v1/public/resumes/ada/og/0123456789abcdef.png"

func projectTestSnapshot(lng, fullName string, scheme *schema.ColorScheme, dateFormat schema.DateFormat, discoverable bool) publicresume.Snapshot {
	var customization schema.Customization
	customization.ColorScheme, customization.DateFormat = scheme, dateFormat
	return publicresume.Snapshot{
		ResumeID: uuid.MustParse("00000000-0000-0000-0000-0000000000c1"), Revision: 7, DiscoveryEnabled: discoverable,
		Public: publicresume.PublicResume{Slug: "ada", Revision: "7", Lng: lng, Document: publicresume.PublicResumeDocument{
			PersonalDetails: publicresume.PublicPersonalDetails{FullName: fullName}, Customization: customization,
		}},
	}
}

func projectTestInput(t *testing.T, snapshot publicresume.Snapshot, available int, projects ...publicresume.PublicProject) projectPageInput {
	t.Helper()
	for i := range projects {
		projects[i].URL = mustPublicOrigin(t).Resolve("/ada/projects/" + projects[i].Slug)
	}
	return projectPageInput{snapshot: snapshot, pages: publicresume.ProjectPages{Projects: projects, Available: available}, origin: mustPublicOrigin(t), imageURL: projectTestCardURL}
}

func schemeOf(value schema.ColorScheme) *schema.ColorScheme { return &value }

func monthOf(value int64) *int64 { return &value }

func TestProjectRequests_BuildTheCorpusEnvelopes(t *testing.T) {
	corpus := readProjectCorpus(t)
	en := "en"
	every := projectTestInput(t, projectTestSnapshot("en", "Ada Lovelace", schemeOf(schema.Dark), schema.MonYYYY, true), 2, publicresume.PublicProject{
		Slug: "analytical-engine", EntryLink: "https://ada.example/notes", Document: project.Document{
			Dates:         &schema.DateRange{Start: schema.YearMonth{Y: 2020, M: monthOf(10)}, End: &schema.YearMonth{Y: 2021, M: monthOf(9)}},
			RepositoryURL: "https://github.com/ada/analytical-engine", DemoURL: "https://ada.example/demo",
			Technologies: []string{"Mathematics", "Punched cards"}, DefaultLanguage: en,
			Text: map[string]project.Text{en: {
				Name: "Analytical Engine notes", Role: "Author", Summary: "Notes on the first mechanical general-purpose computer.",
				Outcome:     "Published the first algorithm intended for a machine.",
				Description: `<p>Translated and annotated <a href="https://en.wikipedia.org/wiki/Analytical_engine" rel="noopener noreferrer">the memoir</a>.</p><ul><li>Note G</li></ul>`,
			}},
		},
	})
	nameOnly := projectTestInput(t, projectTestSnapshot("en", "Ada Lovelace", nil, schema.MmYyyy, false), 1, publicresume.PublicProject{
		Slug: "note-g", Document: project.Document{DefaultLanguage: en, Text: map[string]project.Text{en: {Name: "Note G"}}},
	})
	list := projectTestInput(t, projectTestSnapshot("vi-VN", "Ada Lovelace", schemeOf(schema.System), schema.Yyyy, false), 2,
		publicresume.PublicProject{Slug: "may-phan-tich", Document: project.Document{
			Dates:        &schema.DateRange{Start: schema.YearMonth{Y: 2019}, End: &schema.YearMonth{Y: 2021}},
			Technologies: []string{"Toán học", "Bìa đục lỗ", "Thuật toán", "Số Bernoulli", "Dịch thuật", "Ghi chú", "Lịch sử"}, DefaultLanguage: "vi",
			Text: map[string]project.Text{"vi": {Name: "Máy phân tích", Role: "Tác giả", Summary: "Ghi chú về máy tính cơ học đầu tiên."}, "en": {Name: "Analytical engine"}},
		}},
		publicresume.PublicProject{Slug: "difference-engine", Document: project.Document{DefaultLanguage: en, Text: map[string]project.Text{en: {Name: "Difference Engine study"}}}},
	)
	for name, build := range map[string]func() (any, error){
		"project-en-every-field": func() (any, error) {
			request, err := buildProjectRequest(every)
			request.FaviconHref = "data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%2F%3E"
			return request, err
		},
		"project-en-name-only": func() (any, error) { return buildProjectRequest(nameOnly) },
		"list-vi-two-projects": func() (any, error) { return buildProjectListRequest(list) },
	} {
		t.Run(name, func(t *testing.T) {
			request, err := build()
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var gotValue, wantValue any
			if err := json.Unmarshal(got, &gotValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(corpus[name].Envelope, &wantValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("built envelope differs from the corpus:\n got %s\nwant %s", got, corpus[name].Envelope)
			}
		})
	}
}

func TestChooseProjectText_LanguageRule(t *testing.T) {
	for _, test := range []struct {
		name, resumeLng, defaultLanguage string
		text                             map[string]project.Text
		wantName, wantLang               string
	}{
		{"resume language", "vi-VN", "en", map[string]project.Text{"vi": {Name: "Máy"}, "en": {Name: "Engine"}}, "Máy", ""},
		{"region ignored", "en-GB", "vi", map[string]project.Text{"vi": {Name: "Máy"}, "en": {Name: "Engine"}}, "Engine", ""},
		{"default fallback", "fr", "en", map[string]project.Text{"vi": {Name: "Máy"}, "en": {Name: "Engine"}}, "Engine", "en"},
		{"blank name in the chosen language", "vi", "en", map[string]project.Text{"vi": {Name: " \t", Summary: "Tóm tắt"}, "en": {Name: "Engine"}}, "Engine", "en"},
		{"und uses the default", "und", "vi", map[string]project.Text{"vi": {Name: "Máy"}, "und": {Name: "Undetermined"}}, "Máy", "vi"},
	} {
		t.Run(test.name, func(t *testing.T) {
			text, lang, err := chooseProjectText(project.Document{DefaultLanguage: test.defaultLanguage, Text: test.text}, test.resumeLng)
			if err != nil {
				t.Fatal(err)
			}
			if text.Name != test.wantName || lang != test.wantLang {
				t.Fatalf("chooseProjectText() = %q, lang %q; want %q, lang %q", text.Name, lang, test.wantName, test.wantLang)
			}
		})
	}
	if _, _, err := chooseProjectText(project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: " "}}}, "en"); err == nil {
		t.Fatal("chooseProjectText() accepted a project with no name in any chosen language")
	}
}

func TestProjectEntryLink_ComparisonRule(t *testing.T) {
	const repository, demo = "https://github.com/ada/engine", "https://ada.example/demo"
	for _, test := range []struct {
		name, entry, repository, demo, want string
	}{
		{"differs from both", "https://ada.example/notes", repository, demo, "https://ada.example/notes"},
		{"equals the demo", demo, repository, demo, ""},
		{"equals the repository", repository, repository, demo, ""},
		{"host case is equal", "https://ADA.Example/demo", repository, demo, ""},
		{"default port is equal", "https://ada.example:443/demo", repository, demo, ""},
		{"trailing slash differs", "https://ada.example/demo/", repository, demo, "https://ada.example/demo/"},
		{"path case differs", "https://ada.example/Demo", repository, demo, "https://ada.example/Demo"},
		{"query differs", "https://ada.example/demo?x=1", repository, demo, "https://ada.example/demo?x=1"},
		{"empty path reads as a slash", "https://ada.example/", repository, "https://ada.example", ""},
		{"other port differs", "https://ada.example:8443/demo", repository, demo, "https://ada.example:8443/demo"},
		{"empty port is equal", "https://ada.example:/demo", repository, demo, ""},
		{"final sigma is equal", "https://ada.\u0391\u03a3/demo", repository, "https://ada.\u03b1\u03c2/demo", ""},
		{"dotted capital I is equal", "https://\u0130.example/", repository, "https://i\u0307.example/", ""},
		{"no repository or demo", "https://ada.example/notes", "", "", "https://ada.example/notes"},
		{"sent as stored", "https://Ada.example/notes", repository, demo, "https://Ada.example/notes"},
		{"not https", "http://ada.example/notes", repository, demo, ""},
		{"mailto", "mailto:ada@example.com", repository, demo, ""},
		{"none", "", repository, demo, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := projectEntryLink(test.entry, test.repository, test.demo); got != test.want {
				t.Fatalf("projectEntryLink(%q) = %q, want %q", test.entry, got, test.want)
			}
		})
	}
}

func TestProjectListRequest_RowsCarryAtMostSixTechnologies(t *testing.T) {
	technologies := func(n int) []string {
		out := []string{}
		for i := range n {
			out = append(out, "tech-"+string(rune('a'+i)))
		}
		return out
	}
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 3,
		publicresume.PublicProject{Slug: "eight", Document: project.Document{DefaultLanguage: "en", Technologies: technologies(8), Text: map[string]project.Text{"en": {Name: "Eight"}}}},
		publicresume.PublicProject{Slug: "six", Document: project.Document{DefaultLanguage: "en", Technologies: technologies(6), Text: map[string]project.Text{"en": {Name: "Six"}}}},
		publicresume.PublicProject{Slug: "none", Document: project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "None"}}}},
	)
	request, err := buildProjectListRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	rows := request.Projects
	if len(rows) != 3 || !reflect.DeepEqual(rows[0].Technologies, technologies(6)) || rows[0].MoreTechnologies != 2 ||
		len(rows[1].Technologies) != 6 || rows[1].MoreTechnologies != 0 || rows[2].Technologies == nil || len(rows[2].Technologies) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	if request.PageTitle != "Projects · Ada" || request.Preview.Description != "" || request.Preview.Title != request.PageTitle || request.PageURL != "https://aboutme.example/ada/projects" {
		t.Fatalf("list head = %q %+v %q", request.PageTitle, request.Preview, request.PageURL)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"technologies":[]`)) || bytes.Contains(encoded, []byte("description\":\"<")) {
		t.Fatalf("encoded list = %s", encoded)
	}
}

func TestProjectRequest_DescriptionFallsBackToTheOutcome(t *testing.T) {
	outcome := strings.Repeat("Kết quả ", 40)
	for _, test := range []struct {
		name string
		text project.Text
		want string
	}{
		{"summary", project.Text{Name: "Engine", Summary: "Summary.", Outcome: "Outcome."}, "Summary."},
		{"first 160 runes of the outcome", project.Text{Name: "Engine", Outcome: outcome}, string([]rune(outcome)[:160])},
		{"short outcome", project.Text{Name: "Engine", Summary: "  ", Outcome: "Outcome."}, "Outcome."},
		{"none", project.Text{Name: "Engine"}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := projectTestInput(t, projectTestSnapshot("vi", "Ada", nil, schema.MmYyyy, false), 1,
				publicresume.PublicProject{Slug: "engine", Document: project.Document{DefaultLanguage: "vi", Text: map[string]project.Text{"vi": test.text}}})
			request, err := buildProjectRequest(input)
			if err != nil {
				t.Fatal(err)
			}
			if request.Preview.Description != test.want || request.Preview.Title != "Engine · Ada" || request.PageTitle != "Engine · Ada" {
				t.Fatalf("preview = %+v, want description %q", request.Preview, test.want)
			}
		})
	}
}

func TestProjectRequests_OwnerAndResume(t *testing.T) {
	title := "Ada's page"
	snapshot := projectTestSnapshot("vi-VN", " ", nil, schema.Yyyy, false)
	snapshot.PublicTitle = &title
	input := projectTestInput(t, snapshot, 1, publicresume.PublicProject{Slug: "engine", Document: project.Document{DefaultLanguage: "vi", Text: map[string]project.Text{"vi": {Name: "Máy"}}}})
	list, err := buildProjectListRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	want := directrender.ProjectPageResume{Slug: "ada", URL: "https://aboutme.example/ada", OwnerName: title, Lng: "vi-VN", UILng: "vi", ColorScheme: schema.Light, DateFormat: schema.Yyyy}
	if list.Resume != want || list.PageTitle != "Dự án · "+title {
		t.Fatalf("resume = %+v title %q, want %+v", list.Resume, list.PageTitle, want)
	}
	page, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if page.Resume != want || page.OtherProjects || page.ListURL != "https://aboutme.example/ada/projects" || page.PageURL != "https://aboutme.example/ada/projects/engine" {
		t.Fatalf("page = %+v", page)
	}
}

func readProjectCorpus(t *testing.T) map[string]projectCorpusCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/project-render.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []projectCorpusCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	out := map[string]projectCorpusCase{}
	for _, tc := range corpus.Cases {
		out[tc.Name] = tc
	}
	return out
}

type projectCorpusCase struct {
	Name     string          `json:"name"`
	Envelope json.RawMessage `json:"envelope"`
	HTML     string          `json:"html"`
}
