package publicapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
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

// TestLeasedFlush_RevocationEndsABlockedFlush proves both leased flushes,
// writeWhileLeased's after its write and releaseAfterFlush's before the
// release, run while the lease is held, and that a revocation ends a flush
// blocked on a reader that stopped reading, so the lease drains in time.
func TestLeasedFlush_RevocationEndsABlockedFlush(t *testing.T) {
	tests := []struct {
		name  string
		serve func(http.ResponseWriter, *publicstate.Lease, func())
	}{
		{name: "write_while_leased", serve: func(w http.ResponseWriter, lease *publicstate.Lease, write func()) {
			defer lease.Release()
			if err := writeWhileLeased(w, lease, write); err != nil {
				t.Errorf("writeWhileLeased() = %v", err)
			}
		}},
		{name: "release_after_flush", serve: func(w http.ResponseWriter, lease *publicstate.Lease, write func()) {
			defer releaseAfterFlush(w, lease)
			write()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coordinator, lease, resumeID := newLeaseWriteTestLease(t)
			writer := flushBlockedWriter{newDeadlineBlockedWriter()}
			t.Cleanup(writer.release)
			done := make(chan struct{})
			go func() {
				defer close(done)
				test.serve(writer, lease, func() {
					if _, err := writer.Write([]byte("buffered response")); err != nil {
						t.Error(err)
					}
				})
			}()
			select {
			case <-writer.writing:
			case <-done:
				t.Fatal("the lease was released without a flush")
			case <-time.After(5 * time.Second):
				t.Fatal("the response was never flushed")
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
		})
	}
}

// TestWriteWhileLeased_AFailedFlushIsNotAnError proves a failed final flush
// is not returned, so the caller never writes an error over a committed one.
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
			if readErr = errors.Join(readErr, response.Body.Close()); readErr != nil {
				t.Fatalf("the body did not leave Go while the lease was held: %v", readErr)
			}
			if response.StatusCode != test.wantStatus || !bytes.Equal(body, test.wantBody) {
				t.Fatalf("response = %d with %d bytes, want %d with %d", response.StatusCode, len(body), test.wantStatus, len(test.wantBody))
			}
			letGo()
		})
	}
}

// TestReleaseAfterFlush_ErrorLeavesGoWhileLeased proves over a real
// connection that a short error a leased handler writes leaves Go before
// releaseAfterFlush releases the lease.
func TestReleaseAfterFlush_ErrorLeavesGoWhileLeased(t *testing.T) {
	_, lease, _ := newLeaseWriteTestLease(t)
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer releaseAfterFlush(w, lease)
		serveConditionalError(w, request)
	})
	assertResponseLeavesGoInsideAFlush(t, handler, http.MethodGet, "/", nil, http.StatusBadRequest,
		"{\"error\":{\"code\":\"request_invalid\",\"message\":\"request is invalid\"}}\n")
}

// TestReleaseAfterFlush_APanickingHandlerIsNotFlushed proves a handler that
// panics before writing gets no flush, which would send a 200 header for a
// response it never wrote, and still releases its lease.
func TestReleaseAfterFlush_APanickingHandlerIsNotFlushed(t *testing.T) {
	_, lease, _ := newLeaseWriteTestLease(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		defer releaseAfterFlush(w, lease)
		panic("handler failed")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Start()
	t.Cleanup(server.Close)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err == nil {
		closeErr := response.Body.Close()
		t.Fatalf("a panicking handler sent status %d (close: %v), want no response", response.StatusCode, closeErr)
	}
	// The release runs as the panic unwinds, before the server closes the
	// connection the viewer saw end; releasing cancels the lease's context.
	if lease.Context().Err() == nil {
		t.Fatal("the panicking handler kept its lease")
	}
}

// assertResponseLeavesGoInsideAFlush serves one request through handler on
// a real server whose writer parks inside every flush until the viewer has
// read the response. The viewer must read the whole response while the
// handler is parked in a flush, that is before the handler can release its
// lease; a response Go sends only when the handler returns fails the test.
func assertResponseLeavesGoInsideAFlush(t *testing.T, handler http.Handler, method, path string, header http.Header, wantStatus int, wantBody string) {
	t.Helper()
	parked, letGo := make(chan struct{}), make(chan struct{})
	var parkOnce, letGoOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		handler.ServeHTTP(&flushParkingWriter{ResponseWriter: w, park: func() {
			parkOnce.Do(func() { close(parked) })
			<-letGo
		}}, request)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { letGoOnce.Do(func() { close(letGo) }) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range header {
		request.Header[name] = values
	}
	type result struct {
		status int
		body   []byte
		err    error
	}
	results := make(chan result, 1)
	go func() {
		response, doErr := server.Client().Do(request)
		if doErr != nil {
			results <- result{err: doErr}
			return
		}
		body, readErr := io.ReadAll(response.Body)
		results <- result{status: response.StatusCode, body: body, err: errors.Join(readErr, response.Body.Close())}
	}()
	got := <-results
	if got.err != nil {
		t.Fatalf("the response did not reach the viewer: %v", got.err)
	}
	select {
	case <-parked:
	case <-time.After(time.Second):
		t.Fatalf("the %d response left Go without a flush, so only after the handler released its lease", got.status)
	}
	if got.status != wantStatus || string(got.body) != wantBody {
		t.Fatalf("response = %d %q, want %d %q", got.status, got.body, wantStatus, wantBody)
	}
}

// flushParkingWriter flushes the real connection's writer and then parks
// in park, so the handler stays inside its flush while the test reads.
type flushParkingWriter struct {
	http.ResponseWriter
	park func()
}

// FlushError is what http.ResponseController calls.
func (w *flushParkingWriter) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	w.park()
	return err
}

// Unwrap lets http.ResponseController reach the connection's deadline.
func (w *flushParkingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
