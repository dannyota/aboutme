package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewapi"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

// viewCounts wires anonymous view counting
// (docs/design/viewer-analytics/counting.md): the counter the public HTML
// handler reports crawlers to, and the start, collect, and owner routes.
type viewCounts struct {
	counter *viewcount.Counter
	service *viewapi.Service
}

func newViewCounts(cfg config.Config, logger *slog.Logger, queries *store.Queries, sessions *auth.SessionManager) (viewCounts, error) {
	counter, err := viewcount.New(viewcount.Config{Store: viewcount.PGStore{Queries: queries}, Logger: logger})
	if err != nil {
		return viewCounts{}, fmt.Errorf("create view counter: %w", err)
	}
	service, err := viewapi.New(viewapi.Dependencies{
		Counter: counter, Queries: queries, Sessions: sessions, PublicOrigin: cfg.PublicOrigin,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs), Now: time.Now, Logger: logger,
	})
	if err != nil {
		return viewCounts{}, fmt.Errorf("create view routes: %w", err)
	}
	return viewCounts{counter: counter, service: service}, nil
}

// run flushes counts until ctx ends, then once more; the returned channel
// closes after that final flush.
func (v viewCounts) run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.counter.Run(ctx)
	}()
	return done
}
