package resume_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

func strp(s string) *string { return &s }

func boundUUID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

func baseCustomizationForBounds() schema.Customization {
	return schema.Customization{
		Font:    schema.Font{Family: schema.Inter, BaseSizePx: 14},
		Colors:  schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
		Spacing: schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
		Heading: schema.Heading{Style: schema.Normal, ShowRule: false},
		Layout:  schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
		SectionDisplay: schema.SectionDisplay{
			Skill:    schema.SkillClass{Style: schema.Text},
			Language: schema.LanguageClass{Style: schema.Text},
		},
		PageFormat: schema.A4,
		DateFormat: schema.MmYyyy,
	}
}

func baseDocForBounds() schema.Resume {
	return schema.Resume{
		SchemaVersion:   int64(schema.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{FullName: strp("Ada Lovelace")},
		Content:         map[string]schema.Section{},
		Customization:   baseCustomizationForBounds(),
	}
}

func customSection(entries []schema.CustomEntry) schema.Section {
	return schema.NewCustomSection(nil, nil, entries)
}

func workSection(entries []schema.WorkEntry) schema.Section {
	return schema.NewWorkSection(nil, nil, entries)
}

func profileSection(entries []schema.ProfileEntry) schema.Section {
	return schema.NewProfileSection(nil, nil, entries)
}

func sortedContentKeys(content map[string]schema.Section) []string {
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func lettersContent(n int) map[string]schema.Section {
	if n > 26 {
		panic("lettersContent: n must be <= 26")
	}
	content := make(map[string]schema.Section, n)
	for i := 0; i < n; i++ {
		content[string(rune('a'+i))] = customSection(nil)
	}
	return content
}

func nWorkEntries(n int) []schema.WorkEntry {
	entries := make([]schema.WorkEntry, n)
	for i := 0; i < n; i++ {
		entries[i] = schema.WorkEntry{ID: boundUUID(i)}
	}
	return entries
}

func nPersonalDetails(n int) []schema.PersonalDetail {
	details := make([]schema.PersonalDetail, n)
	for i := 0; i < n; i++ {
		details[i] = schema.PersonalDetail{ID: boundUUID(i), Type: schema.TypeCustom, Value: "x", IsHidden: false}
	}
	return details
}

func docWithWorkField(mutate func(*schema.WorkEntry)) schema.Resume {
	doc := baseDocForBounds()
	entry := schema.WorkEntry{ID: boundUUID(0)}
	mutate(&entry)
	doc.Content = map[string]schema.Section{"work": workSection([]schema.WorkEntry{entry})}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	return doc
}

func padDocToCanonicalBytes(target int) schema.Resume {
	doc := baseDocForBounds()
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Content = map[string]schema.Section{"work": workSection(nil)}

	shell, err := resume.AssembleCanonical(doc)
	if err != nil {
		panic("padDocToCanonicalBytes: assembling shell: " + err.Error())
	}
	remaining := target - len(shell)
	if remaining < 0 {
		panic(fmt.Sprintf("padDocToCanonicalBytes: shell alone is %d bytes, exceeds target %d", len(shell), target))
	}

	var entries []schema.WorkEntry
	for i := 0; remaining > 0; i++ {
		chunk := remaining
		if chunk > 16384 {
			chunk = 16384
		}
		d := strings.Repeat("a", chunk)
		entries = append(entries, schema.WorkEntry{ID: boundUUID(i), Description: &d})
		remaining -= chunk
	}
	doc.Content = map[string]schema.Section{"work": workSection(entries)}

	got, err := resume.AssembleCanonical(doc)
	if err != nil {
		panic("padDocToCanonicalBytes: assembling padded doc: " + err.Error())
	}
	diff := target - len(got)
	if diff != 0 {
		if len(entries) == 0 {
			panic("padDocToCanonicalBytes: no entry to absorb the remaining delta")
		}
		last := len(entries) - 1
		newLen := len(*entries[last].Description) + diff
		if newLen < 0 || newLen > 16384 {
			panic(fmt.Sprintf("padDocToCanonicalBytes: delta %d pushes last entry's description length to %d, outside [0,16384]", diff, newLen))
		}
		d := strings.Repeat("a", newLen)
		entries[last].Description = &d
		doc.Content = map[string]schema.Section{"work": workSection(entries)}
	}
	return doc
}

type boundCase struct {
	Name        string
	BoundPath   string
	Aliases     []string
	Keyword     string
	Limit       int
	Valid       func() schema.Resume
	Invalid     func() schema.Resume
	StoreOnly   bool
	IssueSubstr string
}

var namedBounds = []boundCase{
	{
		Name:      "max-document-bytes",
		BoundPath: "",
		Keyword:   "",
		Limit:     resume.MaxDocumentBytes,
		Valid: func() schema.Resume {
			return padDocToCanonicalBytes(resume.MaxDocumentBytes)
		},
		Invalid: func() schema.Resume {
			return padDocToCanonicalBytes(resume.MaxDocumentBytes + 1)
		},
		StoreOnly:   true,
		IssueSubstr: "exceeds the",
	},
	{
		Name:      "richtext-byte-exact",
		BoundPath: "",
		Keyword:   "",
		Limit:     16384,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			text := strings.Repeat("é", 8192)
			doc.Content = map[string]schema.Section{"profile": profileSection([]schema.ProfileEntry{{ID: boundUUID(0), Text: &text}})}
			doc.Customization.Layout.Sections.Main = []string{"profile"}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			text := strings.Repeat("é", 8192) + "a"
			doc.Content = map[string]schema.Section{"profile": profileSection([]schema.ProfileEntry{{ID: boundUUID(0), Text: &text}})}
			doc.Customization.Layout.Sections.Main = []string{"profile"}
			return doc
		},
		StoreOnly:   true,
		IssueSubstr: "rich-text-byte-length",
	},
	{
		Name:      "content-maxproperties",
		BoundPath: "$.$defs.content.maxProperties",
		Keyword:   "maxProperties",
		Limit:     24,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = lettersContent(24)
			doc.Customization.Layout.Sections.Main = sortedContentKeys(doc.Content)
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = lettersContent(25)
			keys := sortedContentKeys(doc.Content)
			doc.Customization.Layout.Sections.Main = keys[:24]
			return doc
		},
		IssueSubstr: "maxProperties",
	},
	{
		Name:      "section-entries-maxitems",
		BoundPath: "$.$defs.section.oneOf[1].properties.entries.maxItems",
		Aliases: []string{
			"$.$defs.section.oneOf[0].properties.entries.maxItems",
			"$.$defs.section.oneOf[2].properties.entries.maxItems",
			"$.$defs.section.oneOf[3].properties.entries.maxItems",
			"$.$defs.section.oneOf[4].properties.entries.maxItems",
			"$.$defs.section.oneOf[5].properties.entries.maxItems",
			"$.$defs.section.oneOf[6].properties.entries.maxItems",
			"$.$defs.section.oneOf[7].properties.entries.maxItems",
		},
		Keyword: "maxItems",
		Limit:   64,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = map[string]schema.Section{"work": workSection(nWorkEntries(64))}
			doc.Customization.Layout.Sections.Main = []string{"work"}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = map[string]schema.Section{"work": workSection(nWorkEntries(65))}
			doc.Customization.Layout.Sections.Main = []string{"work"}
			return doc
		},
		IssueSubstr: "maxItems",
	},
	{
		Name:      "personaldetails-details-maxitems",
		BoundPath: "$.$defs.personalDetails.properties.details.maxItems",
		Keyword:   "maxItems",
		Limit:     16,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = nPersonalDetails(16)
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = nPersonalDetails(17)
			return doc
		},
		IssueSubstr: "maxItems",
	},
	{
		Name:      "layout-sections-main-maxitems",
		BoundPath: "$.$defs.customization.properties.layout.properties.sections.properties.main.maxItems",
		Keyword:   "maxItems",
		Limit:     24,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = lettersContent(24)
			doc.Customization.Layout.Sections.Main = sortedContentKeys(doc.Content)
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = lettersContent(24)
			main := append(sortedContentKeys(doc.Content), "y")
			doc.Customization.Layout.Sections.Main = main
			return doc
		},
		IssueSubstr: "maxItems",
	},
	{
		Name:      "layout-sections-sidebar-maxitems",
		BoundPath: "$.$defs.customization.properties.layout.properties.sections.properties.sidebar.maxItems",
		Keyword:   "maxItems",
		Limit:     24,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = lettersContent(24)
			doc.Customization.Layout.Sections.Sidebar = sortedContentKeys(doc.Content)
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.Content = lettersContent(24)
			sidebar := append(sortedContentKeys(doc.Content), "z")
			doc.Customization.Layout.Sections.Sidebar = sidebar
			return doc
		},
		IssueSubstr: "maxItems",
	},
	{
		Name:      "sectionkey-maxlength",
		BoundPath: "$.$defs.sectionKey.maxLength",
		Keyword:   "maxLength",
		Limit:     36,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			key := strings.Repeat("a", 36)
			doc.Content = map[string]schema.Section{key: customSection(nil)}
			doc.Customization.Layout.Sections.Main = []string{key}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			key := strings.Repeat("a", 37)
			doc.Content = map[string]schema.Section{key: customSection(nil)}
			doc.Customization.Layout.Sections.Main = []string{key}
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "personaldetail-label-maxlength",
		BoundPath: "$.$defs.personalDetail.properties.label.maxLength",
		Keyword:   "maxLength",
		Limit:     40,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = []schema.PersonalDetail{
				{ID: boundUUID(0), Type: schema.TypeCustom, Value: "x", IsHidden: false, Label: strp(strings.Repeat("a", 40))},
			}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = []schema.PersonalDetail{
				{ID: boundUUID(0), Type: schema.TypeCustom, Value: "x", IsHidden: false, Label: strp(strings.Repeat("a", 41))},
			}
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "iconkey-maxlength",
		BoundPath: "$.$defs.iconKey.maxLength",
		Keyword:   "maxLength",
		Limit:     64,
		Valid: func() schema.Resume {
			doc := docWithWorkField(func(e *schema.WorkEntry) {})
			sec := doc.Content["work"]
			sec.IconKey = strp(strings.Repeat("a", 64))
			doc.Content["work"] = sec
			return doc
		},
		Invalid: func() schema.Resume {
			doc := docWithWorkField(func(e *schema.WorkEntry) {})
			sec := doc.Content["work"]
			sec.IconKey = strp(strings.Repeat("a", 65))
			doc.Content["work"] = sec
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "displayname-maxlength",
		BoundPath: "$.$defs.displayName.maxLength",
		Keyword:   "maxLength",
		Limit:     80,
		Valid: func() schema.Resume {
			doc := docWithWorkField(func(e *schema.WorkEntry) {})
			sec := doc.Content["work"]
			sec.DisplayName = strp(strings.Repeat("a", 80))
			doc.Content["work"] = sec
			return doc
		},
		Invalid: func() schema.Resume {
			doc := docWithWorkField(func(e *schema.WorkEntry) {})
			sec := doc.Content["work"]
			sec.DisplayName = strp(strings.Repeat("a", 81))
			doc.Content["work"] = sec
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "city-country-name-maxlength",
		BoundPath: "$.$defs.workEntry.allOf[1].properties.city.maxLength",
		Aliases: []string{
			"$.$defs.workEntry.allOf[1].properties.country.maxLength",
			"$.$defs.educationEntry.allOf[1].properties.city.maxLength",
			"$.$defs.educationEntry.allOf[1].properties.country.maxLength",
			"$.$defs.customEntry.allOf[1].properties.city.maxLength",
			"$.$defs.skillEntry.allOf[1].properties.name.maxLength",
			"$.$defs.languageEntry.allOf[1].properties.name.maxLength",
		},
		Keyword: "maxLength",
		Limit:   120,
		Valid: func() schema.Resume {
			return docWithWorkField(func(e *schema.WorkEntry) { e.City = strp(strings.Repeat("a", 120)) })
		},
		Invalid: func() schema.Resume {
			return docWithWorkField(func(e *schema.WorkEntry) { e.City = strp(strings.Repeat("a", 121)) })
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "fullname-jobtitle-title-maxlength",
		BoundPath: "$.$defs.personalDetails.properties.fullName.maxLength",
		Aliases: []string{
			"$.$defs.personalDetails.properties.headline.maxLength",
			"$.$defs.workEntry.allOf[1].properties.jobTitle.maxLength",
			"$.$defs.workEntry.allOf[1].properties.employer.maxLength",
			"$.$defs.educationEntry.allOf[1].properties.degree.maxLength",
			"$.$defs.educationEntry.allOf[1].properties.school.maxLength",
			"$.$defs.certificateEntry.allOf[1].properties.title.maxLength",
			"$.$defs.certificateEntry.allOf[1].properties.issuer.maxLength",
			"$.$defs.projectEntry.allOf[1].properties.title.maxLength",
			"$.$defs.projectEntry.allOf[1].properties.subtitle.maxLength",
			"$.$defs.customEntry.allOf[1].properties.title.maxLength",
			"$.$defs.customEntry.allOf[1].properties.subtitle.maxLength",
		},
		Keyword: "maxLength",
		Limit:   160,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.FullName = strp(strings.Repeat("a", 160))
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.FullName = strp(strings.Repeat("a", 161))
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "personaldetail-value-maxlength",
		BoundPath: "$.$defs.personalDetail.properties.value.maxLength",
		Keyword:   "maxLength",
		Limit:     256,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = []schema.PersonalDetail{
				{ID: boundUUID(0), Type: schema.TypeCustom, Value: strings.Repeat("a", 256), IsHidden: false},
			}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = []schema.PersonalDetail{
				{ID: boundUUID(0), Type: schema.TypeCustom, Value: strings.Repeat("a", 257), IsHidden: false},
			}
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "photo-key-maxlength",
		BoundPath: "$.$defs.photo.properties.key.maxLength",
		Keyword:   "maxLength",
		Limit:     512,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Photo = &schema.Photo{Key: "a" + strings.Repeat("b", 511)}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Photo = &schema.Photo{Key: "a" + strings.Repeat("b", 512)}
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "link-maxlength",
		BoundPath: "$.$defs.link.maxLength",
		Keyword:   "maxLength",
		Limit:     2048,
		Valid: func() schema.Resume {
			const prefix = "https://a.co/"
			link := prefix + strings.Repeat("b", 2048-len(prefix))
			return docWithWorkField(func(e *schema.WorkEntry) { e.EmployerLink = &link })
		},
		Invalid: func() schema.Resume {
			const prefix = "https://a.co/"
			link := prefix + strings.Repeat("b", 2049-len(prefix))
			return docWithWorkField(func(e *schema.WorkEntry) { e.EmployerLink = &link })
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:      "richtext-maxlength-codepoints",
		BoundPath: "$.$defs.richText.maxLength",
		Keyword:   "maxLength",
		Limit:     16384,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			text := strings.Repeat("a", 16384)
			doc.Content = map[string]schema.Section{"profile": profileSection([]schema.ProfileEntry{{ID: boundUUID(0), Text: &text}})}
			doc.Customization.Layout.Sections.Main = []string{"profile"}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			text := strings.Repeat("a", 16385)
			doc.Content = map[string]schema.Section{"profile": profileSection([]schema.ProfileEntry{{ID: boundUUID(0), Text: &text}})}
			doc.Customization.Layout.Sections.Main = []string{"profile"}
			return doc
		},
		IssueSubstr: "maxLength",
	},
	{
		Name:    "personaldetail-id-format-uuid",
		Keyword: "",
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = []schema.PersonalDetail{
				{ID: boundUUID(0), Type: schema.TypeCustom, Value: "x", IsHidden: false},
			}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			doc.PersonalDetails.Details = []schema.PersonalDetail{
				{ID: "not-a-uuid", Type: schema.TypeCustom, Value: "x", IsHidden: false},
			}
			return doc
		},
		IssueSubstr: "uuid",
	},
	{
		Name:    "workentry-employerlink-format-uri",
		Keyword: "",
		Valid: func() schema.Resume {
			link := "https://a.co/"
			return docWithWorkField(func(e *schema.WorkEntry) { e.EmployerLink = &link })
		},
		Invalid: func() schema.Resume {
			link := "https://a.co/\n"
			return docWithWorkField(func(e *schema.WorkEntry) { e.EmployerLink = &link })
		},
		IssueSubstr: "uri",
	},
}

func TestBoundsMatrix_LimitAndLimitPlusOne(t *testing.T) {
	seen := map[string]bool{}
	for _, bc := range namedBounds {
		if seen[bc.Name] {
			t.Fatalf("duplicate bound case name %q", bc.Name)
		}
		seen[bc.Name] = true

		bc := bc
		t.Run(bc.Name+"/at_limit_accepted", func(t *testing.T) {
			if err := resume.ValidateForStore(bc.Valid()); err != nil {
				t.Errorf("expected the at-limit (%d) document to be accepted, got: %v", bc.Limit, err)
			}
		})
		t.Run(bc.Name+"/limit_plus_one_rejected", func(t *testing.T) {
			err := resume.ValidateForStore(bc.Invalid())
			if err == nil {
				t.Fatalf("expected the over-limit (%d+1) document to be rejected, got nil", bc.Limit)
			}
			if bc.IssueSubstr != "" && !strings.Contains(err.Error(), bc.IssueSubstr) {
				t.Errorf("expected an issue containing %q, got: %v", bc.IssueSubstr, err)
			}
		})
	}
}

func walkSchemaBounds(t *testing.T) map[string]map[int][]string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(schema.RawSchema, &doc); err != nil {
		t.Fatalf("parsing schema.RawSchema: %v", err)
	}

	found := map[string]map[int][]string{
		"maxLength":     {},
		"maxItems":      {},
		"maxProperties": {},
	}

	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch val := v.(type) {
		case map[string]any:
			for _, kw := range []string{"maxLength", "maxItems", "maxProperties"} {
				raw, ok := val[kw]
				if !ok {
					continue
				}
				n, ok := raw.(float64)
				if !ok {
					t.Fatalf("%s.%s: expected a number, got %T", path, kw, raw)
				}
				limit := int(n)
				found[kw][limit] = append(found[kw][limit], path+"."+kw)
			}
			for key, child := range val {
				walk(path+"."+key, child)
			}
		case []any:
			for i, child := range val {
				walk(fmt.Sprintf("%s[%d]", path, i), child)
			}
		}
	}
	walk("$", doc)
	return found
}

