// Package schema: section.go is hand-written, NOT generated — do not add
// the "Code generated ... DO NOT EDIT" header here, and generate.mjs never
// touches this file.
//
// resume.schema.json's `content[key]` is `section`: an eight-way oneOf on
// sectionType, where each variant's `entries` array holds a different entry
// $def (workEntry, educationEntry, ...). Go has no sum-type/discriminated-
// union output target for that (see scripts/generate.mjs's header), so
// gen/go/resume.go generates the eight entry structs (ProfileEntry,
// WorkEntry, ...) and SectionType, but not Section itself. Section lives
// here instead: one typed slice field per sectionType, of which exactly one
// is populated, plus JSON (un)marshaling that translates to/from the wire
// format's single "entries" array (design spec §3/§5).
//
// Entries are not self-describing; the section's sectionType discriminator
// is what defines an entry's type, not the entry's own shape. Since
// resume.schema.json went draft-permissive (design spec §3, revised
// 2026-08-01 — every domain field optional, only `id` required), this is no
// longer just true in principle, it's structurally unavoidable: e.g.
// languageEntry's fields ({name, level}) are an exact subset of
// skillEntry's ({name, level, infoHtml}). A JSON object {"id":"e1",
// "name":"English","level":5} is simultaneously a valid (partial) SkillEntry
// AND a valid (complete) LanguageEntry — there is no field, required or
// otherwise, that distinguishes them. decodeEntries's
// json.Decoder.DisallowUnknownFields() cannot and does not try to catch
// this: it rejects fields *foreign* to the target type (e.g. an
// education-shaped entry's "degree"/"school"/"schoolLink" inside a
// sectionType: "work" section — none of those are WorkEntry fields), but a
// same-or-subset-shaped entry decoded under the wrong sectionType decodes
// without error, because nothing about the entry itself is wrong — only its
// placement is. Do not attempt to "fix" this with a stricter decoder; ajv
// (packages/schema/test/schema.test.ts) enforces the same rule at the
// schema level for the same reason (see the `section` $def's oneOf) and it
// can't distinguish them either, because there is nothing to distinguish.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Section is content[key]. Build one with the NewXSection constructor
// matching its sectionType, or decode one from JSON via encoding/json
// (UnmarshalJSON below) — both guarantee SectionType matches the populated
// entries slice. A Section struct literal built by hand has no such
// guarantee (nothing stops you setting SectionType to Work while populating
// EducationEntries); Validate and MarshalJSON reject that at the JSON
// boundary, since Go's type system can't reject it at compile time.
type Section struct {
	SectionType SectionType
	// DisplayName and IconKey are *string, not string: resume.schema.json's
	// `section` $def makes both genuinely optional (only sectionType and
	// entries are required — see this file's header and the schema's own
	// $defs.section description), and displayName may ALSO be explicitly ""
	// (cleared while retyping a section title) — a state distinct from
	// "never set". A plain string could not tell "absent" apart from "set to
	// the empty string" (the same "never fabricate a sentinel" rule that
	// governs every other draft-permissive field in this schema), and iconKey
	// specifically has no explicit-empty-string allowance in the schema at
	// all (its pattern requires at least one character when present) — so an
	// absent iconKey re-marshaled as "" would fail the schema's own iconKey
	// pattern on the very next validation pass. nil means the key is absent
	// from the wire form entirely (MarshalJSON below uses `omitempty`);
	// non-nil (including a pointer to "") means the key is present with that
	// value.
	DisplayName *string
	IconKey     *string

	ProfileEntries     []ProfileEntry
	WorkEntries        []WorkEntry
	EducationEntries   []EducationEntry
	SkillEntries       []SkillEntry
	LanguageEntries    []LanguageEntry
	CertificateEntries []CertificateEntry
	ProjectEntries     []ProjectEntry
	CustomEntries      []CustomEntry
}

func NewProfileSection(displayName, iconKey string, entries []ProfileEntry) Section {
	if entries == nil {
		entries = []ProfileEntry{}
	}
	return Section{SectionType: Profile, DisplayName: &displayName, IconKey: &iconKey, ProfileEntries: entries}
}

func NewWorkSection(displayName, iconKey string, entries []WorkEntry) Section {
	if entries == nil {
		entries = []WorkEntry{}
	}
	return Section{SectionType: Work, DisplayName: &displayName, IconKey: &iconKey, WorkEntries: entries}
}

func NewEducationSection(displayName, iconKey string, entries []EducationEntry) Section {
	if entries == nil {
		entries = []EducationEntry{}
	}
	return Section{SectionType: Education, DisplayName: &displayName, IconKey: &iconKey, EducationEntries: entries}
}

func NewSkillSection(displayName, iconKey string, entries []SkillEntry) Section {
	if entries == nil {
		entries = []SkillEntry{}
	}
	return Section{SectionType: Skill, DisplayName: &displayName, IconKey: &iconKey, SkillEntries: entries}
}

func NewLanguageSection(displayName, iconKey string, entries []LanguageEntry) Section {
	if entries == nil {
		entries = []LanguageEntry{}
	}
	return Section{SectionType: Language, DisplayName: &displayName, IconKey: &iconKey, LanguageEntries: entries}
}

func NewCertificateSection(displayName, iconKey string, entries []CertificateEntry) Section {
	if entries == nil {
		entries = []CertificateEntry{}
	}
	return Section{SectionType: Certificate, DisplayName: &displayName, IconKey: &iconKey, CertificateEntries: entries}
}

