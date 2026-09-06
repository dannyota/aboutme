package publicresume

import (
	"errors"
	"strconv"
	"unicode/utf8"

	"golang.org/x/text/language"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/sanitize"
)

// Project creates the only data shape allowed to cross from an owner resume
// into a public representation.
func Project(source resume.Resume, origin PublicOrigin) (PublicResume, error) {
	if source.Slug == nil || *source.Slug == "" {
		return PublicResume{}, errors.New("public resume is missing slug")
	}
	photoURL := ""
	if source.Doc.PersonalDetails.Photo != nil {
		photoURL = origin.Resolve("/api/v1/public/resumes/" + *source.Slug + "/photo")
	}
	document := ProjectDocument(source.Doc, photoURL)
	return PublicResume{
		Slug: *source.Slug, Revision: strconv.FormatInt(source.Revision, 10), Lng: publicLanguage(source.Lng),
		DownloadEnabled: source.DownloadEnabled,
		Document:        document,
	}, nil
}

// ProjectDocument creates the only document shape allowed to cross from an
// owner resume into a public or internal print representation. photoURL is the
// already-authorized render URL; an empty value removes private photo metadata.
func ProjectDocument(source schema.Resume, photoURL string) PublicResumeDocument {
	personal := projectPersonal(source.PersonalDetails, photoURL)
	content := make(PublicContent, len(source.Content))
	for key, section := range source.Content {
		projected, ok := projectSection(section)
		if ok {
			content[key] = projected
		}
	}
	customization := cloneCustomization(source.Customization)
	customization.Layout.Sections.Main = pruneLayout(customization.Layout.Sections.Main, content)
	customization.Layout.Sections.Sidebar = pruneLayout(customization.Layout.Sections.Sidebar, content)
	return PublicResumeDocument{SchemaVersion: source.SchemaVersion, PersonalDetails: personal, Content: content, Customization: customization}
}

func publicLanguage(value *string) string {
	if value == nil || *value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(*value)
	if err != nil {
		return language.Und.String()
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return language.Und.String()
	}
	return canonical
}

func projectPersonal(source schema.PersonalDetails, photoURL string) PublicPersonalDetails {
	details := AbsentPublicDetails()
	if source.Details != nil {
		values := make([]PublicPersonalDetail, 0, len(source.Details))
		for _, detail := range source.Details {
			if detail.IsHidden || detail.Value == "" {
				continue
			}
			values = append(values, PublicPersonalDetail{ID: detail.ID, Label: clonePointer(detail.Label), Type: string(detail.Type), Value: detail.Value})
		}
		details = PresentPublicDetails(values)
	}
	fullName := ""
	if source.FullName != nil {
		fullName = *source.FullName
	}
	out := PublicPersonalDetails{Details: details, FullName: fullName, Headline: clonePointer(source.Headline)}
	if source.Photo != nil && photoURL != "" {
		out.Photo = &PublicPhoto{URL: photoURL}
		if source.Photo.Crop != nil {
			out.Photo.Crop = &PublicPhotoCrop{Height: source.Photo.Crop.Height, Width: source.Photo.Crop.Width, X: source.Photo.Crop.X, Y: source.Photo.Crop.Y}
		}
	}
	return out
}

