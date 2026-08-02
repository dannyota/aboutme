// section_test.go is hand-written, alongside section.go (see that file's
// header) — not generated, not touched by generate.mjs.
package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSectionRoundTrip_EveryDiscriminator(t *testing.T) {
	cases := []struct {
		name    string
		section Section
	}{
		{
			"profile",
			NewProfileSection("Summary", "user", []ProfileEntry{
				{ID: "e1", IsHidden: ptr(false), Text: ptr("Backend engineer.")},
			}),
		},
		{
			"work",
			NewWorkSection("Experience", "briefcase", []WorkEntry{
				{
					ID: "e1", IsHidden: ptr(false), JobTitle: ptr("Engineer"), Employer: ptr("Acme"),
					EmployerLink: ptr(""), City: ptr("Hanoi"), Country: ptr("Vietnam"),
					Dates:       &DateRange{Start: YearMonth{Y: 2020}, End: nil, Present: true},
					Description: ptr(""),
				},
			}),
		},
		{
			"education",
			NewEducationSection("Education", "graduation-cap", []EducationEntry{
				{
					ID: "e1", IsHidden: ptr(false), Degree: ptr("BSc"), School: ptr("MIT"),
					SchoolLink: ptr(""), City: ptr("Cambridge"), Country: ptr("USA"),
					Dates: &DateRange{Start: YearMonth{Y: 2016},
						End: &YearMonth{Y: 2020}, Present: false},
					Description: ptr(""),
				},
			}),
		},
		{
			"skill",
			NewSkillSection("Skills", "star", []SkillEntry{
				{ID: "e1", IsHidden: ptr(false), Name: ptr("Go"), InfoHTML: ptr("")},
			}),
		},
		{
			"language",
			NewLanguageSection("Languages", "globe", []LanguageEntry{
				{ID: "e1", IsHidden: ptr(false), Name: ptr("English"), Level: ptr(int64(5))},
			}),
		},
		{
			"certificate",
			NewCertificateSection("Certificates", "award", []CertificateEntry{
				{
					ID: "e1", IsHidden: ptr(false), Title: ptr("AWS SA"), TitleLink: ptr(""),
					Issuer: ptr("AWS"), Date: &YearMonth{Y: 2023}, Description: ptr(""),
				},
			}),
		},
		{
			"project",
			NewProjectSection("Projects", "folder", []ProjectEntry{
				{
					ID: "e1", IsHidden: ptr(false), Title: ptr("aboutme"), Link: ptr(""),
					Dates:       &DateRange{Start: YearMonth{Y: 2026}, End: nil, Present: true},
					Description: ptr(""),
				},
			}),
		},
		{
			"custom",
			NewCustomSection("Awards", "trophy", []CustomEntry{
				{
					ID: "e1", IsHidden: ptr(false), Title: ptr("Hackathon"), TitleLink: ptr(""),
					Subtitle: ptr("1st place"), City: ptr("Hanoi"),
					Dates:       &DateRange{Start: YearMonth{Y: 2025}, End: nil, Present: true},
					Description: ptr(""),
				},
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.section.Validate(); err != nil {
				t.Fatalf("constructed Section failed Validate: %v", err)
			}

			data, err := json.Marshal(tc.section)
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}

			var decoded Section
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("UnmarshalJSON: %v", err)
			}
			if err := decoded.Validate(); err != nil {
				t.Fatalf("round-tripped Section failed Validate: %v", err)
			}
			if decoded.SectionType != tc.section.SectionType {
				t.Fatalf("SectionType: got %q, want %q", decoded.SectionType, tc.section.SectionType)
			}

			// Re-marshal and compare bytes: catches any field silently
			// dropped or defaulted somewhere in the round trip.
			redata, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("re-MarshalJSON: %v", err)
			}
			if string(redata) != string(data) {
				t.Fatalf("round trip not stable:\n  first:  %s\n  second: %s", data, redata)
			}
		})
	}
}

