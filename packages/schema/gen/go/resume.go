// Code generated from resume.schema.json. DO NOT EDIT.

package schema

import "encoding/json"

// Current resume document shape. See docs/design/data.md for the aggregate and versioning
// contract.
type Resume struct {
	Content         map[string]Section `json:"content"`
	Customization   Customization      `json:"customization"`
	PersonalDetails PersonalDetails    `json:"personalDetails"`
	SchemaVersion   int64              `json:"schemaVersion"`
}

// Resume presentation settings. See docs/design/templates/README.md.
type Customization struct {
	Colors     Colors     `json:"colors"`
	DateFormat DateFormat `json:"dateFormat"`
	Font       Font       `json:"font"`
	// Optional presentation of the top resume header containing the photo, fullName, headline,
	// and contacts. It is distinct from customization.heading, which styles section headings.
	// See docs/design/templates/contract.md.
	Header *HeaderClass `json:"header,omitempty"`
	// Styles section headings. This is distinct from customization.header, which contains the
	// resume top block and fullName. See docs/design/templates/contract.md.
	Heading    Heading    `json:"heading"`
	Layout     Layout     `json:"layout"`
	PageFormat PageFormat `json:"pageFormat"`
	// Display style for skill and language proficiency. See docs/design/templates/contract.md.
	SectionDisplay SectionDisplay `json:"sectionDisplay"`
	Spacing        Spacing        `json:"spacing"`
}

type Colors struct {
	Accent     *string `json:"accent,omitempty"`
	Background string  `json:"background"`
	Primary    string  `json:"primary"`
	// Optional fill color for the region selected by layout.surfaceTarget; absence falls back
	// to colors.background. See docs/design/templates/colors.md.
	Surface *string `json:"surface,omitempty"`
	Text    string  `json:"text"`
}

type Font struct {
	BaseSizePx int64 `json:"baseSizePx"`
	// Stable version-2 font catalog ID in manifest rank order. See
	// docs/design/fonts.md#version-2-catalog.
	Family Family `json:"family"`
}

// Optional presentation of the top resume header containing the photo, fullName, headline,
// and contacts. It is distinct from customization.heading, which styles section headings.
// See docs/design/templates/contract.md.
type HeaderClass struct {
	// Horizontal alignment for the complete top block. See docs/design/templates/contract.md.
	Align Align `json:"align"`
	// Displays contact details inline or stacked while preserving array order. See
	// docs/design/templates/contract.md.
	DetailsLayout DetailsLayout `json:"detailsLayout"`
	// Contact-detail icon style for the top header: none or the Lucide outline glyph. See
	// docs/design/templates/tokens.md.
	IconStyle IconStyle `json:"iconStyle"`
}

// Styles section headings. This is distinct from customization.header, which contains the
// resume top block and fullName. See docs/design/templates/contract.md.
type Heading struct {
	ShowRule bool         `json:"showRule"`
	Style    HeadingStyle `json:"style"`
}

type Layout struct {
	Columns int64 `json:"columns"`
	// Orders section keys in main and sidebar columns. The store requires every content key
	// exactly once across both arrays. See docs/design/data.md#resume-aggregate.
	Sections Sections `json:"sections"`
	// Region filled by colors.surface. With layout.columns set to 1, sidebar renders as none;
	// that degradation is not an error and does not rewrite the draft. See
	// docs/design/templates/contract.md.
	SurfaceTarget *SurfaceTarget `json:"surfaceTarget,omitempty"`
}

// Orders section keys in main and sidebar columns. The store requires every content key
// exactly once across both arrays. See docs/design/data.md#resume-aggregate.
type Sections struct {
	Main    []string `json:"main"`
	Sidebar []string `json:"sidebar"`
}

// Display style for skill and language proficiency. See docs/design/templates/contract.md.
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
	// Optional horizontal and vertical page margins in millimetres, each from 0 to 40. Absence
	// renders as 15 mm. See docs/design/templates/print.md.
	PageMargin *PageMargin `json:"pageMargin,omitempty"`
	SectionGap float64     `json:"sectionGap"`
}

// Optional horizontal and vertical page margins in millimetres, each from 0 to 40. Absence
// renders as 15 mm. See docs/design/templates/print.md.
type PageMargin struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Draft personal details; fullName and details may be absent or empty. See
// docs/design/data.md#draft-and-publish-validation.
type PersonalDetails struct {
	Details  []PersonalDetail `json:"details,omitempty"`
	FullName *string          `json:"fullName,omitempty"`
	Headline *string          `json:"headline,omitempty"`
	Photo    *Photo           `json:"photo,omitempty"`
}

// One contact item; array order is display order. See docs/design/data.md#resume-aggregate.
type PersonalDetail struct {
	ID       string  `json:"id"`
	IsHidden bool    `json:"isHidden"`
	Label    *string `json:"label,omitempty"`
	Type     Type    `json:"type"`
	// Draft contact value. URL contact types accept only an exact lowercase https URI or an
	// empty string. See docs/adr/0013-contact-detail-rendering.md.
	Value string `json:"value"`
}

// Resume-specific private photo object key and optional crop; the key is not a URL. See
// docs/design/security.md#untrusted-media.
type Photo struct {
	Crop *PhotoCrop `json:"crop,omitempty"`
	// Server-owned private-media key with a bounded safe character set. Aggregate validation
	// rejects traversal; see docs/design/security.md#untrusted-media.
	Key string `json:"key"`
}

