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
	// Available counts available projects: on the list, the ones it shows;
	// on a project page, the requested one when shown and every other
	// linked, visible project, whose document the page does not read.
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

// ProjectRead is the project part of an admitted project read, not yet
// decoded. Pages builds it.
type ProjectRead struct {
	public      PublicResume
	origin      PublicOrigin
	rawLinks    json.RawMessage
	rawProjects json.RawMessage
	projectSlug string
}

// ReadProjects reads and admits a live resume for a project route behind the
// same admission as ReadResume: the live gate, the HTML lease at the row's
// revision, and one retry on a changed generation. An empty projectSlug
// reads the list; otherwise only that project. An unknown or non-live
// resume, or a project slug outside the grammar, is ErrNotFound with no
// lease. Admission decodes no project: the caller applies sign in to view
// first and only then builds the pages with ProjectRead.Pages, so a viewer
// without a pass costs the same work for an available and an unavailable
// project (docs/design/projects.md "Project pages and entry points").
func (r *Reader) ReadProjects(ctx context.Context, slug, projectSlug string) (Snapshot, ProjectRead, *publicstate.Lease, error) {
	if projectSlug != "" && !project.ValidSlug(projectSlug) {
		return Snapshot{}, ProjectRead{}, nil, ErrNotFound
	}
	for attempt := 0; attempt < 2; attempt++ {
		row, links, projects, err := r.readProjectRow(ctx, slug, projectSlug)
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, ProjectRead{}, nil, ErrNotFound
		}
		if err != nil {
			return Snapshot{}, ProjectRead{}, nil, ErrUnavailable
		}
		if row.Slug == nil || *row.Slug != slug || !row.Live {
			return Snapshot{}, ProjectRead{}, nil, ErrNotFound
		}
		if row.Revision <= 0 {
			return Snapshot{}, ProjectRead{}, nil, ErrUnavailable
		}
		lease, err := r.coordinator.AcquireResume(ctx, row.ID, row.Revision, publicstate.RepresentationHTML)
		if err != nil {
			var mismatch *publicstate.GenerationMismatchError
			if errors.As(err, &mismatch) && attempt == 0 {
				continue
			}
			return Snapshot{}, ProjectRead{}, nil, ErrUnavailable
		}
		snapshot, err := r.snapshot(row)
		if err != nil {
			lease.Release()
			return Snapshot{}, ProjectRead{}, nil, ErrUnavailable
		}
		read := ProjectRead{public: snapshot.Public, origin: r.origin, rawLinks: links, rawProjects: projects, projectSlug: projectSlug}
		return snapshot, read, lease, nil
	}
	return Snapshot{}, ProjectRead{}, nil, ErrUnavailable
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

// Pages decodes the read and keeps only the projects whose links the public
// projection shows and whose name, in the language project.ChooseText picks,
// the renderer can show, once per project slug, in layout and entry order,
// with each description sanitized again. A malformed column or document is
// ErrUnavailable. Pages that are not Shown answer as an unknown resume.
func (p ProjectRead) Pages() (ProjectPages, error) {
	projectSlug := p.projectSlug
	entries, err := linkedEntries(p.rawLinks, p.public, p.origin)
	if err != nil {
		return ProjectPages{}, ErrUnavailable
	}
	documents, err := storedDocuments(p.rawProjects, projectSlug)
	if err != nil {
		return ProjectPages{}, ErrUnavailable
	}
	var pages ProjectPages
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry.link.ProjectSlug] {
			continue
		}
		seen[entry.link.ProjectSlug] = true
		if projectSlug != "" && entry.link.ProjectSlug != projectSlug {
			// Another project of a page; the page statement reads no other
			// document, so it counts as available.
			pages.Available++
			continue
		}
		raw, found := documents[entry.link.EntryID]
		if !found && projectSlug != "" {
			continue
		}
		if !found {
			return ProjectPages{}, ErrUnavailable
		}
		document, err := publicDocument(raw)
		if err != nil {
			return ProjectPages{}, ErrUnavailable
		}
		if _, _, renderable := project.ChooseText(document, p.public.Lng); !renderable {
			// A name the renderer would read as blank: the project is not
			// available, so it is never shown and never fails a page.
			continue
		}
		pages.Available++
		pages.Projects = append(pages.Projects, PublicProject{Slug: entry.link.ProjectSlug, URL: entry.link.URL, EntryLink: entry.entryLink, Document: document})
	}
	return pages, nil
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
