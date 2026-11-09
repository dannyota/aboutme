package app

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/printapi"
	"github.com/dannyota/aboutme/apps/server/internal/printrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

// NewPrint builds the private print runtime. On linux it creates the
// sandboxed browser renderer with create, and any failure is fatal; on every
// other platform the renderer is nil and the returned queue is already
// closed, so it rejects all admission.
func NewPrint(platform string, config printrender.Config, create func(printrender.Config) (*printrender.Renderer, error)) (*printrender.Renderer, *renderjob.Queue, error) {
	var renderer *printrender.Renderer
	var err error
	if platform == "linux" {
		renderer, err = create(config)
		if err != nil {
			return nil, nil, fmt.Errorf("initialize private browser renderer: %w", err)
		}
	}
	queueRenderer := renderer
	if queueRenderer == nil {
		queueRenderer = &printrender.Renderer{}
	}
	queue, err := renderjob.New(renderjob.Config{Renderer: queueRenderer})
	if err != nil {
		return nil, nil, errors.New("initialize print queue")
	}
	if platform != "linux" {
		// A closed queue rejects all admission before preparing private snapshots.
		if err := queue.Close(); err != nil {
			return nil, nil, err
		}
	}
	return renderer, queue, nil
}

// PrivatePrintHandler serves the private print redemption route when enabled,
// and otherwise answers every request 503 temporarily_unavailable.
func PrivatePrintHandler(queue *renderjob.Queue, enabled bool) (http.Handler, error) {
	if enabled {
		return printapi.NewRedeemHandler(queue)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", api.CacheControlNoStore)
		api.WriteError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "service temporarily unavailable")
	}), nil
}
