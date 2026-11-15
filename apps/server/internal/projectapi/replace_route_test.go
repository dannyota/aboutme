package projectapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type routeLinks struct {
	replacement *resumeapi.ProjectReplacement
}

func (l *routeLinks) LinkedResumeIDs(context.Context, *store.Queries, uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	return nil, errors.New("unused")
}

func (l *routeLinks) ReplaceProject(_ context.Context, replacement resumeapi.ProjectReplacement) (resume.ExecuteResult, error) {
	l.replacement = &replacement
	return resume.ExecuteResult{Outcome: resume.CommitCommitted, Response: resume.StoredResponse{Status: http.StatusOK, Body: []byte(`{"data":{}}`)}}, nil
}

func (l *routeLinks) RemoveProject(context.Context, resumeapi.ProjectRemoval) (resume.ExecuteResult, error) {
	return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("unexpected removal")
}

type routeIdempotency struct {
	executes int
}

func (i *routeIdempotency) Inspect(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.StoredResponse, bool, error) {
	return resume.StoredResponse{}, false, nil
}

func (i *routeIdempotency) Execute(context.Context, uuid.UUID, string, uuid.UUID, [32]byte, func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	i.executes++
	return resume.ExecuteResult{Outcome: resume.CommitNotAttempted}, errors.New("unexpected direct transaction")
}

func TestReplace_RunsThroughTheLinkedResumePlan(t *testing.T) {
	links, idempotency := &routeLinks{}, &routeIdempotency{}
	svc := &Service{idempotency: idempotency, links: links, now: time.Now, allowSlug: func(uuid.UUID, time.Time) bool { return true }}
	sess := store.Session{ID: uuid.New(), UserID: uuid.New()}
	id, key := uuid.New(), uuid.New()
	r := httptest.NewRequestWithContext(auth.ContextWithSession(t.Context(), sess), http.MethodPut, "/api/v1/projects/"+id.String(), strings.NewReader(projectBody))
	r.SetPathValue("id", id.String())
	r.Header.Set("Idempotency-Key", key.String())
	r.Header.Set("If-Match", `"r3"`)
	w := httptest.NewRecorder()
	svc.mutate(w, r)

	if w.Code != http.StatusOK || idempotency.executes != 0 || links.replacement == nil {
		t.Fatalf("PUT = %d %s with %d direct transactions, want one planned replacement", w.Code, w.Body.String(), idempotency.executes)
	}
	operation, fingerprint := identity(http.MethodPut, id, 3, resumeapi.SessionCaller, []byte(projectBody))
	got := links.replacement
	if got.UserID != sess.UserID || got.ProjectID != id || got.Key != key || got.Operation != operation || got.RequestHash != fingerprint {
		t.Fatalf("replacement identity = %v %v %v %q, want the request's", got.UserID, got.ProjectID, got.Key, got.Operation)
	}
	if got.Change == nil || got.Authorize == nil || got.Replace == nil {
		t.Fatal("replacement is missing a transaction step")
	}
}
