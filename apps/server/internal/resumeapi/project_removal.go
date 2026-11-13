package resumeapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

type ProjectRemoval struct {
	UserID      uuid.UUID
	ProjectID   uuid.UUID
	Operation   string
	Key         uuid.UUID
	RequestHash [32]byte
	Authorize   func(context.Context, *store.Queries) error
	Remove      func(context.Context, *store.Queries, []ChangedResume) (resume.StoredResponse, error)
}

func (s *Service) RemoveProject(ctx context.Context, removal ProjectRemoval) (resume.ExecuteResult, error) {
	notAttempted := resume.ExecuteResult{Outcome: resume.CommitNotAttempted}
	reader, readable := s.resumes.(resumePoolReader)
	lister, listable := s.resumes.(resumeTxLister)
	if !readable || !listable || s.idempotency == nil || removal.Authorize == nil || removal.Remove == nil {
		return notAttempted, errors.New("resumeapi: project removal dependencies are unavailable")
	}
	identity := mutationIdentity{UserID: removal.UserID, Operation: removal.Operation, Key: removal.Key, RequestHash: removal.RequestHash}
	plan := func(planCtx context.Context) (plannedSet, error) {
		owned, err := reader.List(planCtx, removal.UserID)
		if err != nil {
			return plannedSet{}, err
		}
		planned, err := linkingResumes(owned, removal.ProjectID)
		if err != nil {
			return plannedSet{}, err
		}
		return plannedSet{resumes: planned, fence: linkingFencePlan(planned)}, nil
	}
	return s.runPlannedSet(ctx, identity, plan, func(planned plannedSet) plannedMutation {
		return func(runCtx context.Context, qtx *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
			return s.removeProjectTx(runCtx, qtx, lister, removal, planned.resumes)
		}
	})
}

func (s *Service) removeProjectTx(ctx context.Context, qtx *store.Queries, lister resumeTxLister, removal ProjectRemoval,
	planned []resume.Resume,
) (resume.StoredResponse, publicstate.CommittedState, error) {
	if err := removal.Authorize(ctx, qtx); err != nil {
		return resume.StoredResponse{}, publicstate.CommittedState{}, err
	}
	owned, err := lister.ListTx(ctx, qtx, removal.UserID)
	if err != nil {
		return resume.StoredResponse{}, publicstate.CommittedState{}, err
	}
	current, err := linkingResumes(owned, removal.ProjectID)
	if err != nil {
		return resume.StoredResponse{}, publicstate.CommittedState{}, err
	}
	if !sameLinkingSet(current, planned) {
		return resume.StoredResponse{}, publicstate.CommittedState{}, errLinkingSetChanged
	}
	changed := make([]ChangedResume, 0, len(current))
	state := publicstate.CommittedState{ResumeRevisions: make(map[uuid.UUID]int64, len(current))}
	for _, item := range current {
		doc := item.Doc
		resume.UnlinkProject(&doc, removal.ProjectID)
		if doc, err = validateDocumentForPersistence(doc); err != nil {
			return resume.StoredResponse{}, publicstate.CommittedState{}, err
		}
		saved, saveErr := s.resumes.SaveDocumentTx(ctx, qtx, removal.UserID, item.ID, doc, item.Revision)
		if saveErr != nil {
			return resume.StoredResponse{}, publicstate.CommittedState{}, saveErr
		}
		if err = requireNextRevision(saved, item.Revision); err != nil {
			return resume.StoredResponse{}, publicstate.CommittedState{}, err
		}
		changed = append(changed, ChangedResume{ID: saved.ID, Revision: saved.Revision})
		state.ResumeRevisions[saved.ID] = saved.Revision
	}
	response, err := removal.Remove(ctx, qtx, changed)
	if err != nil {
		return resume.StoredResponse{}, publicstate.CommittedState{}, err
	}
	return response, state, nil
}

func linkingFencePlan(planned []resume.Resume) publicstate.Plan {
	targets := make([]publicstate.ResumeTarget, len(planned))
	for i, item := range planned {
		class := publicstate.NonDraining
		if item.Live {
			class = publicstate.Revoking
		}
		targets[i] = publicstate.ResumeTarget{ID: item.ID, ExpectedRevision: item.Revision, Class: class}
	}
	return publicstate.Plan{Resumes: targets}
}

func (s *Service) LinkedResumeIDs(ctx context.Context, q *store.Queries, owner uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	lister, ok := s.resumes.(resumeTxLister)
	if !ok {
		return nil, errors.New("resumeapi: resume lister is unavailable")
	}
	owned, err := lister.ListTx(ctx, q, owner)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]uuid.UUID)
	for _, item := range owned {
		links, linkErr := resume.ProjectLinks(item.Doc)
		if linkErr != nil {
			return nil, fmt.Errorf("resumeapi: stored project links: %w", linkErr)
		}
		seen := make(map[uuid.UUID]struct{}, len(links))
		for _, project := range links {
			if _, duplicate := seen[project]; duplicate {
				continue
			}
			seen[project] = struct{}{}
			out[project] = append(out[project], item.ID)
		}
	}
	return out, nil
}