func TestSectionMarshalJSON_WireShape(t *testing.T) {
	section := NewWorkSection("Experience", "briefcase", []WorkEntry{
		{
			ID: "e1", IsHidden: ptr(false), JobTitle: ptr("Engineer"), Employer: ptr("Acme"),
			EmployerLink: ptr(""), City: ptr("Hanoi"), Country: ptr("Vietnam"),
			Dates:       &DateRange{Start: YearMonth{Y: 2020}, End: nil, Present: true},
			Description: ptr(""),
		},
	})

	data, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}

	for _, key := range []string{"sectionType", "displayName", "iconKey", "entries"} {
		if _, ok := wire[key]; !ok {
			t.Errorf("wire object missing key %q; got keys %v", key, wire)
		}
	}
	if len(wire) != 4 {
		t.Errorf("wire object has %d keys, want exactly 4 (sectionType, displayName, iconKey, entries); got %v", len(wire), wire)
	}

	var entries []WorkEntry
	if err := json.Unmarshal(wire["entries"], &entries); err != nil {
		t.Fatalf("entries did not decode as []WorkEntry: %v", err)
	}
	if len(entries) != 1 || entries[0].JobTitle == nil || *entries[0].JobTitle != "Engineer" {
		t.Errorf("entries round trip mismatch: %+v", entries)
	}
}

// TestSectionRoundTrip_AbsentDisplayNameAndIconKeyStayAbsent proves the fix
// for a round-trip fidelity gap: resume.schema.json's `section` $def only
// requires sectionType and entries -- a freshly created section may have
// neither displayName nor iconKey set yet (design spec §3, "a freshly
// created section persists before its title/icon are chosen"). Before
// DisplayName/IconKey became *string, decoding such a section and
// re-marshaling it always produced `"displayName":""` and `"iconKey":""` --
// and an empty iconKey fails the schema's own iconKey pattern
// (`^[a-z0-9]+(-[a-z0-9]+)*$`, which requires at least one character), so a
// legitimately schema-valid draft section became schema-INVALID the moment
// it round-tripped through this package. See
// fixtures/draft-cleared-name-empty-section.json.
func TestSectionRoundTrip_AbsentDisplayNameAndIconKeyStayAbsent(t *testing.T) {
	const payload = `{"sectionType":"work","entries":[]}`
	var section Section
	if err := json.Unmarshal([]byte(payload), &section); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if section.DisplayName != nil {
		t.Errorf("DisplayName: got %q, want nil (absent from the payload)", *section.DisplayName)
	}
	if section.IconKey != nil {
		t.Errorf("IconKey: got %q, want nil (absent from the payload)", *section.IconKey)
	}

	data, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	if _, ok := wire["displayName"]; ok {
		t.Errorf("re-marshaled wire has a displayName key, want it absent: %s", data)
	}
	if _, ok := wire["iconKey"]; ok {
		t.Errorf("re-marshaled wire has an iconKey key, want it absent: %s", data)
	}
}

// TestSectionRoundTrip_ExplicitlyClearedDisplayNameStaysPresentAndEmpty is
// the contrast case: displayName (unlike iconKey) may be explicitly ""
// (cleared while retyping a section title, per the schema's own
// $defs.section description) -- that state must stay distinguishable from
// "absent" and survive a round trip.
func TestSectionRoundTrip_ExplicitlyClearedDisplayNameStaysPresentAndEmpty(t *testing.T) {
	const payload = `{"sectionType":"work","displayName":"","entries":[]}`
	var section Section
	if err := json.Unmarshal([]byte(payload), &section); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if section.DisplayName == nil || *section.DisplayName != "" {
		t.Fatalf("DisplayName: got %v, want a present pointer to \"\"", section.DisplayName)
	}

	data, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal into map: %v", err)
	}
	raw, ok := wire["displayName"]
	if !ok {
		t.Fatalf("re-marshaled wire missing displayName key (explicitly-cleared value was dropped): %s", data)
	}
	if string(raw) != `""` {
		t.Errorf("displayName = %s, want \"\"", raw)
	}
}