func missingBoundCoverage(found map[string]map[int][]string, cases []boundCase) []string {
	covered := map[string]map[int]map[string]bool{}
	for _, bc := range cases {
		if bc.Keyword == "" {
			continue
		}
		if covered[bc.Keyword] == nil {
			covered[bc.Keyword] = map[int]map[string]bool{}
		}
		if covered[bc.Keyword][bc.Limit] == nil {
			covered[bc.Keyword][bc.Limit] = map[string]bool{}
		}
		covered[bc.Keyword][bc.Limit][bc.BoundPath] = true
		for _, alias := range bc.Aliases {
			covered[bc.Keyword][bc.Limit][alias] = true
		}
	}

	var missing []string
	for _, kw := range []string{"maxLength", "maxItems", "maxProperties"} {
		limits := make([]int, 0, len(found[kw]))
		for limit := range found[kw] {
			limits = append(limits, limit)
		}
		sort.Ints(limits)
		for _, limit := range limits {
			paths := append([]string(nil), found[kw][limit]...)
			sort.Strings(paths)
			for _, path := range paths {
				if !covered[kw][limit][path] {
					missing = append(missing, fmt.Sprintf(
						"%s: %d at %s -- no covering test (neither a boundCase.BoundPath nor a listed Aliases entry)",
						kw, limit, path,
					))
				}
			}
		}
	}
	return missing
}

