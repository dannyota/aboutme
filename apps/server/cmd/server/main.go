// Command server runs the aboutme Go API server. This file is wiring only:
// load config, build the store and router, run the HTTP server, and shut
// down gracefully on SIGINT/SIGTERM. All behavior lives in internal/.
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/mcpapi"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const shutdownTimeout = 10 * time.Second

type publicRuntime struct {
	PublicOrigin   publicresume.PublicOrigin
	RenderOrigin   directrender.RenderOrigin
	AppDigest      string
	RendererDigest string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// NewPool does not dial eagerly. The durable public-state read below is the
	// intentional startup gate: the server must not admit public traffic with a
	// guessed discovery generation.
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close(context.Background())
	queries := store.New(pool)
	state, err := queries.GetPublicState(ctx)
	if err != nil {
		return fmt.Errorf("load public state: %w", err)
	}
	if !state.Singleton || state.DiscoveryGeneration <= 0 {
		return errors.New("load public state: invalid durable public state")
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: state.DiscoveryGeneration})
	if err != nil {
		return fmt.Errorf("initialize public coordinator: %w", err)
	}
	runtime, err := parsePublicRuntime(cfg.PublicOrigin, cfg.PublicRenderOrigin, cfg.Env, cfg.AppBuildDigest, cfg.PublicRendererBuildDigest)
	if err != nil {
		return fmt.Errorf("parse public runtime: %w", err)
	}

	authService, err := auth.NewService(logger, cfg, pool)
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
	}
	passwordAuth, err := newPasswordAuth(ctx, logger, cfg, pool, queries)
	if err != nil {
		return fmt.Errorf("create password auth: %w", err)
	}
	blobs, err := newMediaBackend(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create media backend: %w", err)
	}
	projector := docmigrate.NewIdentityProjector()
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: queries, Projector: projector, Coordinator: coordinator, Media: blobs, Origin: runtime.PublicOrigin,
	})
	if err != nil {
		return fmt.Errorf("create public reader: %w", err)
	}
	cache, err := publiccache.New(128, time.Minute, time.Now)
	if err != nil {
		return fmt.Errorf("create public cache: %w", err)
	}
	renderer := directrender.New(runtime.RenderOrigin, nil)
	publicService, err := publicapi.NewService(publicapi.ServiceDependencies{
		Reader: reader, DiscoveryStore: queries, Coordinator: coordinator, Cache: cache, Renderer: renderer,
		PublicOrigin: runtime.PublicOrigin, AppDigest: runtime.AppDigest, RendererDigest: runtime.RendererDigest,
	})
	if err != nil {
		return fmt.Errorf("create public service: %w", err)
	}
	readiness := publicstate.NewReadiness(coordinator, publicstate.ReadinessDependencies{
		PingDatabase: pool.Ping,
		ProbeRenderer: func(probeCtx context.Context) error {
			return renderer.Probe(probeCtx, readinessRenderRequest(runtime.PublicOrigin))
		},
	})
	sessionManager := auth.NewSessionManagerWithPool(pool)
	resumeService := resumeapi.New(
		resume.NewStore(pool, projector),
		resume.NewIdempotencyStore(pool),
		projector,
		blobs,
		resumeapi.Options{
			Logger:         logger,
			SessionManager: sessionManager,
			PublicOrigin:   cfg.PublicOrigin,
			TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
			Coordinator:    coordinator,
			RecoveryPool:   pool,
		},
	)
	agentRoutes, err := newAgentAccessRoutes(ctx, cfg, pool, queries, resumeService, sessionManager)
	if err != nil {
		return fmt.Errorf("create agent access: %w", err)
	}

	handler := api.New(logger, readiness, api.Options{
		// TrustedProxyCIDRs is validated by internal/config (required and
		// loopback-checked in production — see config.Load) and converts
		// directly: api.TrustedProxies is a named []netip.Prefix, the same
		// underlying type config.Config.TrustedProxyCIDRs already is.
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
	}, publicService, authService.RegisterRoutes, resumeService.RegisterRoutes, passwordAuth.service.RegisterRoutes, agentRoutes)

	var lc net.ListenConfig
	addr := net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.Port))
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	// The mail worker runs for the life of the server: SIGINT/SIGTERM cancels
	// workerCtx (via ctx), and Run joins its in-flight sends before returning.
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if runErr := passwordAuth.worker.Run(workerCtx); runErr != nil {
			// Run returns nil on cancellation; a non-nil return is unexpected.
			logger.Error("authmail worker stopped unexpectedly", "err", "worker stopped")
		}
	}()

	logger.Info("starting", "env", cfg.Env)
	err = serve(ctx, logger, ln, handler)
	cancelWorker()
	<-workerDone
	return err
}

