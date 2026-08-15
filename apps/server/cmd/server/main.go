// Command server runs the aboutme Go API server. This file is wiring only:
// load config, build the store and router, run the HTTP server, and shut
// down gracefully on SIGINT/SIGTERM. All behavior lives in internal/.
package main

import (
	"context"
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

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
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

	authService, err := auth.NewService(logger, cfg, store.New(pool))
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
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
	resumeService := resumeapi.New(
		resume.NewStore(pool, projector),
		resume.NewIdempotencyStore(pool),
		projector,
		blobs,
		resumeapi.Options{
			Logger:         logger,
			SessionManager: auth.NewSessionManager(store.New(pool)),
			PublicOrigin:   cfg.PublicOrigin,
			TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
			Coordinator:    coordinator,
			RecoveryPool:   pool,
		},
	)

	handler := api.New(logger, readiness, api.Options{
		// TrustedProxyCIDRs is validated by internal/config (required and
		// loopback-checked in production — see config.Load) and converts
		// directly: api.TrustedProxies is a named []netip.Prefix, the same
		// underlying type config.Config.TrustedProxyCIDRs already is.
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
	}, publicService, authService.RegisterRoutes, resumeService.RegisterRoutes)

	var lc net.ListenConfig
	addr := net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.Port))
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	logger.Info("starting", "env", cfg.Env)
	return serve(ctx, logger, ln, handler)
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