func TestBoundsCompletenessGuard(t *testing.T) {
	found := walkSchemaBounds(t)
	for _, msg := range missingBoundCoverage(found, namedBounds) {
		t.Errorf("schema.RawSchema declares %s -- add a boundCase or explicitly list it in an existing case's Aliases", msg)
	}
}

func TestMissingBoundCoverage_CatchesNewPathReusingAnExistingLimit(t *testing.T) {
	realFullNamePath := "$.$defs.personalDetails.properties.fullName.maxLength"
	found := map[string]map[int][]string{
		"maxLength": {
			160: {realFullNamePath, "$.$defs.tagline.maxLength"},
		},
	}
	missing := missingBoundCoverage(found, namedBounds)
	if len(missing) != 1 {
		t.Fatalf("expected exactly 1 missing entry, got %d: %v", len(missing), missing)
	}
	if !strings.Contains(missing[0], "$.$defs.tagline.maxLength") {
		t.Fatalf("expected the missing entry to name $.$defs.tagline.maxLength, got: %v", missing[0])
	}
	for _, m := range missing {
		if strings.Contains(m, realFullNamePath) {
			t.Fatalf("fullName's own path was reported missing, but it is namedBounds' directly-tested BoundPath: %v", missing)
		}
	}
}

const boundsFixturesSubdir = "bounds"

