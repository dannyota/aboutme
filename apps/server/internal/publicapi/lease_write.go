package publicapi

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// cancelHooks is the part of a public-state lease a response write needs: a
// hook that runs once if a revoking transition cancels the lease.
type cancelHooks interface {
	OnCancel(hook func()) error
}

// writeWhileLeased runs write, which writes one response to w, and then
// flushes w, both for the life of a lease, so every byte of the response
// leaves Go before its caller releases the lease. Without the flush Go's
// server can hold a small body, a body's tail, or a HEAD or 304 header and
// send it only after the handler returns. A revocation during the write or
// the flush sets w's write deadline to now, so a write blocked on a reader
// that stopped reading returns, the handler ends, and the lease it holds is
// released before the transition's drain deadline
// (docs/adr/0010-public-artifact-revocation.md). The hook touches w only
// until the flush returns; a later revocation does nothing. When the hook
// cannot be registered, writeWhileLeased returns the error without calling
// write, so the caller can fail closed. A failed flush is not returned: the
// response is committed by then, and the server closes a connection whose
// write failed. A writer that cannot flush is taken to buffer nothing.
func writeWhileLeased(w http.ResponseWriter, lease cancelHooks, write func()) error {
	controller := http.NewResponseController(w)
	var mu sync.Mutex
	writing := true
	err := lease.OnCancel(func() {
		mu.Lock()
		defer mu.Unlock()
		if writing {
			// A writer without deadline support leaves nothing more to do:
			// the write ends when the reader reads or the connection closes.
			_ = controller.SetWriteDeadline(time.Now()) //nolint:errcheck // See the comment above.
		}
	})
	if err != nil {
		return err
	}
	defer func() {
		mu.Lock()
		writing = false
		mu.Unlock()
	}()
	write()
	_ = controller.Flush() //nolint:errcheck // A committed response cannot be replaced; see the comment above.
	return nil
}

// releaseAfterFlush flushes w under lease's cancel hook and then releases
// the lease. Every leased handler defers it in place of lease.Release, so
// whatever the handler wrote, an error response included, leaves Go before
// the lease is released; Go's server would otherwise send a short buffered
// response only after the handler returns. A revocation during a flush
// blocked on a slow reader sets the write deadline, so the flush returns;
// a lease already revoked sets it at once, so its connection closes rather
// than delivering after release. A panicking handler is not flushed, which
// would commit a 200 header for a response it never wrote.
func releaseAfterFlush(w http.ResponseWriter, lease *publicstate.Lease) {
	defer lease.Release()
	if recovered := recover(); recovered != nil {
		panic(recovered)
	}
	_ = writeWhileLeased(w, lease, func() {}) //nolint:errcheck // A lease rejects only a nil hook, never passed here; the lease is released either way.
}

// serveLeasedResponse writes an HTML route's response while lease stays
// live, so a revocation ends a write blocked on a slow reader. When the
// cancel hook cannot be registered it fails closed with a 503 before writing
// anything.
func serveLeasedResponse(w http.ResponseWriter, request *http.Request, lease *publicstate.Lease, logger *slog.Logger, response SelectedResponse) {
	if err := writeWhileLeased(w, lease, func() { response.ServeHTTP(w, request) }); err != nil {
		logHTMLUnavailable(logger, request, "lease_hook_failed", "")
		serveHTMLError(w, request, http.StatusServiceUnavailable)
	}
}
