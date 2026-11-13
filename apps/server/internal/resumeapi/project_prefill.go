package resumeapi

import (
	"html"
	"strings"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
)

func projectTextFor(library project.Project, lng *string) project.Text {
	if lng != nil {
		primary, _, _ := strings.Cut(strings.ToLower(*lng), "-")
		if primary != "und" {
			if text, ok := library.Text[primary]; ok {
				return text
			}
		}
	}
	return library.Text[library.DefaultLanguage]
}

func prefillProjectEntry(entry *schema.ProjectEntry, library project.Project, lng *string) {
	text := projectTextFor(library, lng)
	entry.Title = prefillValue(text.Name)
	entry.Subtitle = prefillValue(text.Role)
	link := library.RepositoryURL
	if link == "" {
		link = library.DemoURL
	}
	entry.Link = prefillLink(link)
	entry.Dates = library.Dates
	entry.Description = nil
	if text.Summary != "" {
		paragraph := "<p>" + html.EscapeString(text.Summary) + "</p>"
		entry.Description = &paragraph
	}
}

func prefillLink(raw string) *string {
	link, ok := project.NormalizeLink(raw)
	if !ok || link == "" || !resume.ValidEntryLink(link) {
		return nil
	}
	return &link
}

func prefillValue(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
