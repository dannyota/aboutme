package resumeapi

import (
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/sanitize"
)

// richTextPaths is guarded against the embedded schema by
// TestRichTextPaths_ExactlyMatchCurrentSchema. It names semantic section
// types, not content map keys, because users choose the latter.
var richTextPaths = []string{
	"profile.text",
	"work.description",
	"education.description",
	"skill.infoHtml",
	"certificate.description",
	"project.description",
	"custom.description",
}

// sanitizeDocument returns a copy whose schema-declared rich-text fields have
// passed the shared Go sanitizer. Nil stays nil and a present empty string
// stays present.
func sanitizeDocument(doc schema.Resume) schema.Resume {
	out := doc
	out.Content = make(map[string]schema.Section, len(doc.Content))
	for key, section := range doc.Content {
		switch section.SectionType {
		case schema.Profile:
			section.ProfileEntries = cloneEntries(section.ProfileEntries)
			for i := range section.ProfileEntries {
				section.ProfileEntries[i].Text = sanitizeOptional(section.ProfileEntries[i].Text)
			}
		case schema.Work:
			section.WorkEntries = cloneEntries(section.WorkEntries)
			for i := range section.WorkEntries {
				section.WorkEntries[i].Description = sanitizeOptional(section.WorkEntries[i].Description)
			}
		case schema.Education:
			section.EducationEntries = cloneEntries(section.EducationEntries)
			for i := range section.EducationEntries {
				section.EducationEntries[i].Description = sanitizeOptional(section.EducationEntries[i].Description)
			}
		case schema.Skill:
			section.SkillEntries = cloneEntries(section.SkillEntries)
			for i := range section.SkillEntries {
				section.SkillEntries[i].InfoHTML = sanitizeOptional(section.SkillEntries[i].InfoHTML)
			}
		case schema.Certificate:
			section.CertificateEntries = cloneEntries(section.CertificateEntries)
			for i := range section.CertificateEntries {
				section.CertificateEntries[i].Description = sanitizeOptional(section.CertificateEntries[i].Description)
			}
		case schema.Project:
			section.ProjectEntries = cloneEntries(section.ProjectEntries)
			for i := range section.ProjectEntries {
				section.ProjectEntries[i].Description = sanitizeOptional(section.ProjectEntries[i].Description)
			}
		case schema.SectionTypeCustom:
			section.CustomEntries = cloneEntries(section.CustomEntries)
			for i := range section.CustomEntries {
				section.CustomEntries[i].Description = sanitizeOptional(section.CustomEntries[i].Description)
			}
		case schema.Language:
			section.LanguageEntries = cloneEntries(section.LanguageEntries)
		}
		out.Content[key] = section
	}
	return out
}

func cloneEntries[T any](entries []T) []T {
	if entries == nil {
		return nil
	}
	return append(make([]T, 0, len(entries)), entries...)
}

func sanitizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	sanitized := sanitize.RichText(*value)
	return &sanitized
}
