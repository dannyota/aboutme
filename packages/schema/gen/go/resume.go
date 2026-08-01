// Code generated from resume.schema.json. DO NOT EDIT.

package schema

// The resume document jsonb shape: personalDetails, content, customization plus
// schemaVersion. Single source of truth for generated Go/TS/Dart types and store-layer
// validation.
type Resume struct {
	Content         map[string]Section `json:"content"`
	Customization   Customization      `json:"customization"`
	PersonalDetails PersonalDetails    `json:"personalDetails"`
	SchemaVersion   int64              `json:"schemaVersion"`
}

// Mirrors design spec §3: font, colors, spacing, heading, layout (with sections order
// arrays), per-type display configs, pageFormat, date formats.
type Customization struct {
	Colors     Colors     `json:"colors"`
	DateFormat DateFormat `json:"dateFormat"`
	Font       Font       `json:"font"`
	Heading    Heading    `json:"heading"`
	Layout     Layout     `json:"layout"`
	PageFormat PageFormat `json:"pageFormat"`
	// Per-type display configs. Scoped to skill/language, the two builtin types where a
	// proficiency-display style is meaningful.
	SectionDisplay SectionDisplay `json:"sectionDisplay"`
	Spacing        Spacing        `json:"spacing"`
}

type Colors struct {
	Accent     *string `json:"accent,omitempty"`
	Background string  `json:"background"`
	Primary    string  `json:"primary"`
	Text       string  `json:"text"`
}

type Font struct {
	BaseSizePx int64  `json:"baseSizePx"`
	Family     Family `json:"family"`
}

type Heading struct {
	ShowRule bool         `json:"showRule"`
	Style    HeadingStyle `json:"style"`
}

type Layout struct {
	Columns int64 `json:"columns"`
	// Placement of content's section keys into the two layout columns. JSON Schema bounds each
	// array's size and forbids a repeat WITHIN one array (maxItems/uniqueItems below); it
	// cannot express the cross-field aggregate invariant (design spec §3): every content key
	// must appear exactly once across main+sidebar COMBINED — no duplicate across the two
	// arrays, no key absent from content, no content key placed nowhere. That combined rule is
	// store-layer aggregate validation (packages/schema/validation/store.ts
	// validateLayoutSections / gen/go/store_validate.go ValidateLayoutSections), run on every
	// write; see fixtures/store/invalid-layout-duplicate-across-arrays.json,
	// fixtures/store/invalid-layout-missing-content-key.json,
	// fixtures/store/invalid-layout-orphan-content-key.json.
	Sections Sections `json:"sections"`
}

// Placement of content's section keys into the two layout columns. JSON Schema bounds each
// array's size and forbids a repeat WITHIN one array (maxItems/uniqueItems below); it
// cannot express the cross-field aggregate invariant (design spec §3): every content key
// must appear exactly once across main+sidebar COMBINED — no duplicate across the two
// arrays, no key absent from content, no content key placed nowhere. That combined rule is
// store-layer aggregate validation (packages/schema/validation/store.ts
// validateLayoutSections / gen/go/store_validate.go ValidateLayoutSections), run on every
// write; see fixtures/store/invalid-layout-duplicate-across-arrays.json,
// fixtures/store/invalid-layout-missing-content-key.json,
// fixtures/store/invalid-layout-orphan-content-key.json.
type Sections struct {
	Main    []string `json:"main"`
	Sidebar []string `json:"sidebar"`
}

// Per-type display configs. Scoped to skill/language, the two builtin types where a
// proficiency-display style is meaningful.
type SectionDisplay struct {
	Language LanguageClass `json:"language"`
	Skill    SkillClass    `json:"skill"`
}

type LanguageClass struct {
	Style LanguageStyle `json:"style"`
}

type SkillClass struct {
	Style LanguageStyle `json:"style"`
}

type Spacing struct {
	EntryGap   float64 `json:"entryGap"`
	LineHeight float64 `json:"lineHeight"`
	SectionGap float64 `json:"sectionGap"`
}

// Draft-permissive (design spec §3, revised 2026-08-01): fullName and details are both
// optional and may be empty/absent while editing — a cleared name or a not-yet-added
// details array must never block autosave. See
// fixtures/draft-cleared-name-empty-section.json.
type PersonalDetails struct {
	Details  []PersonalDetail `json:"details,omitempty"`
	FullName *string          `json:"fullName,omitempty"`
	Headline *string          `json:"headline,omitempty"`
	Photo    *Photo           `json:"photo,omitempty"`
}

