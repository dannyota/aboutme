package resumeapi

// Project deletion over linking resumes: plan the affected set, close its
// fences in coordinator order, recheck the set under the owner lock, unlink
// and save each resume at its next revision, and replan at most three times
// (docs/design/projects-contract.md "Library mutations").

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

const projectRemovalAttempts = 3

var (
	// ErrPublicStateBusy reports a library mutation whose affected resumes
	// kept changing for every planning attempt; nothing was changed.
	ErrPublicStateBusy = errors.New("resumeapi: public state is busy")

	errLinkingSetChanged = errors.New("resumeapi: linking resume set changed")
)

// ChangedResume is one resume a library mutation advanced.
type ChangedResume struct {
	// ID identifies the owned resume.
	ID uuid.UUID
	// Revision is the resume revision the mutation committed.
	Revision int64
}

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

type resumeTxLister interface {
	ListTx(context.Context, *store.Queries, uuid.UUID) ([]resume.Resume, error)
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
	for attempt := 0; attempt < projectRemovalAttempts; attempt++ {
		owned, err := reader.List(ctx, removal.UserID)
		if err != nil {
			return notAttempted, err
		}
		planned, err := linkingResumes(owned, removal.ProjectID)
		if err != nil {
			return notAttempted, err
		}
		mutate := func(runCtx context.Context, qtx *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
			return s.removeProjectTx(runCtx, qtx, lister, removal, planned)
		}
		var result resume.ExecuteResult
		if len(planned) == 0 {
			result, err = s.idempotency.Execute(ctx, identity.UserID, identity.Operation, identity.Key, identity.RequestHash,
				func(qtx *store.Queries) (resume.StoredResponse, error) {
					response, _, mutateErr := mutate(ctx, qtx)
					return response, mutateErr
				})
		} else {
			plan := linkingFencePlan(planned)
			result, err = s.runMutation(ctx, identity, mutationPlan{
				Fence:  plan,
				Mutate: mutate,
				ReplayState: func(replayCtx context.Context, _ resume.StoredResponse) (publicstate.CommittedState, error) {
					return s.replayCommittedState(replayCtx, removal.UserID, plan, false)
				},
				Recover: &mutationRecovery{pool: s.recoveryPool, identity: identity, plan: plan},
			})
		}
		var mismatch *publicstate.GenerationMismatchError
		if errors.Is(err, errLinkingSetChanged) || errors.As(err, &mismatch) {
			continue
		}
		return result, err
	}
	return notAttempted, ErrPublicStateBusy
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

// linkingResumes returns the resumes that link project, sorted by id so
// every save runs in one row order.
func linkingResumes(owned []resume.Resume, project uuid.UUID) ([]resume.Resume, error) {
	var out []resume.Resume
	for _, item := range owned {
		links, err := resume.ProjectLinks(item.Doc)
		if err != nil {
			return nil, fmt.Errorf("resumeapi: stored project links: %w", err)
		}
		for _, linked := range links {
			if linked == project {
				out = append(out, item)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0 })
	return out, nil
}

func sameLinkingSet(current, planned []resume.Resume) bool {
	if len(current) != len(planned) {
		return false
	}
	for i := range current {
		if current[i].ID != planned[i].ID || current[i].Revision != planned[i].Revision {
			return false
		}
	}
	return true
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
