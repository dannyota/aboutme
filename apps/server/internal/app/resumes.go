package app

import (
	"log/slog"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
	"github.com/dannyota/aboutme/apps/server/internal/showcase"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ResumeDependencies are the resume service's collaborators.
type ResumeDependencies struct {
	// Config supplies the public origin, trusted proxies, and the
	// sign-in-to-view switch.
	Config config.Config
	// Pool backs the resume and idempotency stores and recovery reads.
	Pool *store.Pool
	// Projector projects stored resume documents.
	Projector *docmigrate.Projector
	// Media holds resume photos.
	Media media.Backend
	// Logger receives resume diagnostics.
	Logger *slog.Logger
	// Sessions authenticates owner requests.
	Sessions *auth.SessionManager
	// Coordinator is the shared public-generation coordinator.
	Coordinator *publicstate.Coordinator
	// PrintQueue performs bounded private rendering after owner
	// authorization.
	PrintQueue *renderjob.Queue
	// Showcase applies community-showcase state with resume writes.
	Showcase *showcase.Service
}

// NewResumes returns the authenticated resume service. It creates the
// process-wide photo admission permit that the service's REST and agent upload
// paths share, so a process calls it once.
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
			TrustedProxies:      api.TrustedProxies(cfg.TrustedProxyCIDRs),
			PhotoAdmission:      media.NewPhotoAdmission(),
			Coordinator:         deps.Coordinator,
			RecoveryPool:        deps.Pool,
			PrintQueue:          deps.PrintQueue,
			SignInToViewEnabled: cfg.SignInToViewEnabled,
			Showcase:            deps.Showcase,
		},
	)
}