type agentRouteHandlers struct {
	AuthorizationMetadata http.Handler
	ProtectedMetadata     http.Handler
	Register              http.Handler
	Authorize             http.Handler
	Token                 http.Handler
	Revoke                http.Handler
	MCP                   http.Handler
	Consent               http.Handler
	AgentGrants           http.Handler
	AgentGrant            http.Handler
}

func newAgentRouteRegistrar(enabled bool, registry []publicroots.Route, handlers agentRouteHandlers) (func(*http.ServeMux), error) {
	if !enabled {
		return func(*http.ServeMux) {}, nil
	}
	goRoots := make(map[string]bool, len(registry))
	for _, route := range registry {
		goRoots[route.Root] = route.Dispatch == publicroots.DispatchGo
	}
	for _, root := range []string{".well-known", "oauth", "mcp", "api"} {
		if !goRoots[root] {
			return nil, fmt.Errorf("agent access: public root %q is not dispatched to Go", root)
		}
	}
	if handlers.AuthorizationMetadata == nil || handlers.ProtectedMetadata == nil || handlers.Register == nil ||
		handlers.Authorize == nil || handlers.Token == nil || handlers.Revoke == nil || handlers.MCP == nil ||
		handlers.Consent == nil || handlers.AgentGrants == nil || handlers.AgentGrant == nil {
		return nil, errors.New("agent access: partial route handlers")
	}
	return func(mux *http.ServeMux) {
		mux.Handle("/.well-known/oauth-authorization-server", handlers.AuthorizationMetadata)
		mux.Handle("/.well-known/oauth-protected-resource", handlers.ProtectedMetadata)
		mux.Handle("/oauth/register", handlers.Register)
		mux.Handle("/oauth/authorize", handlers.Authorize)
		mux.Handle("/oauth/token", handlers.Token)
		mux.Handle("/oauth/revoke", handlers.Revoke)
		mux.Handle("/mcp", handlers.MCP)
		mux.Handle("/api/v1/oauth/consent", handlers.Consent)
		mux.Handle("/api/v1/me/agents", handlers.AgentGrants)
		mux.Handle("/api/v1/me/agents/{grantId}", handlers.AgentGrant)
	}, nil
}

func newAgentAccessRoutes(ctx context.Context, cfg config.Config, pool *store.Pool, queries store.OAuthQueries,
	resumes mcpapi.AgentExecutor, sessions *auth.SessionManager,
) (func(*http.ServeMux), error) {
	if err := cfg.ValidateAgentAccess(); err != nil {
		return nil, err
	}
	if !cfg.AgentAccess.Enabled {
		return newAgentRouteRegistrar(false, publicroots.Routes[:], agentRouteHandlers{})
	}
	a := cfg.AgentAccess
	oauthRates, err := oauthsrv.NewRatePolicies(oauthsrv.RateConfig{
		TrustedProxies:   api.TrustedProxies(cfg.TrustedProxyCIDRs),
		RegisterRequests: a.OAuthRegisterRequests, RegisterWindow: a.OAuthRegisterWindow,
		TokenRequests: a.OAuthTokenRequests, TokenWindow: a.OAuthTokenWindow,
		FailedGrantLimit: a.OAuthFailedGrantLimit, FailedGrantWindow: a.OAuthFailedGrantWindow,
		MaxKeys: a.MaxRateKeys,
	})
	if err != nil {
		return nil, err
	}
	oauthService, err := oauthsrv.NewService(ctx, oauthsrv.ServiceDependencies{
		Pool: pool, Queries: queries, Clock: time.Now, Entropy: rand.Reader,
		PublicOrigin: cfg.PublicOrigin, RegisterAdmission: oauthRates, TokenAdmission: oauthRates,
		LiveGrantLimit: a.OAuthLiveGrantLimit,
	})
	if err != nil {
		return nil, err
	}
	bearer, err := mcpapi.NewBearer(mcpapi.BearerDependencies{Queries: queries, Clock: time.Now, PublicOrigin: cfg.PublicOrigin})
	if err != nil {
		return nil, err
	}
	mcpRates, err := mcpapi.NewRatePolicies(mcpapi.RateConfig{
		TokenRequests: a.MCPTokenRequests, TokenWindow: a.MCPTokenWindow,
		UserRequests: a.MCPUserRequests, UserWindow: a.MCPUserWindow,
		ConcurrentPerUser: a.MCPConcurrentPerUser, MaxKeys: a.MaxRateKeys, Clock: time.Now,
	})
	if err != nil {
		return nil, err
	}
	mcpHandler, err := mcpapi.NewServer(mcpapi.ServerDependencies{
		Bearer: bearer, Resumes: resumes, Rates: mcpRates, MaxRequestBodyBytes: a.MCPBodyLimitBytes,
	})
	if err != nil {
		return nil, err
	}
	authorize := auth.OptionalSession(sessions)(http.HandlerFunc(oauthService.HandleAuthorize))
	return newAgentRouteRegistrar(true, publicroots.Routes[:], agentRouteHandlers{
		AuthorizationMetadata: http.HandlerFunc(oauthService.HandleMetadata),
		ProtectedMetadata:     http.HandlerFunc(oauthService.HandleProtectedResourceMetadata),
		Register:              http.HandlerFunc(oauthService.HandleRegister),
		Authorize:             authorize,
		Token:                 http.HandlerFunc(oauthService.HandleToken),
		Revoke:                http.HandlerFunc(oauthService.HandleRevoke),
		MCP:                   mcpHandler,
		Consent:               oauthService.ConsentHTTPHandler(sessions),
		AgentGrants:           oauthService.AgentGrantsHTTPHandler(sessions),
		AgentGrant:            oauthService.AgentGrantHTTPHandler(sessions),
	})
}

