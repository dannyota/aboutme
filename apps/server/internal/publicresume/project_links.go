package publicresume

import (
	"encoding/json"
	"errors"

	"danny.vn/aboutme/apps/server/internal/project"
)

// ProjectLink is the page of one library project that a visible entry of a
// public resume links. It never carries the project id
// (docs/design/projects-contract.md "Project link context").
type ProjectLink struct {
	// EntryID is the id of the linking resume entry.
	EntryID string `json:"entryId"`
	// ProjectSlug is the project's slug in its owner's library.
	ProjectSlug string `json:"projectSlug"`
	// URL is the absolute project page address at the canonical origin.
	URL string `json:"url"`
}

var errLinkContext = errors.New("public resume: invalid project link context")

// storedLink is one element of the snapshot statement's link context.
type storedLink struct {
	EntryID     string `json:"entryId"`
	ProjectSlug string `json:"projectSlug"`
}

// projectLinks turns the statement's link context into page links in the
// public document's layout order, then entry order. Only entries the
// projection shows get a link, so the links can never name an entry the
// public document omits.
func projectLinks(raw json.RawMessage, public PublicResume, origin PublicOrigin) ([]ProjectLink, error) {
	entries, err := linkedEntries(raw, public, origin)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectLink, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.link)
	}
	return out, nil
}

// linkedEntry is one project link with the linking entry's own link in the
// public projection, or "" when the entry has none.
type linkedEntry struct {
	link      ProjectLink
	entryLink string
}

// linkedEntries is projectLinks with each linking entry's own link.
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
