package resumeapi

import (
	"html"
	"strings"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
)

// projectTextFor picks the project text for a resume language: the text
// whose key equals the language's primary subtag, otherwise the default
// language. An unset or und resume language uses the default language
// (docs/design/projects.md "Language").
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

// prefillProjectEntry overwrites the five prefilled fields of entry from the
// library project and leaves every other field as it is. A field the
// library leaves empty is removed, so a refresh equals a fresh link.
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

// prefillLink lowercases the scheme and host of a library link and leaves the
// entry link out when the result still fails the entry link contract, which
// a project saved before the library applied that contract can hold.
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