func parsePublicRuntime(publicOrigin, renderOrigin, environment, appDigest, rendererDigest string) (publicRuntime, error) {
	parsedPublicOrigin, err := publicresume.ParsePublicOrigin(publicOrigin, environment)
	if err != nil {
		return publicRuntime{}, errors.New("PUBLIC_ORIGIN is invalid")
	}
	parsedRenderOrigin, err := directrender.ParseRenderOrigin(renderOrigin, environment)
	if err != nil {
		return publicRuntime{}, errors.New("PUBLIC_RENDER_ORIGIN is invalid")
	}
	if !printableDigest(appDigest) {
		return publicRuntime{}, errors.New("APP_BUILD_DIGEST is invalid")
	}
	if !printableDigest(rendererDigest) {
		return publicRuntime{}, errors.New("PUBLIC_RENDERER_BUILD_DIGEST is invalid")
	}
	return publicRuntime{PublicOrigin: parsedPublicOrigin, RenderOrigin: parsedRenderOrigin, AppDigest: appDigest, RendererDigest: rendererDigest}, nil
}

func printableDigest(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < 0x21 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

// readinessRenderRequest is a closed, current-version public snapshot. The
// direct worker applies the same OpenAPI validator to probes and viewer
// renders, so readiness cannot use an incomplete synthetic value.
func readinessRenderRequest(origin publicresume.PublicOrigin) directrender.PublicRenderRequest {
	return directrender.PublicRenderRequest{
		PublicResume: publicresume.PublicResume{
			Slug:     "readiness-probe",
			Revision: "1",
			Lng:      "en",
			Document: publicresume.PublicResumeDocument{
				SchemaVersion:   schema.CurrentVersion,
				PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Readiness Probe"},
				Content: publicresume.PublicContent{
					"profile": {SectionType: string(schema.Profile), ProfileEntries: []publicresume.PublicProfileEntry{{ID: "00000000-0000-4000-8000-000000000001"}}},
				},
				Customization: schema.Customization{
					Font:           schema.Font{Family: schema.Inter, BaseSizePx: 14},
					Colors:         schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
					Spacing:        schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
					Heading:        schema.Heading{Style: schema.Normal},
					Layout:         schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{"profile"}, Sidebar: []string{}}},
					SectionDisplay: schema.SectionDisplay{Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text}},
					PageFormat:     schema.A4,
					DateFormat:     schema.MmYyyy,
				},
			},
		},
		Mode:             directrender.PublicRenderMode,
		CanonicalOrigin:  origin.String(),
		DiscoveryEnabled: false,
	}
}

func newMediaBackend(ctx context.Context, cfg config.Config) (media.Backend, error) {
	switch cfg.MediaBackend {
	case "fs":
		return media.NewFS(cfg.MediaFSDir)
	case "s3":
		return media.NewS3(ctx, media.S3Config{
			Bucket:          cfg.MediaBucket,
			Region:          cfg.MediaRegion,
			Endpoint:        cfg.MediaEndpoint,
			AccessKeyID:     cfg.MediaAccessKeyID,
			SecretAccessKey: cfg.MediaSecretAccessKey,
			ForcePathStyle:  cfg.MediaForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("unsupported configured backend %q", cfg.MediaBackend)
	}
}

// serve runs handler on ln until ctx is done, then drains in-flight
// requests via a graceful shutdown (bounded by shutdownTimeout) before
// returning. It takes a context and a listener — rather than reading
// signals or a port straight from the environment — so shutdown behavior
// can be exercised directly in tests without touching real OS signals or
// binding a fixed port.
func serve(ctx context.Context, logger *slog.Logger, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", ln.Addr().String())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	//nolint:contextcheck // shutdownCtx is intentionally not derived from ctx: ctx is already canceled (that's why we're here), so inheriting it would abort the drain instantly instead of bounding it by shutdownTimeout.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("server stopped")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}

	if cfg.Env == "dev" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

func parseLevel(raw string) slog.Level {
	switch raw {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
