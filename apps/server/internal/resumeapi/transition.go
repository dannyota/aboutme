package resumeapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const publicStateDrainTimeout = 5 * time.Second

var _ store.PublicMutationQueries = (*store.Queries)(nil)

type mutationIdentity struct {
	UserID      uuid.UUID
	Operation   string
	Key         uuid.UUID
	RequestHash [32]byte
}

type mutationPlan struct {
	Fence       publicstate.Plan
	Mutate      func(context.Context, *store.Queries) (resume.StoredResponse, publicstate.CommittedState, error)
	ReplayState func(context.Context, resume.StoredResponse) (publicstate.CommittedState, error)
	Recover     publicstate.RecoveryResolver
}

type recoveredResponseResolver interface {
	recoveredResponse() (resume.StoredResponse, bool)
}

// runMutation is the sole transition-to-idempotency bridge. Its pre-close
// recheck only decides replay/reuse; Execute remains the final transaction
// authority after every affected public generation is closed.
func (s *Service) runMutation(ctx context.Context, identity mutationIdentity, plan mutationPlan) (resume.ExecuteResult, error) {
	if s.coordinator == nil || s.idempotency == nil || plan.Mutate == nil {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: public transition dependencies are unavailable")
	}
	transition, err := s.coordinator.Begin(ctx, plan.Fence)
	if err != nil {
		var mismatch *publicstate.GenerationMismatchError
		if errors.As(err, &mismatch) {
			recheck, recheckErr := s.idempotency.Recheck(ctx, identity.UserID, identity.Operation, identity.Key, identity.RequestHash)
			if recheckErr != nil {
				return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, recheckErr
			}
			switch recheck.Decision {
			case resume.RecheckReplay:
				return resume.ExecuteResult{Response: recheck.Response, Replayed: true, Outcome: resume.CommitCommitted}, nil
			case resume.RecheckReuse:
				return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, resume.ErrIdempotencyKeyReuse
			case resume.RecheckFresh:
				// The stale plan remains a normal generation mismatch below.
			default:
				return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("resumeapi: invalid idempotency recheck decision")
			}
		}
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, err
	}
	rollback := func(cause error) (resume.ExecuteResult, error) {
		if rollbackErr := transition.Rollback(); rollbackErr != nil {
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, fmt.Errorf("resumeapi: rollback public transition: %w", rollbackErr)
		}
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, cause
	}

	recheck, err := s.idempotency.Recheck(ctx, identity.UserID, identity.Operation, identity.Key, identity.RequestHash)
	if err != nil {
		return rollback(err)
	}
	switch recheck.Decision {
	case resume.RecheckReplay:
		// Recheck runs while ownership is Begun. Exact replay owns no fresh
		// mutation, so it must release ownership without closing admission.
		if rollbackErr := transition.Rollback(); rollbackErr != nil {
			return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, rollbackErr
		}
		return resume.ExecuteResult{Response: recheck.Response, Replayed: true, Outcome: resume.CommitCommitted}, nil
	case resume.RecheckReuse:
		return rollback(resume.ErrIdempotencyKeyReuse)
	case resume.RecheckFresh:
	default:
		return rollback(errors.New("resumeapi: invalid idempotency recheck decision"))
	}

	now := time.Now()
	if s.clock != nil {
		now = s.clock()
	}
	if closeErr := transition.Close(ctx, now.Add(publicStateDrainTimeout)); closeErr != nil {
		return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, closeErr
	}
	var committed publicstate.CommittedState
	result, executeErr := s.idempotency.Execute(ctx, identity.UserID, identity.Operation, identity.Key, identity.RequestHash,
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			response, state, mutateErr := plan.Mutate(ctx, qtx)
			if mutateErr == nil {
				committed = state
			}
			return response, mutateErr
		},
	)
	switch result.Outcome {
	case resume.CommitCommitted:
		state := committed
		if result.Replayed {
			if plan.ReplayState == nil {
				return result, fmt.Errorf("resumeapi: replay state resolver is unavailable")
			}
			state, err = plan.ReplayState(ctx, result.Response)
			if err != nil {
				return result, err
			}
		}
		if err := transition.Commit(state); err != nil {
			return result, err
		}
	case resume.CommitNotAttempted, resume.CommitDefinitelyRolledBack:
		if err := transition.Rollback(); err != nil {
			return result, err
		}
	case resume.CommitUnknown:
		if err := transition.Recover(context.WithoutCancel(ctx), plan.Recover); err != nil {
			return result, err
		}
		if resolver, ok := plan.Recover.(recoveredResponseResolver); ok {
			if response, recovered := resolver.recoveredResponse(); recovered {
				result.Response = response
				result.Replayed = true
				result.Outcome = resume.CommitCommitted
				executeErr = nil
			}
		}
	default:
		return result, errors.New("resumeapi: invalid idempotency commit outcome")
	}
	return result, executeErr
}
