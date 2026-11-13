package publicapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// TestProjectPages_RevocationEndsABlockedWrite proves a revoking transition
// ends a project response blocked on a slow reader, fresh or cached: the
// lease's cancel hook sets the write deadline, the write returns, the
// handler ends and the lease drains before the transition's deadline.
func TestProjectPages_RevocationEndsABlockedWrite(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "fresh"
		if cached {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			readStore := projectPageFixture(t)
			_, client := newFakeProjectRenderer(t)
			reader, coordinator := projectTestReaderAndCoordinator(t, readStore)
			handlers := newProjectHandlers(t, reader, client)
			if cached {
				if primed := handlers.serve(http.MethodGet, "/ada/projects/note-g"); primed.Code != http.StatusOK {
					t.Fatalf("priming status = %d", primed.Code)
				}
			}
			writer := newDeadlineBlockedWriter()
			t.Cleanup(writer.release)
			done := make(chan struct{})
			go func() {
				defer close(done)
				handlers.page.ServeHTTP(writer, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ada/projects/note-g", nil))
			}()
			select {
			case <-writer.writing:
			case <-time.After(5 * time.Second):
				t.Fatal("the handler never wrote the body")
			}
			transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{
				{ID: readStore.row.ID, ExpectedRevision: readStore.row.Revision, Class: publicstate.Revoking},
			}})
			if err != nil {
				t.Fatal(err)
			}
			closeErr := transition.Close(context.Background(), time.Now().Add(time.Second))
			writer.release()
			<-done
			if closeErr != nil {
				t.Fatalf("revocation did not end the blocked write and drain the lease: %v", closeErr)
			}
			if !writer.deadlineSet() {
				t.Fatal("revocation did not set the write deadline")
			}
			if err := transition.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