func projectSection(source schema.Section) (PublicSection, bool) {
	out := PublicSection{SectionType: string(source.SectionType), DisplayName: clonePointer(source.DisplayName), IconKey: clonePointer(source.IconKey)}
	switch source.SectionType {
	case schema.Profile:
		for _, entry := range source.ProfileEntries {
			if !hidden(entry.IsHidden) {
				out.ProfileEntries = append(out.ProfileEntries, PublicProfileEntry{ID: entry.ID, Text: sanitizeOptional(entry.Text)})
			}
		}
		return out, len(out.ProfileEntries) != 0
	case schema.Work:
		for _, entry := range source.WorkEntries {
			if !hidden(entry.IsHidden) {
				out.WorkEntries = append(out.WorkEntries, PublicWorkEntry{City: clonePointer(entry.City), Country: clonePointer(entry.Country), Dates: projectDates(entry.Dates), Description: sanitizeOptional(entry.Description), Employer: clonePointer(entry.Employer), EmployerLink: clonePointer(entry.EmployerLink), ID: entry.ID, JobTitle: clonePointer(entry.JobTitle)})
			}
		}
		return out, len(out.WorkEntries) != 0
	case schema.Education:
		for _, entry := range source.EducationEntries {
			if !hidden(entry.IsHidden) {
				out.EducationEntries = append(out.EducationEntries, PublicEducationEntry{City: clonePointer(entry.City), Country: clonePointer(entry.Country), Dates: projectDates(entry.Dates), Degree: clonePointer(entry.Degree), Description: sanitizeOptional(entry.Description), ID: entry.ID, School: clonePointer(entry.School), SchoolLink: clonePointer(entry.SchoolLink)})
			}
		}
		return out, len(out.EducationEntries) != 0
	case schema.Skill:
		for _, entry := range source.SkillEntries {
			if !hidden(entry.IsHidden) {
				out.SkillEntries = append(out.SkillEntries, PublicSkillEntry{ID: entry.ID, InfoHTML: sanitizeOptional(entry.InfoHTML), Level: clonePointer(entry.Level), Name: clonePointer(entry.Name)})
			}
		}
		return out, len(out.SkillEntries) != 0
	case schema.Language:
		for _, entry := range source.LanguageEntries {
			if !hidden(entry.IsHidden) {
				out.LanguageEntries = append(out.LanguageEntries, PublicLanguageEntry{ID: entry.ID, Level: clonePointer(entry.Level), Name: clonePointer(entry.Name)})
			}
		}
		return out, len(out.LanguageEntries) != 0
	case schema.Certificate:
		for _, entry := range source.CertificateEntries {
			if !hidden(entry.IsHidden) {
				out.CertificateEntries = append(out.CertificateEntries, PublicCertificateEntry{Date: projectYearMonth(entry.Date), Description: sanitizeOptional(entry.Description), ID: entry.ID, Issuer: clonePointer(entry.Issuer), Title: clonePointer(entry.Title), TitleLink: clonePointer(entry.TitleLink)})
			}
		}
		return out, len(out.CertificateEntries) != 0
	case schema.Project:
		for _, entry := range source.ProjectEntries {
			if !hidden(entry.IsHidden) {
				out.ProjectEntries = append(out.ProjectEntries, PublicProjectEntry{Dates: projectDates(entry.Dates), Description: sanitizeOptional(entry.Description), ID: entry.ID, Link: clonePointer(entry.Link), Title: clonePointer(entry.Title)})
			}
		}
		return out, len(out.ProjectEntries) != 0
	case schema.SectionTypeCustom:
		for _, entry := range source.CustomEntries {
			if !hidden(entry.IsHidden) {
				out.CustomEntries = append(out.CustomEntries, PublicCustomEntry{City: clonePointer(entry.City), Dates: projectDates(entry.Dates), Description: sanitizeOptional(entry.Description), ID: entry.ID, Subtitle: clonePointer(entry.Subtitle), Title: clonePointer(entry.Title), TitleLink: clonePointer(entry.TitleLink)})
			}
		}
		return out, len(out.CustomEntries) != 0
	default:
		return PublicSection{}, false
	}
}

func hidden(value *bool) bool { return value != nil && *value }
func sanitizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	out := sanitize.RichText(*value)
	return &out
}
func projectYearMonth(source *schema.YearMonth) *PublicYearMonth {
	if source == nil {
		return nil
	}
	return &PublicYearMonth{M: clonePointer(source.M), Y: source.Y}
}

func clonePointer[T any](source *T) *T {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

func cloneStrings(source []string) []string {
	if source == nil {
		return nil
	}
	return append([]string{}, source...)
}

func cloneCustomization(source schema.Customization) schema.Customization {
	out := source
	out.Colors.Accent = clonePointer(source.Colors.Accent)
	out.Colors.Surface = clonePointer(source.Colors.Surface)
	out.Header = clonePointer(source.Header)
	out.Layout.SurfaceTarget = clonePointer(source.Layout.SurfaceTarget)
	out.Layout.Sections.Main = cloneStrings(source.Layout.Sections.Main)
	out.Layout.Sections.Sidebar = cloneStrings(source.Layout.Sections.Sidebar)
	out.Spacing.PageMargin = clonePointer(source.Spacing.PageMargin)
	return out
}
func projectDates(source *schema.DateRange) *PublicDateRange {
	if source == nil {
		return nil
	}
	return &PublicDateRange{End: projectYearMonth(source.End), Present: source.Present, Start: PublicYearMonth{M: clonePointer(source.Start.M), Y: source.Start.Y}}
}
func pruneLayout(source []string, content PublicContent) []string {
	if source == nil {
		return nil
	}
	out := make([]string, 0, len(source))
	for _, key := range source {
		if _, ok := content[key]; ok {
			out = append(out, key)
		}
	}
	return out
}
