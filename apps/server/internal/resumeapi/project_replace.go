package resumeapi

// Project replacement over linking resumes: when the stored slug or document
// changes, every live resume that links the project advances its revision,
// without a document change, under the planned-set pattern in
// project_plan.go. Drafts serve no public page, so they never advance here.

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

// ProjectReplacement replaces one library project in one idempotent
// transaction.
type ProjectReplacement struct {
	// UserID is the owner of the project and the resumes.
	UserID uuid.UUID
	// ProjectID is the replaced project.
	ProjectID uuid.UUID
	// Operation is the hex operation digest keying the idempotency record.
	Operation string
	// Key is the request's Idempotency-Key.
	Key uuid.UUID
	// RequestHash is the request fingerprint stored with the record.
	RequestHash [32]byte
	// Change compares the stored project with the replacement. It runs on
	// the pool to plan, and again in the transaction after Authorize to
	// recheck the plan.
	Change func(context.Context, *store.Queries) (project.Change, error)
	// Authorize runs first in the owner-locked transaction: it rechecks the
	// caller, the project precondition and, for a slug change, recent
	// reauthentication.
	Authorize func(context.Context, *store.Queries) error
	// Replace runs last in the transaction, after every linking live resume
	// advanced: it writes the project and returns the response to store.
	Replace func(context.Context, *store.Queries, []ChangedResume) (resume.StoredResponse, error)
}

type resumeRevisionAdvancer interface {
	AdvanceRevisionTx(context.Context, *store.Queries, uuid.UUID, uuid.UUID, int64) (resume.Resume, error)
}

// ReplaceProject runs a project replacement. A slug change revokes the
// public requests of each linking live resume, a document-only change fences
// them without draining, and an unchanged replacement advances no resume.
func (s *Service) ReplaceProject(ctx context.Context, replacement ProjectReplacement) (resume.ExecuteResult, error) {
	notAttempted := resume.ExecuteResult{Outcome: resume.CommitNotAttempted}
	reader, readable := s.resumes.(resumePoolReader)
	lister, listable := s.resumes.(resumeTxLister)
	advancer, advanceable := s.resumes.(resumeRevisionAdvancer)
	if !readable || !listable || !advanceable || s.idempotency == nil || s.recoveryPool == nil ||
		replacement.Change == nil || replacement.Authorize == nil || replacement.Replace == nil {
		return notAttempted, errors.New("resumeapi: project replacement dependencies are unavailable")
	}
	identity := mutationIdentity{UserID: replacement.UserID, Operation: replacement.Operation, Key: replacement.Key, RequestHash: replacement.RequestHash}
	plan := func(planCtx context.Context) (plannedSet, error) {
		change := s.preflightChange(planCtx, replacement)
		if change == project.Unchanged {
			return plannedSet{}, nil
		}
		owned, err := reader.List(planCtx, replacement.UserID)
		if err != nil {
			return plannedSet{}, err
		}
		return replacementSet(owned, replacement.ProjectID, change)
	}
	return s.runPlannedSet(ctx, identity, plan, func(planned plannedSet) plannedMutation {
		return func(runCtx context.Context, qtx *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
			return s.replaceProjectTx(runCtx, qtx, lister, advancer, replacement, planned)
		}
	})
}

// preflightChange compares the project on the pool to plan the fences. A
// preflight that cannot compare, such as for a stale revision, plans no
// change: the transaction then gives the authoritative answer, a replay or
// the error, and replans if its own comparison finds resumes to advance.
func (s *Service) preflightChange(ctx context.Context, replacement ProjectReplacement) project.Change {
	change, err := replacement.Change(ctx, store.New(s.recoveryPool))
	if err != nil {
		return project.Unchanged
	}
	return change
}

// replaceProjectTx recomputes the change and its set under the owner lock,
// requires the planned set with the planned fences, advances each resume's
// revision by one, and then writes the project.
func (s *Service) replaceProjectTx(ctx context.Context, qtx *store.Queries, lister resumeTxLister, advancer resumeRevisionAdvancer,
	replacement ProjectReplacement, planned plannedSet,
) (resume.StoredResponse, publicstate.CommittedState, error) {
	fail := func(err error) (resume.StoredResponse, publicstate.CommittedState, error) {
		return resume.StoredResponse{}, publicstate.CommittedState{}, err
	}
	if err := replacement.Authorize(ctx, qtx); err != nil {
		return fail(err)
	}
	change, err := replacement.Change(ctx, qtx)
	if err != nil {
		return fail(err)
	}
	current := plannedSet{}
	if change != project.Unchanged {
		owned, listErr := lister.ListTx(ctx, qtx, replacement.UserID)
		if listErr != nil {
			return fail(listErr)
		}
		if current, err = replacementSet(owned, replacement.ProjectID, change); err != nil {
			return fail(err)
		}
	}
	if !sameFence(current.fence, planned.fence) {
		return fail(errLinkingSetChanged)
	}
	changed := make([]ChangedResume, 0, len(current.resumes))
	state := publicstate.CommittedState{ResumeRevisions: make(map[uuid.UUID]int64, len(current.resumes))}
	for _, item := range current.resumes {
		advanced, advanceErr := advancer.AdvanceRevisionTx(ctx, qtx, replacement.UserID, item.ID, item.Revision)
		if advanceErr != nil {
			return fail(advanceErr)
		}
		if err = requireNextRevision(advanced, item.Revision); err != nil {
			return fail(err)
		}
		changed = append(changed, ChangedResume{ID: advanced.ID, Revision: advanced.Revision})
		state.ResumeRevisions[advanced.ID] = advanced.Revision
	}
	response, err := replacement.Replace(ctx, qtx, changed)
	if err != nil {
		return fail(err)
	}
	return response, state, nil
}

// replacementSet plans the live resumes that link projectID, in id order:
// a slug change revokes their public requests, and a document change fences
// them without draining.
func replacementSet(owned []resume.Resume, projectID uuid.UUID, change project.Change) (plannedSet, error) {
	linking, err := linkingResumes(owned, projectID)
	if err != nil {
		return plannedSet{}, err
	}
	class := publicstate.NonDraining
	if change == project.SlugChanged {
		class = publicstate.Revoking
	}
	set := plannedSet{}
	for _, item := range linking {
		if !item.Live {
			continue
		}
		set.resumes = append(set.resumes, item)
		set.fence.Resumes = append(set.fence.Resumes, publicstate.ResumeTarget{ID: item.ID, ExpectedRevision: item.Revision, Class: class})
	}
	return set, nil
}

// sameFence reports whether two plans fence the same resumes at the same
// revisions with the same class.
func sameFence(current, planned publicstate.Plan) bool {
	if current.DiscoveryGeneration != nil || planned.DiscoveryGeneration != nil || len(current.Resumes) != len(planned.Resumes) {
		return false
	}
	for i := range current.Resumes {
		if current.Resumes[i] != planned.Resumes[i] {
			return false
		}
	}
	return true
}
