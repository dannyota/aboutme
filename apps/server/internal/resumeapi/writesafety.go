package resumeapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type mutationHeaders struct {
	Key              uuid.UUID
	ExpectedRevision *int64
	WireVersion      int32
}

type boundedInput struct {
	Payload []byte
	Value   any
}

type idempotencyInspection struct {
	Operation   string
	RequestHash [32]byte
	Response    resume.StoredResponse
	Replayed    bool
}

type preparedInput struct {
	Input         boundedInput
	Value         any
	ExecuteBefore time.Time
}

type mutationContext struct {
	UserID                      uuid.UUID
	SessionID                   uuid.UUID
	Session                     store.Session
	Agent                       *AgentPrincipal
	ExpectedDiscoveryGeneration *int64
	ExpectedRevision            *int64
	WireVersion                 int32
	Operation                   string
	RequestHash                 [32]byte
	CurrentResume               *resume.Resume
}

type mutationRunResult struct {
	Response resume.StoredResponse
}

type mutationOperation interface {
	Run(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error)
}

type mutationOperationFunc func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error)

// Run adapts a function to mutationOperation.
func (f mutationOperationFunc) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	return f(ctx, qtx, mutation, prepared)
}

type mutationSpec struct {
	RegisteredOperation string
	RequireMatch        bool
	Decode              func(*http.Request) (boundedInput, error)
	CanonicalTargets    func(boundedInput) ([]string, error)
	SemanticInputs      func(boundedInput) ([]string, error)
	Prepare             func(context.Context, boundedInput, idempotencyInspection) (preparedInput, error)
	Run                 mutationOperation
	Finalize            func(context.Context, preparedInput, resume.ExecuteResult, error)
	Transition          func(context.Context, resume.Resume, preparedInput) (mutationTransition, error)
}

type mutationTransition struct {
	ResumeID uuid.UUID
	Class    publicstate.TransitionClass
	Global   bool
	Retire   bool
	// Slugs is the complete old/new claim set resolved during preflight. The
	// callback locks this exact set before public state or any row operation.
	Slugs   []string
	Publish *publishRecoveryProof
}

type boundMutation struct {
	service   *Service
	ctx       context.Context
	operation mutationOperation
	mutation  mutationContext
	prepared  preparedInput
}

func (b boundMutation) run(qtx *store.Queries) (resume.StoredResponse, error) {
	mutation, err := b.service.transactionMutation(b.ctx, qtx, b.mutation)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	runResult, err := b.operation.Run(b.ctx, qtx, mutation, b.prepared)
	return runResult.Response, err
}

// executeMutation applies the shared mutation order. Once preparation
// succeeds, Finalize runs for every Execute outcome, including errors.
func (s *Service) executeMutation(w http.ResponseWriter, r *http.Request, spec mutationSpec) {
	headers, headerErr := parseMutationHeaders(r, spec.RequireMatch, s.acceptedVersions)
	if headerErr != nil {
		writeResumeError(w, headerErr)
		return
	}
	if spec.Decode == nil || spec.CanonicalTargets == nil || spec.Run == nil {
		writeResumeError(w, &clientError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "an internal error occurred"})
		return
	}
	input, err := spec.Decode(r)
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	targets, err := spec.CanonicalTargets(input)
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	if len(targets)%2 != 0 {
		writeResumeError(w, internalClientError())
		return
	}
	semanticInputs := []string(nil)
	if spec.SemanticInputs != nil {
		semanticInputs, err = spec.SemanticInputs(input)
		if err != nil {
			writeResumeError(w, mapMutationError(err))
			return
		}
		if len(semanticInputs)%2 != 0 {
			writeResumeError(w, internalClientError())
			return
		}
	}
	operationDigest := operationHash(r.Method, spec.RegisteredOperation, targets)
	operation := hexDigest(operationDigest)
	precondition := "absent"
	if headers.ExpectedRevision != nil {
		precondition = strconv.FormatInt(*headers.ExpectedRevision, 10)
	}
	fingerprint := requestHash(headers.WireVersion, precondition, semanticInputs, input.Payload)
	sess, sessionOK := auth.SessionFromContext(r.Context())
	agent, agentOK := agentPrincipalFromContext(r.Context())
	if !sessionOK && !agentOK {
		writeResumeError(w, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"})
		return
	}
	userID := sess.UserID
	if agentOK {
		userID = agent.userID
	}
	if s.idempotency == nil {
		writeResumeError(w, &clientError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "an internal error occurred"})
		return
	}
	stored, replayed, inspectErr := s.idempotency.Inspect(r.Context(), userID, operation, headers.Key, fingerprint)
	inspection := idempotencyInspection{Operation: operation, RequestHash: fingerprint, Response: stored, Replayed: replayed}
	if inspectErr != nil {
		writeResumeError(w, mapMutationError(inspectErr))
		return
	}
	if replayed {
		s.writeMutationResponse(w, stored)
		return
	}
	prepared := preparedInput{Input: input, Value: input.Value}
	if spec.Prepare != nil {
		prepared, err = spec.Prepare(r.Context(), input, inspection)
		if err != nil {
			writeResumeError(w, mapMutationError(err))
			return
		}
	}
	mutation := mutationContext{
		UserID: userID, SessionID: sess.ID, Session: sess, ExpectedRevision: headers.ExpectedRevision,
		WireVersion: headers.WireVersion, Operation: operation, RequestHash: fingerprint,
	}
	if agentOK {
		mutation.Agent = &agent
	}
	if spec.Transition != nil {
		transitionContext := context.WithValue(r.Context(), mutationRequestKeyContext{}, headers.Key)
		result, transitionErr := s.executeTransition(transitionContext, mutation, prepared, spec)
		if spec.Finalize != nil {
			spec.Finalize(r.Context(), prepared, result, transitionErr)
		}
		if transitionErr != nil {
			writeResumeError(w, s.mapMutationErrorAtWire(transitionErr, headers.WireVersion))
			return
		}
		s.writeMutationResponse(w, result.Response)
		return
	}
	executeContext := r.Context()
	cancelExecute := func() {}
	if !prepared.ExecuteBefore.IsZero() {
		now := time.Now()
		if s.clock != nil {
			now = s.clock()
		}
		remaining := prepared.ExecuteBefore.Sub(now)
		if remaining <= 0 {
			deadlineErr := context.DeadlineExceeded
			if spec.Finalize != nil {
				spec.Finalize(r.Context(), prepared, resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, deadlineErr)
			}
			writeResumeError(w, s.mapMutationErrorAtWire(deadlineErr, headers.WireVersion))
			return
		}
		executeContext, cancelExecute = context.WithTimeout(r.Context(), remaining)
	}
	defer cancelExecute()
	callback := boundMutation{service: s, ctx: executeContext, operation: spec.Run, mutation: mutation, prepared: prepared}
	result, executeErr := s.idempotency.Execute(
		executeContext, userID, operation, headers.Key, fingerprint, callback.run,
	)
	if spec.Finalize != nil {
		spec.Finalize(r.Context(), prepared, result, executeErr)
	}
	if executeErr != nil {
		writeResumeError(w, s.mapMutationErrorAtWire(executeErr, headers.WireVersion))
		return
	}
	s.writeMutationResponse(w, result.Response)
}

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
