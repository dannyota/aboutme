package app

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

// ViewCounts wires anonymous view counting
// (docs/design/viewer-analytics/counting.md): the counter the public HTML
// handler reports crawlers to, and the start, collect, and owner routes.
type ViewCounts struct {
	// Counter judges and buffers views; Run flushes them to the database.
	Counter *viewcount.Counter
	// Service serves the start, collect, and owner view routes.
	Service *viewapi.Service
}

// NewViewCounts builds the view counter and its routes.
func NewViewCounts(cfg config.Config, logger *slog.Logger, queries *store.Queries, sessions *auth.SessionManager, viewPassKey []byte) (ViewCounts, error) {
	counter, err := viewcount.New(viewcount.Config{Store: viewcount.PGStore{Queries: queries}, Logger: logger})
	if err != nil {
		return ViewCounts{}, fmt.Errorf("create view counter: %w", err)
	}
	service, err := viewapi.New(viewapi.Dependencies{
		Counter: counter, Queries: queries, Sessions: sessions, PublicOrigin: cfg.PublicOrigin,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs), Now: time.Now, Logger: logger,
		ViewPassKey: viewPassKey,
	})
	if err != nil {
		return ViewCounts{}, fmt.Errorf("create view routes: %w", err)
	}
	return ViewCounts{Counter: counter, Service: service}, nil
}

// Run flushes counts until ctx ends, then once more; the returned channel
// closes after that final flush.
func (v ViewCounts) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.Counter.Run(ctx)
	}()
	return done
}
