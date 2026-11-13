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

type PublicProject struct {
	Slug      string
	URL       string
	EntryLink string
	Document  project.Document
}

type ProjectPages struct {
	Projects  []PublicProject
	Available int
}

func (p ProjectPages) Shown() bool {
	return len(p.Projects) != 0
}

type storedProject struct {
	EntryID     string          `json:"entryId"`
	ProjectSlug string          `json:"projectSlug"`
	Document    json.RawMessage `json:"document"`
}

type ProjectRead struct {
	public      PublicResume
	origin      PublicOrigin
	rawLinks    json.RawMessage
	rawProjects json.RawMessage
	projectSlug string
}

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

func (r *Reader) readProjectRow(ctx context.Context, slug, projectSlug string) (store.Resume, json.RawMessage, json.RawMessage, error) {
	if projectSlug == "" {
		read, err := r.store.GetPublicResumeProjectList(ctx, slug)
		return read.Resume, read.Projects, read.Projects, err
	}
	read, err := r.store.GetPublicResumeProject(ctx, store.GetPublicResumeProjectParams{Slug: slug, ProjectSlug: projectSlug})
	return read.Resume, read.ProjectLinks, read.Projects, err
}

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
	var others map[string]json.RawMessage
	if projectSlug != "" {
		if others, err = storedDocuments(p.rawLinks, ""); err != nil {
			return ProjectPages{}, ErrUnavailable
		}
	}
	var pages ProjectPages
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if seen[entry.link.ProjectSlug] {
			continue
		}
		seen[entry.link.ProjectSlug] = true
		if projectSlug != "" && entry.link.ProjectSlug != projectSlug {
			renderable, err := renderableProject(others[entry.link.EntryID], p.public.Lng)
			if err != nil {
				return ProjectPages{}, ErrUnavailable
			}
			if renderable {
				pages.Available++
			}
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
			continue
		}
		pages.Available++
		pages.Projects = append(pages.Projects, PublicProject{Slug: entry.link.ProjectSlug, URL: entry.link.URL, EntryLink: entry.entryLink, Document: document})
	}
	return pages, nil
}

func renderableProject(raw json.RawMessage, resumeLng string) (bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, errLinkContext
	}
	var document project.Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return false, err
	}
	_, _, renderable := project.ChooseText(document, resumeLng)
	return renderable, nil
}

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
