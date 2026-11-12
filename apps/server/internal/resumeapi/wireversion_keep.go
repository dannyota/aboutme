package resumeapi

// Older-client write rules: what a client at an older document version
// cannot express is restored from the stored document after its write
// (docs/adr/0004-resume-document-contract.md "Versions and converters").

import (
	schema "danny.vn/aboutme/packages/schema/gen/go"
)

// keepV3Fields restores what a v1 or v2 client cannot express: the stored
// body alignment, and the display of every detail that survives the write,
// matched by id. A detail the client added gets none.
// See docs/adr/0013-resume-header-and-contacts.md.
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

// keepV4Fields restores what a v1 to v3 client cannot express: the stored
// header photo position, and the subtitle of every project entry that
// survives the write, matched by entry id. The position applies only while
// both documents have a header, so a client that removes the header removes
// the position with it. A project entry the client added gets no subtitle.
// See docs/adr/0013-resume-header-and-contacts.md.
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

// keepV5Fields restores what a v1 to v4 client cannot express: the stored
// color scheme. It copies the leaf unconditionally, because customization
// exists in every version and no parent can disappear.
// See docs/design/public-page-theme.md.
func keepV5Fields(doc *schema.Resume, current schema.Resume) {
	doc.Customization.ColorScheme = current.Customization.ColorScheme
}

// keepV6Fields restores what a v1 to v5 client cannot express: the project
// link of every project entry that survives the write, matched by entry id.
// A project entry the client added gets no link.
// See docs/design/projects.md "Selection".
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
