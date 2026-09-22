package resumeapi

// Mutation-transaction session/token re-authentication, and the transition
// helpers that decide a mutation's public-state class and lock its slugs.

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// transactionMutation reauthenticates the transaction against the durable
// session row immediately before a mutation callback can write.
func (s *Service) transactionMutation(ctx context.Context, qtx *store.Queries, mutation mutationContext) (mutationContext, error) {
	if qtx == nil {
		return mutationContext{}, errors.New("resumeapi: mutation transaction is unavailable")
	}
	if mutation.Agent != nil {
		authority, err := qtx.GetOAuthTokenAuthorityByDigest(ctx, mutation.Agent.tokenDigest[:])
		s.recordTransactionOrder("token")
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return mutationContext{}, ErrAgentAccessUnavailable
			}
			return mutationContext{}, fmt.Errorf("resumeapi: read mutation token authority: %w", err)
		}
		now := time.Now()
		if s.clock != nil {
			now = s.clock()
		}
		token, grant := authority.OAuthToken, authority.OAuthGrant
		scopes, scopeErr := oauthsrv.ParseScopes(grant.Scopes)
		if scopeErr != nil || !scopes.Has(oauthsrv.ScopeResumesWrite) ||
			subtle.ConstantTimeCompare(token.TokenDigest, mutation.Agent.tokenDigest[:]) != 1 ||
			token.ID != mutation.Agent.tokenID || token.UserID != mutation.Agent.userID || token.GrantID != mutation.Agent.grantID ||
			token.Kind != string(oauthsrv.TokenKindAccess) || token.UserID != authority.User.ID || token.UserID != grant.UserID ||
			token.ClientID != grant.ClientID || token.GrantID != grant.ID || token.RevokedAt != nil || token.SupersededAt != nil ||
			grant.RevokedAt != nil || !token.ExpiresAt.After(now) || !token.FamilyExpiresAt.After(now) {
			return mutationContext{}, ErrAgentAccessUnavailable
		}
		return mutation, nil
	}
	session, err := qtx.GetSessionByID(ctx, mutation.SessionID)
	s.recordTransactionOrder("session")
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return mutationContext{}, auth.ErrSessionInvalid
		}
		return mutationContext{}, fmt.Errorf("resumeapi: read mutation session: %w", err)
	}
	if session.ID != mutation.SessionID || session.UserID != mutation.UserID {
		return mutationContext{}, auth.ErrSessionInvalid
	}
	now := time.Now()
	if s.clock != nil {
		now = s.clock()
	}
	if err := auth.RequireLiveSession(session, now); err != nil {
		return mutationContext{}, err
	}
	mutation.Session = session
	return mutation, nil
}

// requireSensitiveResumeAuthority locks the user, then the concrete caller
// session, then the second-factor policy -- the lock order in
// docs/design/second-factor-authentication.md -- and requires a fresh primary
// and, for an enrolled account, factor proof before a slug-releasing resume
// mutation (a rename or a delete) commits. Only a cookie-session mutation
// reaches this check; an agent write authenticates through its OAuth grant
// and token in transactionMutation instead.
func (s *Service) requireSensitiveResumeAuthority(ctx context.Context, qtx *store.Queries, mutation mutationContext, now time.Time) error {
	user, err := qtx.GetUserForUpdate(ctx, mutation.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrSessionInvalid
		}
		return fmt.Errorf("resumeapi: lock sensitive mutation user: %w", err)
	}
	s.recordTransactionOrder("user")
	sess, err := qtx.GetSessionByIDForUpdate(ctx, mutation.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.ErrSessionInvalid
		}
		return fmt.Errorf("resumeapi: lock sensitive mutation session: %w", err)
	}
	s.recordTransactionOrder("session")
	policy, err := qtx.GetSecondFactorPolicyForUpdate(ctx, user.ID)
	var policyPtr *store.SecondFactorPolicy
	switch {
	case err == nil:
		policyPtr = &policy
	case errors.Is(err, pgx.ErrNoRows):
		// Unenrolled account: RequireRecentSecondFactorReauth checks primary
		// proof only.
	default:
		return fmt.Errorf("resumeapi: lock sensitive mutation factor policy: %w", err)
	}
	s.recordTransactionOrder("policy")
	return auth.RequireRecentSecondFactorReauth(user, policyPtr, sess, now)
}

func (s *Service) recordTransactionOrder(step string) {
	if s.transactionOrderHook != nil {
		s.transactionOrderHook(step)
	}
}

func (s *Service) recordPublishPreflightOrder(step string) {
	if s.publishPreflightOrderHook != nil {
		s.publishPreflightOrderHook(step)
	}
}

