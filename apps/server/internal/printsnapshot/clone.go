package printsnapshot

import (
	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func cloneDocument(source publicresume.PublicResumeDocument) publicresume.PublicResumeDocument {
	out := source
	out.PersonalDetails = clonePersonalDetails(source.PersonalDetails)
	out.Content = make(publicresume.PublicContent, len(source.Content))
	for key, section := range source.Content {
		out.Content[key] = cloneSection(section)
	}
	out.Customization = cloneCustomization(source.Customization)
	return out
}

func clonePersonalDetails(source publicresume.PublicPersonalDetails) publicresume.PublicPersonalDetails {
	out := source
	if source.Details.Present() {
		details := source.Details.Value()
		for index := range details {
			details[index].Label = clonePointer(details[index].Label)
			details[index].Display = clonePointer(details[index].Display)
		}
		out.Details = publicresume.PresentPublicDetails(details)
	} else {
		out.Details = publicresume.AbsentPublicDetails()
	}
	out.Headline = clonePointer(source.Headline)
	if source.Photo != nil {
		photo := *source.Photo
		if source.Photo.Crop != nil {
			crop := *source.Photo.Crop
			photo.Crop = &crop
		}
		out.Photo = &photo
	}
	return out
}

func cloneSection(source publicresume.PublicSection) publicresume.PublicSection {
	out := source
	out.DisplayName = clonePointer(source.DisplayName)
	out.IconKey = clonePointer(source.IconKey)
	out.ProfileEntries = append([]publicresume.PublicProfileEntry(nil), source.ProfileEntries...)
	for index := range out.ProfileEntries {
		out.ProfileEntries[index].Text = clonePointer(source.ProfileEntries[index].Text)
	}
	out.WorkEntries = append([]publicresume.PublicWorkEntry(nil), source.WorkEntries...)
	for index := range out.WorkEntries {
		entry, original := &out.WorkEntries[index], source.WorkEntries[index]
		entry.City, entry.Country, entry.Description = clonePointer(original.City), clonePointer(original.Country), clonePointer(original.Description)
		entry.Employer, entry.EmployerLink, entry.JobTitle = clonePointer(original.Employer), clonePointer(original.EmployerLink), clonePointer(original.JobTitle)
		entry.Dates = cloneDates(original.Dates)
	}
	out.EducationEntries = append([]publicresume.PublicEducationEntry(nil), source.EducationEntries...)
	for index := range out.EducationEntries {
		entry, original := &out.EducationEntries[index], source.EducationEntries[index]
		entry.City, entry.Country, entry.Degree = clonePointer(original.City), clonePointer(original.Country), clonePointer(original.Degree)
		entry.Description, entry.School, entry.SchoolLink = clonePointer(original.Description), clonePointer(original.School), clonePointer(original.SchoolLink)
		entry.Dates = cloneDates(original.Dates)
	}
	out.SkillEntries = append([]publicresume.PublicSkillEntry(nil), source.SkillEntries...)
	for index := range out.SkillEntries {
		entry, original := &out.SkillEntries[index], source.SkillEntries[index]
		entry.InfoHTML, entry.Level, entry.Name = clonePointer(original.InfoHTML), clonePointer(original.Level), clonePointer(original.Name)
	}
	out.LanguageEntries = append([]publicresume.PublicLanguageEntry(nil), source.LanguageEntries...)
	for index := range out.LanguageEntries {
		entry, original := &out.LanguageEntries[index], source.LanguageEntries[index]
		entry.Level, entry.Name = clonePointer(original.Level), clonePointer(original.Name)
	}
	out.CertificateEntries = append([]publicresume.PublicCertificateEntry(nil), source.CertificateEntries...)
	for index := range out.CertificateEntries {
		entry, original := &out.CertificateEntries[index], source.CertificateEntries[index]
		entry.Date = cloneYearMonth(original.Date)
		entry.Description, entry.Issuer, entry.Title, entry.TitleLink = clonePointer(original.Description), clonePointer(original.Issuer), clonePointer(original.Title), clonePointer(original.TitleLink)
	}
	out.ProjectEntries = append([]publicresume.PublicProjectEntry(nil), source.ProjectEntries...)
	for index := range out.ProjectEntries {
		entry, original := &out.ProjectEntries[index], source.ProjectEntries[index]
		entry.Dates = cloneDates(original.Dates)
		entry.Description, entry.Link, entry.Title = clonePointer(original.Description), clonePointer(original.Link), clonePointer(original.Title)
		entry.Subtitle = clonePointer(original.Subtitle)
	}
	out.CustomEntries = append([]publicresume.PublicCustomEntry(nil), source.CustomEntries...)
	for index := range out.CustomEntries {
		entry, original := &out.CustomEntries[index], source.CustomEntries[index]
		entry.Dates = cloneDates(original.Dates)
		entry.City, entry.Description, entry.Subtitle = clonePointer(original.City), clonePointer(original.Description), clonePointer(original.Subtitle)
		entry.Title, entry.TitleLink = clonePointer(original.Title), clonePointer(original.TitleLink)
	}
	return out
}

func cloneDates(source *publicresume.PublicDateRange) *publicresume.PublicDateRange {
	if source == nil {
		return nil
	}
	out := *source
	out.End = cloneYearMonth(source.End)
	out.Start.M = clonePointer(source.Start.M)
	return &out
}

func cloneYearMonth(source *publicresume.PublicYearMonth) *publicresume.PublicYearMonth {
	if source == nil {
		return nil
	}
	out := *source
	out.M = clonePointer(source.M)
	return &out
}

func cloneHeader(source *schema.HeaderClass) *schema.HeaderClass {
	out := clonePointer(source)
	if out != nil {
		out.PhotoPosition = clonePointer(source.PhotoPosition)
	}
	return out
}

func cloneCustomization(source schema.Customization) schema.Customization {
	out := source
	out.ColorScheme = nil
	out.Font.TextAlign = clonePointer(source.Font.TextAlign)
	out.Colors.Accent = clonePointer(source.Colors.Accent)
	out.Colors.Surface = clonePointer(source.Colors.Surface)
	out.Header = cloneHeader(source.Header)
	out.Layout.SurfaceTarget = clonePointer(source.Layout.SurfaceTarget)
	out.Layout.Sections.Main = cloneStrings(source.Layout.Sections.Main)
	out.Layout.Sections.Sidebar = cloneStrings(source.Layout.Sections.Sidebar)
	out.Spacing.PageMargin = clonePointer(source.Spacing.PageMargin)
	return out
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