func NewProjectSection(displayName, iconKey string, entries []ProjectEntry) Section {
	if entries == nil {
		entries = []ProjectEntry{}
	}
	return Section{SectionType: Project, DisplayName: &displayName, IconKey: &iconKey, ProjectEntries: entries}
}

func NewCustomSection(displayName, iconKey string, entries []CustomEntry) Section {
	if entries == nil {
		entries = []CustomEntry{}
	}
	return Section{SectionType: SectionTypeCustom, DisplayName: &displayName, IconKey: &iconKey, CustomEntries: entries}
}

// Validate reports whether exactly one of Section's entries slices is
// non-nil and it's the one SectionType selects. The NewXSection
// constructors and UnmarshalJSON both always produce a Section that passes;
// this exists to catch hand-built struct literals (and is called by
// MarshalJSON, so json.Marshal itself rejects a mismatched Section).
func (s Section) Validate() error {
	populated := map[SectionType]bool{
		Profile:           s.ProfileEntries != nil,
		Work:              s.WorkEntries != nil,
		Education:         s.EducationEntries != nil,
		Skill:             s.SkillEntries != nil,
		Language:          s.LanguageEntries != nil,
		Certificate:       s.CertificateEntries != nil,
		Project:           s.ProjectEntries != nil,
		SectionTypeCustom: s.CustomEntries != nil,
	}

	count := 0
	for _, isSet := range populated {
		if isSet {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("schema: Section must populate exactly one entries slice, found %d", count)
	}
	if !populated[s.SectionType] {
		return fmt.Errorf("schema: Section.SectionType %q does not match its populated entries slice", s.SectionType)
	}
	return nil
}

// sectionWire is content[key]'s actual JSON shape: sectionType plus one
// shared "entries" array (resume.schema.json's `section` $def). Section
// splits that array into a typed slice per sectionType internally;
// sectionWire is only the wire format at the Marshal/Unmarshal boundary.
type sectionWire struct {
	SectionType SectionType `json:"sectionType"`
	// omitempty on a *string omits the key only when the pointer itself is
	// nil -- a non-nil pointer to "" still gets written out. That is exactly
	// "absent stays absent, explicitly-cleared stays present-and-empty" (see
	// Section.DisplayName/IconKey's comment above).
	DisplayName *string `json:"displayName,omitempty"`
	IconKey     *string `json:"iconKey,omitempty"`
	Entries     any     `json:"entries"`
}

func (s Section) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}

	var entries any
	switch s.SectionType {
	case Profile:
		entries = s.ProfileEntries
	case Work:
		entries = s.WorkEntries
	case Education:
		entries = s.EducationEntries
	case Skill:
		entries = s.SkillEntries
	case Language:
		entries = s.LanguageEntries
	case Certificate:
		entries = s.CertificateEntries
	case Project:
		entries = s.ProjectEntries
	case SectionTypeCustom:
		entries = s.CustomEntries
	default:
		// Unreachable once Validate has passed: populated's keys above are
		// exactly the SectionType values Validate checks against.
		return nil, fmt.Errorf("schema: unknown SectionType %q", s.SectionType)
	}

	return json.Marshal(sectionWire{
		SectionType: s.SectionType,
		DisplayName: s.DisplayName,
		IconKey:     s.IconKey,
		Entries:     entries,
	})
}

func (s *Section) UnmarshalJSON(data []byte) error {
	var wire struct {
		SectionType SectionType     `json:"sectionType"`
		DisplayName *string         `json:"displayName,omitempty"`
		IconKey     *string         `json:"iconKey,omitempty"`
		Entries     json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("schema: decoding Section: %w", err)
	}

	decoded := Section{SectionType: wire.SectionType, DisplayName: wire.DisplayName, IconKey: wire.IconKey}

	switch wire.SectionType {
	case Profile:
		entries, err := decodeEntries[ProfileEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding profile entries: %w", err)
		}
		decoded.ProfileEntries = entries
	case Work:
		entries, err := decodeEntries[WorkEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding work entries: %w", err)
		}
		decoded.WorkEntries = entries
	case Education:
		entries, err := decodeEntries[EducationEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding education entries: %w", err)
		}
		decoded.EducationEntries = entries
	case Skill:
		entries, err := decodeEntries[SkillEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding skill entries: %w", err)
		}
		decoded.SkillEntries = entries
	case Language:
		entries, err := decodeEntries[LanguageEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding language entries: %w", err)
		}
		decoded.LanguageEntries = entries
	case Certificate:
		entries, err := decodeEntries[CertificateEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding certificate entries: %w", err)
		}
		decoded.CertificateEntries = entries
	case Project:
		entries, err := decodeEntries[ProjectEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding project entries: %w", err)
		}
		decoded.ProjectEntries = entries
	case SectionTypeCustom:
		entries, err := decodeEntries[CustomEntry](wire.Entries)
		if err != nil {
			return fmt.Errorf("schema: decoding custom entries: %w", err)
		}
		decoded.CustomEntries = entries
	default:
		return fmt.Errorf("schema: unknown sectionType %q", wire.SectionType)
	}

	*s = decoded
	return nil
}

// decodeEntries decodes a JSON array into []T, rejecting any element
// carrying a field T doesn't declare. This is what makes an education-
// shaped entry (degree/school/schoolLink) inside a "work" section fail
// instead of silently decoding into a WorkEntry with those fields dropped
// and jobTitle/employer left as zero values.
func decodeEntries[T any](raw json.RawMessage) ([]T, error) {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(raw, &rawItems); err != nil {
		return nil, err
	}

	entries := make([]T, len(rawItems))
	for i, item := range rawItems {
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&entries[i]); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
	}
	return entries, nil
}