func TestSectionUnmarshalJSON_RejectsCrossTypeEntry(t *testing.T) {
	// An education-shaped entry carrying degree/school/schoolLink — none of
	// which are WorkEntry fields — inside a sectionType: "work" section.
	// This is genuinely detectable and must keep failing: unlike the
	// skill/language case (see
	// TestSectionUnmarshalJSON_AcceptsStructurallyIndistinguishableSubset
	// below), degree/school/schoolLink are foreign to WorkEntry, not a
	// subset of it — city/country/dates/description are shared with
	// education, but that's not what's under test here.
	const payload = `{
		"sectionType": "work",
		"displayName": "Experience",
		"iconKey": "briefcase",
		"entries": [
			{
				"id": "e1",
				"degree": "BSc",
				"school": "MIT",
				"schoolLink": ""
			}
		]
	}`

	var section Section
	err := json.Unmarshal([]byte(payload), &section)
	if err == nil {
		t.Fatalf("expected an error decoding an education-shaped entry into a work section, got none")
	}
	if !strings.Contains(err.Error(), "degree") {
		t.Errorf("expected the error to name the offending field \"degree\", got: %v", err)
	}
}

// TestSectionUnmarshalJSON_AcceptsStructurallyIndistinguishableSubset
// documents a known, expected non-bug (see section.go's package doc and
// design spec §3): languageEntry's fields ({name, level}) are an exact
// subset of skillEntry's ({name, level, infoHtml}), and with every domain
// field now optional (draft-permissive), a language-shaped payload is also
// a perfectly valid partial SkillEntry. There is no field-based check —
// required, foreign, or otherwise — that can reject this, because nothing
// about the entry itself is invalid; only its placement under the wrong
// sectionType is "wrong", and Go entries are not self-describing about
// that. The section's sectionType discriminator is the only source of
// truth for an entry's type. This test asserts the (correct) acceptance,
// specifically so nobody "fixes" decodeEntries into trying to reject it.
func TestSectionUnmarshalJSON_AcceptsStructurallyIndistinguishableSubset(t *testing.T) {
	const payload = `{
		"sectionType": "skill",
		"displayName": "Skills",
		"iconKey": "code",
		"entries": [
			{"id": "e1", "name": "English", "level": 5}
		]
	}`

	var section Section
	if err := json.Unmarshal([]byte(payload), &section); err != nil {
		t.Fatalf("expected a language-shaped {name, level} entry to decode as a valid partial SkillEntry, got error: %v", err)
	}
	if len(section.SkillEntries) != 1 {
		t.Fatalf("expected exactly one decoded SkillEntry, got %d", len(section.SkillEntries))
	}
	entry := section.SkillEntries[0]
	if entry.Name == nil || *entry.Name != "English" {
		t.Errorf("Name: got %v, want a pointer to \"English\"", entry.Name)
	}
	if entry.Level == nil || *entry.Level != 5 {
		t.Errorf("Level: got %v, want a pointer to 5", entry.Level)
	}
	if entry.InfoHTML != nil {
		t.Errorf("InfoHTML: got %v, want nil (absent in the payload — infoHtml is optional, so this is not an error)", entry.InfoHTML)
	}
}

