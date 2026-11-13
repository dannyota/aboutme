package publicapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// recordingHooks stands in for a lease: it keeps the registered hook, or
// fails to register one.
type recordingHooks struct {
	hook func()
	err  error
}

func (h *recordingHooks) OnCancel(hook func()) error {
	if h.err != nil {
		return h.err
	}
	h.hook = hook
	return nil
}

func TestWriteWhileLeased_FailsClosedWhenTheHookCannotRegister(t *testing.T) {
	wrote := false
	err := writeWhileLeased(newDeadlineBlockedWriter(), &recordingHooks{err: errors.New("cannot register")}, func() { wrote = true })
	if err == nil || wrote {
		t.Fatalf("writeWhileLeased() error = %v, wrote = %t; want an error and no write", err, wrote)
	}
}

func TestWriteWhileLeased_HookTouchesTheWriterOnlyDuringTheWrite(t *testing.T) {
	hooks := &recordingHooks{}
	writer := newDeadlineBlockedWriter()
	if err := writeWhileLeased(writer, hooks, func() {}); err != nil {
		t.Fatal(err)
	}
	if hooks.hook == nil {
		t.Fatal("writeWhileLeased() registered no cancel hook")
	}
	hooks.hook()
	if writer.deadlineSet() {
		t.Fatal("a cancel hook after the write ended set the writer's deadline")
	}
}

// TestWriteWhileLeased_RevocationEndsABlockedFinalFlush proves the final
// flush runs while the lease is held, and that a revocation ends a flush
// blocked on a reader that stopped reading, so the lease still drains
// before the transition's deadline.
func TestWriteWhileLeased_RevocationEndsABlockedFinalFlush(t *testing.T) {
	coordinator, lease, resumeID := newLeaseWriteTestLease(t)
	writer := flushBlockedWriter{newDeadlineBlockedWriter()}
	t.Cleanup(writer.release)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer lease.Release()
		write := func() {
			if _, err := writer.Write([]byte("buffered body")); err != nil {
				t.Error(err)
			}
		}
		if err := writeWhileLeased(writer, lease, write); err != nil {
			t.Errorf("writeWhileLeased() = %v", err)
		}
	}()
	select {
	case <-writer.writing:
	case <-done:
		t.Fatal("the leased write returned without flushing")
	case <-time.After(5 * time.Second):
		t.Fatal("the leased write never flushed")
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: resumeID, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	closeErr := transition.Close(context.Background(), time.Now().Add(time.Second))
	writer.release()
	<-done
	if closeErr != nil {
		t.Fatalf("revocation did not end the blocked flush and drain the lease: %v", closeErr)
	}
	if !writer.deadlineSet() {
		t.Fatal("revocation did not set the write deadline")
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

// TestWriteWhileLeased_AFailedFlushIsNotAnError proves a failed final flush
// is not returned: the response is committed by then, so the caller must
// not write an error over it.
func TestWriteWhileLeased_AFailedFlushIsNotAnError(t *testing.T) {
	writer := flushBlockedWriter{newDeadlineBlockedWriter()}
	if err := writer.SetWriteDeadline(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := writeWhileLeased(writer, &recordingHooks{}, func() {}); err != nil {
		t.Fatalf("writeWhileLeased() = %v after a failed flush, want nil", err)
	}
}

// TestServeLeasedResponse_ResponseLeavesGoWhileLeased proves over a real
// connection that the whole response leaves Go while its lease is held.
func TestServeLeasedResponse_ResponseLeavesGoWhileLeased(t *testing.T) {
	assertLeasedResponseLeavesGoWhileLeased(t, func(w http.ResponseWriter, request *http.Request, lease *publicstate.Lease, response SelectedResponse) {
		serveLeasedResponse(w, request, lease, nil, response)
	})
}

// assertLeasedResponseLeavesGoWhileLeased serves a small body, a body whose
// tail Go's server buffers, a HEAD, and a 304 through serve on a real
// server. The handler keeps the lease after serve returns until the viewer
// has read the whole response, so a byte Go sends only when the handler
// returns, which is after the lease is released, fails the test.
func assertLeasedResponseLeavesGoWhileLeased(t *testing.T, serve func(http.ResponseWriter, *http.Request, *publicstate.Lease, SelectedResponse)) {
	t.Helper()
	small := leaseWriteTestResponse(t, []byte("small body\n"))
	// Go's server writes this body through to the socket except its last
	// 1,104 bytes, which stay in its 4 KiB connection buffer.
	tail := leaseWriteTestResponse(t, bytes.Repeat([]byte("t"), 5000))
	tests := []struct {
		name        string
		method      string
		response    SelectedResponse
		ifNoneMatch bool
		wantStatus  int
		wantBody    []byte
	}{
		{name: "small_body", method: http.MethodGet, response: small, wantStatus: http.StatusOK, wantBody: small.Body},
		{name: "buffered_tail", method: http.MethodGet, response: tail, wantStatus: http.StatusOK, wantBody: tail.Body},
		{name: "head", method: http.MethodHead, response: small, wantStatus: http.StatusOK},
		{name: "not_modified", method: http.MethodGet, response: small, ifNoneMatch: true, wantStatus: http.StatusNotModified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, lease, _ := newLeaseWriteTestLease(t)
			hold := make(chan struct{})
			var holdOnce sync.Once
			letGo := func() { holdOnce.Do(func() { close(hold) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				defer lease.Release()
				serve(w, request, lease, test.response)
				<-hold
			}))
			t.Cleanup(server.Close)
			t.Cleanup(letGo)

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, test.method, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.ifNoneMatch {
				request.Header.Set("If-None-Match", test.response.Header.Get("ETag"))
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("the response did not leave Go while the lease was held: %v", err)
			}
			body, readErr := io.ReadAll(response.Body)
			if closeErr := response.Body.Close(); readErr == nil {
				readErr = closeErr
			}
			if readErr != nil {
				t.Fatalf("the body did not leave Go while the lease was held: %v", readErr)
			}
			if response.StatusCode != test.wantStatus || !bytes.Equal(body, test.wantBody) {
				t.Fatalf("response = %d with %d bytes, want %d with %d", response.StatusCode, len(body), test.wantStatus, len(test.wantBody))
			}
			letGo()
		})
	}
}

func leaseWriteTestResponse(t *testing.T, body []byte) SelectedResponse {
	t.Helper()
	response, err := NewSelectedResponse(http.StatusOK, "text/plain; charset=utf-8", "no-cache, must-revalidate", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

// newLeaseWriteTestLease returns a coordinator and a lease it admitted on a
// resume at revision 1.
func newLeaseWriteTestLease(t *testing.T) (*publicstate.Coordinator, *publicstate.Lease, uuid.UUID) {
	t.Helper()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	resumeID := uuid.New()
	lease, err := coordinator.AcquireResume(context.Background(), resumeID, 1, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, lease, resumeID
}

// flushBlockedWriter takes every write into a buffer, as Go's server does,
// and blocks its flush until a write deadline is set, as a connection to a
// reader that stopped reading does.
type flushBlockedWriter struct {
	*deadlineBlockedWriter
}

func (w flushBlockedWriter) Write(body []byte) (int, error) { return len(body), nil }

// FlushError is what http.ResponseController calls.
func (w flushBlockedWriter) FlushError() error {
	_, err := w.deadlineBlockedWriter.Write(nil)
	return err
}

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
