package publicresume

import (
	"encoding/json"
	"errors"

	"danny.vn/aboutme/apps/server/internal/project"
)

type ProjectLink struct {
	EntryID     string `json:"entryId"`
	ProjectSlug string `json:"projectSlug"`
	URL         string `json:"url"`
}

var errLinkContext = errors.New("public resume: invalid project link context")

type storedLink struct {
	EntryID     string `json:"entryId"`
	ProjectSlug string `json:"projectSlug"`
}

func ProjectLinks(raw json.RawMessage, public PublicResume, origin PublicOrigin) ([]ProjectLink, error) {
	entries, err := linkedEntries(raw, public, origin)
	if err != nil {
		return nil, err
	}
	documents, err := storedDocuments(raw, "")
	if err != nil {
		return nil, err
	}
	out := make([]ProjectLink, 0, len(entries))
	for _, entry := range entries {
		available, err := renderableProject(documents[entry.link.EntryID], public.Lng)
		if err != nil {
			return nil, errLinkContext
		}
		if available {
			out = append(out, entry.link)
		}
	}
	return out, nil
}

type linkedEntry struct {
	link      ProjectLink
	entryLink string
}

func linkedEntries(raw json.RawMessage, public PublicResume, origin PublicOrigin) ([]linkedEntry, error) {
	var stored []storedLink
	if err := json.Unmarshal(raw, &stored); err != nil || stored == nil {
		return nil, errLinkContext
	}
	slugs := make(map[string]string, len(stored))
	for _, link := range stored {
		if link.EntryID == "" || !project.ValidSlug(link.ProjectSlug) {
			return nil, errLinkContext
		}
		slugs[link.EntryID] = link.ProjectSlug
	}
	out := []linkedEntry{}
	sections := public.Document.Customization.Layout.Sections
	for _, keys := range [][]string{sections.Main, sections.Sidebar} {
		for _, key := range keys {
			for _, entry := range public.Document.Content[key].ProjectEntries {
				slug, linked := slugs[entry.ID]
				if !linked {
					continue
				}
				url := origin.Resolve("/" + public.Slug + "/projects/" + slug)
				if url == "" {
					return nil, errLinkContext
				}
				own := ""
				if entry.Link != nil {
					own = *entry.Link
				}
				out = append(out, linkedEntry{link: ProjectLink{EntryID: entry.ID, ProjectSlug: slug, URL: url}, entryLink: own})
			}
		}
	}
	return out, nil
}
