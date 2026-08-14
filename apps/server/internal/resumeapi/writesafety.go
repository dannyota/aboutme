package resumeapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	idempotencyOperationDomain = "aboutme.idempotency.operation.v1"
	idempotencyRequestDomain   = "aboutme.idempotency.request.v1"
	maxJSONBodyBytes           = 256 * 1024
	maxJSONDepth               = 100
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
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeResumeError(w, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"})
		return
	}
	if s.idempotency == nil {
		writeResumeError(w, &clientError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "an internal error occurred"})
		return
	}
	stored, replayed, inspectErr := s.idempotency.Inspect(r.Context(), sess.UserID, operation, headers.Key, fingerprint)
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
		UserID: sess.UserID, SessionID: sess.ID, Session: sess, ExpectedRevision: headers.ExpectedRevision,
		WireVersion: headers.WireVersion, Operation: operation, RequestHash: fingerprint,
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
		executeContext, sess.UserID, operation, headers.Key, fingerprint, callback.run,
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

// transactionMutation reauthenticates the transaction against the durable
// session row immediately before a mutation callback can write.
func (s *Service) transactionMutation(ctx context.Context, qtx *store.Queries, mutation mutationContext) (mutationContext, error) {
	if qtx == nil {
		return mutationContext{}, errors.New("resumeapi: mutation transaction is unavailable")
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
	key, _ := ctx.Value(mutationRequestKeyContext{}).(uuid.UUID)
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
	session, ok := auth.SessionFromContext(ctx)
	if !ok {
		return mutationTransition{}, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"}
	}
	if err := auth.RequireRecentReauth(session, s.clock()); err != nil {
		return mutationTransition{}, err
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

func parseMutationHeaders(r *http.Request, requireMatch bool, accepted []int32) (mutationHeaders, *clientError) {
	keyValues := r.Header.Values("Idempotency-Key")
	if len(keyValues) == 0 {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_required", Message: "Idempotency-Key is required"}
	}
	if len(keyValues) != 1 || keyValues[0] == "" || strings.Contains(keyValues[0], ",") {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Message: "Idempotency-Key must be one UUID"}
	}
	key, err := uuid.Parse(keyValues[0])
	if err != nil {
		return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "idempotency_key_invalid", Message: "Idempotency-Key must be one UUID"}
	}

	matchValues := r.Header.Values("If-Match")
	var revision *int64
	if !requireMatch {
		if len(matchValues) != 0 {
			return mutationHeaders{}, &clientError{Status: http.StatusBadRequest, Code: "precondition_not_supported", Message: "If-Match is not supported when creating a resume"}
		}
	} else {
		if len(matchValues) == 0 {
			return mutationHeaders{}, &clientError{Status: http.StatusPreconditionRequired, Code: "precondition_required", Message: "If-Match is required"}
		}
		parsed, parseErr := parseIfMatch(matchValues)
		if parseErr != nil {
			return mutationHeaders{}, parseErr
		}
		revision = &parsed
	}

	version, versionErr := resolveWireVersion(r.Header, accepted)
	if versionErr != nil {
		return mutationHeaders{}, versionErr
	}
	return mutationHeaders{Key: key, ExpectedRevision: revision, WireVersion: version}, nil
}

func parseIfMatch(values []string) (int64, *clientError) {
	malformed := func() (int64, *clientError) {
		return 0, &clientError{Status: http.StatusBadRequest, Code: "precondition_malformed", Message: `If-Match must have the form "r<revision>"`}
	}
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return malformed()
	}
	value := values[0]
	if len(value) < 4 || !strings.HasPrefix(value, `"r`) || !strings.HasSuffix(value, `"`) {
		return malformed()
	}
	digits := value[2 : len(value)-1]
	if digits == "" || (len(digits) > 1 && digits[0] == '0') {
		return malformed()
	}
	revision, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || revision < 1 {
		return malformed()
	}
	return revision, nil
}

