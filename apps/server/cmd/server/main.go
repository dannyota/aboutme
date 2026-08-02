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
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const shutdownTimeout = 10 * time.Second

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

	// NewPool does not dial the database eagerly, so a DB outage at boot
	// does not prevent the server from starting — /readyz will report the
	// outage once traffic starts arriving.
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close(context.Background())

	authService, err := auth.NewService(cfg, store.New(pool))
	if err != nil {
		return fmt.Errorf("create auth service: %w", err)
	}

	handler := api.New(logger, pool, api.Options{
		// TrustedProxyCIDRs is validated by internal/config (required and
		// loopback-checked in production — see config.Load) and converts
		// directly: api.TrustedProxies is a named []netip.Prefix, the same
		// underlying type config.Config.TrustedProxyCIDRs already is.
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
	}, authService.RegisterRoutes)

	var lc net.ListenConfig
	addr := net.JoinHostPort(cfg.ListenHost, strconv.Itoa(cfg.Port))
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	logger.Info("starting", "env", cfg.Env)
	return serve(ctx, logger, ln, handler)
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
