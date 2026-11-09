package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

// servePair runs the public and private print servers until ctx ends or one
// of them stops, then stops rendering and joins both.
func servePair(ctx context.Context, logger *slog.Logger, publicListener net.Listener, publicHandler http.Handler,
	printListener net.Listener, printHandler http.Handler, stopRendering func(),
) error {
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- serve(serverCtx, logger, publicListener, publicHandler) }()
	go func() {
		results <- serveHTTP(serverCtx, logger, printListener, &http.Server{
			Handler: printHandler, ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
			IdleTimeout: time.Second, MaxHeaderBytes: 4096,
		})
	}()
	var first error
	remaining := 2
	select {
	case first = <-results:
		remaining--
	case <-ctx.Done():
	}
	cancel()
	stopRendering()
	for range remaining {
		first = errors.Join(first, <-results)
	}
	return first
}

// serve runs handler on ln until ctx is done, then drains in-flight
// requests via a graceful shutdown (bounded by shutdownTimeout) before
// returning. It takes a context and a listener, rather than reading
// signals or a port straight from the environment, so shutdown behavior
// can be exercised directly in tests without touching real OS signals or
// binding a fixed port.
func serve(ctx context.Context, logger *slog.Logger, ln net.Listener, handler http.Handler) error {
	return serveHTTP(ctx, logger, ln, &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	})
}

func serveHTTP(ctx context.Context, logger *slog.Logger, ln net.Listener, srv *http.Server) error {
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
