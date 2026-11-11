package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	// ErrProjectNotFound reports a project link that names no project of the
	// resume's owner. Another account's project is reported identically, so
	// a link answer is never an existence oracle.
	ErrProjectNotFound = errors.New("resume: linked project not found")
	// ErrProjectAlreadyLinked reports two entries of one resume that name the
	// same project.
	ErrProjectAlreadyLinked = errors.New("resume: project already linked")
)

// ProjectLinks returns the project each linked project entry of doc names,
// keyed by entry id. Links compare as UUID values, so two spellings of one
// id are one project.
func ProjectLinks(doc schema.Resume) (map[string]uuid.UUID, error) {
	links := make(map[string]uuid.UUID)
	for _, section := range doc.Content {
		for _, entry := range section.ProjectEntries {
			if entry.ProjectID == nil {
				continue
			}
			id, err := uuid.Parse(*entry.ProjectID)
			if err != nil || id == uuid.Nil {
				return nil, ErrProjectNotFound
			}
			links[entry.ID] = id
		}
	}
	return links, nil
}

// ChangesProjectLinks reports whether next adds a link or changes one that
// stored holds, matched by entry id. Keeping or removing links is no change.
// A nil stored document means a new resume, where every link is an addition.
func ChangesProjectLinks(stored *schema.Resume, next schema.Resume) (bool, error) {
	after, err := ProjectLinks(next)
	if err != nil {
		return false, err
	}
	before := map[string]uuid.UUID{}
	if stored != nil {
		if before, err = ProjectLinks(*stored); err != nil {
			return false, err
		}
	}
	for entryID, project := range after {
		if previous, ok := before[entryID]; !ok || previous != project {
			return true, nil
		}
	}
	return false, nil
}

// UnlinkProject removes every link to project from doc, keeping each entry
// and its text, and reports whether any link was removed.
func UnlinkProject(doc *schema.Resume, project uuid.UUID) bool {
	removed := false
	for _, section := range doc.Content {
		for index := range section.ProjectEntries {
			entry := &section.ProjectEntries[index]
			if entry.ProjectID == nil {
				continue
			}
			if id, err := uuid.Parse(*entry.ProjectID); err == nil && id == project {
				entry.ProjectID = nil
				removed = true
			}
		}
	}
	return removed
}

// checkProjectLinksTx requires every link of doc to name a distinct project
// of owner. It takes the owner row lock first, which every REST and agent
// write already holds, so a project deletion that rewrites the owner's
// resumes cannot interleave with this check.
func checkProjectLinksTx(ctx context.Context, qtx *store.Queries, owner uuid.UUID, doc schema.Resume) error {
	links, err := ProjectLinks(doc)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(links))
	seen := make(map[uuid.UUID]struct{}, len(links))
	for _, id := range links {
		if _, duplicate := seen[id]; duplicate {
			return ErrProjectAlreadyLinked
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if _, err = qtx.LockUserForResumeWrite(ctx, owner); err != nil {
		return fmt.Errorf("resume: project links: lock owner row: %w", err)
	}
	owned, err := qtx.CountOwnedProjects(ctx, store.CountOwnedProjectsParams{UserID: owner, Ids: ids})
	if err != nil {
		return fmt.Errorf("resume: project links: %w", err)
	}
	if owned != int64(len(ids)) {
		return ErrProjectNotFound
	}
	return nil
}