func decodeJSONBody(r *http.Request, target any) (boundedInput, error) {
	if err := requireJSONContentType(r.Header); err != nil {
		return boundedInput{}, err
	}
	raw, err := readBoundedBody(r.Body, maxJSONBodyBytes)
	if err != nil {
		return boundedInput{}, err
	}
	if err := validateJSONTokens(raw, maxJSONDepth); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body is not valid JSON"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body does not match the operation"}
	}
	if err := requireDecoderEOF(dec); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body has trailing data"}
	}
	return boundedInput{Payload: raw, Value: target}, nil
}

func decodeDeleteBody(r *http.Request) (boundedInput, error) {
	if values := r.Header.Values("Content-Type"); len(values) > 0 {
		if len(values) != 1 || !isJSONContentType(values[0]) {
			return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE Content-Type must be one application/json value"}
		}
	}
	if r.ContentLength > 0 {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE requests have no body"}
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE requests have no body"}
	}
	return boundedInput{Payload: []byte{}}, nil
}

func requireJSONContentType(header http.Header) error {
	values := header.Values("Content-Type")
	if len(values) != 1 || !isJSONContentType(values[0]) {
		return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "Content-Type must be one application/json value"}
	}
	return nil
}

func isJSONContentType(value string) bool {
	if strings.Contains(value, ",") {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil || mediaType != "application/json" {
		return false
	}
	delete(params, "charset")
	return len(params) == 0
}

func readBoundedBody(body io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body could not be read"}
	}
	if int64(len(raw)) > limit {
		return nil, &clientError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body exceeds the 262144 byte limit"}
	}
	return raw, nil
}

func validateJSONTokens(raw []byte, maxDepth int) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := consumeJSONValue(dec, 0, maxDepth); err != nil {
		return err
	}
	return requireDecoderEOF(dec)
}

func consumeJSONValue(dec *json.Decoder, depth, maxDepth int) error {
	if depth > maxDepth {
		return errors.New("JSON nesting is too deep")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, keyErr := dec.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if consumeErr := consumeJSONValue(dec, depth+1, maxDepth); consumeErr != nil {
				return consumeErr
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if consumeErr := consumeJSONValue(dec, depth+1, maxDepth); consumeErr != nil {
				return consumeErr
			}
		}
		_, err = dec.Token()
		return err
	default:
		return errors.New("unexpected closing JSON delimiter")
	}
}

func requireDecoderEOF(dec *json.Decoder) error {
	var trailing any
	err := dec.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func operationHash(method, operation string, targets []string) [32]byte {
	fields := [][]byte{[]byte("method"), []byte(strings.ToUpper(method)), []byte("operation"), []byte(operation)}
	for _, target := range targets {
		fields = append(fields, []byte(target))
	}
	return tupleHash(idempotencyOperationDomain, fields)
}

func requestHash(version int32, precondition string, semanticInputs []string, payload []byte) [32]byte {
	fields := [][]byte{
		[]byte("wire_version"), []byte(wireVersionString(version)),
		[]byte("if_match"), []byte(precondition),
	}
	for _, input := range semanticInputs {
		fields = append(fields, []byte(input))
	}
	fields = append(fields, []byte("payload"), payload)
	return tupleHash(idempotencyRequestDomain, fields)
}

func tupleHash(domain string, fields [][]byte) [32]byte {
	var encoded bytes.Buffer
	var length [4]byte
	encoded.WriteString(domain)
	encoded.WriteByte(0)
	binary.BigEndian.PutUint32(length[:], tupleLength(len(fields)))
	encoded.Write(length[:])
	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], tupleLength(len(field)))
		encoded.Write(length[:])
		encoded.Write(field)
	}
	return sha256.Sum256(encoded.Bytes())
}

func tupleLength(length int) uint32 {
	if length < 0 || uint64(length) > uint64(^uint32(0)) {
		panic("resumeapi: tuple field exceeds uint32 length encoding")
	}
	return uint32(length)
}

func hexDigest(digest [32]byte) string { return hex.EncodeToString(digest[:]) }
