package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/realtime"
	"danny.vn/aboutme/apps/server/internal/realtimeapi"
	"danny.vn/aboutme/apps/server/internal/renderjob"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/showcase"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/viewcount"
)

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

func NewReader(queries *store.Queries, projector *docmigrate.Projector, coordinator *publicstate.Coordinator,
	blobs media.Backend, runtime PublicRuntime,
) (*publicresume.Reader, error) {
	return publicresume.NewReader(publicresume.ReaderDependencies{
		Store: queries, Projector: projector, Coordinator: coordinator, Media: blobs, Origin: runtime.PublicOrigin,
	})
}

func NewShowcase(pool *store.Pool, reader *publicresume.Reader, projector *docmigrate.Projector, logger *slog.Logger) (*showcase.Service, error) {
	return showcase.New(showcase.Dependencies{DB: pool, Reader: reader, Projector: projector, Logger: logger})
}

func NewStreams(cfg config.Config, hub *realtime.Hub, queries *store.Queries, sessions *auth.SessionManager,
	coordinator *publicstate.Coordinator, viewPassKey []byte,
) (*realtimeapi.Service, error) {
	return realtimeapi.New(realtimeapi.Dependencies{
		Hub: hub, Store: queries, Sessions: sessions, Coordinator: coordinator,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs), ViewPassKey: viewPassKey,
	})
}

type PublicDependencies struct {
	Config      config.Config
	Runtime     PublicRuntime
	Logger      *slog.Logger
	Reader      *publicresume.Reader
	Queries     *store.Queries
	Coordinator *publicstate.Coordinator
	Cache       *publiccache.Cache
	Renderer    *directrender.Client
	Streams     *realtimeapi.Service
	PrintQueue  *renderjob.Queue
	Cards       publicapi.PreviewCards
	Views       *viewcount.Counter
	Showcase    *showcase.Service
	ViewPassKey []byte
}

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

func joinInviteTarget(cfg config.Config) string {
	if cfg.PasswordRegistrationDisabled {
		return "/login"
	}
	return "/register"
}
