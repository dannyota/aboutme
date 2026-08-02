package resume_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

// schemaFixturesDir locates packages/schema/fixtures relative to this
// package -- apps/server/internal/resume -> ../../../../packages/schema/fixtures.
func schemaFixturesDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "schema", "fixtures"))
	if err != nil {
		t.Fatalf("resolving fixtures dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixtures dir %s does not exist: %v", dir, err)
	}
	return dir
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(schemaFixturesDir(t), name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// rawParts is the on-disk full-document shape split into DecodeParts'
// three-part-plus-version input, mirroring how a Postgres row's four
// columns (schema_version, personal_details, content, customization) would
// be read back.
type rawParts struct {
	SchemaVersion   int32           `json:"schemaVersion"`
	PersonalDetails json.RawMessage `json:"personalDetails"`
	Content         json.RawMessage `json:"content"`
	Customization   json.RawMessage `json:"customization"`
}

func splitFixture(t *testing.T, name string) rawParts {
	t.Helper()
	var parts rawParts
	if err := json.Unmarshal(readFixture(t, name), &parts); err != nil {
		t.Fatalf("splitting fixture %s: %v", name, err)
	}
	return parts
}

// roundTripFixtures are every fixture Step 1 requires a byte-stable
// parts->doc->parts round trip for: minimal, full, and the two draft-
// permissive fixtures (draft-partial has an absent employer/city/etc.;
// draft-cleared-name-empty-section has an explicitly-cleared "" fullName
// and a section with neither displayName nor iconKey present).
var roundTripFixtures = []string{
	"minimal.json",
	"full.json",
	"draft-partial.json",
	"draft-cleared-name-empty-section.json",
}

func TestCodec_RoundTrip_ByteStable(t *testing.T) {
	for _, name := range roundTripFixtures {
		t.Run(name, func(t *testing.T) {
			parts := splitFixture(t, name)

			doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
			if err != nil {
				t.Fatalf("DecodeParts: %v", err)
			}

			// Fidelity: EncodeParts must not silently drop or rewrite a field
			// (e.g. a stray `json:"-"` or missing struct tag on a future
			// schema field). Comparing doc against the raw fixture bytes
			// isn't the right check (see the byte-stability comment below --
			// omitempty legitimately normalizes some inputs), but doc and
			// doc2 (decoded back from doc's OWN EncodeParts output) both pass
			// through the identical marshaler, so AssembleCanonical(doc) and
			// AssembleCanonical(doc2) must match byte-for-byte: any field the
			// codec dropped on the way out shows up here as a difference,
			// even though the earlier byte-stability checks below (which
			// only ever compare pass-1-output against pass-2-output, never
			// against doc itself) would not catch it.
			docCanonical, err := resume.AssembleCanonical(doc)
			if err != nil {
				t.Fatalf("AssembleCanonical(doc): %v", err)
			}

			pd1, c1, cu1, err := resume.EncodePartsForTest(doc)
			if err != nil {
				t.Fatalf("EncodeParts (pass 1): %v", err)
			}

			doc2, err := resume.DecodeParts(pd1, c1, cu1, parts.SchemaVersion)
			if err != nil {
				t.Fatalf("DecodeParts (pass 2): %v", err)
			}

			doc2Canonical, err := resume.AssembleCanonical(doc2)
			if err != nil {
				t.Fatalf("AssembleCanonical(doc2): %v", err)
			}
			if !bytes.Equal(docCanonical, doc2Canonical) {
				t.Errorf("EncodeParts/DecodeParts lost fidelity -- AssembleCanonical(doc) != AssembleCanonical(doc2):\n doc:  %s\n doc2: %s", docCanonical, doc2Canonical)
			}

			pd2, c2, cu2, err := resume.EncodePartsForTest(doc2)
			if err != nil {
				t.Fatalf("EncodeParts (pass 2): %v", err)
			}

			// Byte-stable: once a document has passed through the codec's own
			// EncodeParts once, encoding it again produces byte-identical
			// parts. (Comparing pass 0's hand-authored fixture bytes directly
			// against pass 1 is not the right invariant: e.g. an explicit
			// `"details": []` in the fixture and an omitted `details` key are
			// both "no details" and both marshal identically via `omitempty`
			// -- that's a legitimate normalization, not drift.)
			if !bytes.Equal(pd1, pd2) {
				t.Errorf("personalDetails not byte-stable:\n pass1=%s\n pass2=%s", pd1, pd2)
			}
			if !bytes.Equal(c1, c2) {
				t.Errorf("content not byte-stable:\n pass1=%s\n pass2=%s", c1, c2)
			}
			if !bytes.Equal(cu1, cu2) {
				t.Errorf("customization not byte-stable:\n pass1=%s\n pass2=%s", cu1, cu2)
			}

			// Once normalized (pass 1 onward), the decoded VALUE is also a
			// fixed point: decoding pass 2's parts must reproduce doc2
			// exactly.
			doc3, err := resume.DecodeParts(pd2, c2, cu2, parts.SchemaVersion)
			if err != nil {
				t.Fatalf("DecodeParts (pass 3): %v", err)
			}
			if !reflect.DeepEqual(doc2, doc3) {
				t.Errorf("decoded document not stable after normalization:\n doc2=%#v\n doc3=%#v", doc2, doc3)
			}
		})
	}
}

// TestCodec_PartsNeverContainSchemaVersion is D4: the three jsonb parts must
// never carry a "schemaVersion" key -- it lives in the row's own
// schema_version column, injected back in only by AssembleCanonical/
// DecodeParts, never stored alongside personalDetails/content/customization.
func TestCodec_PartsNeverContainSchemaVersion(t *testing.T) {
	parts := splitFixture(t, "full.json")
	doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
	if err != nil {
		t.Fatalf("DecodeParts: %v", err)
	}

	pd, content, cu, err := resume.EncodePartsForTest(doc)
	if err != nil {
		t.Fatalf("EncodeParts: %v", err)
	}

	for label, part := range map[string]json.RawMessage{
		"personalDetails": pd,
		"content":         content,
		"customization":   cu,
	} {
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(part, &generic); err != nil {
			t.Fatalf("%s: unmarshaling generically: %v", label, err)
		}
		if _, ok := generic["schemaVersion"]; ok {
			t.Errorf("%s contains a schemaVersion key, want it absent (D4)", label)
		}
	}
}

// TestCodec_DraftPermissive_AbsentVsEmptyDistinguishable proves the spec's
// "never fabricate a sentinel" rule survives DecodeParts: an absent field
// (never typed) and an explicitly-cleared "" field (typed then deleted) must
// stay distinguishable after decode -- draft-partial.json's work entry omits
// employer/city/country/dates/description entirely (never typed), while
// full.json's second custom entry sets titleLink/subtitle/city to "" explicitly
// (cleared).
func TestCodec_DraftPermissive_AbsentVsEmptyDistinguishable(t *testing.T) {
	t.Run("absent field decodes to nil, not a fabricated empty string", func(t *testing.T) {
		parts := splitFixture(t, "draft-partial.json")
		doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
		if err != nil {
			t.Fatalf("DecodeParts: %v", err)
		}
		work, ok := doc.Content["work"]
		if !ok || len(work.WorkEntries) != 1 {
			t.Fatalf("expected exactly one work entry, got %#v", doc.Content["work"])
		}
		entry := work.WorkEntries[0]
		if entry.JobTitle == nil || *entry.JobTitle != "Engineer" {
			t.Fatalf("jobTitle: got %v, want a present \"Engineer\"", entry.JobTitle)
		}
		for name, got := range map[string]*string{
			"employer":    entry.Employer,
			"city":        entry.City,
			"country":     entry.Country,
			"description": entry.Description,
		} {
			if got != nil {
				t.Errorf("%s: got %q (present), want nil (never entered, absent from the fixture)", name, *got)
			}
		}
	})

	t.Run("explicitly-cleared field decodes to a pointer to empty string, not nil", func(t *testing.T) {
		parts := splitFixture(t, "full.json")
		doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
		if err != nil {
			t.Fatalf("DecodeParts: %v", err)
		}
		custom, ok := doc.Content["a6a0a5fa-7fe4-4d52-be40-0da2db95de12"]
		if !ok || len(custom.CustomEntries) != 2 {
			t.Fatalf("expected exactly two custom entries, got %#v", doc.Content["a6a0a5fa-7fe4-4d52-be40-0da2db95de12"])
		}
		entry := custom.CustomEntries[1] // "Hackathon Winner": titleLink/subtitle/city/description all explicitly ""
		for name, got := range map[string]*string{
			"titleLink":   entry.TitleLink,
			"subtitle":    entry.Subtitle,
			"city":        entry.City,
			"description": entry.Description,
		} {
			if got == nil {
				t.Errorf("%s: got nil (absent), want a present pointer to \"\" (explicitly cleared in the fixture)", name)
				continue
			}
			if *got != "" {
				t.Errorf("%s: got %q, want \"\"", name, *got)
			}
		}
	})
}

// TestCodec_UnknownField_StrictDecodeError proves DecodeParts strict-decodes
// every part: an unknown key anywhere in personalDetails or customization,
// or a field foreign to an entry's sectionType inside content, is a decode
// error rather than a silently-dropped field.
func TestCodec_UnknownField_StrictDecodeError(t *testing.T) {
	base := splitFixture(t, "minimal.json")

	cases := []struct {
		name            string
		personalDetails json.RawMessage
		content         json.RawMessage
		customization   json.RawMessage
	}{
		{
			name:            "unknown top-level key in personalDetails",
			personalDetails: json.RawMessage(`{"fullName":"Ada","details":[],"bogus":true}`),
			content:         base.Content,
			customization:   base.Customization,
		},
		{
			name:            "a schemaVersion key smuggled into personalDetails",
			personalDetails: json.RawMessage(`{"fullName":"Ada","details":[],"schemaVersion":1}`),
			content:         base.Content,
			customization:   base.Customization,
		},
		{
			name:            "unknown top-level key in customization",
			personalDetails: base.PersonalDetails,
			content:         base.Content,
			customization:   json.RawMessage(`{"font":{"family":"Inter","baseSizePx":14},"colors":{"primary":"#1a1a1a","text":"#1a1a1a","background":"#ffffff"},"spacing":{"sectionGap":16,"entryGap":8,"lineHeight":1.4},"heading":{"style":"normal","showRule":false},"layout":{"columns":1,"sections":{"main":[],"sidebar":[]}},"sectionDisplay":{"skill":{"style":"text"},"language":{"style":"text"}},"pageFormat":"a4","dateFormat":"MM/YYYY","bogus":true}`),
		},
		{
			name:            "a field foreign to workEntry inside content",
			personalDetails: base.PersonalDetails,
			content:         json.RawMessage(`{"work":{"sectionType":"work","entries":[{"id":"018f0000-0000-7000-8000-000000000001","degree":"not a work field"}]}}`),
			customization:   base.Customization,
		},
		{
			// content is a map, not a struct, so DisallowUnknownFields cannot
			// apply to it directly -- but trailing data after the single
			// JSON value must still be rejected, same as personalDetails and
			// customization (round-2 review finding: this asymmetry existed
			// only because the check was originally added to strictUnmarshal
			// alone, which content's map decode doesn't go through).
			name:            "trailing data after content's JSON value",
			personalDetails: base.PersonalDetails,
			content:         json.RawMessage(`{}garbage`),
			customization:   base.Customization,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resume.DecodeParts(tc.personalDetails, tc.content, tc.customization, base.SchemaVersion); err == nil {
				t.Fatal("expected a decode error, got nil")
			}
		})
	}
}

