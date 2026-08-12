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
			section.ProfileEntries = append([]schema.ProfileEntry(nil), section.ProfileEntries...)
			for i := range section.ProfileEntries {
				section.ProfileEntries[i].Text = sanitizeOptional(section.ProfileEntries[i].Text)
			}
		case schema.Work:
			section.WorkEntries = append([]schema.WorkEntry(nil), section.WorkEntries...)
			for i := range section.WorkEntries {
				section.WorkEntries[i].Description = sanitizeOptional(section.WorkEntries[i].Description)
			}
		case schema.Education:
			section.EducationEntries = append([]schema.EducationEntry(nil), section.EducationEntries...)
			for i := range section.EducationEntries {
				section.EducationEntries[i].Description = sanitizeOptional(section.EducationEntries[i].Description)
			}
		case schema.Skill:
			section.SkillEntries = append([]schema.SkillEntry(nil), section.SkillEntries...)
			for i := range section.SkillEntries {
				section.SkillEntries[i].InfoHTML = sanitizeOptional(section.SkillEntries[i].InfoHTML)
			}
		case schema.Certificate:
			section.CertificateEntries = append([]schema.CertificateEntry(nil), section.CertificateEntries...)
			for i := range section.CertificateEntries {
				section.CertificateEntries[i].Description = sanitizeOptional(section.CertificateEntries[i].Description)
			}
		case schema.Project:
			section.ProjectEntries = append([]schema.ProjectEntry(nil), section.ProjectEntries...)
			for i := range section.ProjectEntries {
				section.ProjectEntries[i].Description = sanitizeOptional(section.ProjectEntries[i].Description)
			}
		case schema.SectionTypeCustom:
			section.CustomEntries = append([]schema.CustomEntry(nil), section.CustomEntries...)
			for i := range section.CustomEntries {
				section.CustomEntries[i].Description = sanitizeOptional(section.CustomEntries[i].Description)
			}
		}
		out.Content[key] = section
	}
	return out
}

func sanitizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	sanitized := sanitize.RichText(*value)
	return &sanitized
}
