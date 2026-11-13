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
	ErrProjectNotFound      = errors.New("resume: linked project not found")
	ErrProjectAlreadyLinked = errors.New("resume: project already linked")
)

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
