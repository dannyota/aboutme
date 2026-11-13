package publicapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

const (
	leaseTestPNGPath          = "/api/v1/public/resumes/ada-lovelace/og.png"
	leaseTestConditionalError = "{\"error\":{\"code\":\"request_invalid\",\"message\":\"request is invalid\"}}\n"
	leaseTestPublicNotFound   = "{\"error\":{\"code\":\"public_not_found\",\"message\":\"public resume not found\"}}\n"
)

func TestLeasedErrorResponses_LeaveGoWhileLeased(t *testing.T) {
	t.Run("artifact_malformed_conditional", func(t *testing.T) {
		handlers, _, _ := newArtifactHarness(t, mustNotRenderQueue(), 2, time.Now)
		assertResponseLeavesGoInsideAFlush(t, handlers.png, http.MethodGet, leaseTestPNGPath,
			http.Header{"If-None-Match": {"unquoted"}}, http.StatusBadRequest, leaseTestConditionalError)
	})
	t.Run("json_pass_refusal", func(t *testing.T) {
		slug, reader, coordinator, backing := publicServiceReaderWithStore(t)
		backing.row.SignInToView = true
		service := newPublicServiceForReader(t, reader, coordinator, newLeaseTestCache(t))
		assertResponseLeavesGoInsideAFlush(t, service, http.MethodGet, "/api/v1/public/resumes/"+slug, nil, http.StatusNotFound, leaseTestPublicNotFound)
	})
}

func TestLeasedErrorResponses_RevocationEndsABlockedErrorFlush(t *testing.T) {
	handlers, backing, coordinator := newArtifactHarness(t, mustNotRenderQueue(), 2, time.Now)
	writer := flushBlockedWriter{newDeadlineBlockedWriter()}
	t.Cleanup(writer.release)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, leaseTestPNGPath, nil)
	request.RemoteAddr = "192.0.2.30:1234"
	request.Header.Set("If-None-Match", "unquoted")
	done := make(chan struct{})
	go func() {
		defer close(done)
		handlers.png.ServeHTTP(writer, request)
	}()
	select {
	case <-writer.writing:
	case <-done:
		t.Fatal("the handler released its lease without flushing its error response")
	case <-time.After(5 * time.Second):
		t.Fatal("the error response was never flushed")
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	closeErr := transition.Close(context.Background(), time.Now().Add(time.Second))
	writer.release()
	<-done
	if closeErr != nil {
		t.Fatalf("revocation did not end the blocked error flush and drain the lease: %v", closeErr)
	}
	if !writer.deadlineSet() {
		t.Fatal("revocation did not set the write deadline")
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func mustNotRenderQueue() RenderQueue {
	return artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		return renderjob.Result{}, errors.New("must not render")
	})
}
