package resumeapi

// Public-state transitions: executeTransition and its preflight, idempotency
// recheck, transaction lock order, committed-state report, recovery, and
// single retry after a discovery-generation mismatch.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// executeTransition runs spec.Run under the public-state fence. Inside the
// transaction it locks the transition slugs, then public state for a global
// transition, then rereads the resume and rechecks the caller before Run.
func (s *Service) executeTransition(ctx context.Context, mutation mutationContext, prepared preparedInput,
	spec mutationSpec,
) (resume.ExecuteResult, error) {
	if s.coordinator == nil || s.recoveryPool == nil {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: public transition dependencies are unavailable")
	}
	reader, ok := s.resumes.(resumePoolReader)
	if !ok || mutation.ExpectedRevision == nil {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, internalClientError()
	}
	runCtx := ctx
	cancelRun := func() {}
	if !prepared.ExecuteBefore.IsZero() {
		now := time.Now()
		if s.clock != nil {
			now = s.clock()
		}
		remaining := prepared.ExecuteBefore.Sub(now)
		if remaining <= 0 {
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, context.DeadlineExceeded
		}
		runCtx, cancelRun = context.WithTimeout(ctx, remaining)
	}
	defer cancelRun()
	identity := mutationIdentity{UserID: mutation.UserID, Operation: mutation.Operation, Key: mutationKeyFromContext(ctx), RequestHash: mutation.RequestHash}
	if identity.Key == uuid.Nil {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: transition mutation identity is missing idempotency key")
	}
	recheckPreflight := func() (resume.ExecuteResult, bool, error) {
		recheck, recheckErr := s.idempotency.Recheck(runCtx, identity.UserID, identity.Operation, identity.Key, identity.RequestHash)
		if recheckErr != nil {
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, true, recheckErr
		}
		switch recheck.Decision {
		case resume.RecheckReplay:
			return resume.ExecuteResult{Response: recheck.Response, Replayed: true, Outcome: resume.CommitCommitted}, true, nil
		case resume.RecheckReuse:
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, true, resume.ErrIdempotencyKeyReuse
		case resume.RecheckFresh:
			return resume.ExecuteResult{}, false, nil
		default:
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, true, errors.New("resumeapi: invalid idempotency recheck decision")
		}
	}
	current, err := reader.Get(runCtx, mutation.UserID, resumeIDFromPrepared(prepared))
	if err != nil {
		if errors.Is(err, resume.ErrNotFound) {
			if result, decided, recheckErr := recheckPreflight(); decided {
				return result, recheckErr
			}
		}
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, err
	}
	if current.Revision != *mutation.ExpectedRevision {
		if result, decided, recheckErr := recheckPreflight(); decided {
			return result, recheckErr
		}
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, &resume.RevisionMismatchError{CurrentRevision: current.Revision, Current: current}
	}
	descriptor, err := spec.Transition(runCtx, current, prepared)
	if err != nil {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, err
	}
	if descriptor.ResumeID != current.ID {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: transition target does not match preflight row")
	}
	if descriptor.Publish != nil {
		input, ok := prepared.Value.(publishMutationPrepared)
		if !ok {
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: publish transition has the wrong prepared input")
		}
		input.ReleasedAt = descriptor.Publish.ReleasedAt
		prepared.Value = input
	}
	var deleteProof *deleteRecoveryProof
	if descriptor.Retire {
		deleteProof = &deleteRecoveryProof{ResumeID: current.ID}
		if photo := current.Doc.PersonalDetails.Photo; photo != nil {
			deleteProof.PhotoKey = photo.Key
		}
		if current.Slug != nil {
			releasedAt := time.Now()
			if s.clock != nil {
				releasedAt = s.clock()
			}
			releasedAt = normalizePostgresTimestamp(releasedAt)
			input, ok := prepared.Value.(deletePreparedInput)
			if !ok {
				return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: delete transition has the wrong prepared input")
			}
			input.ReleasedAt = releasedAt
			prepared.Value = input
			slug := *current.Slug
			deleteProof.Slug, deleteProof.ReleasedAt = &slug, releasedAt
		}
	}
	plan := publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: current.ID, ExpectedRevision: current.Revision, Class: descriptor.Class,
	}}}
	if descriptor.Global {
		state, stateErr := store.New(s.recoveryPool).GetPublicState(runCtx)
		if stateErr != nil {
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, stateErr
		}
		plan.DiscoveryGeneration = &state.DiscoveryGeneration
	}
	runPlan := func(currentPlan publicstate.Plan) (resume.ExecuteResult, error) {
		return s.runMutation(runCtx, identity, mutationPlan{
			Fence: currentPlan,
			Mutate: func(runCtx context.Context, qtx *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error) {
				transactionMutation := mutation
				if err := lockTransitionSlugs(runCtx, qtx, descriptor.Slugs); err != nil {
					return resume.StoredResponse{}, publicstate.CommittedState{}, err
				}
				s.recordTransactionOrder("slug")
				if currentPlan.DiscoveryGeneration != nil {
					if _, lockErr := qtx.LockPublicState(runCtx); lockErr != nil {
						return resume.StoredResponse{}, publicstate.CommittedState{}, lockErr
					}
					public, publicErr := qtx.GetPublicState(runCtx)
					if publicErr != nil {
						return resume.StoredResponse{}, publicstate.CommittedState{}, publicErr
					}
					if public.DiscoveryGeneration != *currentPlan.DiscoveryGeneration {
						return resume.StoredResponse{}, publicstate.CommittedState{}, &publicstate.GenerationMismatchError{Expected: *currentPlan.DiscoveryGeneration, Actual: public.DiscoveryGeneration}
					}
					transactionMutation.ExpectedDiscoveryGeneration = currentPlan.DiscoveryGeneration
					s.recordTransactionOrder("public_state")
				}
				transactionCurrent, currentErr := s.resumes.GetTx(runCtx, qtx, mutation.UserID, current.ID)
				if currentErr != nil {
					return resume.StoredResponse{}, publicstate.CommittedState{}, currentErr
				}
				if transactionCurrent.Revision != current.Revision {
					return resume.StoredResponse{}, publicstate.CommittedState{}, &resume.RevisionMismatchError{CurrentRevision: transactionCurrent.Revision, Current: transactionCurrent}
				}
				transactionMutation.CurrentResume = &transactionCurrent
				s.recordTransactionOrder("resume")
				transactionMutation, sessionErr := s.transactionMutation(runCtx, qtx, transactionMutation)
				if sessionErr != nil {
					return resume.StoredResponse{}, publicstate.CommittedState{}, sessionErr
				}
				runResult, runErr := spec.Run.Run(runCtx, qtx, transactionMutation, prepared)
				if runErr != nil {
					return resume.StoredResponse{}, publicstate.CommittedState{}, runErr
				}
				if deleteProof != nil && deleteProof.Slug != nil {
					tombstone, tombstoneErr := qtx.GetSlugTombstoneForUpdate(runCtx, *deleteProof.Slug)
					if tombstoneErr != nil {
						return resume.StoredResponse{}, publicstate.CommittedState{}, tombstoneErr
					}
					deleteProof.ReleasedAt = tombstone.ReleasedAt
				}
				state := publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{current.ID: current.Revision + 1}}
				if descriptor.Retire {
					state.ResumeRevisions = nil
					state.RetiredResumes = []uuid.UUID{current.ID}
				}
				if currentPlan.DiscoveryGeneration != nil {
					durable, readErr := qtx.GetPublicState(runCtx)
					if readErr != nil {
						return resume.StoredResponse{}, publicstate.CommittedState{}, readErr
					}
					state.DiscoveryGeneration = &durable.DiscoveryGeneration
				}
				return runResult.Response, state, nil
			},
			ReplayState: func(replayCtx context.Context, _ resume.StoredResponse) (publicstate.CommittedState, error) {
				return s.replayCommittedState(replayCtx, mutation.UserID, currentPlan, descriptor.Retire)
			},
			Recover: &mutationRecovery{pool: s.recoveryPool, identity: identity, plan: currentPlan, retire: descriptor.Retire, delete: deleteProof, publish: descriptor.Publish},
		})
	}
	result, transitionErr := runPlan(plan)
	var mismatch *publicstate.GenerationMismatchError
	if errors.As(transitionErr, &mismatch) {
		// A contender may have read the old owner generation before the winner
		// committed. First distinguish that real content race from an unrelated
		// discovery-generation advance.
		fresh, readErr := reader.Get(runCtx, mutation.UserID, current.ID)
		if readErr != nil {
			return result, readErr
		}
		if fresh.Revision != current.Revision {
			return result, &resume.RevisionMismatchError{CurrentRevision: fresh.Revision, Current: fresh}
		}
		if plan.DiscoveryGeneration == nil {
			// A non-global fence mismatch means another owner completed between
			// preflight and Begin. Even when a test seam observes the same durable
			// revision, it is a stale optimistic-concurrency result, never 500.
			return result, &resume.RevisionMismatchError{CurrentRevision: fresh.Revision, Current: fresh}
		}
		public, readErr := store.New(s.recoveryPool).GetPublicState(runCtx)
		if readErr != nil {
			return result, readErr
		}
		plan.DiscoveryGeneration = &public.DiscoveryGeneration
		result, transitionErr = runPlan(plan)
		if errors.As(transitionErr, &mismatch) {
			return result, &clientError{Status: http.StatusServiceUnavailable, Code: "public_state_busy", Message: "public state is busy", Headers: map[string]string{"Retry-After": "1"}}
		}
	}
	return result, transitionErr
}
