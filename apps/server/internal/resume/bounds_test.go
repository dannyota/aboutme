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

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

// bounds_test.go is the size-bounds harness (task brief Step 3/3b): a
// named-bound limit/limit+1 matrix, a completeness guard that fails loudly
// if a schema bound has no covering test, and a generated cross-language
// verdict-parity corpus consumed by both this file (jsonschema/v6) and
// packages/schema/test/bounds-parity.test.ts (ajv).

// --- small construction helpers ---

func strp(s string) *string { return &s }

// boundUUID returns a deterministic, distinct, format:uuid-valid string for
// index n. Committed generated fixtures must never depend on a random or
// time-based source (CLAUDE.md: tests/generators are deterministic).
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

// baseDocForBounds is a fully schema-valid, fully store-valid starting point
// (empty content) every bound case mutates a copy of.
func baseDocForBounds() schema.Resume {
	return schema.Resume{
		SchemaVersion:   1,
		PersonalDetails: schema.PersonalDetails{FullName: strp("Ada Lovelace")},
		Content:         map[string]schema.Section{},
		Customization:   baseCustomizationForBounds(),
	}
}

// Section builders that leave DisplayName/IconKey nil (absent) unless a
// case specifically sets them -- both are optional in resume.schema.json,
// and an empty (as opposed to absent) iconKey fails its own pattern.
func customSection(entries []schema.CustomEntry) schema.Section {
	if entries == nil {
		entries = []schema.CustomEntry{}
	}
	return schema.Section{SectionType: schema.SectionTypeCustom, CustomEntries: entries}
}

func workSection(entries []schema.WorkEntry) schema.Section {
	if entries == nil {
		entries = []schema.WorkEntry{}
	}
	return schema.Section{SectionType: schema.Work, WorkEntries: entries}
}

func profileSection(entries []schema.ProfileEntry) schema.Section {
	if entries == nil {
		entries = []schema.ProfileEntry{}
	}
	return schema.Section{SectionType: schema.Profile, ProfileEntries: entries}
}

