package accountapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) exportProjects(ctx context.Context, q *store.Queries, owner uuid.UUID) ([]project.Project, error) {
	rows, err := q.ListAccountExportProjects(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("read export projects: %w", err)
	}
	if len(rows) > project.MaxProjects {
		return nil, fmt.Errorf("account export project count exceeds limit")
	}
	projects := make([]project.Project, len(rows))
	for i, row := range rows {
		projects[i], err = project.FromRow(row)
		if err != nil {
			return nil, err
		}
	}
	return projects, nil
}
