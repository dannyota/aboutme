package app

import (
	"log/slog"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/accountapi"
	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// AccountDependencies are the account service's collaborators.
type AccountDependencies struct {
	// Config supplies trusted proxies and the public origin.
	Config config.Config
	// Pool runs account transactions.
	Pool *store.Pool
	// Sessions authenticates account requests.
	Sessions *auth.SessionManager
	// Coordinator is the shared public-generation coordinator.
	Coordinator *publicstate.Coordinator
	// Media holds resume photos for export and deletion.
	Media media.Backend
	// Projector projects stored resume documents.
	Projector *docmigrate.Projector
	// Logger receives account diagnostics.
	Logger *slog.Logger
}

// NewAccount returns the authenticated account service.
func NewAccount(deps AccountDependencies) (*accountapi.Service, error) {
	return accountapi.New(accountapi.Dependencies{
		Pool: deps.Pool, Sessions: deps.Sessions, Coordinator: deps.Coordinator,
		Media: deps.Media, Projector: deps.Projector, Logger: deps.Logger,
		TrustedProxies: api.TrustedProxies(deps.Config.TrustedProxyCIDRs),
		PublicOrigin:   deps.Config.PublicOrigin, Now: time.Now,
	})
}
