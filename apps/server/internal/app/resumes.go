package app

import (
	"log/slog"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/renderjob"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/showcase"
	"danny.vn/aboutme/apps/server/internal/store"
)

type ResumeDependencies struct {
	Config      config.Config
	Pool        *store.Pool
	Projector   *docmigrate.Projector
	Media       media.Backend
	Logger      *slog.Logger
	Sessions    *auth.SessionManager
	Coordinator *publicstate.Coordinator
	PrintQueue  *renderjob.Queue
	Showcase    *showcase.Service
	Origin      publicresume.PublicOrigin
}

func NewResumes(deps ResumeDependencies) *resumeapi.Service {
	cfg := deps.Config
	return resumeapi.New(
		resume.NewStore(deps.Pool, deps.Projector, resume.WithShowcaseSync(deps.Showcase)),
		resume.NewIdempotencyStore(deps.Pool),
		deps.Projector,
		deps.Media,
		resumeapi.Options{
			Logger:              deps.Logger,
			SessionManager:      deps.Sessions,
			PublicOrigin:        cfg.PublicOrigin,
			CanonicalOrigin:     deps.Origin,
			TrustedProxies:      api.TrustedProxies(cfg.TrustedProxyCIDRs),
			PhotoAdmission:      media.NewPhotoAdmission(),
			PhotoNormalizer:     media.NewPhotoNormalizer(),
			Coordinator:         deps.Coordinator,
			RecoveryPool:        deps.Pool,
			PrintQueue:          deps.PrintQueue,
			SignInToViewEnabled: cfg.SignInToViewEnabled,
			Showcase:            deps.Showcase,
		},
	)
}
