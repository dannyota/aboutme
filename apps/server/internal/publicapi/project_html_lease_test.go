package publicapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// deadlineBlockedWriter blocks every body write until a write deadline is
// set, as a connection to a reader that stopped reading does.
type deadlineBlockedWriter struct {
	header       http.Header
	writing      chan struct{}
	deadline     chan struct{}
	released     chan struct{}
	writingOnce  sync.Once
	deadlineOnce sync.Once
	releaseOnce  sync.Once
}

func newDeadlineBlockedWriter() *deadlineBlockedWriter {
	return &deadlineBlockedWriter{header: http.Header{}, writing: make(chan struct{}), deadline: make(chan struct{}), released: make(chan struct{})}
}

func (w *deadlineBlockedWriter) Header() http.Header { return w.header }
func (w *deadlineBlockedWriter) WriteHeader(int)     {}
func (w *deadlineBlockedWriter) Write([]byte) (int, error) {
	w.writingOnce.Do(func() { close(w.writing) })
	select {
	case <-w.deadline:
		return 0, os.ErrDeadlineExceeded
	case <-w.released:
		return 0, errors.New("released by the test")
	}
}

// SetWriteDeadline is what http.ResponseController calls.
func (w *deadlineBlockedWriter) SetWriteDeadline(time.Time) error {
	w.deadlineOnce.Do(func() { close(w.deadline) })
	return nil
}

func (w *deadlineBlockedWriter) release() { w.releaseOnce.Do(func() { close(w.released) }) }

func (w *deadlineBlockedWriter) deadlineSet() bool {
	select {
	case <-w.deadline:
		return true
	default:
		return false
	}
}

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
