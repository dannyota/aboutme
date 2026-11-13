package resumeapi

import (
	schema "danny.vn/aboutme/packages/schema/gen/go"
)

func keepV3Fields(doc *schema.Resume, current schema.Resume) {
	doc.Customization.Font.TextAlign = current.Customization.Font.TextAlign
	displays := make(map[string]*schema.Display, len(current.PersonalDetails.Details))
	for _, detail := range current.PersonalDetails.Details {
		displays[detail.ID] = detail.Display
	}
	for index := range doc.PersonalDetails.Details {
		doc.PersonalDetails.Details[index].Display = displays[doc.PersonalDetails.Details[index].ID]
	}
}

func keepV4Fields(doc *schema.Resume, current schema.Resume) {
	if doc.Customization.Header != nil && current.Customization.Header != nil {
		doc.Customization.Header.PhotoPosition = current.Customization.Header.PhotoPosition
	}
	subtitles := make(map[string]*string)
	for _, section := range current.Content {
		if section.SectionType != schema.Project {
			continue
		}
		for _, entry := range section.ProjectEntries {
			subtitles[entry.ID] = entry.Subtitle
		}
	}
	for _, section := range doc.Content {
		if section.SectionType != schema.Project {
			continue
		}
		for index := range section.ProjectEntries {
			section.ProjectEntries[index].Subtitle = subtitles[section.ProjectEntries[index].ID]
		}
	}
}

func keepV5Fields(doc *schema.Resume, current schema.Resume) {
	doc.Customization.ColorScheme = current.Customization.ColorScheme
}

func keepV6Fields(doc *schema.Resume, current schema.Resume) {
	links := make(map[string]*string)
	for _, section := range current.Content {
		for _, entry := range section.ProjectEntries {
			links[entry.ID] = entry.ProjectID
		}
	}
	for _, section := range doc.Content {
		for index := range section.ProjectEntries {
			section.ProjectEntries[index].ProjectID = links[section.ProjectEntries[index].ID]
		}
	}
}
