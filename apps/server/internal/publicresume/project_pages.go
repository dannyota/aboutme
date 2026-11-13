package publicresume

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/sanitize"
	"danny.vn/aboutme/apps/server/internal/store"
)

// PublicProject is one available library project of a public resume
// (docs/design/projects-contract.md "Project link context"). It never
// carries the project id.
type PublicProject struct {
	// Slug is the project's slug in its owner's library.
	Slug string
	// URL is the absolute project page address at the canonical origin.
	URL string
	// EntryLink is the linking entry's own link in the public projection,
	// exactly as stored, or "" when the entry has none.
	EntryLink string
	// Document is the library project's stored document. The list reads it
	// with every description removed.
	Document project.Document
}

// ProjectPages is what one project route may show of an admitted resume.
type ProjectPages struct {
	// Projects holds every available project in layout and entry order for
	// the list, or only the requested project for a project page. It is
	// empty when the page is unavailable.
	Projects []PublicProject
	// Available counts every available project of the resume.
	Available int
}

// Shown reports whether the requested page has something to show: the list
// has an available project, or the requested project is available. A page
// that does not answers exactly as an unknown resume, but only after the
// caller's pass check, so a viewer without a pass cannot tell an available
// project from an unavailable one.
func (p ProjectPages) Shown() bool {
	return len(p.Projects) != 0
}

// storedProject is one element of a project statement's projects column.
type storedProject struct {
	EntryID     string          `json:"entryId"`
	ProjectSlug string          `json:"projectSlug"`
	Document    json.RawMessage `json:"document"`
}

// ReadProjects reads a live resume and its available library projects
// behind the same admission as ReadResume: the live gate, the HTML lease at
// the row's revision, and one retry on a changed generation. An empty
// projectSlug reads the list; otherwise only that project. A project is
// available only when its linking entry is one the public projection shows,
// as projectLinks decides. An unknown or non-live resume, or a project slug
// outside the grammar, is ErrNotFound with no lease. A live resume with no
// available project, or without the requested one, is admitted: it returns
// the snapshot and lease with pages that are not Shown, so the caller can
// apply sign in to view first (docs/design/projects.md "Project pages and
// entry points").
func (r *Reader) ReadProjects(ctx context.Context, slug, projectSlug string) (Snapshot, ProjectPages, *publicstate.Lease, error) {
	if projectSlug != "" && !project.ValidSlug(projectSlug) {
		return Snapshot{}, ProjectPages{}, nil, ErrNotFound
	}
	for attempt := 0; attempt < 2; attempt++ {
		row, links, projects, err := r.readProjectRow(ctx, slug, projectSlug)
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, ProjectPages{}, nil, ErrNotFound
		}
		if err != nil {
			return Snapshot{}, ProjectPages{}, nil, ErrUnavailable
		}
		if row.Slug == nil || *row.Slug != slug || !row.Live {
			return Snapshot{}, ProjectPages{}, nil, ErrNotFound
		}
		if row.Revision <= 0 {
			return Snapshot{}, ProjectPages{}, nil, ErrUnavailable
		}
		lease, err := r.coordinator.AcquireResume(ctx, row.ID, row.Revision, publicstate.RepresentationHTML)
		if err != nil {
			var mismatch *publicstate.GenerationMismatchError
			if errors.As(err, &mismatch) && attempt == 0 {
				continue
			}
			return Snapshot{}, ProjectPages{}, nil, ErrUnavailable
		}
		snapshot, pages, err := r.projectPages(row, links, projects, projectSlug)
		if err != nil {
			lease.Release()
			return Snapshot{}, ProjectPages{}, nil, err
		}
		return snapshot, pages, lease, nil
	}
	return Snapshot{}, ProjectPages{}, nil, ErrUnavailable
}

// readProjectRow runs the list or the project page statement and returns
// the row, its link context and its projects column. The list statement's
// projects column is also its link context.
func (r *Reader) readProjectRow(ctx context.Context, slug, projectSlug string) (store.Resume, json.RawMessage, json.RawMessage, error) {
	if projectSlug == "" {
		read, err := r.store.GetPublicResumeProjectList(ctx, slug)
		return read.Resume, read.Projects, read.Projects, err
	}
	read, err := r.store.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: slug, ProjectSlug: projectSlug})
	return read.Resume, read.ProjectLinks, read.Projects, err
}

// projectPages projects the row and keeps only the projects whose links the
// public projection shows, once per project slug, in layout and entry order.
func (r *Reader) projectPages(row store.Resume, rawLinks, rawProjects json.RawMessage, projectSlug string) (Snapshot, ProjectPages, error) {
	snapshot, err := r.snapshot(row)
	if err != nil {
		return Snapshot{}, ProjectPages{}, ErrUnavailable
	}
	entries, err := linkedEntries(rawLinks, snapshot.Public, r.origin)
	if err != nil {
		return Snapshot{}, ProjectPages{}, ErrUnavailable
	}
	snapshot.ProjectLinks = make([]ProjectLink, 0, len(entries))
	for _, entry := range entries {
		snapshot.ProjectLinks = append(snapshot.ProjectLinks, entry.link)
	}
	documents, err := storedDocuments(rawProjects, projectSlug)
	if err != nil {
		return Snapshot{}, ProjectPages{}, ErrUnavailable
	}
	var pages ProjectPages
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry.link.ProjectSlug] {
			continue
		}
		seen[entry.link.ProjectSlug] = true
		pages.Available++
		raw, found := documents[entry.link.EntryID]
		if projectSlug != "" && (entry.link.ProjectSlug != projectSlug || !found) {
			continue
		}
		if !found {
			return Snapshot{}, ProjectPages{}, ErrUnavailable
		}
		document, err := publicDocument(raw)
		if err != nil {
			return Snapshot{}, ProjectPages{}, ErrUnavailable
		}
		pages.Projects = append(pages.Projects, PublicProject{Slug: entry.link.ProjectSlug, URL: entry.link.URL, EntryLink: entry.entryLink, Document: document})
	}
	return snapshot, pages, nil
}

// publicDocument decodes a stored project document and sanitizes each
// description again with the current policy, as the resume projection does
// for its rich text.
func publicDocument(raw json.RawMessage) (project.Document, error) {
	var document project.Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return project.Document{}, err
	}
	for key, text := range document.Text {
		text.Description = sanitize.RichText(text.Description)
		document.Text[key] = text
	}
	return document, nil
}

// storedDocuments indexes a projects column by entry id. A project page
// keeps only rows of the requested slug.
func storedDocuments(raw json.RawMessage, projectSlug string) (map[string]json.RawMessage, error) {
	var stored []storedProject
	if err := json.Unmarshal(raw, &stored); err != nil || stored == nil {
		return nil, errLinkContext
	}
	documents := make(map[string]json.RawMessage, len(stored))
	for _, row := range stored {
		if row.EntryID == "" || !project.ValidSlug(row.ProjectSlug) {
			return nil, errLinkContext
		}
		if projectSlug == "" || row.ProjectSlug == projectSlug {
			documents[row.EntryID] = row.Document
		}
	}
	return documents, nil
}