func sortedContentKeys(content map[string]schema.Section) []string {
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// lettersContent returns n content sections keyed "a", "b", "c", ... (n <=
// 26): all lowercase-letter keys, so every one satisfies sectionKey's
// `^[a-z]+$` pattern branch regardless of count.
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

// docWithWorkField builds a single-work-entry document (layout-consistent:
// "work" is placed in layout.sections.main) and lets the caller mutate that
// one entry -- the shared shape behind every entry-level maxLength case
// below (city/country/name class, fullName/.../title class via
// personalDetails instead, etc.)
func docWithWorkField(mutate func(*schema.WorkEntry)) schema.Resume {
	doc := baseDocForBounds()
	entry := schema.WorkEntry{ID: boundUUID(0)}
	mutate(&entry)
	doc.Content = map[string]schema.Section{"work": workSection([]schema.WorkEntry{entry})}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	return doc
}

// padDocToCanonicalBytes builds a document whose AssembleCanonical output is
// exactly target bytes: a "work" section holding as many full (<=16384
// byte) ASCII-padded descriptions as needed, with the last entry's
// description trimmed/grown by the exact remaining delta. ASCII 'a' never
// needs JSON escaping, so each character changes the marshaled length by
// exactly one byte -- the single delta-adjustment below always lands
// exactly on target.
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

// --- the named-bound matrix ---

// boundCase is one schema (or store-layer) size bound, exercised by an
// at-limit document (schema-valid, and store-valid unless noted otherwise)
// and a one-past-the-limit document (rejected). Keyword+Limit is what the
// completeness guard (TestBoundsCompletenessGuard) cross-checks against
// every maxLength/maxItems/maxProperties declaration found by walking
// schema.RawSchema; Keyword is "" for MaxDocumentBytes, which is a
// store-layer bound with no corresponding JSON-Schema keyword at all.
type boundCase struct {
	Name        string
	BoundPath   string
	Keyword     string
	Limit       int
	Valid       func() schema.Resume
	Invalid     func() schema.Resume
	StoreOnly   bool // true: JSON-Schema layer accepts BOTH valid and invalid; only the store layer distinguishes them
	IssueSubstr string
}

// namedBounds is the harness's exercised-bounds inventory: every
// maxLength/maxItems/maxProperties bound in resume.schema.json, one pair
// per distinct (keyword, limit) class (a future value repeated across
// several fields, e.g. maxLength:160 on both fullName and jobTitle, needs
// only one representative pair -- the JSON-Schema maxLength check is
// generic string-length logic, identical regardless of field name), plus
// the two store-layer-only bounds (MaxDocumentBytes, rich-text byte-exact).
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
			text := strings.Repeat("é", 8192) // 8192 code points, 16384 UTF-8 bytes
			doc.Content = map[string]schema.Section{"profile": profileSection([]schema.ProfileEntry{{ID: boundUUID(0), Text: &text}})}
			doc.Customization.Layout.Sections.Main = []string{"profile"}
			return doc
		},
		Invalid: func() schema.Resume {
			doc := baseDocForBounds()
			text := strings.Repeat("é", 8192) + "a" // 8193 code points (still <16384), 16385 bytes
			doc.Content = map[string]schema.Section{"profile": profileSection([]schema.ProfileEntry{{ID: boundUUID(0), Text: &text}})}
			doc.Customization.Layout.Sections.Main = []string{"profile"}
			return doc
		},
		StoreOnly:   true,
		IssueSubstr: "rich-text-byte-length",
	},
	{
		Name:      "content-maxproperties",
		BoundPath: "$defs.content.maxProperties",
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
			// Keep layout.sections.main at 24 (its own limit) so this case
			// isolates content.maxProperties -- the 25th key is left an
			// orphan (a benign, expected store-only side issue), not also
			// pushed over layout's own maxItems bound.
			doc.Customization.Layout.Sections.Main = keys[:24]
			return doc
		},
		IssueSubstr: "maxProperties",
	},
	{
		Name:      "section-entries-maxitems",
		BoundPath: "$defs.section.oneOf[*].properties.entries.maxItems",
		Keyword:   "maxItems",
		Limit:     64,
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
		BoundPath: "$defs.personalDetails.properties.details.maxItems",
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
		BoundPath: "$defs.customization.properties.layout.properties.sections.properties.main.maxItems",
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
			// content.maxProperties stays at 24 (still valid); main gets one
			// EXTRA key never placed in content ("y"), isolating
			// layout.sections.main's own maxItems bound. The orphan "y" is a
			// benign, expected store-only side issue.
			main := append(sortedContentKeys(doc.Content), "y")
			doc.Customization.Layout.Sections.Main = main
			return doc
		},
		IssueSubstr: "maxItems",
	},
	{
		Name:      "sectionkey-maxlength",
		BoundPath: "$defs.sectionKey.maxLength",
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
		BoundPath: "$defs.personalDetail.properties.label.maxLength",
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
		BoundPath: "$defs.iconKey.maxLength",
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
		BoundPath: "$defs.displayName.maxLength",
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
		BoundPath: "$defs.workEntry.allOf[1].properties.city.maxLength",
		Keyword:   "maxLength",
		Limit:     120,
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
		BoundPath: "$defs.personalDetails.properties.fullName.maxLength",
		Keyword:   "maxLength",
		Limit:     160,
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
		BoundPath: "$defs.personalDetail.properties.value.maxLength",
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
		BoundPath: "$defs.photo.properties.key.maxLength",
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
		BoundPath: "$defs.link.maxLength",
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
		BoundPath: "$defs.richText.maxLength",
		Keyword:   "maxLength",
		Limit:     16384,
		Valid: func() schema.Resume {
			doc := baseDocForBounds()
			text := strings.Repeat("a", 16384) // ASCII: code points == bytes
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

// --- completeness guard ---

// walkSchemaBounds parses schema.RawSchema generically (not through the
// compiled *jsonschema.Schema, which resolves $refs and would collapse
// e.g. richText's single maxLength declaration into many call sites) and
// walks EVERY object node for a maxLength/maxItems/maxProperties key,
// recording every occurrence's (keyword, limit). A future schema bound with
// no corresponding pair in namedBounds fails TestBoundsCompletenessGuard
// loudly instead of silently shipping unenforced.
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

func TestBoundsCompletenessGuard(t *testing.T) {
	found := walkSchemaBounds(t)

	covered := map[string]map[int]bool{
		"maxLength":     {},
		"maxItems":      {},
		"maxProperties": {},
	}
	for _, bc := range namedBounds {
		if bc.Keyword == "" {
			continue
		}
		covered[bc.Keyword][bc.Limit] = true
	}

	for _, kw := range []string{"maxLength", "maxItems", "maxProperties"} {
		limits := make([]int, 0, len(found[kw]))
		for limit := range found[kw] {
			limits = append(limits, limit)
		}
		sort.Ints(limits)
		for _, limit := range limits {
			if !covered[kw][limit] {
				t.Errorf(
					"schema.RawSchema declares %s: %d (at %s) with no corresponding limit/limit+1 test in namedBounds -- add a boundCase for it",
					kw, limit, strings.Join(found[kw][limit], ", "),
				)
			}
		}
	}
}

// --- generated cross-language verdict-parity corpus (Step 3b, D1(e)) ---

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

// storeFixtureSchemaExpectations is each fixtures/store/*.json file's
// JSON-Schema-LAYER verdict specifically -- distinct from
// storeFixtureExpectations (validate_test.go), which is the OVERALL
// ValidateForStore verdict. Most of these files are schema-valid (the
// store-layer aggregate rules they exist to exercise -- entry-id
// duplicates, byte length, layout aggregate, date-range order -- are simply
// not expressible in JSON Schema at all); the three hostile-sectiontype
// fixtures and the missing-dates-start/personal-detail-url-scheme fixtures
// are genuinely schema-invalid too (verified directly against each file's
// content: an out-of-enum sectionType, a required key absent, and a
// scheme-restricted value respectively).
var storeFixtureSchemaExpectations = map[string]bool{ // name -> schema-valid
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

// generatedBoundsCorpus computes the corpus files + manifest this task
// commits, purely from namedBounds + storeFixtureSchemaExpectations/
// storeFixtureExpectations -- the single generation function both the drift
// check and (if REGENERATE_BOUNDS_CORPUS=1) the regeneration path use.
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

// TestBoundsCorpus_MatchesCommitted is the generated-artifact drift check
// every other generated artifact in this repo has (rawschema.go/
// rawschema_test.go, gen/go/resume.go/gen.test.ts): it recomputes the
// corpus + manifest from source (namedBounds) and asserts the committed
// files are byte-identical. Set REGENERATE_BOUNDS_CORPUS=1 to write the
// freshly computed content instead of failing -- the regeneration path for
// a deliberate bound change.
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

	// Also catch a committed file namedBounds no longer produces (a removed
	// or renamed bound case leaving a stale file behind).
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

// --- cross-language verdict parity: the Go half (D1(e)) ---

// TestSchemaVerdictParity_Go runs jsonschema/v6 -- via the SAME compiled
// schema ValidateForStore uses -- over the COMMITTED bounds corpus, the
// COMMITTED manifest's store-fixture rows, and every existing
// packages/schema/fixtures/*.json top-level fixture (naming convention).
// packages/schema/test/bounds-parity.test.ts is the other half: it reads
// the identical committed files and runs ajv over them, asserting the same
// verdicts. A disagreement in either direction is a red build in whichever
// side's test that is -- this test and that one, not the shared schema
// file, are what make "one schema, both languages" true.
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
