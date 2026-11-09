package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/realtimeapi"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/showcase"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

// NewPublicState reads the durable public state and returns the public
// coordinator seeded with its discovery generation. A missing, non-singleton,
// or non-positive state fails, so the server never admits public traffic with
// a guessed discovery generation.
func NewPublicState(ctx context.Context, queries *store.Queries) (*publicstate.Coordinator, error) {
	state, err := queries.GetPublicState(ctx)
	if err != nil {
		return nil, fmt.Errorf("load public state: %w", err)
	}
	if !state.Singleton || state.DiscoveryGeneration <= 0 {
		return nil, errors.New("load public state: invalid durable public state")
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: state.DiscoveryGeneration})
	if err != nil {
		return nil, fmt.Errorf("initialize public coordinator: %w", err)
	}
	return coordinator, nil
}

// NewReader returns the public resume reader behind public-state admission.
func NewReader(queries *store.Queries, projector *docmigrate.Projector, coordinator *publicstate.Coordinator,
	blobs media.Backend, runtime PublicRuntime,
) (*publicresume.Reader, error) {
	return publicresume.NewReader(publicresume.ReaderDependencies{
		Store: queries, Projector: projector, Coordinator: coordinator, Media: blobs, Origin: runtime.PublicOrigin,
	})
}

// NewShowcase returns the community showcase service.
func NewShowcase(pool *store.Pool, reader *publicresume.Reader, projector *docmigrate.Projector, logger *slog.Logger) (*showcase.Service, error) {
	return showcase.New(showcase.Dependencies{DB: pool, Reader: reader, Projector: projector, Logger: logger})
}

// NewStreams returns the realtime metadata stream service.
func NewStreams(cfg config.Config, hub *realtime.Hub, queries *store.Queries, sessions *auth.SessionManager,
	coordinator *publicstate.Coordinator, viewPassKey []byte,
) (*realtimeapi.Service, error) {
	return realtimeapi.New(realtimeapi.Dependencies{
		Hub: hub, Store: queries, Sessions: sessions, Coordinator: coordinator,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs), ViewPassKey: viewPassKey,
	})
}

// PublicDependencies are the public service's collaborators.
type PublicDependencies struct {
	// Config supplies trusted proxies, sign-in-to-view providers, and the join
	// invite target.
	Config config.Config
	// Runtime supplies the canonical origin and build digests.
	Runtime PublicRuntime
	// Logger receives closed, content-free diagnostics.
	Logger *slog.Logger
	// Reader reads public resume projections.
	Reader *publicresume.Reader
	// Queries serves discovery reads.
	Queries *store.Queries
	// Coordinator is the shared public-generation coordinator.
	Coordinator *publicstate.Coordinator
	// Cache holds public artifacts.
	Cache *publiccache.Cache
	// Renderer is the direct public render worker client.
	Renderer *directrender.Client
	// Streams supplies the public live handler.
	Streams *realtimeapi.Service
	// PrintQueue is the shared print queue for public artifacts.
	PrintQueue *renderjob.Queue
	// Cards serves stored preview cards (ADR 0014); nil keeps the og.png
	// share image.
	Cards publicapi.PreviewCards
	// Views records crawler and link-preview fetches of resume pages.
	Views *viewcount.Counter
	// Showcase serves the community showcase listing.
	Showcase *showcase.Service
	// ViewPassKey seals and opens the __Host-view-pass cookie.
	ViewPassKey []byte
}

// NewPublic returns the public resume, discovery, and artifact service.
func NewPublic(deps PublicDependencies) (*publicapi.Service, error) {
	cfg, runtime := deps.Config, deps.Runtime
	return publicapi.NewService(publicapi.ServiceDependencies{
		Reader: deps.Reader, DiscoveryStore: deps.Queries, Coordinator: deps.Coordinator, Cache: deps.Cache, Renderer: deps.Renderer,
		PublicOrigin: runtime.PublicOrigin, AppDigest: runtime.AppDigest, RendererDigest: runtime.RendererDigest,
		Live: deps.Streams.PublicHandler(), PrintQueue: deps.PrintQueue,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs), Clock: time.Now, Logger: deps.Logger,
		Cards: deps.Cards, Views: deps.Views, Showcase: deps.Showcase,
		ViewPassKey: deps.ViewPassKey, GateProviders: signInToViewGateProviders(cfg), JoinInviteTarget: joinInviteTarget(cfg),
	})
}

// signInToViewGateProviders is the fixed subset of ["google","linkedin"] the
// sign-in-to-view gate offers: each provider enabled for account sign-in
// (docs/design/viewer-analytics/sign-in-to-view.md "Gate"; ADR 0016).
func signInToViewGateProviders(cfg config.Config) []string {
	providers := make([]string, 0, 2)
	if cfg.ProviderLogin.Google {
		providers = append(providers, "google")
	}
	if cfg.ProviderLogin.LinkedIn {
		providers = append(providers, "linkedin")
	}
	return providers
}

// joinInviteTarget is "/register" when password registration is open, else
// "/login" (docs/design/viewer-analytics/sign-in-to-view.md "Join invite").
func joinInviteTarget(cfg config.Config) string {
	if cfg.PasswordRegistrationDisabled {
		return "/login"
	}
	return "/register"
}