type manifestRow struct {
	File        string `json:"file"`
	BoundPath   string `json:"boundPath,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Expect      string `json:"expect"`
	StoreExpect string `json:"storeExpect"`
}

type boundsManifestDoc struct {
	Bounds        []manifestRow `json:"bounds"`
	StoreFixtures []manifestRow `json:"storeFixtures"`
}

var storeFixtureSchemaExpectations = map[string]bool{
	"invalid-duplicate-detail-id.json":                true,
	"invalid-duplicate-entry-id.json":                 true,
	"invalid-hostile-sectiontype-constructor.json":    false,
	"invalid-hostile-sectiontype-hasownproperty.json": false,
	"invalid-hostile-sectiontype-proto.json":          false,
	"invalid-layout-duplicate-across-arrays.json":     true,
	"invalid-layout-missing-content-key.json":         true,
	"invalid-layout-orphan-content-key.json":          true,
	"invalid-missing-dates-start.json":                false,
	"invalid-oversize-richtext-bytes.json":            true,
	"invalid-personal-detail-url-scheme.json":         false,
	"invalid-reversed-date-range.json":                true,
	"valid-unique-entry-id.json":                      true,
}

func generatedBoundsCorpus(t *testing.T) (files map[string][]byte, doc boundsManifestDoc) {
	t.Helper()
	files = map[string][]byte{}

	for _, bc := range namedBounds {
		validBytes, err := resume.AssembleCanonical(bc.Valid())
		if err != nil {
			t.Fatalf("%s: assembling valid doc: %v", bc.Name, err)
		}
		invalidBytes, err := resume.AssembleCanonical(bc.Invalid())
		if err != nil {
			t.Fatalf("%s: assembling invalid doc: %v", bc.Name, err)
		}

		validFile := boundsFixturesSubdir + "/" + bc.Name + "-valid.json"
		invalidFile := boundsFixturesSubdir + "/" + bc.Name + "-invalid.json"
		files[validFile] = append(validBytes, '\n')
		files[invalidFile] = append(invalidBytes, '\n')

		invalidExpect := "invalid"
		if bc.StoreOnly {
			invalidExpect = "valid"
		}
		doc.Bounds = append(doc.Bounds,
			manifestRow{File: validFile, BoundPath: bc.BoundPath, Limit: bc.Limit, Expect: "valid", StoreExpect: "valid"},
			manifestRow{File: invalidFile, BoundPath: bc.BoundPath, Limit: bc.Limit, Expect: invalidExpect, StoreExpect: "invalid"},
		)
	}
	sort.Slice(doc.Bounds, func(i, j int) bool { return doc.Bounds[i].File < doc.Bounds[j].File })

	storeDir := filepath.Join(schemaFixturesDir(t), "store")
	for _, name := range listJSONFixtures(t, storeDir) {
		schemaValid, ok := storeFixtureSchemaExpectations[name]
		if !ok {
			t.Fatalf("%s: no schema-verdict expectation in storeFixtureSchemaExpectations", name)
		}
		storeValid, ok := storeFixtureExpectations[name]
		if !ok {
			t.Fatalf("%s: no expectation in storeFixtureExpectations", name)
		}
		expect := "invalid"
		if schemaValid {
			expect = "valid"
		}
		storeExpect := "invalid"
		if storeValid {
			storeExpect = "valid"
		}
		doc.StoreFixtures = append(doc.StoreFixtures, manifestRow{
			File: "store/" + name, Expect: expect, StoreExpect: storeExpect,
		})
	}
	sort.Slice(doc.StoreFixtures, func(i, j int) bool { return doc.StoreFixtures[i].File < doc.StoreFixtures[j].File })

	return files, doc
}

func marshalManifest(t *testing.T, doc boundsManifestDoc) []byte {
	t.Helper()
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshaling manifest: %v", err)
	}
	return append(out, '\n')
}

func TestBoundsCorpus_MatchesCommitted(t *testing.T) {
	fixturesDir := schemaFixturesDir(t)
	files, doc := generatedBoundsCorpus(t)
	files["bounds/manifest.json"] = marshalManifest(t, doc)

	regenerate := os.Getenv("REGENERATE_BOUNDS_CORPUS") == "1"

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		want := files[name]
		path := filepath.Join(fixturesDir, filepath.FromSlash(name))

		if regenerate {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("%s: creating directory: %v", name, err)
			}
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatalf("%s: writing: %v", name, err)
			}
			continue
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: reading committed file: %v (run with REGENERATE_BOUNDS_CORPUS=1 to create it)", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: committed content has drifted from namedBounds (run with REGENERATE_BOUNDS_CORPUS=1 to regenerate)", name)
		}
	}

	if !regenerate {
		entries, err := os.ReadDir(filepath.Join(fixturesDir, boundsFixturesSubdir))
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("reading %s: %v", boundsFixturesSubdir, err)
		}
		for _, e := range entries {
			name := boundsFixturesSubdir + "/" + e.Name()
			if _, ok := files[name]; !ok {
				t.Errorf("%s is committed but no longer produced by namedBounds -- stale generated file", name)
			}
		}
	}
}

func TestSchemaVerdictParity_Go(t *testing.T) {
	sch := resume.CompiledSchemaPointerForTest()
	fixturesDir := schemaFixturesDir(t)

	checkVerdict := func(t *testing.T, label string, raw []byte, wantValid bool) {
		t.Helper()
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("%s: parsing JSON: %v", label, err)
		}
		gotValid := sch.Validate(instance) == nil
		if gotValid != wantValid {
			t.Errorf("%s: jsonschema/v6 says valid=%v, manifest/convention says valid=%v", label, gotValid, wantValid)
		}
	}

	t.Run("bounds_corpus", func(t *testing.T) {
		manifestBytes, err := os.ReadFile(filepath.Join(fixturesDir, boundsFixturesSubdir, "manifest.json"))
		if err != nil {
			t.Fatalf("reading committed manifest.json: %v", err)
		}
		var manifest boundsManifestDoc
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			t.Fatalf("parsing committed manifest.json: %v", err)
		}
		if len(manifest.Bounds) == 0 {
			t.Fatal("committed manifest.json has no bounds rows")
		}
		for _, row := range manifest.Bounds {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, filepath.FromSlash(row.File)))
			if err != nil {
				t.Fatalf("%s: reading: %v", row.File, err)
			}
			checkVerdict(t, row.File, raw, row.Expect == "valid")
		}

		if len(manifest.StoreFixtures) == 0 {
			t.Fatal("committed manifest.json has no storeFixtures rows")
		}
		for _, row := range manifest.StoreFixtures {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, filepath.FromSlash(row.File)))
			if err != nil {
				t.Fatalf("%s: reading: %v", row.File, err)
			}
			checkVerdict(t, row.File, raw, row.Expect == "valid")
		}
	})

	t.Run("top_level_fixtures", func(t *testing.T) {
		for _, name := range listJSONFixtures(t, fixturesDir) {
			raw, err := os.ReadFile(filepath.Join(fixturesDir, name))
			if err != nil {
				t.Fatalf("%s: reading: %v", name, err)
			}
			wantValid := !strings.HasPrefix(name, "invalid-")
			checkVerdict(t, name, raw, wantValid)
		}
	})
}
