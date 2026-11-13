package app

import (
	"log/slog"
	"time"

	"danny.vn/aboutme/apps/server/internal/accountapi"
	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type AccountDependencies struct {
	Config      config.Config
	Pool        *store.Pool
	Sessions    *auth.SessionManager
	Coordinator *publicstate.Coordinator
	Media       media.Backend
	Projector   *docmigrate.Projector
	Logger      *slog.Logger
}

func NewAccount(deps AccountDependencies) (*accountapi.Service, error) {
	return accountapi.New(accountapi.Dependencies{
		Pool: deps.Pool, Sessions: deps.Sessions, Coordinator: deps.Coordinator,
		Media: deps.Media, Projector: deps.Projector, Logger: deps.Logger,
		TrustedProxies: api.TrustedProxies(deps.Config.TrustedProxyCIDRs),
		PublicOrigin:   deps.Config.PublicOrigin, Now: time.Now,
	})
}
