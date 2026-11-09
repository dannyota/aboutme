package resumeapi

// Mutation admission: the request, input, and context types every resume
// mutation shares, the transaction callback binding, and executeMutation's
// HTTP admission order from headers to the idempotent write.

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

// mutationHeaders holds a mutation request's parsed headers.
type mutationHeaders struct {
	// Key is the request's Idempotency-Key.
	Key uuid.UUID
	// ExpectedRevision is the If-Match revision, or nil when the route
	// takes no precondition.
	ExpectedRevision *int64
	// WireVersion is the resume schema version resolved from the request.
	WireVersion int32
}

// boundedInput is a mutation request decoded within its size bounds.
type boundedInput struct {
	// Payload is the bounded request bytes the request fingerprint covers.
	Payload []byte
	// Value is the decoded request value that later steps type-assert.
	Value any
}

// idempotencyInspection is the idempotency store's decision for a request
// before preparation runs.
type idempotencyInspection struct {
	// Operation is the hex digest of the method, registered operation, and
	// canonical targets.
	Operation string
	// RequestHash is the request fingerprint over wire version,
	// precondition, semantic inputs, and payload.
	RequestHash [32]byte
	// Response is the committed response when Replayed is true.
	Response resume.StoredResponse
	// Replayed reports whether a committed record already answers this key
	// and fingerprint.
	Replayed bool
}

// preparedInput is a mutation's input after preparation.
type preparedInput struct {
	// Input is the decoded request preparation started from.
	Input boundedInput
	// Value is the operation-specific prepared value that Run type-asserts.
	Value any
	// ExecuteBefore, when not zero, bounds execution: the mutation is not
	// attempted after it passes, and the execution context ends there.
	ExecuteBefore time.Time
}

// mutationContext identifies the caller and request a mutation runs for.
type mutationContext struct {
	// UserID is the account the mutation acts for: the agent token's user
	// for an agent caller, otherwise the session's user.
	UserID uuid.UUID
	// SessionID is the caller's session ID, or the zero UUID without a
	// session.
	SessionID uuid.UUID
	// Session is the caller's session; the transaction replaces it with the
	// durable row it rechecks.
	Session store.Session
	// Agent is the agent principal for an agent token caller, or nil.
	Agent *AgentPrincipal
	// ExpectedDiscoveryGeneration is the discovery generation a global
	// transition locked and matched in its transaction, or nil.
	ExpectedDiscoveryGeneration *int64
	// ExpectedRevision is the If-Match revision, or nil when the route
	// takes no precondition.
	ExpectedRevision *int64
	// WireVersion is the resume schema version for the request and response.
	WireVersion int32
	// Operation is the hex operation digest that keys the idempotency record.
	Operation string
	// RequestHash is the request fingerprint stored with the idempotency
	// record.
	RequestHash [32]byte
	// CurrentResume is the resume row a transition read in its transaction
	// at the preflight revision, or nil outside a transition.
	CurrentResume *resume.Resume
}

// mutationRunResult is the result of one mutationOperation run.
type mutationRunResult struct {
	// Response is the response the idempotency store records and the
	// handler writes.
	Response resume.StoredResponse
}

// mutationOperation performs one mutation inside its database transaction.
type mutationOperation interface {
	// Run performs the mutation with qtx after the transaction rechecks the
	// caller, and returns the response to record.
	Run(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error)
}

// mutationOperationFunc is a function that implements mutationOperation.
type mutationOperationFunc func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error)

// Run adapts a function to mutationOperation.
func (f mutationOperationFunc) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	return f(ctx, qtx, mutation, prepared)
}

// mutationSpec describes one mutation route for executeMutation.
type mutationSpec struct {
	// RegisteredOperation is the route's operation name, hashed into the
	// idempotency operation digest.
	RegisteredOperation string
	// RequireMatch reports whether the route requires If-Match; when false
	// the route rejects one.
	RequireMatch bool
	// Decode reads and bounds the request. Required.
	Decode func(*http.Request) (boundedInput, error)
	// CanonicalTargets returns name and value pairs that identify the
	// mutation target in the operation digest. Required.
	CanonicalTargets func(boundedInput) ([]string, error)
	// SemanticInputs, when set, returns name and value pairs added to the
	// request fingerprint.
	SemanticInputs func(boundedInput) ([]string, error)
	// Prepare, when set, builds the prepared input after idempotency
	// inspection; otherwise the decoded value is used as is.
	Prepare func(context.Context, boundedInput, idempotencyInspection) (preparedInput, error)
	// Run performs the mutation in its transaction. Required.
	Run mutationOperation
	// Finalize, when set, runs after every execution outcome once
	// preparation succeeds, including errors.
	Finalize func(context.Context, preparedInput, resume.ExecuteResult, error)
	// Transition, when set, sends the mutation through executeTransition
	// and describes its public-state transition.
	Transition func(context.Context, resume.Resume, preparedInput) (mutationTransition, error)
}

// mutationTransition describes a mutation's public-state transition for one
// resume.
type mutationTransition struct {
	// ResumeID is the resume the transition changes; it must match the
	// preflight row.
	ResumeID uuid.UUID
	// Class is the transition's public-state class.
	Class publicstate.TransitionClass
	// Global reports whether the transition also fences and locks the
	// discovery generation.
	Global bool
	// Retire reports whether the transition retires the resume from public
	// state.
	Retire bool
	// Slugs is the complete old/new claim set resolved during preflight. The
	// callback locks this exact set before public state or any row operation.
	Slugs []string
	// Publish is the recovery proof for a publish transition, or nil.
	Publish *publishRecoveryProof
}

// boundMutation binds an operation to its execution context and inputs as
// the idempotency store's transaction callback.
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
