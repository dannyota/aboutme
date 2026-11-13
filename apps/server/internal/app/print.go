package app

import (
	"errors"
	"fmt"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/printapi"
	"danny.vn/aboutme/apps/server/internal/printrender"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

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
		if err := queue.Close(); err != nil {
			return nil, nil, err
		}
	}
	return renderer, queue, nil
}

func PrivatePrintHandler(queue *renderjob.Queue, enabled bool) (http.Handler, error) {
	if enabled {
		return printapi.NewRedeemHandler(queue)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", api.CacheControlNoStore)
		api.WriteError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "service temporarily unavailable")
	}), nil
}
