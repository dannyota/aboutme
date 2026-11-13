package previewmeta

import (
	"strings"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const SiteName = "aboutme.vn"

const (
	MaxTitleGraphemes       = 70
	MaxDescriptionGraphemes = 160
	MaxImageTextGraphemes   = 200
	MaxDescriptionBytes     = 1024
	MaxImageTextBytes       = 1024
)

type Meta struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Locale      string `json:"locale"`
	ImageAlt    string `json:"imageAlt"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

func For(resume publicresume.PublicResume, publicTitle *string) Meta {
	contacts := contactValues(resume.Document.PersonalDetails)
	return Meta{
		Title:       Title(publicTitle, resume.Document.PersonalDetails.FullName, resume.Slug, contacts),
		Description: Description(resume, contacts),
		Locale:      Locale(resume.Lng),
		ImageAlt:    ImageText(resume.Document.PersonalDetails, resume.Slug, contacts),
	}
}

func Title(publicTitle *string, fullName, slug string, contacts []string) string {
	if publicTitle != nil && *publicTitle != "" {
		return *publicTitle
	}
	if name := firstGraphemes(Scrub(fullName, contacts), MaxTitleGraphemes, 0); name != "" {
		return name
	}
	return siteSlug(slug)
}

func Description(resume publicresume.PublicResume, contacts []string) string {
	document := resume.Document
	if text := Scrub(summary(document), contacts); text != "" {
		return Cut(text)
	}
	headline := ""
	if document.PersonalDetails.Headline != nil {
		headline = Normalize(*document.PersonalDetails.Headline)
	}
	if text := Scrub(joinPresent(" · ", headline, latestRole(document)), contacts); text != "" {
		return Cut(text)
	}
	if isVietnamese(resume.Lng) {
		return "CV trên " + SiteName
	}
	return "Resume on " + SiteName
}

func ImageText(person publicresume.PublicPersonalDetails, slug string, contacts []string) string {
	name := cardField(person.FullName, contacts)
	if name == "" {
		return siteSlug(slug)
	}
	headline := ""
	if person.Headline != nil {
		headline = cardField(*person.Headline, contacts)
	}
	return firstGraphemes(joinPresent(" · ", name, headline), MaxImageTextGraphemes, MaxImageTextBytes)
}

func CardText(person publicresume.PublicPersonalDetails) (name, headline string) {
	contacts := contactValues(person)
	name = cardField(person.FullName, contacts)
	if name == "" || person.Headline == nil {
		return name, ""
	}
	return name, cardField(*person.Headline, contacts)
}

func Locale(lng string) string {
	switch lng {
	case "vi":
		return "vi_VN"
	case "en":
		return "en_US"
	}
	language, region, found := strings.Cut(lng, "-")
	if !found || !lowerLetters(language, 2, 3) || !upperLetters(region, 2) {
		return ""
	}
	return language + "_" + region
}

func cardField(value string, contacts []string) string {
	normalized := Normalize(value)
	if Scrub(normalized, contacts) != normalized {
		return ""
	}
	return normalized
}

func summary(document publicresume.PublicResumeDocument) string {
	section, ok := firstSection(document, schema.Profile)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(section.ProfileEntries))
	for _, entry := range section.ProfileEntries {
		if entry.Text != nil {
			parts = append(parts, richTextPlain(*entry.Text))
		}
	}
	return Normalize(strings.Join(parts, " "))
}

func latestRole(document publicresume.PublicResumeDocument) string {
	section, ok := firstSection(document, schema.Work)
	if !ok || len(section.WorkEntries) == 0 {
		return ""
	}
	entry := section.WorkEntries[0]
	title, employer := "", ""
	if entry.JobTitle != nil {
		title = Normalize(*entry.JobTitle)
	}
	if entry.Employer != nil {
		employer = Normalize(*entry.Employer)
	}
	return joinPresent(", ", title, employer)
}

func firstSection(document publicresume.PublicResumeDocument, sectionType schema.SectionType) (publicresume.PublicSection, bool) {
	layout := document.Customization.Layout.Sections
	for _, keys := range [][]string{layout.Main, layout.Sidebar} {
		for _, key := range keys {
			section, ok := document.Content[key]
			if ok && section.SectionType == string(sectionType) {
				return section, true
			}
		}
	}
	return publicresume.PublicSection{}, false
}

func contactValues(person publicresume.PublicPersonalDetails) []string {
	var values []string
	if !person.Details.Present() {
		return values
	}
	for _, detail := range person.Details.Value() {
		if value := Normalize(detail.Value); value != "" {
			values = append(values, value)
		}
	}
	sortLongestFirst(values)
	return values
}

func joinPresent(separator string, values ...string) string {
	present := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			present = append(present, value)
		}
	}
	return strings.Join(present, separator)
}

func siteSlug(slug string) string { return SiteName + "/" + slug }

func isVietnamese(lng string) bool {
	language, _, _ := strings.Cut(lng, "-")
	return strings.EqualFold(language, "vi")
}

func lowerLetters(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for index := range len(value) {
		if value[index] < 'a' || value[index] > 'z' {
			return false
		}
	}
	return true
}

func upperLetters(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for index := range len(value) {
		if value[index] < 'A' || value[index] > 'Z' {
			return false
		}
	}
	return true
}
