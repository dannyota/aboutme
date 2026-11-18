package resumeapi

import (
	"context"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

type ProjectEvidenceEdit struct {
	UserID      uuid.UUID
	ProjectID   uuid.UUID
	Operation   string
	Key         uuid.UUID
	RequestHash [32]byte
	Change      func(context.Context, *store.Queries) (project.Change, error)
	Authorize   func(context.Context, *store.Queries) error
	Apply       func(context.Context, *store.Queries, []ChangedResume) (resume.StoredResponse, error)
}

func (s *Service) EditProjectEvidence(ctx context.Context, edit ProjectEvidenceEdit) (resume.ExecuteResult, error) {
	change := edit.Change
	if change == nil {
		change = func(context.Context, *store.Queries) (project.Change, error) { return project.DocumentChanged, nil }
	}
	return s.ReplaceProject(ctx, ProjectReplacement{
		UserID: edit.UserID, ProjectID: edit.ProjectID, Operation: edit.Operation, Key: edit.Key, RequestHash: edit.RequestHash,
		Change: change, Authorize: edit.Authorize, Replace: edit.Apply,
	})
}
