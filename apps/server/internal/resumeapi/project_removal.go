package resumeapi

// Project deletion over linking resumes: every resume that links the
// project, live or not, is unlinked and saved at its next revision under the
// planned-set pattern in project_plan.go.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

// ProjectRemoval deletes one library project and unlinks it from every
// resume of its owner in one idempotent transaction.
type ProjectRemoval struct {
	// UserID is the owner of the project and the resumes.
	UserID uuid.UUID
	// ProjectID is the project every link to which is removed.
	ProjectID uuid.UUID
	// Operation is the hex operation digest keying the idempotency record.
	Operation string
	// Key is the request's Idempotency-Key.
	Key uuid.UUID
	// RequestHash is the request fingerprint stored with the record.
	RequestHash [32]byte
	// Authorize runs first in the owner-locked transaction: it rechecks the
	// caller and the project precondition.
	Authorize func(context.Context, *store.Queries) error
	// Remove runs last in the transaction, after every linking resume is
	// saved: it deletes the project and returns the response to store.
	Remove func(context.Context, *store.Queries, []ChangedResume) (resume.StoredResponse, error)
}

// RemoveProject runs a project deletion under the public-state fences of
// every resume that links the project, live or not.
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

// removeProjectTx requires the planned set at the planned revisions under the
// owner lock, then unlinks and saves each resume at its next revision.
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

// linkingFencePlan revokes a live resume, whose project page stops, and
// fences a draft without draining it.
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

// LinkedResumeIDs returns, for each project the owner's resumes link, the
// ids of those resumes in resume list order. q may be a pool or a
// transaction.
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