// TestSectionUnmarshalJSON_RejectsForeignFieldUnderWrongSectionType is the
// contrast case for the test above: what DisallowUnknownFields actually
// detects is a field foreign to the target entry type, regardless of
// whether other fields in the payload happen to be required or optional.
// "employer" is a WorkEntry field, not a SkillEntry field, so this payload
// — unlike the skill/language subset case — is genuinely invalid.
func TestSectionUnmarshalJSON_RejectsForeignFieldUnderWrongSectionType(t *testing.T) {
	const payload = `{
		"sectionType": "skill",
		"displayName": "Skills",
		"iconKey": "code",
		"entries": [
			{"id": "e1", "name": "Go", "employer": "Acme"}
		]
	}`

	var section Section
	err := json.Unmarshal([]byte(payload), &section)
	if err == nil {
		t.Fatalf("expected an error decoding a work-shaped \"employer\" field into a skill section, got none")
	}
	if !strings.Contains(err.Error(), "employer") {
		t.Errorf("expected the error to name the offending field \"employer\", got: %v", err)
	}
}

func TestSectionUnmarshalJSON_RejectsUnknownSectionType(t *testing.T) {
	const payload = `{
		"sectionType": "hobby",
		"displayName": "Hobbies",
		"iconKey": "heart",
		"entries": []
	}`

	var section Section
	if err := json.Unmarshal([]byte(payload), &section); err == nil {
		t.Fatalf("expected an error decoding an unknown sectionType, got none")
	}
}

func TestSectionValidate_RejectsDiscriminatorEntryMismatch(t *testing.T) {
	// Built by hand (not via a constructor or UnmarshalJSON): SectionType
	// says "work" but the populated slice is EducationEntries.
	mismatched := Section{
		SectionType:      Work,
		DisplayName:      ptr("Experience"),
		IconKey:          ptr("briefcase"),
		EducationEntries: []EducationEntry{},
	}

	if err := mismatched.Validate(); err == nil {
		t.Fatalf("expected Validate to reject a SectionType/entries-slice mismatch, got nil")
	}
	if _, err := json.Marshal(mismatched); err == nil {
		t.Fatalf("expected MarshalJSON to reject a SectionType/entries-slice mismatch, got nil error")
	}
}

func TestSectionValidate_RejectsNoEntriesPopulated(t *testing.T) {
	empty := Section{SectionType: Work, DisplayName: ptr("Experience"), IconKey: ptr("briefcase")}
	if err := empty.Validate(); err == nil {
		t.Fatalf("expected Validate to reject a Section with no entries slice populated, got nil")
	}
}

func TestSectionValidate_RejectsMultipleEntriesPopulated(t *testing.T) {
	both := Section{
		SectionType:      Work,
		DisplayName:      ptr("Experience"),
		IconKey:          ptr("briefcase"),
		WorkEntries:      []WorkEntry{},
		EducationEntries: []EducationEntry{},
	}
	if err := both.Validate(); err == nil {
		t.Fatalf("expected Validate to reject a Section with two entries slices populated, got nil")
	}
}

func TestResumeContent_RoundTripsThroughSection(t *testing.T) {
	resume := Resume{
		SchemaVersion: 1,
		PersonalDetails: PersonalDetails{
			FullName: ptr("Jane Doe"),
			Details:  []PersonalDetail{},
		},
		Content: map[string]Section{
			"work": NewWorkSection("Experience", "briefcase", []WorkEntry{
				{
					ID: "e1", IsHidden: ptr(false), JobTitle: ptr("Engineer"), Employer: ptr("Acme"),
					EmployerLink: ptr(""), City: ptr("Hanoi"), Country: ptr("Vietnam"),
					Dates:       &DateRange{Start: YearMonth{Y: 2020}, End: nil, Present: true},
					Description: ptr(""),
				},
			}),
		},
		Customization: Customization{},
	}

	data, err := json.Marshal(resume)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	var decoded Resume
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	work, ok := decoded.Content["work"]
	if !ok {
		t.Fatalf("decoded Resume.Content missing \"work\" key; got %v", decoded.Content)
	}
	if work.SectionType != Work || len(work.WorkEntries) != 1 ||
		work.WorkEntries[0].JobTitle == nil || *work.WorkEntries[0].JobTitle != "Engineer" {
		t.Fatalf("decoded work section mismatch: %+v", work)
	}
}
