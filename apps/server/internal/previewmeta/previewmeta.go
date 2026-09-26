// Package previewmeta derives the link-preview text of a live resume page: the
// preview title, the description, the Open Graph locale, and the image text.
// Every value comes from the admitted public snapshot and the validated public
// title, never from hidden fields. See docs/design/link-previews.md, "Text
// rules", and docs/adr/0055-stored-link-preview-card.md.
package previewmeta

import (
	"strings"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// SiteName is the og:site_name value and the host shown in fallback text.
const SiteName = "aboutme.vn"

const (
	// MaxTitleGraphemes bounds a preview title built from the full name.
	MaxTitleGraphemes = 70
	// MaxDescriptionGraphemes bounds the description.
	MaxDescriptionGraphemes = 160
	// MaxImageTextGraphemes bounds the image text.
	MaxImageTextGraphemes = 200
	// MaxDescriptionBytes bounds the UTF-8 size of a description whose
	// grapheme clusters are unusually long, such as emoji sequences, so the
	// render request stays inside its fixed byte limit. Ordinary Latin,
	// Vietnamese, and CJK text reaches the grapheme limit first.
	MaxDescriptionBytes = 1024
	// MaxImageTextBytes bounds the UTF-8 size of the image text the same way.
	MaxImageTextBytes = 1024
)

// Meta is the preview text for one live resume page. The renderer writes each
// value into the page head, and the public HTML validator accepts exactly
// these values.
type Meta struct {
	// Title is og:title.
	Title string `json:"title"`
	// Description is the description meta and og:description.
	Description string `json:"description"`
	// Locale is og:locale, or "" when the resume language maps to none.
	Locale string `json:"locale"`
	// ImageAlt is og:image:alt and twitter:image:alt.
	ImageAlt string `json:"imageAlt"`
}

// For derives the preview text of resume. publicTitle is the owner's validated
// public page title, or nil when none is set.
func For(resume publicresume.PublicResume, publicTitle *string) Meta {
	contacts := contactValues(resume.Document.PersonalDetails)
	return Meta{
		Title:       Title(publicTitle, resume.Document.PersonalDetails.FullName, resume.Slug, contacts),
		Description: Description(resume, contacts),
		Locale:      Locale(resume.Lng),
		ImageAlt:    ImageText(resume.Document.PersonalDetails, resume.Slug, contacts),
	}
}

// Title is the preview title: the public title as written when set, else the
// scrubbed full name cut to MaxTitleGraphemes, else the site and slug.
func Title(publicTitle *string, fullName, slug string, contacts []string) string {
	if publicTitle != nil && *publicTitle != "" {
		return *publicTitle
	}
	if name := firstGraphemes(Scrub(fullName, contacts), MaxTitleGraphemes, 0); name != "" {
		return name
	}
	return siteSlug(slug)
}

// Description is the summary, or its fallback, as plain text cut to
// MaxDescriptionGraphemes.
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

// ImageText describes the share image: the name, plus a spaced middle dot and
// the headline, cut to MaxImageTextGraphemes. A name or headline that loses a
// scrubbed token is left out, as it is on the preview card. Without a name it
// is the site and slug.
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

// Locale maps a BCP 47 resume language to an Open Graph locale: vi to vi_VN,
// en to en_US, and language-REGION to language_REGION. Every other language,
// including und, maps to "".
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

// cardField is a normalized name or headline, or "" when scrubbing would
// change it.
func cardField(value string, contacts []string) string {
	normalized := Normalize(value)
	if Scrub(normalized, contacts) != normalized {
		return ""
	}
	return normalized
}

// summary is the plain text of the first profile section in layout order,
// main before sidebar. Sections outside the layout never count.
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

// latestRole is the first entry of the first work section in layout order, as
// "<job title>, <employer>" or whichever field exists.
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

// contactValues lists the normalized visible contact values, longest first,
// so a value that contains another is removed whole.
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