type PhotoCrop struct {
	Height float64 `json:"height"`
	Width  float64 `json:"width"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// Draft-permissive profile entry. See docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
type ProfileEntry struct {
	ID       string  `json:"id"`
	IsHidden *bool   `json:"isHidden,omitempty"`
	Text     *string `json:"text,omitempty"`
}

// Draft-permissive work entry. See docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
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

// Date range with a required start. Present ranges have a null end; completed ranges have
// an end. The store checks ordering. See docs/design/data.md#bounds-and-invariants.
type DateRange struct {
	End     *YearMonth `json:"end"`
	Present bool       `json:"present"`
	Start   YearMonth  `json:"start"`
}

type YearMonth struct {
	M *int64 `json:"m,omitempty"`
	Y int64  `json:"y"`
}

// Draft-permissive education entry. See docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
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

// Draft-permissive skill entry with an optional proficiency level from 0 to 5. See
// docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
type SkillEntry struct {
	ID       string  `json:"id"`
	InfoHTML *string `json:"infoHtml,omitempty"`
	IsHidden *bool   `json:"isHidden,omitempty"`
	Level    *int64  `json:"level,omitempty"`
	Name     *string `json:"name,omitempty"`
}

// Draft-permissive language entry selected by its parent sectionType. See
// docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
type LanguageEntry struct {
	ID       string  `json:"id"`
	IsHidden *bool   `json:"isHidden,omitempty"`
	Level    *int64  `json:"level,omitempty"`
	Name     *string `json:"name,omitempty"`
}

// Draft-permissive certificate entry with an optional single year-month date. See
// docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
type CertificateEntry struct {
	Date        *YearMonth `json:"date,omitempty"`
	Description *string    `json:"description,omitempty"`
	ID          string     `json:"id"`
	IsHidden    *bool      `json:"isHidden,omitempty"`
	Issuer      *string    `json:"issuer,omitempty"`
	Title       *string    `json:"title,omitempty"`
	TitleLink   *string    `json:"titleLink,omitempty"`
}

// Draft-permissive project entry. See docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
type ProjectEntry struct {
	Dates       *DateRange `json:"dates,omitempty"`
	Description *string    `json:"description,omitempty"`
	ID          string     `json:"id"`
	IsHidden    *bool      `json:"isHidden,omitempty"`
	Link        *string    `json:"link,omitempty"`
	Title       *string    `json:"title,omitempty"`
}

// Draft-permissive custom entry. See docs/design/data.md#resume-aggregate.
//
// Draft entry base. Only id is required; absent and explicitly cleared fields remain
// distinct. See docs/design/data.md#draft-and-publish-validation.
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

// Stable version-2 font catalog ID in manifest rank order. See
// docs/design/fonts.md#version-2-catalog.
type Family string

const (
	Alegreya                 Family = "alegreya"
	Aleo                     Family = "aleo"
	AtkinsonHyperlegibleNext Family = "atkinson-hyperlegible-next"
	Barlow                   Family = "barlow"
	BeVietnamPro             Family = "be-vietnam-pro"
	CormorantGaramond        Family = "cormorant-garamond"
	CrimsonPro               Family = "crimson-pro"
	DmSans                   Family = "dm-sans"
	EbGaramond               Family = "eb-garamond"
	FiraSans                 Family = "fira-sans"
	Inter                    Family = "inter"
	Literata                 Family = "literata"
	Montserrat               Family = "montserrat"
	Newsreader               Family = "newsreader"
	NotoSans                 Family = "noto-sans"
	NotoSerif                Family = "noto-serif"
	NunitoSans               Family = "nunito-sans"
	OpenSans                 Family = "open-sans"
	PlusJakartaSans          Family = "plus-jakarta-sans"
	Roboto                   Family = "roboto"
	RobotoMono               Family = "roboto-mono"
	RobotoSerif              Family = "roboto-serif"
	SourceSans3              Family = "source-sans-3"
	SpaceMono                Family = "space-mono"
	Spectral                 Family = "spectral"
	WorkSans                 Family = "work-sans"
)

// Horizontal alignment for the complete top block. See docs/design/templates/contract.md.
type Align string

const (
	Center Align = "center"
	Left   Align = "left"
)

// Displays contact details inline or stacked while preserving array order. See
// docs/design/templates/contract.md.
type DetailsLayout string

const (
	Inline  DetailsLayout = "inline"
	Stacked DetailsLayout = "stacked"
)

// Contact-detail icon style for the top header: none or the Lucide outline glyph. See
// docs/design/templates/tokens.md.
type IconStyle string

const (
	IconStyleNone IconStyle = "none"
	Outline       IconStyle = "outline"
)

type HeadingStyle string

const (
	Normal    HeadingStyle = "normal"
	Titlecase HeadingStyle = "titlecase"
	Uppercase HeadingStyle = "uppercase"
)

// Region filled by colors.surface. With layout.columns set to 1, sidebar renders as none;
// that degradation is not an error and does not rewrite the draft. See
// docs/design/templates/contract.md.
type SurfaceTarget string

const (
	Header            SurfaceTarget = "header"
	Sidebar           SurfaceTarget = "sidebar"
	SurfaceTargetNone SurfaceTarget = "none"
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

// MarshalJSON preserves the schema's absent-versus-explicit-empty distinction
// for personalDetails.details. encoding/json's ordinary omitempty rule would
// collapse a non-nil empty slice to absence.
func (p PersonalDetails) MarshalJSON() ([]byte, error) {
	type personalDetailsJSON PersonalDetails
	if p.Details == nil {
		return json.Marshal(personalDetailsJSON(p))
	}
	return json.Marshal(struct {
		Details []PersonalDetail `json:"details"`
		personalDetailsJSON
	}{Details: p.Details, personalDetailsJSON: personalDetailsJSON(p)})
}
