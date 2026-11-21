package resume

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

type CheckpointActor struct {
	Kind string  `json:"kind"`
	Name *string `json:"name,omitempty"`
}

type CheckpointSummary struct {
	ID        uuid.UUID       `json:"id"`
	StartedAt time.Time       `json:"startedAt"`
	Cause     string          `json:"cause"`
	Actor     CheckpointActor `json:"actor"`
	Title     string          `json:"title"`
	ByteSize  int32           `json:"byteSize"`
}

type CheckpointDetail struct {
	CheckpointSummary
	Lng           string             `json:"lng"`
	SchemaVersion int32              `json:"schemaVersion"`
	Document      CheckpointDocument `json:"document"`
}

func checkpointActor(name *string) CheckpointActor {
	if name != nil {
		return CheckpointActor{Kind: "assistant", Name: name}
	}
	return CheckpointActor{Kind: "owner"}
}

func CheckpointSummaryForRow(row store.ResumeCheckpoint) CheckpointSummary {
	return CheckpointSummary{ID: row.ID, StartedAt: row.StartedAt, Cause: row.Cause,
		Actor: checkpointActor(row.ActorName), Title: row.Title, ByteSize: row.ByteSize}
}

func (s *Store) ListCheckpoints(ctx context.Context, arg store.ListResumeCheckpointsParams) ([]CheckpointSummary, error) {
	if err := RequireCheckpointResumeTx(ctx, s.q, arg.UserID, arg.ResumeID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListResumeCheckpoints(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("resume: list checkpoints: %w", err)
	}
	out := make([]CheckpointSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, CheckpointSummary{ID: row.ID, StartedAt: row.StartedAt, Cause: row.Cause,
			Actor: checkpointActor(row.ActorName), Title: row.Title, ByteSize: row.ByteSize})
	}
	return out, nil
}

func (s *Store) GetCheckpoint(ctx context.Context, arg store.GetResumeCheckpointParams) (CheckpointDetail, error) {
	row, err := ReadCheckpointTx(ctx, s.q, arg)
	if err != nil {
		return CheckpointDetail{}, err
	}
	state, err := ProjectCheckpoint(s.proj, row)
	if err != nil {
		return CheckpointDetail{}, err
	}
	return CheckpointDetail{CheckpointSummary: CheckpointSummaryForRow(row), Lng: ProjectLanguage(state.Lng),
		SchemaVersion: state.SchemaVersion, Document: state.Document}, nil
}

func ReadCheckpointTx(ctx context.Context, q *store.Queries, arg store.GetResumeCheckpointParams) (store.ResumeCheckpoint, error) {
	row, err := q.GetResumeCheckpoint(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ResumeCheckpoint{}, ErrNotFound
	}
	if err != nil {
		return store.ResumeCheckpoint{}, fmt.Errorf("resume: get checkpoint: %w", err)
	}
	return row, nil
}

func RequireCheckpointResumeTx(ctx context.Context, q *store.Queries, userID, resumeID uuid.UUID) error {
	ok, err := q.ResumeCheckpointOwnerExists(ctx, store.ResumeCheckpointOwnerExistsParams{ResumeID: resumeID, UserID: userID})
	if err != nil {
		return fmt.Errorf("resume: check history ownership: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}
