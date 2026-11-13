package resume_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

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

var roundTripFixtures = []string{
	"minimal.json",
	"full.json",
	"draft-partial.json",
	"draft-cleared-name-empty-section.json",
	"draft-cleared-contact-value.json",
}

func TestCodec_RoundTrip_ByteStable(t *testing.T) {
	for _, name := range roundTripFixtures {
		t.Run(name, func(t *testing.T) {
			parts := splitFixture(t, name)

			doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
			if err != nil {
				t.Fatalf("DecodeParts: %v", err)
			}

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

			if !bytes.Equal(pd1, pd2) {
				t.Errorf("personalDetails not byte-stable:\n pass1=%s\n pass2=%s", pd1, pd2)
			}
			if !bytes.Equal(c1, c2) {
				t.Errorf("content not byte-stable:\n pass1=%s\n pass2=%s", c1, c2)
			}
			if !bytes.Equal(cu1, cu2) {
				t.Errorf("customization not byte-stable:\n pass1=%s\n pass2=%s", cu1, cu2)
			}

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
			t.Errorf("%s contains a schemaVersion key, want it absent", label)
		}
	}
}

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
		entry := custom.CustomEntries[1]
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

func TestCodec_ClearedContactValue_Preserved(t *testing.T) {
	parts := splitFixture(t, "draft-cleared-contact-value.json")
	doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
	if err != nil {
		t.Fatalf("DecodeParts: %v", err)
	}

	if got := len(doc.PersonalDetails.Details); got != 1 {
		t.Fatalf("len(PersonalDetails.Details) = %d, want 1 (the entry must not be dropped)", got)
	}
	entry := doc.PersonalDetails.Details[0]
	if entry.Value != "" {
		t.Errorf("Details[0].Value = %q, want \"\" (explicitly cleared, not fabricated)", entry.Value)
	}
	if entry.Type != schema.Linkedin {
		t.Errorf("Details[0].Type = %q, want %q", entry.Type, schema.Linkedin)
	}

	pd, content, cu, err := resume.EncodePartsForTest(doc)
	if err != nil {
		t.Fatalf("EncodeParts: %v", err)
	}
	roundTripped, err := resume.DecodeParts(pd, content, cu, parts.SchemaVersion)
	if err != nil {
		t.Fatalf("DecodeParts (round trip): %v", err)
	}
	if got := len(roundTripped.PersonalDetails.Details); got != 1 {
		t.Fatalf("after round trip: len(PersonalDetails.Details) = %d, want 1", got)
	}
	if roundTripped.PersonalDetails.Details[0].Value != "" {
		t.Errorf("after round trip: Details[0].Value = %q, want \"\"", roundTripped.PersonalDetails.Details[0].Value)
	}
	if !reflect.DeepEqual(doc.PersonalDetails, roundTripped.PersonalDetails) {
		t.Errorf("PersonalDetails changed across a round trip:\n before=%#v\n after=%#v", doc.PersonalDetails, roundTripped.PersonalDetails)
	}

	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(pd, &encoded); err != nil {
		t.Fatalf("unmarshaling encoded personalDetails: %v", err)
	}
	rawDetails, ok := encoded["details"]
	if !ok {
		t.Fatalf("encoded personalDetails has no \"details\" key: %s", pd)
	}
	var detailEntries []map[string]json.RawMessage
	if err := json.Unmarshal(rawDetails, &detailEntries); err != nil {
		t.Fatalf("unmarshaling encoded personalDetails.details: %v", err)
	}
	if len(detailEntries) != 1 {
		t.Fatalf("encoded personalDetails.details has %d entries, want 1", len(detailEntries))
	}
	rawValue, ok := detailEntries[0]["value"]
	if !ok {
		t.Fatalf("encoded personalDetails.details[0] has no \"value\" key; the cleared value was omitted, not preserved: %s", rawDetails)
	}
	if got := string(rawValue); got != `""` {
		t.Errorf("encoded personalDetails.details[0].value = %s, want %s", got, `""`)
	}
}

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
			customization:   json.RawMessage(`{"font":{"family":"inter","baseSizePx":14},"colors":{"primary":"#1a1a1a","text":"#1a1a1a","background":"#ffffff"},"spacing":{"sectionGap":16,"entryGap":8,"lineHeight":1.4},"heading":{"style":"normal","showRule":false},"layout":{"columns":1,"sections":{"main":[],"sidebar":[]}},"sectionDisplay":{"skill":{"style":"text"},"language":{"style":"text"}},"pageFormat":"a4","dateFormat":"MM/YYYY","bogus":true}`),
		},
		{
			name:            "a field foreign to workEntry inside content",
			personalDetails: base.PersonalDetails,
			content:         json.RawMessage(`{"work":{"sectionType":"work","entries":[{"id":"018f0000-0000-7000-8000-000000000001","degree":"not a work field"}]}}`),
			customization:   base.Customization,
		},
		{
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
	if string(sv) != "6" {
		t.Errorf("schemaVersion = %s, want 6", sv)
	}
	for _, key := range []string{"personalDetails", "content", "customization"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("canonical document missing %q", key)
		}
	}
}

func TestCodec_EncodeParts_Minimal_MatchesSchemaType(t *testing.T) {
	doc := schema.Resume{
		SchemaVersion:   int64(schema.CurrentVersion),
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