// One contact chip. Display order is the array order of personalDetails.details (no
// separate detailsOrder field — order lives where it's used, mirroring how
// customization.layout.sections orders content sections).
type PersonalDetail struct {
	ID       string  `json:"id"`
	IsHidden bool    `json:"isHidden"`
	Label    *string `json:"label,omitempty"`
	Type     Type    `json:"type"`
	// Draft-permissive (design spec §3, revised 2026-08-01): may be explicitly cleared ("")
	// while the user retypes it, same rule as every other free-text field. For type in
	// {website, linkedin, github, twitter} the allOf below additionally restricts this to an
	// https:// URL (or "") — see its description for why (phase-gate review finding C1).
	Value string `json:"value"`
}

// Resume photos live per-doc (design spec §3: avatar_key is account-only; distinct from
// this). key is an S3 object key, not a URL.
type Photo struct {
	Crop *PhotoCrop `json:"crop,omitempty"`
	// S3 object key (phase-gate review finding M6): restricted to AWS's documented 'safe'
	// key-character set (alnum, !-_.*'()) plus "/" for the pseudo-directory delimiter our own
	// upload path uses (see fixtures' "resumes/<user>/photo-original.jpg" keys). The FIRST
	// character must be alphanumeric — this excludes a leading "."/"_"/"!" etc, e.g.
	// ".hidden.jpg" or "_x.jpg" (phase-gate re-review finding NEW-M5, verified harmless against
	// both committed fixture keys). This is a storage-key SAFETY bound (blocks an absolute URL
	// like "https://evil.example.com/x.jpg", since ":" is not in the allowed set), not a
	// key-construction naming CONVENTION — the design spec does not define one, so none is
	// invented here (CLAUDE.md: 'do not invent a contract'). Path traversal (a ".." substring,
	// e.g. "../../other-user/secret.jpg") is deliberately NOT rejected by this pattern — see
	// phase-gate re-review finding NEW-M3: the natural regex form (a negative lookahead) is
	// outside JSON Schema's portable regex subset and does not compile under Go's RE2 engine,
	// which design spec §3 commits any future generated Go pattern-validator to using.
	// validation/store.ts's validatePhotoKeyTraversal / gen/go/store_validate.go's
	// ValidatePhotoKeyTraversal enforce the ".." rejection instead, as a plain substring check
	// neither language needs a regex for.
	Key string `json:"key"`
}

