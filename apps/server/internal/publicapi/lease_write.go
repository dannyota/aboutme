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

// writeWhileLeased runs write, which writes one response to w, for the life
// of a lease. A revocation during the write sets w's write deadline to now,
// so a write blocked on a reader that stopped reading returns, the handler
// ends, and the lease it holds is released before the transition's drain
// deadline (docs/adr/0010-public-artifact-revocation.md). The hook touches w
// only while write runs; once write returns, a late revocation does
// nothing. When the hook cannot be registered, writeWhileLeased returns the
// error without calling write, so the caller can fail closed.
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
	return nil
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
