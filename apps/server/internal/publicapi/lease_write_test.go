package publicapi

import (
	"errors"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
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