type PhotoCrop struct {
	Height float64 `json:"height"`
	Width  float64 `json:"width"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// design spec §3 entry-fields table: profile. Draft-permissive — see entryBase.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type ProfileEntry struct {
	ID       string  `json:"id"`
	IsHidden *bool   `json:"isHidden,omitempty"`
	Text     *string `json:"text,omitempty"`
}

// design spec §3 entry-fields table: work. Draft-permissive — see entryBase.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type WorkEntry struct {
	City         *string    `json:"city,omitempty"`
	Country      *string    `json:"country,omitempty"`
	Dates        *DateRange `json:"dates,omitempty"`
	Description  *string    `json:"description,omitempty"`
	Employer     *string    `json:"employer,omitempty"`
	EmployerLink *string    `json:"employerLink,omitempty"`
	ID           string     `json:"id"`
	IsHidden     *bool      `json:"isHidden,omitempty"`
	JobTitle     *string    `json:"jobTitle,omitempty"`
}

// present ⇒ end===null and ¬present ⇒ end≠null are both enforced here (design spec §3).
// start≤end is a cross-field numeric comparison JSON Schema cannot express cleanly and is
// left to the store layer (AC-DOC-003), matching how duplicate-entry-id (AC-DOC-002) is
// deferred to fixtures/store/.
type DateRange struct {
	End     *YearMonth `json:"end"`
	Present bool       `json:"present"`
	Start   YearMonth  `json:"start"`
}

type YearMonth struct {
	M *int64 `json:"m,omitempty"`
	Y int64  `json:"y"`
}

// design spec §3 entry-fields table: education. Draft-permissive — see entryBase.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type EducationEntry struct {
	City        *string    `json:"city,omitempty"`
	Country     *string    `json:"country,omitempty"`
	Dates       *DateRange `json:"dates,omitempty"`
	Degree      *string    `json:"degree,omitempty"`
	Description *string    `json:"description,omitempty"`
	ID          string     `json:"id"`
	IsHidden    *bool      `json:"isHidden,omitempty"`
	School      *string    `json:"school,omitempty"`
	SchoolLink  *string    `json:"schoolLink,omitempty"`
}

// design spec §3 entry-fields table: skill. Draft-permissive — see entryBase (level was
// already the one optional field even before that rule; now every field here is optional).
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type SkillEntry struct {
	ID       string  `json:"id"`
	InfoHTML *string `json:"infoHtml,omitempty"`
	IsHidden *bool   `json:"isHidden,omitempty"`
	Level    *int64  `json:"level,omitempty"`
	Name     *string `json:"name,omitempty"`
}

// design spec §3 entry-fields table: language. Draft-permissive — see entryBase. Note:
// languageEntry's field set ({name, level}) is a structural subset of skillEntry's ({name,
// level, infoHtml}) — see gen/go/section.go for why entries are not self-describing and the
// section's sectionType discriminator, not the entry's shape, is what defines its type.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type LanguageEntry struct {
	ID       string  `json:"id"`
	IsHidden *bool   `json:"isHidden,omitempty"`
	Level    *int64  `json:"level,omitempty"`
	Name     *string `json:"name,omitempty"`
}

// design spec §3 entry-fields table: certificate. date is a single {y,m?}, not a range.
// Draft-permissive — see entryBase.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type CertificateEntry struct {
	Date        *YearMonth `json:"date,omitempty"`
	Description *string    `json:"description,omitempty"`
	ID          string     `json:"id"`
	IsHidden    *bool      `json:"isHidden,omitempty"`
	Issuer      *string    `json:"issuer,omitempty"`
	Title       *string    `json:"title,omitempty"`
	TitleLink   *string    `json:"titleLink,omitempty"`
}

// design spec §3 entry-fields table: project. Draft-permissive — see entryBase.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type ProjectEntry struct {
	Dates       *DateRange `json:"dates,omitempty"`
	Description *string    `json:"description,omitempty"`
	ID          string     `json:"id"`
	IsHidden    *bool      `json:"isHidden,omitempty"`
	Link        *string    `json:"link,omitempty"`
	Title       *string    `json:"title,omitempty"`
}

// design spec §3 entry-fields table: custom. Draft-permissive — see entryBase.
//
// Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on
// every entry, so a half-typed entry from an autosaving editor persists and reloads exactly
// as typed. Never fabricate a sentinel value for an absent field — absence ("never
// entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged.
// Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later
// validation layer, not enforced here.
type CustomEntry struct {
	City        *string    `json:"city,omitempty"`
	Dates       *DateRange `json:"dates,omitempty"`
	Description *string    `json:"description,omitempty"`
	ID          string     `json:"id"`
	IsHidden    *bool      `json:"isHidden,omitempty"`
	Subtitle    *string    `json:"subtitle,omitempty"`
	Title       *string    `json:"title,omitempty"`
	TitleLink   *string    `json:"titleLink,omitempty"`
}

type DateFormat string

const (
	MmYyyy  DateFormat = "MM/YYYY"
	MonYYYY DateFormat = "Mon YYYY"
	Yyyy    DateFormat = "YYYY"
)

type Family string

const (
	Alegreya     Family = "Alegreya"
	BeVietnamPro Family = "Be Vietnam Pro"
	Inter        Family = "Inter"
	RobotoSerif  Family = "Roboto Serif"
	SourceSans3  Family = "Source Sans 3"
)

type HeadingStyle string

const (
	Normal    HeadingStyle = "normal"
	Titlecase HeadingStyle = "titlecase"
	Uppercase HeadingStyle = "uppercase"
)

type PageFormat string

const (
	A4     PageFormat = "a4"
	Letter PageFormat = "letter"
)

type LanguageStyle string

const (
	Bar  LanguageStyle = "bar"
	Dots LanguageStyle = "dots"
	Tag  LanguageStyle = "tag"
	Text LanguageStyle = "text"
)

type Type string

const (
	Email      Type = "email"
	Github     Type = "github"
	Linkedin   Type = "linkedin"
	Location   Type = "location"
	Phone      Type = "phone"
	Twitter    Type = "twitter"
	TypeCustom Type = "custom"
	Website    Type = "website"
)

type SectionType string

const (
	Certificate       SectionType = "certificate"
	Education         SectionType = "education"
	Language          SectionType = "language"
	Profile           SectionType = "profile"
	Project           SectionType = "project"
	SectionTypeCustom SectionType = "custom"
	Skill             SectionType = "skill"
	Work              SectionType = "work"
)
