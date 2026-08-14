package resumeapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestExecuteMutation_ChangedSemanticInputRejectsIdempotencyKeyReuse(t *testing.T) {
	t.Parallel()

	idempotency := newSemanticIdempotencyBoundary()
	service := &Service{idempotency: idempotency, acceptedVersions: []int32{2}}
	userID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	targetID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60")
	key := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61")
	payload := []byte(`{"value":"unchanged"}`)
	semanticValue := "baseline"
	operationRuns := 0
	state := 0
	spec := mutationSpec{
		RegisteredOperation: "semanticInputMutation",
		RequireMatch:        true,
		Decode: func(r *http.Request) (boundedInput, error) {
			body, err := io.ReadAll(r.Body)
			return boundedInput{Payload: body}, err
		},
		CanonicalTargets: func(boundedInput) ([]string, error) {
			return []string{"resume_id", targetID.String()}, nil
		},
		SemanticInputs: func(boundedInput) ([]string, error) {
			return []string{"mode", semanticValue}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			operationRuns++
			state++
			return mutationRunResult{Response: resume.StoredResponse{Status: http.StatusNoContent}}, nil
		}),
	}
	request := func() *http.Request {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/semantic", bytes.NewReader(payload))
		req.Header.Set("Idempotency-Key", key.String())
		req.Header.Set("If-Match", `"r42"`)
		return req.WithContext(auth.ContextWithSession(req.Context(), store.Session{UserID: userID}))
	}

	first := httptest.NewRecorder()
	service.executeMutation(first, request(), spec)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status = %d body=%q, want 204", first.Code, first.Body.String())
	}
	stateAfterFirst := state

	semanticValue = "changed"
	second := httptest.NewRecorder()
	service.executeMutation(second, request(), spec)
	if second.Code != http.StatusConflict || !bytes.Contains(second.Body.Bytes(), []byte(`"code":"idempotency_key_reuse"`)) {
		t.Fatalf("changed-semantic response = status %d body=%q, want 409 idempotency_key_reuse", second.Code, second.Body.String())
	}
	if operationRuns != 1 {
		t.Fatalf("operation runs = %d, want 1", operationRuns)
	}
	if state != stateAfterFirst {
		t.Fatalf("state after rejected reuse = %d, want unchanged %d", state, stateAfterFirst)
	}
	if got := idempotency.recordCount(); got != 1 {
		t.Fatalf("stored idempotency records = %d, want 1", got)
	}
}

type semanticIdempotencyRecordKey struct {
	userID    uuid.UUID
	operation string
	key       uuid.UUID
}

type semanticIdempotencyRecord struct {
	requestHash [32]byte
	response    resume.StoredResponse
}

type semanticIdempotencyBoundary struct {
	records map[semanticIdempotencyRecordKey]semanticIdempotencyRecord
}

func newSemanticIdempotencyBoundary() *semanticIdempotencyBoundary {
	return &semanticIdempotencyBoundary{records: make(map[semanticIdempotencyRecordKey]semanticIdempotencyRecord)}
}

func (s *semanticIdempotencyBoundary) Inspect(_ context.Context, userID uuid.UUID, operation string,
	key uuid.UUID, requestHash [32]byte,
) (resume.StoredResponse, bool, error) {
	record, ok := s.records[semanticIdempotencyRecordKey{userID: userID, operation: operation, key: key}]
	if !ok {
		return resume.StoredResponse{}, false, nil
	}
	if record.requestHash != requestHash {
		return resume.StoredResponse{}, false, resume.ErrIdempotencyKeyReuse
	}
	return record.response, true, nil
}

func (s *semanticIdempotencyBoundary) Execute(_ context.Context, userID uuid.UUID, operation string,
	key uuid.UUID, requestHash [32]byte, mutate func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	recordKey := semanticIdempotencyRecordKey{userID: userID, operation: operation, key: key}
	if record, ok := s.records[recordKey]; ok {
		if record.requestHash != requestHash {
			return resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, resume.ErrIdempotencyKeyReuse
		}
		return resume.ExecuteResult{Response: record.response, Replayed: true, Outcome: resume.CommitCommitted}, nil
	}
	response, err := mutate(nil)
	if err != nil {
		return resume.ExecuteResult{Outcome: resume.CommitDefinitelyRolledBack}, err
	}
	s.records[recordKey] = semanticIdempotencyRecord{requestHash: requestHash, response: response}
	return resume.ExecuteResult{Response: response, Outcome: resume.CommitCommitted}, nil
}

func (s *semanticIdempotencyBoundary) recordCount() int {
	return len(s.records)
}