func (s *Service) currentMutationResume(ctx context.Context, qtx *store.Queries, mutation mutationContext, resumeID uuid.UUID) (resume.Resume, error) {
	if mutation.CurrentResume != nil {
		if mutation.CurrentResume.ID != resumeID || mutation.CurrentResume.UserID != mutation.UserID {
			return resume.Resume{}, errors.New("resumeapi: transaction resume target does not match mutation")
		}
		return *mutation.CurrentResume, nil
	}
	return s.resumes.GetTx(ctx, qtx, mutation.UserID, resumeID)
}

func (s *Service) replayCommittedState(ctx context.Context, userID uuid.UUID, plan publicstate.Plan, retire bool) (publicstate.CommittedState, error) {
	if s.recoveryPool == nil {
		return publicstate.CommittedState{}, errors.New("resumeapi: replay state recovery pool is unavailable")
	}
	q := store.New(s.recoveryPool)
	state := publicstate.CommittedState{ResumeRevisions: make(map[uuid.UUID]int64)}
	for _, target := range plan.Resumes {
		row, err := q.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: target.ID, UserID: userID})
		if retire && errors.Is(err, pgx.ErrNoRows) {
			state.RetiredResumes = append(state.RetiredResumes, target.ID)
			continue
		}
		if err != nil {
			return publicstate.CommittedState{}, fmt.Errorf("resumeapi: replay committed resume state: %w", err)
		}
		state.ResumeRevisions[target.ID] = row.Revision
	}
	if plan.DiscoveryGeneration != nil {
		public, err := q.GetPublicState(ctx)
		if err != nil {
			return publicstate.CommittedState{}, fmt.Errorf("resumeapi: replay committed discovery state: %w", err)
		}
		state.DiscoveryGeneration = &public.DiscoveryGeneration
	}
	return state, nil
}

// mutationRequestKeys carries the already parsed idempotency key through the
// request-local context without changing every P2B operation signature.
type mutationRequestKeyContext struct{}

func mutationKeyFromContext(ctx context.Context) uuid.UUID {
	key, ok := ctx.Value(mutationRequestKeyContext{}).(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return key
}

func resumeIDFromPrepared(prepared preparedInput) uuid.UUID {
	switch input := prepared.Value.(type) {
	case aggregatePreparedInput:
		return input.ResumeID
	case resumeMetadataPrepared:
		return input.ResumeID
	case personalDetailsPrepared:
		return input.ResumeID
	case deletePreparedInput:
		return input.ResumeID
	case publishMutationPrepared:
		return input.ResumeID
	case photoUploadInput:
		return input.ResumeID
	case photoCropInput:
		return input.ResumeID
	case photoDeleteInput:
		return input.ResumeID
	default:
		return uuid.Nil
	}
}

func (s *Service) nonDrainingTransition(_ context.Context, current resume.Resume, prepared preparedInput) (mutationTransition, error) {
	if id := resumeIDFromPrepared(prepared); id == uuid.Nil || id != current.ID {
		return mutationTransition{}, errors.New("resumeapi: revision mutation has no resume target")
	}
	return mutationTransition{ResumeID: current.ID, Class: publicstate.NonDraining}, nil
}

func (s *Service) deleteTransition(ctx context.Context, current resume.Resume, prepared preparedInput) (mutationTransition, error) {
	if id := resumeIDFromPrepared(prepared); id == uuid.Nil || id != current.ID {
		return mutationTransition{}, errors.New("resumeapi: delete mutation has no resume target")
	}
	if current.Slug == nil {
		return mutationTransition{ResumeID: current.ID, Class: publicstate.NonDraining, Retire: true}, nil
	}
	if _, agentOK := agentPrincipalFromContext(ctx); !agentOK {
		session, ok := auth.SessionFromContext(ctx)
		if !ok {
			return mutationTransition{}, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"}
		}
		if err := auth.RequireRecentReauth(session, s.clock()); err != nil {
			return mutationTransition{}, err
		}
	}
	return mutationTransition{ResumeID: current.ID, Class: publicstate.Revoking, Global: true, Retire: true, Slugs: []string{*current.Slug}}, nil
}

func lockTransitionSlugs(ctx context.Context, qtx *store.Queries, slugs []string) error {
	for _, slug := range slugs {
		if err := qtx.LockSlugClaim(ctx, slug); err != nil {
			return err
		}
	}
	return nil
}

func normalizePostgresTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func (s *Service) writeMutationResponse(w http.ResponseWriter, response resume.StoredResponse) {
	if s.writeResponse == nil {
		writeStoredResponse(w, response)
		return
	}
	s.writeResponse(w, response)
}
