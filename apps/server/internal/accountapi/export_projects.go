package accountapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

type exportProject struct {
	project.Project
	Evidence []project.Evidence `json:"evidence"`
}

func (s *Service) exportProjects(ctx context.Context, q *store.Queries, owner uuid.UUID) ([]exportProject, error) {
	rows, err := q.ListAccountExportProjects(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("read export projects: %w", err)
	}
	if len(rows) > project.MaxProjects {
		return nil, fmt.Errorf("account export project count exceeds limit")
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	evidence, err := project.NewStore().EvidenceByProjectTx(ctx, q, owner, ids)
	if err != nil {
		return nil, fmt.Errorf("read export evidence: %w", err)
	}
	projects := make([]exportProject, len(rows))
	for i, row := range rows {
		projects[i].Project, err = project.FromRow(row)
		if err != nil {
			return nil, err
		}
		projects[i].Evidence = evidence[row.ID]
	}
	return projects, nil
}
