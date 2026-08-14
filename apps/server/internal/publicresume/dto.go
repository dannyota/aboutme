package publicresume

import (
	"bytes"
	"encoding/json"
	"sort"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

type PublicResume struct {
	Slug            string               `json:"slug"`
	Revision        string               `json:"revision"`
	Lng             string               `json:"lng"`
	DownloadEnabled bool                 `json:"downloadEnabled"`
	Document        PublicResumeDocument `json:"document"`
}

type PublicResumeDocument struct {
	SchemaVersion   int64                 `json:"schemaVersion"`
	PersonalDetails PublicPersonalDetails `json:"personalDetails"`
	Content         PublicContent         `json:"content"`
	Customization   schema.Customization  `json:"customization"`
}

type PublicPersonalDetails struct {
	Details  PublicDetails
	FullName string       `json:"fullName"`
	Headline *string      `json:"headline,omitempty"`
	Photo    *PublicPhoto `json:"photo,omitempty"`
}

type PublicDetails struct {
	present bool
	value   []PublicPersonalDetail
}

func AbsentPublicDetails() PublicDetails { return PublicDetails{} }

func PresentPublicDetails(value []PublicPersonalDetail) PublicDetails {
	return PublicDetails{present: true, value: append([]PublicPersonalDetail{}, value...)}
}

func (d PublicDetails) Present() bool { return d.present }

func (d PublicDetails) Value() []PublicPersonalDetail {
	return append([]PublicPersonalDetail{}, d.value...)
}

func (p PublicPersonalDetails) MarshalJSON() ([]byte, error) {
	type wire struct {
		Details  []PublicPersonalDetail `json:"details,omitempty"`
		FullName string                 `json:"fullName"`
		Headline *string                `json:"headline,omitempty"`
		Photo    *PublicPhoto           `json:"photo,omitempty"`
	}
	w := wire{FullName: p.FullName, Headline: p.Headline, Photo: p.Photo}
	if p.Details.present {
		w.Details = p.Details.Value()
		return json.Marshal(struct {
			Details  []PublicPersonalDetail `json:"details"`
			FullName string                 `json:"fullName"`
			Headline *string                `json:"headline,omitempty"`
			Photo    *PublicPhoto           `json:"photo,omitempty"`
		}{w.Details, w.FullName, w.Headline, w.Photo})
	}
	return json.Marshal(w)
}

type PublicPersonalDetail struct {
	ID    string  `json:"id"`
	Label *string `json:"label,omitempty"`
	Type  string  `json:"type"`
	Value string  `json:"value"`
}

type PublicPhoto struct {
	URL  string           `json:"url"`
	Crop *PublicPhotoCrop `json:"crop,omitempty"`
}
type PublicPhotoCrop struct {
	Height float64 `json:"height"`
	Width  float64 `json:"width"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}
type PublicYearMonth struct {
	M *int64 `json:"m,omitempty"`
	Y int64  `json:"y"`
}
type PublicDateRange struct {
	End     *PublicYearMonth `json:"end"`
	Present bool             `json:"present"`
	Start   PublicYearMonth  `json:"start"`
}
type PublicProfileEntry struct {
	ID   string  `json:"id"`
	Text *string `json:"text,omitempty"`
}
type PublicWorkEntry struct {
	City         *string          `json:"city,omitempty"`
	Country      *string          `json:"country,omitempty"`
	Dates        *PublicDateRange `json:"dates,omitempty"`
	Description  *string          `json:"description,omitempty"`
	Employer     *string          `json:"employer,omitempty"`
	EmployerLink *string          `json:"employerLink,omitempty"`
	ID           string           `json:"id"`
	JobTitle     *string          `json:"jobTitle,omitempty"`
}
type PublicEducationEntry struct {
	City        *string          `json:"city,omitempty"`
	Country     *string          `json:"country,omitempty"`
	Dates       *PublicDateRange `json:"dates,omitempty"`
	Degree      *string          `json:"degree,omitempty"`
	Description *string          `json:"description,omitempty"`
	ID          string           `json:"id"`
	School      *string          `json:"school,omitempty"`
	SchoolLink  *string          `json:"schoolLink,omitempty"`
}
type PublicSkillEntry struct {
	ID       string  `json:"id"`
	InfoHTML *string `json:"infoHtml,omitempty"`
	Level    *int64  `json:"level,omitempty"`
	Name     *string `json:"name,omitempty"`
}
type PublicLanguageEntry struct {
	ID    string  `json:"id"`
	Level *int64  `json:"level,omitempty"`
	Name  *string `json:"name,omitempty"`
}
type PublicCertificateEntry struct {
	Date        *PublicYearMonth `json:"date,omitempty"`
	Description *string          `json:"description,omitempty"`
	ID          string           `json:"id"`
	Issuer      *string          `json:"issuer,omitempty"`
	Title       *string          `json:"title,omitempty"`
	TitleLink   *string          `json:"titleLink,omitempty"`
}
type PublicProjectEntry struct {
	Dates       *PublicDateRange `json:"dates,omitempty"`
	Description *string          `json:"description,omitempty"`
	ID          string           `json:"id"`
	Link        *string          `json:"link,omitempty"`
	Title       *string          `json:"title,omitempty"`
}
type PublicCustomEntry struct {
	City        *string          `json:"city,omitempty"`
	Dates       *PublicDateRange `json:"dates,omitempty"`
	Description *string          `json:"description,omitempty"`
	ID          string           `json:"id"`
	Subtitle    *string          `json:"subtitle,omitempty"`
	Title       *string          `json:"title,omitempty"`
	TitleLink   *string          `json:"titleLink,omitempty"`
}

type PublicSection struct {
	SectionType        string
	DisplayName        *string
	IconKey            *string
	ProfileEntries     []PublicProfileEntry
	WorkEntries        []PublicWorkEntry
	EducationEntries   []PublicEducationEntry
	SkillEntries       []PublicSkillEntry
	LanguageEntries    []PublicLanguageEntry
	CertificateEntries []PublicCertificateEntry
	ProjectEntries     []PublicProjectEntry
	CustomEntries      []PublicCustomEntry
}

func (s PublicSection) MarshalJSON() ([]byte, error) {
	entries := any(nil)
	switch s.SectionType {
	case string(schema.Profile):
		entries = s.ProfileEntries
	case string(schema.Work):
		entries = s.WorkEntries
	case string(schema.Education):
		entries = s.EducationEntries
	case string(schema.Skill):
		entries = s.SkillEntries
	case string(schema.Language):
		entries = s.LanguageEntries
	case string(schema.Certificate):
		entries = s.CertificateEntries
	case string(schema.Project):
		entries = s.ProjectEntries
	case string(schema.SectionTypeCustom):
		entries = s.CustomEntries
	}
	return json.Marshal(struct {
		SectionType string  `json:"sectionType"`
		DisplayName *string `json:"displayName,omitempty"`
		IconKey     *string `json:"iconKey,omitempty"`
		Entries     any     `json:"entries"`
	}{s.SectionType, s.DisplayName, s.IconKey, entries})
}

type PublicContent map[string]PublicSection

func (c PublicContent) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range keys {
		if i != 0 {
			out.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(c[key])
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