// TestCodec_AssembleCanonical_IncludesSchemaVersion proves AssembleCanonical
// marshals the whole document -- including SchemaVersion, which the three
// jsonb parts never carry themselves (D4) -- into one canonical JSON blob.
func TestCodec_AssembleCanonical_IncludesSchemaVersion(t *testing.T) {
	parts := splitFixture(t, "minimal.json")
	doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
	if err != nil {
		t.Fatalf("DecodeParts: %v", err)
	}

	canonical, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("AssembleCanonical: %v", err)
	}

	var generic map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &generic); err != nil {
		t.Fatalf("unmarshaling canonical output: %v", err)
	}
	sv, ok := generic["schemaVersion"]
	if !ok {
		t.Fatal("canonical document has no schemaVersion key")
	}
	if string(sv) != "1" {
		t.Errorf("schemaVersion = %s, want 1", sv)
	}
	for _, key := range []string{"personalDetails", "content", "customization"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("canonical document missing %q", key)
		}
	}
}

func TestCodec_EncodeParts_Minimal_MatchesSchemaType(t *testing.T) {
	// Sanity check that EncodeParts' three outputs decode back into the
	// generated schema types cleanly (a caller re-reading them, e.g. across
	// a store round trip through Postgres, gets back a well-typed value).
	doc := schema.Resume{
		SchemaVersion:   1,
		PersonalDetails: schema.PersonalDetails{},
		Content:         map[string]schema.Section{},
		Customization: schema.Customization{
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
		},
	}

	pd, content, cu, err := resume.EncodePartsForTest(doc)
	if err != nil {
		t.Fatalf("EncodeParts: %v", err)
	}

	roundTripped, err := resume.DecodeParts(pd, content, cu, int32(doc.SchemaVersion))
	if err != nil {
		t.Fatalf("DecodeParts: %v", err)
	}
	if !reflect.DeepEqual(doc, roundTripped) {
		t.Fatalf("round trip changed the document:\n want=%#v\n got=%#v", doc, roundTripped)
	}
}
