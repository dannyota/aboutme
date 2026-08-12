package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestDeleteJobFailureRollsBackMutationAndIdempotencyRecord(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	beforeRecords := h.snapshotUserTable(t, "idempotency_records")
	hookErr := errors.New("deletion job insert failed")

	spec := mutationSpec{
		RegisteredOperation: "deleteResumeRollbackTest",
		RequireMatch:        true,
		Decode:              decodeDeleteBody,
		CanonicalTargets: func(boundedInput) ([]string, error) {
			return []string{"resume_id", created.ID.String()}, nil
		},
		Prepare: func(context.Context, boundedInput, idempotencyInspection) (preparedInput, error) {
			return preparedInput{Value: deletePreparedInput{
				ResumeID: created.ID,
				BeforeDelete: func(context.Context, *store.Queries, resume.Resume) error {
					return hookErr
				},
				Response: operationResponse,
			}}, nil
		},
		Run: deleteOperation{service: h.service},
	}
	req := httptest.NewRequestWithContext(auth.ContextWithSession(context.Background(), h.session), http.MethodDelete, "/", nil)
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, created.Revision))
	rec := httptest.NewRecorder()
	h.service.executeMutation(rec, req, spec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	got, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("get resume after rollback: %v", err)
	}
	if got.Revision != created.Revision {
		t.Fatalf("revision after rollback = %d, want %d", got.Revision, created.Revision)
	}
	if afterRecords := h.snapshotUserTable(t, "idempotency_records"); afterRecords != beforeRecords {
		t.Fatalf("callback failure stored idempotency record: before=%q after=%q", beforeRecords, afterRecords)
	}
}

func TestSameIdempotencyKeyOnDifferentConcreteTargetsIsDistinct(t *testing.T) {
	h := newResumeAPITestHarness(t)
	var calls atomic.Int32
	spec := mutationSpec{
		RegisteredOperation: "targetScopeTest",
		Decode: func(r *http.Request) (boundedInput, error) {
			var target string
			input, err := decodeJSONBody(r, &target)
			input.Value = target
			return input, err
		},
		CanonicalTargets: func(input boundedInput) ([]string, error) {
			target, ok := input.Value.(string)
			if !ok {
				return nil, errors.New("target input has the wrong type")
			}
			return []string{"resume_id", target}, nil
		},
		Run: mutationOperationFunc(func(context.Context, *store.Queries, mutationContext, preparedInput) (mutationRunResult, error) {
			calls.Add(1)
			return mutationRunResult{Response: resume.StoredResponse{Status: http.StatusNoContent}}, nil
		}),
	}
	key := uuid.NewString()
	for index := 0; index < 2; index++ {
		target := uuid.NewString()
		req := httptest.NewRequestWithContext(auth.ContextWithSession(context.Background(), h.session), http.MethodPost, "/", bytes.NewBufferString(strconv.Quote(target)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set(wireVersionHeader, wireVersionString(docmigrate.CurrentVersion))
		rec := httptest.NewRecorder()
		h.service.executeMutation(rec, req, spec)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("target %d status = %d, want 204 (body=%s)", index, rec.Code, rec.Body.String())
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("operation calls = %d, want two distinct mutations", got)
	}
}

func TestLiveHTTPWriteReadSanitizesBeforePersistedBounds(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	spec := mutationSpec{
		RegisteredOperation: "liveSanitizeWriteTest",
		RequireMatch:        true,
		Decode: func(r *http.Request) (boundedInput, error) {
			var document json.RawMessage
			input, err := decodeJSONBody(r, &document)
			input.Value = document
			return input, err
		},
		CanonicalTargets: func(boundedInput) ([]string, error) {
			return []string{"resume_id", created.ID.String()}, nil
		},
		Prepare: func(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			document, ok := input.Value.(json.RawMessage)
			if !ok {
				return preparedInput{}, errors.New("document input has the wrong type")
			}
			return preparedInput{Value: aggregatePreparedInput{
				ResumeID: created.ID,
				Apply: func(json.RawMessage) (json.RawMessage, error) {
					return document, nil
				},
				Response: operationResponse,
			}}, nil
		},
		Run: aggregateOperation{service: h.service},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			r = r.WithContext(auth.ContextWithSession(r.Context(), h.session))
			h.service.executeMutation(w, r, spec)
		case http.MethodGet:
			stored, err := h.resumes.Get(r.Context(), h.userID, created.ID)
			if err != nil {
				writeResumeError(w, mapMutationError(err))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(stored.Doc); err != nil {
				return
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	write := func(document schema.Resume, revision int64) testHTTPResponse {
		t.Helper()
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatalf("marshal write document: %v", err)
		}
		req, err := http.NewRequestWithContext(h.ctx, http.MethodPatch, server.URL, bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("build write request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, revision))
		req.Header.Set(wireVersionHeader, wireVersionString(docmigrate.CurrentVersion))
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("perform write: %v", err)
		}
		return snapshotHTTPResponse(t, response)
	}
	read := func() schema.Resume {
		t.Helper()
		request, err := http.NewRequestWithContext(h.ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatalf("build read request: %v", err)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatalf("perform read: %v", err)
		}
		snapshot := snapshotHTTPResponse(t, response)
		if snapshot.status != http.StatusOK {
			t.Fatalf("read status = %d, want 200 (body=%s)", snapshot.status, snapshot.body)
		}
		var document schema.Resume
		if err := json.Unmarshal(snapshot.body, &document); err != nil {
			t.Fatalf("decode read: %v", err)
		}
		return document
	}

	removable := "<!--" + strings.Repeat("x", schema.MaxRichTextBytes+1) + "--><script>alert(1)</script><p>safe</p>"
	accepted := created.Doc
	accepted.Content = map[string]schema.Section{
		"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{
			ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &removable,
		}}),
	}
	accepted.Customization.Layout.Sections.Main = []string{"profile"}
	accepted.Customization.Layout.Sections.Sidebar = []string{}
	probe, err := json.Marshal(accepted)
	if err != nil {
		t.Fatalf("marshal sanitization probe: %v", err)
	}
	if _, err := h.service.applyAtWireVersion(created.Doc, docmigrate.CurrentVersion, func(json.RawMessage) (json.RawMessage, error) {
		return probe, nil
	}); err != nil {
		t.Fatalf("sanitization probe: %v", err)
	}
	first := write(accepted, created.Revision)
	if first.status != http.StatusNoContent || len(first.body) != 0 {
		t.Fatalf("sanitized write = status %d body %q, want bodyless 204", first.status, first.body)
	}
	stored := read()
	text := stored.Content["profile"].ProfileEntries[0].Text
	if text == nil || *text != "<p>safe</p>" {
		t.Fatalf("read-back rich text = %v, want sanitized <p>safe</p>", text)
	}

	over := "<p>" + strings.Repeat("a", schema.MaxRichTextBytes+1) + "</p>"
	rejected := stored
	rejected.Content["profile"] = schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{
		ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &over,
	}})
	second := write(rejected, created.Revision+1)
	if second.status != http.StatusUnprocessableEntity {
		t.Fatalf("over-bound write status = %d, want 422 (body=%s)", second.status, second.body)
	}
	if !bytes.Contains(second.body, []byte(`"code":"document_invalid"`)) {
		t.Fatalf("over-bound response = %s, want document_invalid", second.body)
	}
	after := read()
	afterText := after.Content["profile"].ProfileEntries[0].Text
	if afterText == nil || *afterText != "<p>safe</p>" {
		t.Fatalf("rejected write changed stored rich text: %v", afterText)
	}
}
