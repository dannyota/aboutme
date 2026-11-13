package resumeapi

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
	Slugs    []string
	Publish  *publishRecoveryProof
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
