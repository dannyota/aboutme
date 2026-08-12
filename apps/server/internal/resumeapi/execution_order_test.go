package resumeapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type orderedIdempotency struct {
	events          *[]string
	inspectResponse resume.StoredResponse
	inspectReplay   bool
	inspectErr      error
	executeResult   resume.ExecuteResult
	executeErr      error
	runCallback     bool
}

func (s *orderedIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	*s.events = append(*s.events, "inspect")
	return s.inspectResponse, s.inspectReplay, s.inspectErr
}

func (s *orderedIdempotency) Execute(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID, _ [32]byte,
	mutate func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	*s.events = append(*s.events, "execute")
	if s.runCallback {
		response, err := mutate(nil)
		if err != nil {
			return s.executeResult, err
		}
		s.executeResult.Response = response
	}
	return s.executeResult, s.executeErr
}

func orderedMutationRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	session := store.Session{UserID: uuid.New()}
	return req.WithContext(auth.ContextWithSession(req.Context(), session))
}

func orderedMutationSpec(events *[]string) mutationSpec {
	return mutationSpec{
		RegisteredOperation: "orderedMutation",
		Decode: func(*http.Request) (boundedInput, error) {
			*events = append(*events, "decode")
			return boundedInput{Payload: []byte(`{}`)}, nil
		},
		CanonicalTargets: func(boundedInput) ([]string, error) {
			*events = append(*events, "targets")
			return []string{"resume_id", uuid.Nil.String()}, nil
		},
		SemanticInputs: func(boundedInput) ([]string, error) {
			*events = append(*events, "semantic")
			return []string{"mode", "test"}, nil
		},
		Prepare: func(context.Context, boundedInput, idempotencyInspection) (preparedInput, error) {
			*events = append(*events, "prepare")
			return preparedInput{}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			*events = append(*events, "operation")
			return mutationRunResult{Response: resume.StoredResponse{Status: http.StatusNoContent}}, nil
		}),
		Finalize: func(context.Context, preparedInput, resume.ExecuteResult, error) {
			*events = append(*events, "finalize")
		},
	}
}

func TestExecuteMutation_OrderFresh(t *testing.T) {
	t.Parallel()
	var events []string
	service := orderedService(&events, &orderedIdempotency{
		events: &events, runCallback: true,
		executeResult: resume.ExecuteResult{Outcome: resume.CommitCommitted},
	})
	rec := httptest.NewRecorder()
	service.executeMutation(rec, orderedMutationRequest(t), orderedMutationSpec(&events))
	want := []string{"decode", "targets", "semantic", "inspect", "prepare", "execute", "operation", "finalize", "writer"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestExecuteMutation_PreflightReplaySkipsPreparationOperationAndFinalize(t *testing.T) {
	t.Parallel()
	var events []string
	service := orderedService(&events, &orderedIdempotency{
		events: &events, inspectReplay: true,
		inspectResponse: resume.StoredResponse{Status: http.StatusNoContent},
	})
	rec := httptest.NewRecorder()
	service.executeMutation(rec, orderedMutationRequest(t), orderedMutationSpec(&events))
	want := []string{"decode", "targets", "semantic", "inspect", "writer"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestExecuteMutation_ConcurrentReplayAfterPrepareFinalizes(t *testing.T) {
	t.Parallel()
	var events []string
	service := orderedService(&events, &orderedIdempotency{
		events: &events,
		executeResult: resume.ExecuteResult{
			Response: resume.StoredResponse{Status: http.StatusNoContent},
			Replayed: true, Outcome: resume.CommitCommitted,
		},
	})
	rec := httptest.NewRecorder()
	service.executeMutation(rec, orderedMutationRequest(t), orderedMutationSpec(&events))
	want := []string{"decode", "targets", "semantic", "inspect", "prepare", "execute", "finalize", "writer"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestExecuteMutation_CallbackFailureFinalizesAndWritesError(t *testing.T) {
	t.Parallel()
	var events []string
	injected := errors.New("callback failed")
	spec := orderedMutationSpec(&events)
	spec.Run = mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
		events = append(events, "operation")
		return mutationRunResult{}, injected
	})
	service := orderedService(&events, &orderedIdempotency{
		events: &events, runCallback: true,
		executeResult: resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack},
	})
	rec := httptest.NewRecorder()
	service.executeMutation(rec, orderedMutationRequest(t), spec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	want := []string{"decode", "targets", "semantic", "inspect", "prepare", "execute", "operation", "finalize"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestExecuteMutation_CandidateCleanupFailureCannotReplaceStoredSuccess(t *testing.T) {
	t.Parallel()
	var events []string
	backend := &candidateBackend{deleteErr: errors.New("cleanup failed")}
	service := orderedService(&events, &orderedIdempotency{
		events: &events,
		executeResult: resume.ExecuteResult{
			Response: resume.StoredResponse{Status: http.StatusNoContent},
			Replayed: true, Outcome: resume.CommitCommitted,
		},
	})
	service.blobs = backend
	service.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	spec := orderedMutationSpec(&events)
	cleanup := service.finalizePhotoCandidate(photoCandidate{Key: "candidate", Created: true})
	spec.Finalize = func(ctx context.Context, prepared preparedInput, result resume.ExecuteResult, err error) {
		events = append(events, "finalize")
		cleanup(ctx, prepared, result, err)
	}
	rec := httptest.NewRecorder()
	service.executeMutation(rec, orderedMutationRequest(t), spec)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("response = status %d body %q, want stored bodyless 204", rec.Code, rec.Body.String())
	}
	if backend.deletes != 1 {
		t.Fatalf("cleanup attempts = %d, want 1", backend.deletes)
	}
	want := []string{"decode", "targets", "semantic", "inspect", "prepare", "execute", "finalize", "writer"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func orderedService(events *[]string, idempotency idempotencyBoundary) *Service {
	return &Service{
		idempotency: idempotency, acceptedVersions: []int32{2},
		writeResponse: func(w http.ResponseWriter, response resume.StoredResponse) {
			*events = append(*events, "writer")
			writeStoredResponse(w, response)
		},
	}
}
