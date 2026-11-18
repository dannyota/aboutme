package resumeapi

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

type EvidenceDisconnect struct {
	UserID      uuid.UUID
	Operation   string
	Key         uuid.UUID
	RequestHash [32]byte
	Projects    func(context.Context, *store.Queries) ([]uuid.UUID, error)
	Authorize   func(context.Context, *store.Queries) error
	Apply       func(context.Context, *store.Queries, []ChangedResume) (resume.StoredResponse, error)
}

func (s *Service) DisconnectEvidence(ctx context.Context, d EvidenceDisconnect) (resume.ExecuteResult, error) {
	notAttempted := resume.ExecuteResult{Outcome: resume.CommitNotAttempted}
	reader, readable := s.resumes.(resumePoolReader)
	lister, listable := s.resumes.(resumeTxLister)
	advancer, advanceable := s.resumes.(resumeRevisionAdvancer)
	if !readable || !listable || !advanceable || s.idempotency == nil || s.recoveryPool == nil ||
		d.Projects == nil || d.Authorize == nil || d.Apply == nil {
		return notAttempted, errors.New("resumeapi: evidence disconnect dependencies are unavailable")
	}
	identity := mutationIdentity{UserID: d.UserID, Operation: d.Operation, Key: d.Key, RequestHash: d.RequestHash}
	plan := func(planCtx context.Context) (plannedSet, error) {
		projects, err := d.Projects(planCtx, store.New(s.recoveryPool))
		if err != nil || len(projects) == 0 {
			return plannedSet{}, err
		}
		owned, err := reader.List(planCtx, d.UserID)
		if err != nil {
			return plannedSet{}, err
		}
		return evidenceLinkingSet(owned, projects)
	}
	return s.runPlannedSet(ctx, identity, plan, func(planned plannedSet) plannedMutation {
		return func(runCtx context.Context, qtx *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
			return s.disconnectEvidenceTx(runCtx, qtx, lister, advancer, d, planned)
		}
	})
}

func (s *Service) disconnectEvidenceTx(ctx context.Context, qtx *store.Queries, lister resumeTxLister, advancer resumeRevisionAdvancer,
	d EvidenceDisconnect, planned plannedSet,
) (resume.StoredResponse, publicstate.CommittedState, error) {
	fail := func(err error) (resume.StoredResponse, publicstate.CommittedState, error) {
		return resume.StoredResponse{}, publicstate.CommittedState{}, err
	}
	if err := d.Authorize(ctx, qtx); err != nil {
		return fail(err)
	}
	projects, err := d.Projects(ctx, qtx)
	if err != nil {
		return fail(err)
	}
	current := plannedSet{}
	if len(projects) > 0 {
		owned, listErr := lister.ListTx(ctx, qtx, d.UserID)
		if listErr != nil {
			return fail(listErr)
		}
		if current, err = evidenceLinkingSet(owned, projects); err != nil {
			return fail(err)
		}
	}
	if !sameFence(current.fence, planned.fence) {
		return fail(errLinkingSetChanged)
	}
	changed := make([]ChangedResume, 0, len(current.resumes))
	state := publicstate.CommittedState{ResumeRevisions: make(map[uuid.UUID]int64, len(current.resumes))}
	for _, item := range current.resumes {
		advanced, advanceErr := advancer.AdvanceRevisionTx(ctx, qtx, d.UserID, item.ID, item.Revision)
		if advanceErr != nil {
			return fail(advanceErr)
		}
		if err = requireNextRevision(advanced, item.Revision); err != nil {
			return fail(err)
		}
		changed = append(changed, ChangedResume{ID: advanced.ID, Revision: advanced.Revision})
		state.ResumeRevisions[advanced.ID] = advanced.Revision
	}
	response, err := d.Apply(ctx, qtx, changed)
	if err != nil {
		return fail(err)
	}
	return response, state, nil
}

func evidenceLinkingSet(owned []resume.Resume, projects []uuid.UUID) (plannedSet, error) {
	withEvidence := make(map[uuid.UUID]bool, len(projects))
	for _, id := range projects {
		withEvidence[id] = true
	}
	set := plannedSet{}
	for _, item := range owned {
		if !item.Live {
			continue
		}
		links, err := resume.ProjectLinks(item.Doc)
		if err != nil {
			return plannedSet{}, fmt.Errorf("resumeapi: stored project links: %w", err)
		}
		for _, linked := range links {
			if withEvidence[linked] {
				set.resumes = append(set.resumes, item)
				break
			}
		}
	}
	sort.Slice(set.resumes, func(i, j int) bool { return bytes.Compare(set.resumes[i].ID[:], set.resumes[j].ID[:]) < 0 })
	for _, item := range set.resumes {
		set.fence.Resumes = append(set.fence.Resumes, publicstate.ResumeTarget{ID: item.ID, ExpectedRevision: item.Revision, Class: publicstate.NonDraining})
	}
	return set, nil
}
