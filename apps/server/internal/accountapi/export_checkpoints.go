package accountapi

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) exportCheckpoints(ctx context.Context, q *store.Queries, userID uuid.UUID, now time.Time) (map[uuid.UUID][]resume.CheckpointDetail, error) {
	rows, err := q.ListAccountResumeCheckpoints(ctx, store.ListAccountResumeCheckpointsParams{
		UserID: userID, Cutoff: now.Add(-resume.CheckpointRetention), AcceptedVersions: docmigrate.AcceptedVersions(),
	})
	if err != nil {
		return nil, fmt.Errorf("read resume checkpoints: %w", err)
	}
	out := make(map[uuid.UUID][]resume.CheckpointDetail)
	for _, row := range rows {
		state, projectErr := resume.ProjectCheckpoint(s.projector, row)
		if projectErr != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "account export checkpoint skipped", "class", "checkpoint_conversion_failed")
			}
			continue
		}
		out[row.ResumeID] = append(out[row.ResumeID], resume.CheckpointDetail{
			CheckpointSummary: resume.CheckpointSummaryForRow(row), Lng: projectExportLanguage(state.Lng),
			SchemaVersion: state.SchemaVersion, Document: state.Document,
		})
	}
	return out, nil
}
