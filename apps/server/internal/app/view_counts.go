package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/viewapi"
	"danny.vn/aboutme/apps/server/internal/viewcount"
)

type ViewCounts struct {
	Counter *viewcount.Counter
	Service *viewapi.Service
}

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

func (v ViewCounts) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.Counter.Run(ctx)
	}()
	return done
}
