package publicapi

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

type cancelHooks interface {
	OnCancel(hook func()) error
}

func writeWhileLeased(w http.ResponseWriter, lease cancelHooks, write func()) error {
	controller := http.NewResponseController(w)
	var mu sync.Mutex
	writing := true
	err := lease.OnCancel(func() {
		mu.Lock()
		defer mu.Unlock()
		if writing {
			_ = controller.SetWriteDeadline(time.Now()) //nolint:errcheck // A writer without deadline support leaves nothing more to do.
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
	_ = controller.Flush() //nolint:errcheck // A committed response cannot be replaced.
	return nil
}

func releaseAfterFlush(w http.ResponseWriter, lease *publicstate.Lease) {
	defer lease.Release()
	if recovered := recover(); recovered != nil {
		panic(recovered)
	}
	_ = writeWhileLeased(w, lease, func() {}) //nolint:errcheck // A lease rejects only a nil hook, never passed here; the lease is released either way.
}

func serveLeasedResponse(w http.ResponseWriter, request *http.Request, lease *publicstate.Lease, logger *slog.Logger, response SelectedResponse) {
	if err := writeWhileLeased(w, lease, func() { response.ServeHTTP(w, request) }); err != nil {
		logHTMLUnavailable(logger, request, "lease_hook_failed", "")
		serveHTMLError(w, request, http.StatusServiceUnavailable)
	}
}
