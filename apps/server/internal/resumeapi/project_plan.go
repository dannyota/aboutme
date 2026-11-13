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

const libraryPlanAttempts = 3

var (
	ErrPublicStateBusy = errors.New("resumeapi: public state is busy")

	errLinkingSetChanged = errors.New("resumeapi: linking resume set changed")
)

type ChangedResume struct {
	ID       uuid.UUID
	Revision int64
}

type resumeTxLister interface {
	ListTx(context.Context, *store.Queries, uuid.UUID) ([]resume.Resume, error)
}

type plannedSet struct {
	resumes []resume.Resume
	fence   publicstate.Plan
}

type plannedMutation func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error)

func (s *Service) runPlannedSet(ctx context.Context, identity mutationIdentity,
	plan func(context.Context) (plannedSet, error), mutate func(plannedSet) plannedMutation,
) (resume.ExecuteResult, error) {
	notAttempted := resume.ExecuteResult{Outcome: resume.CommitNotAttempted}
	for attempt := 0; attempt < libraryPlanAttempts; attempt++ {
		planned, err := plan(ctx)
		if err != nil {
			return notAttempted, err
		}
		run := mutate(planned)
		var result resume.ExecuteResult
		if len(planned.fence.Resumes) == 0 {
			result, err = s.idempotency.Execute(ctx, identity.UserID, identity.Operation, identity.Key, identity.RequestHash,
				func(qtx *store.Queries) (resume.StoredResponse, error) {
					response, _, mutateErr := run(ctx, qtx)
					return response, mutateErr
				})
		} else {
			fence := planned.fence
			result, err = s.runMutation(ctx, identity, mutationPlan{
				Fence:  fence,
				Mutate: run,
				ReplayState: func(replayCtx context.Context, _ resume.StoredResponse) (publicstate.CommittedState, error) {
					return s.replayCommittedState(replayCtx, identity.UserID, fence, false)
				},
				Recover: &mutationRecovery{pool: s.recoveryPool, identity: identity, plan: fence},
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
